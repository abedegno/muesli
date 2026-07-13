package api

import "testing"

func TestDecideStreamOutboundAction(t *testing.T) {
	cases := []struct {
		name  string
		state streamOutboundQueueState
		kind  streamOutboundMessageKind
		want  streamOutboundAction
	}{
		{
			name:  "first partial queues",
			state: streamOutboundQueueState{hasPendingPartial: false},
			kind:  streamOutboundMessagePartial,
			want:  streamOutboundActionQueuePartial,
		},
		{
			name:  "second partial coalesces",
			state: streamOutboundQueueState{hasPendingPartial: true},
			kind:  streamOutboundMessagePartial,
			want:  streamOutboundActionCoalescePartial,
		},
		{
			name:  "final bypasses partial lane",
			state: streamOutboundQueueState{hasPendingPartial: true},
			kind:  streamOutboundMessageFinal,
			want:  streamOutboundActionQueuePriority,
		},
		{
			name:  "partial drops once final is pending",
			state: streamOutboundQueueState{hasPendingFinal: true, hasPendingPartial: true},
			kind:  streamOutboundMessagePartial,
			want:  streamOutboundActionDropPartial,
		},
		{
			name:  "final still prioritizes when idle",
			state: streamOutboundQueueState{hasPendingPartial: false},
			kind:  streamOutboundMessageFinal,
			want:  streamOutboundActionQueuePriority,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := decideStreamOutboundAction(tc.state, tc.kind); got != tc.want {
				t.Fatalf("decideStreamOutboundAction(%+v, %v) = %v, want %v", tc.state, tc.kind, got, tc.want)
			}
		})
	}
}
