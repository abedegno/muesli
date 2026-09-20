"""Proves build_documents_section_prompt (the ONLY prompt builder this
plugin actually invokes for a cross-meeting analysis run -- see llm.py's
`use_documents = bool(req.documents)`) sends the exact envelope Go's
internal/execution admission preflight measures.

internal/execution/prompt_test.go's TestBuildSectionPromptMatchesProviderEnvelopeGolden
builds the same prompt (shared corpus + "Decisions"/"List decisions."
section) on the Go side and asserts it against the same golden fixture
checked in here under tests/testdata/cross_prompts/. If either
build_documents_section_prompt or Go's buildSectionPrompt ever drifts --
e.g. a longer/shorter output instruction on one side only -- one of these
two tests fails, so admission can never silently under-count the real
provider prompt (the review finding fixed on PR #773).
"""

from pathlib import Path

from ollama_app.prompt import build_documents_section_prompt
from ollama_app.schema import Document, GenerateRequest, Template, TemplateSection

GOLDEN_DIR = Path(__file__).parent / "testdata" / "cross_prompts"


def _corpus() -> str:
    return (GOLDEN_DIR / "two_meetings.golden").read_text()


def _golden_section_prompt() -> str:
    return (GOLDEN_DIR / "two_meetings_decisions_section.golden").read_text()


def test_build_documents_section_prompt_matches_go_admission_golden():
    """The plugin's real prompt for a documents (cross-meeting) request must
    be byte-for-byte the same envelope Go's admission preflight measured for
    the identical corpus/heading/instruction -- proving admission's byte
    count is a real upper bound over what is actually sent to the model,
    not a shorter synthetic placeholder."""
    req = GenerateRequest(
        documents=[Document(note_id="note-1")],
        notes_markdown=_corpus(),
        template=Template(sections=[TemplateSection(heading="Decisions", instruction="List decisions.")]),
    )
    prompt = build_documents_section_prompt(req, "Decisions", "List decisions.")
    assert prompt == _golden_section_prompt()


def test_build_documents_section_prompt_forwards_corpus_verbatim():
    # req.notes_markdown (the Go-admitted corpus) must appear byte-for-byte
    # at the start of the prompt -- never re-derived or reformatted.
    corpus = _corpus()
    req = GenerateRequest(
        documents=[Document(note_id="note-1")],
        notes_markdown=corpus,
        template=Template(sections=[TemplateSection(heading="Decisions", instruction="List decisions.")]),
    )
    prompt = build_documents_section_prompt(req, "Decisions", "List decisions.")
    assert prompt.startswith(corpus)
