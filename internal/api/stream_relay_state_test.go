package api

import (
	"testing"

	"github.com/abedegno/muesli/internal/plugin"
)

func TestStreamingSegmentRelayStateDropsLateEventsForSameT0(t *testing.T) {
	state := newStreamingSegmentRelayState()

	cases := []struct {
		name string
		ev   plugin.StreamingEvent
		want bool
	}{
		{name: "partial 1", ev: plugin.StreamingEvent{Type: "segment", T0: 1.25, Final: false}, want: true},
		{name: "partial 2", ev: plugin.StreamingEvent{Type: "segment", T0: 1.25, Final: false}, want: true},
		{name: "final", ev: plugin.StreamingEvent{Type: "segment", T0: 1.25, Final: true}, want: true},
		{name: "late partial", ev: plugin.StreamingEvent{Type: "segment", T0: 1.25, Final: false}, want: false},
		{name: "duplicate final", ev: plugin.StreamingEvent{Type: "segment", T0: 1.25, Final: true}, want: false},
		{name: "new utterance", ev: plugin.StreamingEvent{Type: "segment", T0: 2.50, Final: false}, want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := state.shouldRelay(tc.ev); got != tc.want {
				t.Fatalf("shouldRelay(%+v) = %v, want %v", tc.ev, got, tc.want)
			}
		})
	}
}
