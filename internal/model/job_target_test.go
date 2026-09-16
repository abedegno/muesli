package model

import "testing"

func TestJobTargetKind(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		job     Job
		want    JobTargetKind
		wantErr bool
	}{
		{name: "note only", job: Job{ID: "j1", NoteID: "note_1"}, want: JobTargetNote},
		{name: "calendar event only", job: Job{ID: "j2", CalendarEventID: "event_1"}, want: JobTargetCalendarEvent},
		{name: "neither", job: Job{ID: "j3"}, wantErr: true},
		{name: "both", job: Job{ID: "j4", NoteID: "note_1", CalendarEventID: "event_1"}, wantErr: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := tc.job.TargetKind()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("TargetKind() = %v, nil, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("TargetKind() unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("TargetKind() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestJobPreGenerateJSONOmitsUnsetTarget(t *testing.T) {
	t.Parallel()

	noteJob := Job{ID: "j1", NoteID: "note_1", Type: JobTranscribe, Status: JobPending}
	got := assertJSONRoundTrip(t, noteJob, []string{"id", "note_id", "type", "status", "attempts", "priority"})
	if got.CalendarEventID != "" || got.BriefID != "" || got.BriefGeneration != 0 {
		t.Fatalf("note job round trip gained an event target: %#v", got)
	}

	eventJob := Job{
		ID: "j2", CalendarEventID: "event_1", BriefID: "brief_1", BriefGeneration: 3,
		Type: JobPreGenerate, Status: JobPending,
	}
	got2 := assertJSONRoundTrip(t, eventJob, []string{"id", "calendar_event_id", "brief_id", "brief_generation", "type", "status", "attempts", "priority"})
	if got2.NoteID != "" {
		t.Fatalf("event job round trip gained a note target: %#v", got2)
	}
}
