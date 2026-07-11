// This file assembles the deterministic, pure-logic prompt/message sequence
// for one chat-with-your-notes turn: a fixed system directive, a numbered
// retrieval block (sources), the prior conversation history, and the new
// user message. It performs no I/O, DB, or HTTP calls -- callers (a later
// item, CHT04) are responsible for fetching sources via Retriever and
// persisting/sending the resulting messages.
package chat

import (
	"fmt"
	"strings"

	"github.com/abedegno/muesli/internal/model"
)

// SystemDirective is the fixed system-role instruction sent as the first
// message of every chat turn. It tells the agent to (a) answer only from the
// numbered sources supplied in the retrieval block, (b) cite claims inline
// with bracketed markers (e.g. [1], [2]) matching the source numbers, and (c)
// use speaker names verbatim as given in the sources -- mirroring the
// verbatim-alias convention in internal/worker/speaker_alias.go's and
// internal/chat/retrieval.go's speakerAliasDirective.
const SystemDirective = "You are a note-taking assistant answering questions using only the numbered sources provided below. " +
	"Base every claim strictly on those sources; do not use outside knowledge or speculate beyond what they say. " +
	"Cite the source(s) supporting each claim inline with bracketed markers matching the source numbers, e.g. [1] or [2][3]. " +
	"Use speaker names exactly as they appear in the sources verbatim; do not infer, abbreviate, or merge names."

// noSourcesNotice is the retrieval-block text used when there are no sources
// to render, so the prompt still clearly states that no sources are
// available (and, implicitly, that no citations are possible).
const noSourcesNotice = "No sources are available for this query."

// PromptMessage is one role-tagged message in the assembled prompt sequence.
// Role is one of "system", "user", or "assistant".
type PromptMessage struct {
	Role    string
	Content string
}

// RenderRetrievalBlock renders sources into a numbered list, 1-indexed in
// input order (index 0 -> [1], etc.) -- the same numbering later used for
// citation markers in the assistant's response (see ParseCitations). Each
// entry shows the note title, a human-readable mm:ss timestamp derived from
// StartMS, and the snippet. When sources is empty, it returns a fixed
// "no sources" notice instead of an empty list.
func RenderRetrievalBlock(sources []TranscriptRef) string {
	if len(sources) == 0 {
		return noSourcesNotice
	}

	lines := make([]string, 0, len(sources))
	for i, src := range sources {
		lines = append(lines, fmt.Sprintf("[%d] %s (%s): %s", i+1, src.NoteTitle, formatTimestamp(src.StartMS), src.Snippet))
	}
	return strings.Join(lines, "\n")
}

// formatTimestamp renders a millisecond offset as a deterministic mm:ss
// string (e.g. 65000 -> "01:05"). Negative values are clamped to zero.
func formatTimestamp(startMS int) string {
	if startMS < 0 {
		startMS = 0
	}
	totalSeconds := startMS / 1000
	minutes := totalSeconds / 60
	seconds := totalSeconds % 60
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}

// BuildPrompt assembles the full message sequence for one chat turn, in
// order: the system directive, the numbered retrieval block (built from
// sources), the prior conversation messages (role-tagged, in chronological
// order), and finally the new user message. It is a plain deterministic
// transformation with no I/O, DB, or HTTP calls.
//
// model.Message.Role is passed through as-is as PromptMessage.Role; callers
// are expected to only ever have "user"/"assistant" (and, in principle,
// "system") roles stored in history.
func BuildPrompt(sources []TranscriptRef, history []model.Message, userMessage string) []PromptMessage {
	prompt := make([]PromptMessage, 0, len(history)+3)

	prompt = append(prompt, PromptMessage{Role: "system", Content: SystemDirective})
	prompt = append(prompt, PromptMessage{Role: "system", Content: RenderRetrievalBlock(sources)})

	for _, msg := range history {
		prompt = append(prompt, PromptMessage{Role: msg.Role, Content: msg.Content})
	}

	prompt = append(prompt, PromptMessage{Role: "user", Content: userMessage})

	return prompt
}
