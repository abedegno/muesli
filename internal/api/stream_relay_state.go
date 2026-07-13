package api

import "github.com/abedegno/muesli/internal/plugin"

type streamingSegmentRelayState struct {
	finalizedByT0 map[int]struct{}
}

func newStreamingSegmentRelayState() *streamingSegmentRelayState {
	return &streamingSegmentRelayState{
		finalizedByT0: make(map[int]struct{}),
	}
}

func (s *streamingSegmentRelayState) shouldRelay(ev plugin.StreamingEvent) bool {
	key := secondsToMS(ev.T0)
	if _, ok := s.finalizedByT0[key]; ok {
		return false
	}
	if ev.Final {
		s.finalizedByT0[key] = struct{}{}
	}
	return true
}
