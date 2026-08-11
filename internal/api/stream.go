package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/plugin"
	"github.com/abedegno/muesli/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
)

const streamingAudioBuffer = 16

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
}

func (s *Server) handleNoteStream(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	noteID := chi.URLParam(r, "id")
	if !validNoteID(noteID) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if _, err := s.deps.Store.GetNote(r.Context(), uid, noteID); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
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
		SampleRate:   16000,
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
	if modelWasLoading {
		if err := writeStreamControl(conn, map[string]string{"type": "ready"}); err != nil {
			_ = sess.Close()
			_ = closeWebsocketCleanly(conn)
			return
		}
	}

	audioCh := make(chan []byte, streamingAudioBuffer)
	outboundCh := newStreamOutboundMailbox()
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
		for {
			msgType, payload, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived) || errors.Is(err, io.EOF) {
					return
				}
				closeAll()
				return
			}
			if msgType != websocket.BinaryMessage {
				continue
			}
			frame := append([]byte(nil), payload...)
			if err := enqueueAudioFrame(r.Context(), audioCh, frame); err != nil {
				closeAll()
				return
			}
		}
	}()

	// Audio buffer -> plugin.
	go func() {
		defer wg.Done()
		for frame := range audioCh {
			if err := sess.WriteAudio(frame); err != nil {
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
					if err := s.deps.Store.AppendProvisionalTranscriptSegment(r.Context(), noteID, model.Transcript{
						TranscriberPlugin: plug.Name,
					}, seg); err != nil {
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

func enqueueAudioFrame(ctx context.Context, ch chan []byte, frame []byte) error {
	select {
	case ch <- frame:
		return nil
	default:
	}
	select {
	case <-ch:
	default:
	}
	select {
	case ch <- frame:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func secondsToMS(seconds float64) int {
	return int(math.Round(seconds * 1000))
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
