Golden fixtures shared with `internal/execution/testdata/cross_prompts` (Go).
Keep both copies byte-identical; `internal/execution`'s Go tests are the
source of truth (see `internal/execution/prompt_test.go`).

- `two_meetings.golden`: the rendered corpus (`buildCorpus`/`PreparedExecution.Corpus`),
  sent verbatim as `GenerateRequest.notes_markdown`.
- `two_meetings_decisions_section.golden`: the complete section prompt --
  the corpus above plus the `## The section to write` / `## Output` envelope
  -- exactly as sent to the model for a cross-meeting analysis run. Go's
  `buildSectionPrompt` (what `Admit` measures) and this plugin's
  `build_documents_section_prompt` (what is actually sent) must both produce
  this fixture byte-for-byte; see `test_documents_prompt.py`. This is the
  admission byte-bound proof for the review finding fixed on PR #773: admission
  must measure the real provider envelope, not a shorter synthetic placeholder.
