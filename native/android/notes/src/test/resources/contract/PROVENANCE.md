# Contract fixture provenance (issue #768 Task 1)

All fixtures in this directory are derived from the authoritative #767 mobile
API contract at dependency commit `d0a8e26` (origin/main), specifically:

- `internal/api/mobile_notes.go` (response DTOs, error envelopes)
- `internal/api/mobile_notes_test.go` (list/detail/error assertions)
- `internal/api/mobile_notes_e2e_test.go` (pagination/isolation assertions)
- `internal/auth/middleware.go` (401/503 envelope literals)

No local Postgres was available while authoring this PR (see the dispatch
note in the PR description), so these bytes were produced by a throwaway,
now-deleted `go run` program (`internal/api` temporary exported helpers +
a `cmd/tmpfixturegen` main) that constructed the exact struct literals the
#767 tests assert against and marshaled them with the real production DTOs
in `internal/api/mobile_notes.go` -- the real marshal code path, not
hand-typed JSON. `internal/api/mobile_notes_fixture_capture_test.go` is the
permanent, DB-backed, CI-only test that captures a live response from the
real handler and keeps this corpus honest over time.

| File | Source case | HTTP status | SHA-256 |
| --- | --- | --- | --- |
| list_populated_nonterminal.json | mobile_notes_test.go `TestMobileNotesListPaginationAndFields` (tagged note, pinned note, non-final page) | 200 | c8e9130790b2a738659481064e846168f5b09805ec143b7553ab8c33f1bc91a2 |
| list_populated_terminal.json | mobile_notes_test.go `TestMobileNotesListPaginationAndFields` (final page, no next_cursor) | 200 | 365c74a5623393f5368d2d18ed8ad220954fc6cb9e57b6b39c47af20db30528c |
| list_empty.json | mobile_notes_test.go `TestMobileNotesListPaginationAndFields` (empty first page) | 200 | eef46741adfc3a9f76294d3b78f37a45f113092ac9d44ee77c7a038a88ff09a1 |
| detail_complete.json | mobile_notes_test.go `TestMobileNoteDetail`/`TestMobileNotesFieldMinimization` shapes, populated with tags + two summaries (ready+pending) per the plan's Task 1 seeding recipe (CreateNote, UpdateNoteBody, AddNoteTag, CreateTemplate, CreatePendingSummary, CompleteSummary) | 200 | f498ee7123b1929c53fca6f1a9d0ef3e1cb189d368fa5b8dfe6c5866b73a65c6 |
| detail_optional_absent.json | mobile_notes_test.go `TestMobileNoteDetail` (no started_at/ended_at, no tags, no summaries) | 200 | 23ecaece81d5bf26e20e9608079b79acd6b7d1c2054e12da3ad282d9e1b84e0b |
| error_401.json | internal/auth/middleware.go unauthorized literal | 401 | 3341bff062d83e3afeb943f83bbb079f5b45cc050e796ec282dde50b5906cb31 |
| error_404.json | mobile_notes_test.go `TestMobileNoteDetail` "other owner sees 404" | 404 | b52893b7fcb4d0203d423e9572b847dbb93a1b9a58f76f2c4075b99d9661a50f |
| error_500.json | mobile_notes_test.go `TestMobileNotes500` | 500 | 5b078e4cd38acbba010a54cbb2d420a7c38c9c9ee61d81b79a383cd282ad1db3 |
| error_503.json | internal/auth/middleware.go resolver-failure literal | 503 | d89536ac6d0ba50394a0f28ed5fef6990972dfa5d088525bcc144cea8ea168d1 |

Regenerate with `sha256sum native/android/notes/src/test/resources/contract/*.json`
if a fixture ever changes.
