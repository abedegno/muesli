package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/plugin"
	"github.com/abedegno/muesli/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const streamingAudioBuffer = 16

const (
	streamingSampleRate     = 16000
	streamingBytesPerSample = 2 // mono signed 16-bit little-endian PCM
)

var streamUpgrader = websocket.Upgrader{
	CheckOrigin: func(*http.Request) bool { return true },
}

type streamSegmentMessage struct {
	Type        string  `json:"type"`
	Text        string  `json:"text"`
	StartMS     int     `json:"start_ms"`
	EndMS       int     `json:"end_ms"`
	Speaker     *string `json:"speaker"`
	Provisional bool    `json:"provisional"`
	Final       bool    `json:"final"`
	StreamID    string  `json:"stream_id,omitempty"`
	NoteID      string  `json:"note_id,omitempty"`
	DroppedMS   int64   `json:"dropped_duration_ms,omitempty"`
}

type streamAudioFrame struct {
	data        []byte
	startSample int64
}

type streamDropRun struct {
	startSample    int64
	droppedSamples int64
}

type streamDropTracker struct {
	run *streamDropRun
}

// observe records a dropped frame. A nil frame means audio flowed without a
// drop and closes the current contiguous run, if any.
func (t *streamDropTracker) observe(dropped *streamAudioFrame) *streamDropRun {
	if dropped != nil {
		if t.run == nil {
			t.run = &streamDropRun{startSample: dropped.startSample}
		}
		t.run.droppedSamples += int64(len(dropped.data) / streamingBytesPerSample)
		return nil
	}
	resolved := t.run
	t.run = nil
	return resolved
}

func (t *streamDropTracker) flush() *streamDropRun {
	return t.observe(nil)
}

type streamGapState struct {
	mu               sync.Mutex
	nextSegmentIsGap bool
}

func (s *streamGapState) markGap() {
	s.mu.Lock()
	s.nextSegmentIsGap = true
	s.mu.Unlock()
}

func (s *streamGapState) applyToNextSegment(seg *model.Segment) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.nextSegmentIsGap {
		return
	}
	if seg.Boundary == "" {
		seg.Boundary = "gap"
	} else if seg.Boundary != "gap" {
		seg.Boundary += ",gap"
	}
	s.nextSegmentIsGap = false
}

