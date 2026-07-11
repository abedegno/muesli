from ollama_app.prompt import build_section_prompt
from ollama_app.schema import GenerateRequest, Segment, Template, TemplateSection


def _req():
    return GenerateRequest(
        transcript=[
            Segment(start_ms=0, end_ms=1000, text="We will ship Friday.", source="mixed"),
            Segment(start_ms=1000, end_ms=2000, text="Alice owns the release.", source="mixed"),
        ],
        notes_markdown="- ship date?\n- owner?",
        template=Template(
            sections=[
                TemplateSection(heading="Overview", instruction="Summarise in 2 sentences."),
                TemplateSection(heading="Action items", instruction="List owners + deadlines."),
            ]
        ),
    )


def _prompt():
    return build_section_prompt(_req(), "Overview", "Summarise in 2 sentences.")


def test_prompt_includes_the_section_notes_and_transcript():
    prompt = _prompt()
    assert "Overview" in prompt and "Summarise in 2 sentences." in prompt
    assert "ship date?" in prompt  # notes_markdown
    assert "We will ship Friday." in prompt  # transcript text
    # transcript lines carry their segment timestamps so refs are groundable
    assert "[0]" in prompt or "0-1000" in prompt


def test_prompt_requests_structured_json_for_one_section():
    prompt = _prompt()
    assert "JSON" in prompt
    assert "content_markdown" in prompt
    assert "refs" in prompt


def test_prompt_has_no_placeholder_tokens_and_forbids_them():
    # The old prompt showed the model a literal scaffold ("<section heading>",
    # "<markdown>"), which weak models copy verbatim instead of filling in.
    # The prompt must never contain angle-bracket placeholder tokens, and must
    # explicitly forbid the model from emitting them.
    prompt = _prompt()
    assert "<section heading>" not in prompt
    assert "<markdown>" not in prompt
    assert "placeholder" in prompt.lower()


def test_prompt_does_not_ask_the_model_for_a_heading_field():
    # The heading comes from the template (the server fills it); the model only
    # writes content. The output schema must not request a "heading" key.
    prompt = _prompt()
    assert '"heading"' not in prompt


def test_prompt_has_no_copyable_example_and_grounds_the_model():
    # Regression: a concrete in-prompt example ("Bob will profile the slow database
    # migration…") was copied verbatim by the 1B model instead of summarising the
    # real transcript. The prompt must carry NO example content, and must instruct
    # the model to use only the provided notes/transcript and not invent facts.
    prompt = _prompt().lower()
    assert "bob" not in prompt  # no leftover example actors
    assert "database migration" not in prompt
    assert "do not invent" in prompt
    assert "only" in prompt  # "using ONLY the user's notes and the meeting transcript"


def test_prompt_uses_speaker_attribution_when_present():
    req = GenerateRequest(
        transcript=[
            Segment(start_ms=0, end_ms=1000, text="Let's ship Friday.", source="mixed", speaker="Speaker 1"),
            Segment(start_ms=1000, end_ms=2000, text="I'll handle the release.", source="mixed", speaker="Speaker 2"),
        ],
        notes_markdown="- notes",
        template=Template(
            sections=[TemplateSection(heading="Overview", instruction="Summarise.")]
        ),
    )
    prompt = build_section_prompt(req, "Overview", "Summarise.")
    assert "Speaker 1:" in prompt
    assert "Speaker 2:" in prompt


def test_prompt_no_speaker_attribution_when_absent():
    req = GenerateRequest(
        transcript=[
            Segment(start_ms=0, end_ms=1000, text="We will ship Friday.", source="mixed"),
            Segment(start_ms=1000, end_ms=2000, text="Alice owns the release.", source="mixed"),
        ],
        notes_markdown="- notes",
        template=Template(
            sections=[TemplateSection(heading="Overview", instruction="Summarise.")]
        ),
    )
    prompt = build_section_prompt(req, "Overview", "Summarise.")
    # No stray colon in the index-label pattern: lines should be "[0] text", not "[0] : text"
    assert "[0] We will ship Friday." in prompt
    assert "[1] Alice owns the release." in prompt


def test_prompt_mixed_speaker_and_no_speaker():
    req = GenerateRequest(
        transcript=[
            Segment(start_ms=0, end_ms=1000, text="Hello everyone.", source="mixed", speaker="Alice"),
            Segment(start_ms=1000, end_ms=2000, text="Background noise.", source="mixed"),
            Segment(start_ms=2000, end_ms=3000, text="Thanks for joining.", source="mixed", speaker="Bob"),
        ],
        notes_markdown="- notes",
        template=Template(
            sections=[TemplateSection(heading="Overview", instruction="Summarise.")]
        ),
    )
    prompt = build_section_prompt(req, "Overview", "Summarise.")
    assert "Alice: Hello everyone." in prompt
    assert "[1] Background noise." in prompt
    assert "Bob: Thanks for joining." in prompt
