from .schema import GenerateRequest

SYSTEM = (
    "You are a meeting-notes assistant. Write ONE section of a clean, factual "
    "summary, using ONLY the user's notes and the meeting transcript provided below. "
    "Base every statement strictly on that content — do NOT invent people, names, "
    "tasks, dates, decisions, numbers, or any fact that is not present in the notes "
    "or transcript. Do NOT carry over examples or content from these instructions. "
    "Synthesise in your own words: do NOT copy transcript lines verbatim, and never "
    "include the bracketed segment markers like [0] or the (start-end) timestamps in "
    "your content. If the notes and transcript contain nothing relevant to THIS "
    "section, write exactly: None recorded. Where a statement comes from the "
    "transcript, cite the supporting segment indices in `refs` (not in the text). "
    "Write real prose or bullet points — never output placeholder text. Respond with "
    "JSON only."
)


def build_section_prompt(req: GenerateRequest, heading: str, instruction: str) -> str:
    """Build the prompt for ONE summary section. Generating a single section per
    call keeps the task narrow, which small local models handle far more reliably
    than emitting a whole multi-section JSON array at once.

    Deliberately contains NO example content: a weak model will copy a concrete
    example verbatim instead of writing from the transcript. The JSON *shape* is
    enforced out-of-band by the model's structured-output format (see llm.py), so
    no in-prompt schema sample is needed. Transcript lines are numbered by segment
    index so the model can cite them in `refs`."""
    # Index-only line format. The (start-end)ms range was noise the model copied
    # into its output; refs cite by index alone, so the timestamps aren't needed here.
    transcript = "\n".join(
        f"[{i}] {seg.speaker}: {seg.text}" if seg.speaker else f"[{i}] {seg.text}"
        for i, seg in enumerate(req.transcript)
    )
    return (
        f"{SYSTEM}\n\n"
        f"## The section to write\n{heading} — {instruction}\n\n"
        f"## User notes (Markdown)\n{req.notes_markdown or '(none)'}\n\n"
        f"## Transcript (each line: [index] (start-end) text)\n{transcript or '(empty)'}\n\n"
        f"## Output\n"
        f'Return a JSON object with "content_markdown" (the content for the '
        f'"{heading}" section, written only from the notes and transcript above) and '
        f'optional "refs" (a list of transcript indices that support it).'
    )


DOCUMENTS_OUTPUT_INSTRUCTION = (
    'Return a JSON object with "content_markdown" (the content for this section, '
    "citing sources with the bracketed numbers given in the corpus above, e.g. "
    '[1] or [2][3]) and optional "refs" (always omit or leave empty for a '
    "cross-meeting analysis run -- citations live inline in content_markdown, not in refs)."
)


def build_documents_section_prompt(req: GenerateRequest, heading: str, instruction: str) -> str:
    """Build the prompt for ONE section of a cross-meeting analysis run
    (issue #765): req.documents is present instead of req.transcript.

    req.notes_markdown carries the ALREADY-RENDERED corpus -- the exact
    canonical text internal/execution (Go) built and admission-checked before
    ever calling this plugin (see internal/execution/prompt.go's buildCorpus
    and the shared goldens under internal/execution/testdata/cross_prompts,
    mirrored here under tests/testdata/cross_prompts). It is forwarded
    VERBATIM, byte-for-byte, into this section's prompt -- this function only
    adds the section-specific instruction and output-format framing around
    it, exactly like build_section_prompt does for the legacy transcript
    path. Never re-derives or reformats the corpus from req.documents; doing
    so would risk silently diverging from the byte-bounded prompt Go already
    admitted.
    """
    return (
        f"{req.notes_markdown}\n\n"
        f"## The section to write\n{heading} — {instruction}\n\n"
        f"## Output\n{DOCUMENTS_OUTPUT_INSTRUCTION}"
    )