func (s *Server) handleNoteStream(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	noteID := chi.URLParam(r, "id")
	if !validNoteID(noteID) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	note, err := s.deps.Store.GetNote(r.Context(), uid, noteID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// A mid-pipeline note already has batch work in flight, so starting a live
	// stream would race a second transcription path against it. Ready and failed
	// notes remain streamable so callers can deliberately record more audio.
	switch note.Status {
	case model.NoteRecording, model.NoteUploaded, model.NoteTranscribing, model.NoteSummarizing:
		writeError(w, http.StatusConflict, "note is not ready to stream")
		return
	}

	conn, err := streamUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	plug, err := s.streamingTranscriber(r.Context())
	if err != nil {
		_ = writeStreamControl(conn, map[string]string{"type": "unavailable"})
		_ = closeWebsocketCleanly(conn)
		return
	}

	streamClient := plugin.NewStreaming(plug.EndpointURL, plug.Token)
	modelWasLoading := false
	sess, err := streamClient.Open(r.Context(), plugin.StreamingStartRequest{
		LanguageHint: "",
		Options:      json.RawMessage(`{}`),
		Config:       plug.Config,
		SampleRate:   streamingSampleRate,
		Channels:     1,
	}, func(ev plugin.StreamingEvent) {
		if ev.Type == "loading" {
			modelWasLoading = true
			_ = writeStreamControl(conn, map[string]string{"type": "loading"})
		}
	})
	if err != nil {
		_ = writeStreamControl(conn, map[string]string{"type": "unavailable"})
		_ = closeWebsocketCleanly(conn)
		return
	}

	// The transcript this stream owns is created only now, once a streaming
	// plugin has been found AND its session has opened. Creating it earlier
	// sealed and deleted whatever transcript the note already had — including a
	// batch transcript and all of its segments — before the server knew whether
	// it could transcribe anything at all, so a note could be emptied and then
	// told "unavailable". It still precedes every byte of audio: nothing is read
	// from the socket until the reader goroutine below starts, so a gap arriving
	// before the first segment always has a transcript to attach to.
	//
	// Interim stream identity. Plan 2 replaces this with the client-supplied
	// stream_id from the start handshake; the mechanism below is unchanged by
	// that swap.
	streamID := uuid.NewString()

	expectedGeneration, err := s.deps.Store.CurrentTranscriptGeneration(r.Context(), noteID)
	if err != nil {
		s.failStreamStart(conn, sess, noteID, "internal_error", err)
		return
	}
	liveTranscript, err := s.deps.Store.CreateStreamTranscript(
		r.Context(), noteID, streamID, "streaming", "", expectedGeneration)
	if err != nil {
		s.failStreamStart(conn, sess, noteID, streamStartFailureReason(err), err)
		return
	}

	if modelWasLoading {
		if err := writeStreamControl(conn, map[string]string{"type": "ready"}); err != nil {
			_ = sess.Close()
			_ = closeWebsocketCleanly(conn)
			return
		}
	}

	audioCh := make(chan streamAudioFrame, streamingAudioBuffer)
	outboundCh := newStreamOutboundMailbox()
	gapState := &streamGapState{}
	var closeAudioOnce sync.Once
	closeAudio := func() {
		closeAudioOnce.Do(func() {
			close(audioCh)
		})
	}
	var closeSessionOnce sync.Once
	closeSession := func() {
		closeSessionOnce.Do(func() {
			_ = sess.Close()
		})
	}
	var closeSocketOnce sync.Once
	closeSocket := func() {
		closeSocketOnce.Do(func() {
			_ = closeWebsocketCleanly(conn)
		})
	}
	var closeAllOnce sync.Once
	closeAll := func() {
		closeAllOnce.Do(func() {
			outboundCh.close()
			closeSession()
			closeSocket()
		})
	}

	var wg sync.WaitGroup
	wg.Add(4)

	// Client -> audio buffer.
	go func() {
		defer wg.Done()
		defer closeAudio()
		var nextSample int64
		var dropTracker streamDropTracker
		resolveDropRun := func(dropRun *streamDropRun) bool {
			if err := s.resolveStreamDropRun(r.Context(), liveTranscript.ID, streamID, noteID, dropRun, gapState, outboundCh); err != nil {
				return false
			}
			return true
		}
		flushDropRun := func() bool {
			return resolveDropRun(dropTracker.flush())
		}
		defer func() {
			if !flushDropRun() {
				closeAll()
			}
		}()
		for {
			msgType, payload, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived) || errors.Is(err, io.EOF) {
					return
				}
				_ = flushDropRun()
				closeAll()
				return
			}
			if msgType != websocket.BinaryMessage {
				continue
			}
			frame := streamAudioFrame{data: append([]byte(nil), payload...), startSample: nextSample}
			nextSample += int64(len(frame.data) / streamingBytesPerSample)
			dropped, err := enqueueAudioFrame(r.Context(), audioCh, frame)
			if err != nil {
				_ = flushDropRun()
				closeAll()
				return
			}
			if !resolveDropRun(dropTracker.observe(dropped)) {
				_ = flushDropRun()
				closeAll()
				return
			}
		}
	}()

	// Audio buffer -> plugin.
	go func() {
		defer wg.Done()
		for frame := range audioCh {
			if err := sess.WriteAudio(frame.data); err != nil {
				closeAll()
				return
			}
		}
		if err := sess.Stop(); err != nil {
			closeAll()
			return
		}
	}()

	// Plugin -> client + persistence.
	go func() {
		defer wg.Done()
		defer closeSession()
		partialWritten := false
		relayState := newStreamingSegmentRelayState()
		for {
			ev, err := sess.Recv()
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived) || errors.Is(err, io.EOF) {
					outboundCh.close()
					return
				}
				closeAll()
				return
			}
			switch ev.Type {
			case "ready":
				continue
			case "segment":
				if !relayState.shouldRelay(ev) {
					continue
				}
				seg := model.Segment{
					StartMS:    secondsToMS(ev.T0),
					EndMS:      secondsToMS(ev.T1),
					Text:       ev.Text,
					Source:     "streaming",
					Speaker:    "",
					Words:      nil,
					Confidence: nil,
				}
				if ev.Speaker != nil {
					seg.Speaker = *ev.Speaker
				}
				if ev.Final {
					gapState.applyToNextSegment(&seg)
					if err := s.deps.Store.AppendStreamSegment(r.Context(), liveTranscript.ID, streamID, seg); err != nil {
						if errors.Is(err, store.ErrStreamSuperseded) {
							// Batch transcription replaced this transcript while a
							// final was in flight. Dropping it is correct — the
							// batch result supersedes it.
							slog.InfoContext(r.Context(), "dropping final for superseded stream",
								"note_id", noteID, "stream_id", streamID)
							closeAll()
							return
						}
						closeAll()
						return
					}
					if !partialWritten {
						if err := s.deps.Store.SetNotePartialTranscript(r.Context(), noteID, true); err != nil {
							closeAll()
							return
						}
						partialWritten = true
					}
				}
				outboundCh.enqueue(streamSegmentMessage{
					Type:        "segment",
					Text:        seg.Text,
					StartMS:     seg.StartMS,
					EndMS:       seg.EndMS,
					Speaker:     speakerPtr(seg.Speaker),
					Provisional: true,
					Final:       ev.Final,
				})
			case "error":
				closeAll()
				return
			default:
				closeAll()
				return
			}
		}
	}()

	go func() {
		defer wg.Done()
		defer closeSession()
		defer closeSocket()
		if err := outboundCh.runWriter(func(msg streamSegmentMessage) error {
			return writeStreamControl(conn, msg)
		}); err != nil {
			closeAll()
			return
		}
	}()

	wg.Wait()
}

