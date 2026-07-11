package worker

import (
	"strings"
	"unicode/utf8"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/plugin"
)

// truncationUsageThreshold is the fraction of a plugin-reported max token
// budget at (or above) which we treat the response as likely truncated by a
// context-window overflow.
const truncationUsageThreshold = 0.95

// terminalPunctuation is the set of characters a properly-terminated summary
// is expected to end with once trailing whitespace is stripped.
const terminalPunctuation = ".!?\")]"

// DetectTruncation is a pure heuristic for flagging a summary that looks
// 'ready' but may have been silently cut short by the model's context window
// overflowing on a long transcript. It returns true when EITHER:
//
//   - usage is non-nil, reports a positive MaxTokens, and TokensUsed is at or
//     above ~95% of MaxTokens ("close to the wall"); OR
//   - the summary text (all sections' ContentMarkdown concatenated, right-
//     trimmed of whitespace) is non-empty and its last character is not one of
//     the terminal-punctuation set: . ! ? " ) ]
//
// An empty/whitespace-only summary text is NOT flagged truncated by the text
// heuristic (that's a distinct empty/failed case, out of scope here) — only
// the usage heuristic can flag it.
func DetectTruncation(sections []model.SummarySection, usage *plugin.GenerateUsage) bool {
	if usage != nil && usage.MaxTokens > 0 {
		if float64(usage.TokensUsed) >= truncationUsageThreshold*float64(usage.MaxTokens) {
			return true
		}
	}

	var b strings.Builder
	for _, sec := range sections {
		b.WriteString(sec.ContentMarkdown)
	}
	text := strings.TrimRight(b.String(), " \t\n\r\v\f")
	if text == "" {
		return false
	}

	last, _ := utf8.DecodeLastRuneInString(text)
	return !strings.ContainsRune(terminalPunctuation, last)
}
