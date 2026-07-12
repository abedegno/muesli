package actionitems

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/abedegno/muesli/internal/model"
)

// ActionItem is a structured action item extracted from a note.
type ActionItem struct {
	Text    string `json:"text"`
	Owner   string `json:"owner"`
	DueHint string `json:"due_hint"`
}

// Decision is a structured decision extracted from a note.
type Decision struct {
	Text string `json:"text"`
}

// Input is the minimal note snapshot required to extract action items.
type Input struct {
	Transcript []model.Segment
	Summary    []model.SummarySection
}

// Result is the structured extraction output.
type Result struct {
	ActionItems []ActionItem
	Decisions   []Decision
}

// Generator produces the raw model response for a prompt.
type Generator interface {
	Generate(context.Context, string) (string, error)
}

// Extractor turns a note snapshot into structured action items and decisions.
type Extractor interface {
	Extract(context.Context, Input) (Result, error)
}

// Service extracts action items and decisions using a text generator.
type Service struct {
	Generator Generator
}

// New returns a Service backed by gen.
func New(gen Generator) *Service {
	return &Service{Generator: gen}
}

// Extract builds a prompt from the note snapshot, asks the generator for a raw
// response, and parses that response into structured action items and decisions.
func (s *Service) Extract(ctx context.Context, input Input) (Result, error) {
	if s == nil || s.Generator == nil {
		return Result{ActionItems: []ActionItem{}, Decisions: []Decision{}}, nil
	}
	raw, err := s.Generator.Generate(ctx, buildPrompt(input))
	if err != nil {
		return Result{}, err
	}
	return parseResponse(raw), nil
}

func buildPrompt(input Input) string {
	var b strings.Builder
	b.WriteString("Extract action items and decisions from this meeting note.\n\n")
	b.WriteString("Transcript:\n")
	if len(input.Transcript) == 0 {
		b.WriteString("- (empty)\n")
	} else {
		for i, seg := range input.Transcript {
			fmt.Fprintf(&b, "%d. %s\n", i+1, strings.TrimSpace(seg.Text))
		}
	}
	b.WriteString("\nSummary:\n")
	if len(input.Summary) == 0 {
		b.WriteString("- (empty)\n")
	} else {
		for i, section := range input.Summary {
			fmt.Fprintf(&b, "%d. %s: %s\n", i+1, strings.TrimSpace(section.Heading), strings.TrimSpace(section.ContentMarkdown))
		}
	}
	b.WriteString("\nReturn either JSON with action_items and decisions arrays, or plain bullet points if needed.\n")
	return b.String()
}

type responseShape struct {
	ActionItems []ActionItem `json:"action_items"`
	Decisions   []Decision   `json:"decisions"`
}

var dueHintRe = regexp.MustCompile(`(?i)\s+(?:by|due)\s+(.+)$`)

func parseResponse(raw string) Result {
	empty := Result{ActionItems: []ActionItem{}, Decisions: []Decision{}}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return empty
	}
	if body, ok := jsonBody(trimmed); ok {
		var shaped responseShape
		if err := json.Unmarshal([]byte(body), &shaped); err == nil && (len(shaped.ActionItems) > 0 || len(shaped.Decisions) > 0 || strings.Contains(body, `"action_items"`) || strings.Contains(body, `"decisions"`)) {
			return normalize(shaped.ActionItems, shaped.Decisions)
		}
	}
	return parseBulletResponse(trimmed)
}

func normalize(items []ActionItem, decisions []Decision) Result {
	if items == nil {
		items = []ActionItem{}
	}
	if decisions == nil {
		decisions = []Decision{}
	}
	return Result{ActionItems: items, Decisions: decisions}
}

func jsonBody(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed, true
	}
	firstNL := strings.IndexByte(trimmed, '\n')
	if firstNL == -1 {
		return "", false
	}
	if end := strings.LastIndex(trimmed, "```"); end > firstNL {
		return strings.TrimSpace(trimmed[firstNL+1 : end]), true
	}
	return "", false
}

func parseBulletResponse(raw string) Result {
	result := Result{ActionItems: []ActionItem{}, Decisions: []Decision{}}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = stripBulletPrefix(line)
		if line == "" {
			continue
		}
		if decision, ok := parseDecisionLine(line); ok {
			result.Decisions = append(result.Decisions, decision)
			continue
		}
		if item, ok := parseActionItemLine(line); ok {
			result.ActionItems = append(result.ActionItems, item)
		}
	}
	return result
}

func stripBulletPrefix(line string) string {
	for _, prefix := range []string{"- ", "* ", "• "} {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(line[len(prefix):])
		}
	}
	if len(line) >= 3 && line[1] == '.' && line[2] == ' ' && line[0] >= '0' && line[0] <= '9' {
		return strings.TrimSpace(line[3:])
	}
	return line
}

func parseDecisionLine(line string) (Decision, bool) {
	if len(line) < 9 || !strings.EqualFold(line[:9], "decision:") {
		return Decision{}, false
	}
	text := strings.TrimSpace(line[9:])
	if text == "" {
		return Decision{}, false
	}
	return Decision{Text: text}, true
}

func parseActionItemLine(line string) (ActionItem, bool) {
	if strings.HasPrefix(strings.ToLower(line), "[x]") || strings.HasPrefix(strings.ToLower(line), "[ ]") {
		line = strings.TrimSpace(line[3:])
	}
	colon := strings.IndexByte(line, ':')
	if colon <= 0 {
		return ActionItem{}, false
	}
	owner := strings.TrimSpace(line[:colon])
	body := strings.TrimSpace(line[colon+1:])
	if owner == "" || body == "" {
		return ActionItem{}, false
	}
	text, dueHint := splitDueHint(body)
	if text == "" {
		return ActionItem{}, false
	}
	return ActionItem{Text: text, Owner: owner, DueHint: dueHint}, true
}

func splitDueHint(text string) (string, string) {
	m := dueHintRe.FindStringSubmatch(text)
	if len(m) != 2 {
		return strings.TrimSpace(text), ""
	}
	return strings.TrimSpace(text[:len(text)-len(m[0])]), strings.TrimSpace(m[1])
}