func (s *Server) resolveStreamDropRun(ctx context.Context, transcriptID, streamID, noteID string, dropRun *streamDropRun, gapState *streamGapState, outboundCh *streamOutboundMailbox) error {
	if dropRun == nil {
		return nil
	}
	dropped := dropRun.droppedSamples
	gap := model.TranscriptGap{
		ID:             uuid.NewString(),
		TranscriptID:   transcriptID,
		StreamID:       streamID,
		StartSample:    dropRun.startSample,
		DroppedSamples: &dropped,
		Origin:         "server",
	}
	if err := s.deps.Store.AppendTranscriptGap(ctx, transcriptID, streamID, gap); err != nil {
		return err
	}
	gapState.markGap()
	outboundCh.enqueue(streamSegmentMessage{
		Type:      "gap",
		StreamID:  streamID,
		NoteID:    noteID,
		DroppedMS: dropped * 1000 / streamingSampleRate,
		Final:     true,
	})
	slog.WarnContext(ctx, "stream audio: dropped frames under backpressure",
		"note_id", noteID, "stream_id", streamID,
		"dropped_samples", dropped,
		"dropped_duration_ms", dropped*1000/streamingSampleRate)
	return nil
}

func (s *Server) streamingTranscriber(ctx context.Context) (model.Plugin, error) {
	if s.deps.Crypto == nil {
		return model.Plugin{}, errors.New("streaming plugin unavailable")
	}
	plug, err := s.deps.Store.DefaultPlugin(ctx, s.deps.Crypto, model.PluginStreamingTranscriber)
	if errors.Is(err, store.ErrNotFound) {
		return model.Plugin{}, err
	}
	if err != nil {
		return model.Plugin{}, err
	}

	client := plugin.New(plug.EndpointURL, plug.Token)
	info, err := client.Info(ctx)
	if err != nil {
		return model.Plugin{}, err
	}
	if info.Kind != model.PluginStreamingTranscriber || info.PluginAPI != 1 {
		return model.Plugin{}, fmt.Errorf("streaming plugin kind mismatch: %s", info.Kind)
	}
	if err := client.Health(ctx); err != nil {
		return model.Plugin{}, err
	}
	return plug, nil
}

func enqueueAudioFrame(ctx context.Context, ch chan streamAudioFrame, frame streamAudioFrame) (*streamAudioFrame, error) {
	select {
	case ch <- frame:
		return nil, nil
	default:
	}
	var dropped *streamAudioFrame
	select {
	case oldest := <-ch:
		dropped = &oldest
	default:
	}
	select {
	case ch <- frame:
		return dropped, nil
	case <-ctx.Done():
		return dropped, ctx.Err()
	}
}

func secondsToMS(seconds float64) int {
	return int(math.Round(seconds * 1000))
}

// streamStartFailureReason names why the stream could not take ownership of a
// transcript. It rides on the "unavailable" control message rather than
// replacing it: the socket is already open by the time this can happen, so
// there is no HTTP status left to send, and the relay degrades on `type` alone
// (src/main/noteStreamRelay.ts). The reason exists so a client or a log can
// tell "this note already has a batch transcript, live capture will never work
// here" apart from "no streaming plugin".
func streamStartFailureReason(err error) string {
	switch {
	case errors.Is(err, store.ErrBatchTranscriptExists):
		return "batch_transcript_exists"
	case errors.Is(err, store.ErrGenerationMismatch):
		return "transcript_changed"
	default:
		return "internal_error"
	}
}

// failStreamStart reports a start-handshake failure over the already-open
// socket and tears down the plugin session that was opened for it. No
// transcript exists at this point, so there is nothing to unwind in the store.
func (s *Server) failStreamStart(conn *websocket.Conn, sess *plugin.StreamingSession, noteID, reason string, err error) {
	slog.Warn("live stream start rejected", "note_id", noteID, "reason", reason, "error", err)
	_ = writeStreamControl(conn, map[string]string{"type": "unavailable", "reason": reason})
	_ = sess.Close()
	_ = closeWebsocketCleanly(conn)
}

func writeStreamControl(conn *websocket.Conn, v any) error {
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return conn.WriteJSON(v)
}

func closeWebsocketCleanly(conn *websocket.Conn) error {
	deadline := time.Now().Add(2 * time.Second)
	if err := conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), deadline); err != nil {
		_ = conn.Close()
		return err
	}
	return conn.Close()
}

func speakerPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
