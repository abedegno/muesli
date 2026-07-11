package calendar

// DiffExternalIDs returns the external IDs of fetched events in order.
// The sync layer uses this as the keep-set when pruning missing upstream rows.
func DiffExternalIDs(fetched []NormalizedEvent) []string {
	out := make([]string, 0, len(fetched))
	for _, ev := range fetched {
		out = append(out, ev.ExternalID)
	}
	return out
}
