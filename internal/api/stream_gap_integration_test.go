package api

import (
	"context"
	"testing"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

// This test intentionally uses the real migrated store. CI supplies its
// database; local runs of DB-backed tests are excluded by the contributor
// workflow.
func TestResolveStreamDropRunPersistsGapAndMarksNextSegment(t *testing.T) {
	ctx := context.Background()
	st := store.New(testutil.NewPool(t))
	user, err := st.CreateUser(ctx, "stream-gap@example.com", "hash")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	note, err := st.CreateNote(ctx, user.ID, "Backpressure")
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	const streamID = "stream-gap"
	live, err := st.CreateStreamTranscript(ctx, note.ID, streamID, "streaming", "", 0)
	if err != nil {
		t.Fatalf("CreateStreamTranscript: %v", err)
	}

	srv := NewServer(Deps{Store: st})
	gapState := &streamGapState{}
	outbound := newStreamOutboundMailbox()
	if err := srv.resolveStreamDropRun(ctx, live.ID, streamID, note.ID,
		&streamDropRun{startSample: 320, droppedSamples: 640}, gapState, outbound); err != nil {
		t.Fatalf("resolveStreamDropRun: %v", err)
	}
	seg := model.Segment{StartMS: 20, EndMS: 80, Text: "after gap", Source: "streaming"}
	gapState.applyToNextSegment(&seg)
	if err := st.AppendStreamSegment(ctx, live.ID, streamID, seg); err != nil {
		t.Fatalf("AppendStreamSegment: %v", err)
	}

	got, err := st.GetTranscript(ctx, note.ID)
	if err != nil {
		t.Fatalf("GetTranscript: %v", err)
	}
	if len(got.Gaps) != 1 || got.Gaps[0].StartSample != 320 || got.Gaps[0].DroppedSamples == nil || *got.Gaps[0].DroppedSamples != 640 || got.Gaps[0].Origin != "server" || got.Gaps[0].StreamID != streamID {
		t.Fatalf("gaps = %+v, want persisted server gap", got.Gaps)
	}
	if len(got.Segments) != 1 || got.Segments[0].Boundary != "gap" {
		t.Fatalf("segments = %+v, want next segment boundary=gap", got.Segments)
	}
	msg, ok := outbound.nextPriority()
	if !ok || msg.Type != "gap" || msg.StreamID != streamID || msg.NoteID != note.ID || msg.DroppedMS != 40 {
		t.Fatalf("outbound gap = %+v, %v", msg, ok)
	}
}
