package api_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"testing"
)

// readableMethods is every viewer-readable method issue #12 introduced.
// Adding a new one to the store without also adding it here (and to
// readableAllowlist below) is caught the moment it is called from
// notes.go/notes_full.go, forcing a deliberate review of the widening.
var readableMethods = map[string]bool{
	"GetReadableNote":       true,
	"ListReadableNotes":     true,
	"ReadableNoteFolderIDs": true,
	"GetReadableFolder":     true,
	"GetReadableNoteBody":   true,
	"GetReadableNoteTags":   true,
	"GetReadableTranscript": true,
	"GetReadableSummaries":  true,
}

// readableAllowlist is the exhaustive map of handler -> readable methods it
// may call. A readable-method call found in a handler NOT listed here, or a
// listed handler calling a readable method NOT in its set, fails the guard.
var readableAllowlist = map[string]map[string]bool{
	"handleGetNote":   {"GetReadableNote": true, "ReadableNoteFolderIDs": true},
	"handleListNotes": {"GetReadableFolder": true, "ListReadableNotes": true},
	"handleGetNoteFull": {
		"GetReadableNote": true, "ReadableNoteFolderIDs": true,
		"GetReadableNoteTags": true, "GetReadableNoteBody": true,
		"GetReadableTranscript": true, "GetReadableSummaries": true,
	},
}

// unscopedComponentMethods must never be called from handleGetNoteFull --
// doing so would bypass the viewer authorization GetReadable* adds.
var unscopedComponentMethods = map[string]bool{
	"NoteFolderIDs": true,
	"NoteBody":      true,
	"NoteTags":      true,
	"GetTranscript": true,
	"GetSummaries":  true,
}

// apiGoFiles returns every non-test .go source file in this package's
// directory (".").
func apiGoFiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	var files []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || len(name) < 3 || name[len(name)-3:] != ".go" {
			continue
		}
		if len(name) > 8 && name[len(name)-8:] == "_test.go" {
			continue
		}
		files = append(files, name)
	}
	return files
}

// scanMethodCallsByFileFunc parses each of files and records, per (file,
// enclosing method), every selector-call name it makes -- e.g.
// scanned["notes_export.go"]["handleGetNoteExport"]["GetNote"].
func scanMethodCallsByFileFunc(t *testing.T, files []string) map[string]map[string]map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	found := map[string]map[string]map[string]bool{}
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		f, err := parser.ParseFile(fset, file, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Body == nil {
				continue
			}
			fname := fn.Name.Name
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if found[file] == nil {
					found[file] = map[string]map[string]bool{}
				}
				if found[file][fname] == nil {
					found[file][fname] = map[string]bool{}
				}
				found[file][fname][sel.Sel.Name] = true
				return true
			})
		}
	}
	return found
}

// readableAllowedFiles is where a readable-method call is permitted at all
// -- issue #12 explicitly restricts every GetReadable*/ListReadable*/
// ReadableNoteFolderIDs call to the display/exact-folder branches of these
// two files. A call appearing in ANY other file in this package fails the
// guard immediately, regardless of which function it's in.
var readableAllowedFiles = map[string]bool{
	"notes.go":      true,
	"notes_full.go": true,
}

// TestReadableMethodCallsiteAllowlist is the Go-AST guard (issue #12, task
// 8): it parses every non-test .go file in this package and asserts every
// call to a GetReadable*/ListReadable*/ReadableNoteFolderIDs method appears
// ONLY in notes.go/notes_full.go, and there only in its allowed handler --
// in both directions, so an unreviewed new call site (in ANY file) fails
// loudly, and so does silently dropping an expected one. It also forbids
// handleGetNoteFull from calling any unscoped component method.
func TestReadableMethodCallsiteAllowlist(t *testing.T) {
	byFile := scanMethodCallsByFileFunc(t, apiGoFiles(t))

	found := map[string]map[string]bool{} // fname -> method set, notes.go/notes_full.go only
	for file, byFunc := range byFile {
		for fname, calls := range byFunc {
			var readableCalls []string
			for name := range calls {
				if readableMethods[name] {
					readableCalls = append(readableCalls, name)
				}
			}
			if len(readableCalls) == 0 {
				continue
			}
			sort.Strings(readableCalls)
			if !readableAllowedFiles[file] {
				t.Fatalf("function %s in %s calls readable method(s) %v, but only notes.go/notes_full.go may -- review this widening deliberately", fname, file, readableCalls)
			}
			found[fname] = calls
		}
	}

	if calls := found["handleGetNoteFull"]; calls != nil {
		var leaked []string
		for name := range calls {
			if unscopedComponentMethods[name] {
				leaked = append(leaked, name)
			}
		}
		if len(leaked) > 0 {
			sort.Strings(leaked)
			t.Fatalf("handleGetNoteFull must never call unscoped component methods, found: %v", leaked)
		}
	}

	for fname, calls := range found {
		var readableCalls []string
		for name := range calls {
			if readableMethods[name] {
				readableCalls = append(readableCalls, name)
			}
		}
		sort.Strings(readableCalls)
		allowed, ok := readableAllowlist[fname]
		if !ok {
			t.Fatalf("function %s calls readable method(s) %v but is not in readableAllowlist -- review and add it deliberately", fname, readableCalls)
		}
		for _, name := range readableCalls {
			if !allowed[name] {
				t.Fatalf("function %s calls %s, not permitted for it (allowed: %v)", fname, name, allowed)
			}
		}
	}

	for fname, allowed := range readableAllowlist {
		calls := found[fname]
		for name := range allowed {
			if calls == nil || !calls[name] {
				t.Fatalf("expected %s to call %s per readableAllowlist, but it does not -- update the allowlist if this was intentionally removed", fname, name)
			}
		}
	}
}

// reviewedGetNoteCallers is the exhaustive, reviewed inventory of every
// production call site of the owner-only Store.GetNote across internal/api,
// captured when issue #12 introduced the viewer-readable alternative. Any
// new caller not in this set fails TestGetNoteCallsiteInventory, forcing a
// deliberate decision: owner-only (add it here) or viewer-readable (use
// GetReadableNote, which has its own allowlist above).
var reviewedGetNoteCallers = map[string]map[string]bool{
	"chat.go":                     {"handleSendMessage": true, "chatSources": true},
	"export_batch.go":             {"handleBatchExport": true},
	"import_audio.go":             {"handleImportNote": true},
	"notes_export.go":             {"handleGetNoteExport": true},
	"notes_template_summarize.go": {"handleSummarizeTemplate": true},
	"stream.go":                   {"handleNoteStream": true},
	"upload.go":                   {"handleAudioUploadURL": true, "handleAudioDownloadURL": true},
	"notes.go": {
		"handleUpdateNoteTitle": true,
		"handleSetNoteEvent":    true,
		"handleResummarize":     true,
		"handleRetranscribe":    true,
		"handleRetryNote":       true,
		"handleProcessNextNote": true,
	},
}

// TestGetNoteCallsiteInventory parses every non-test .go file in
// internal/api and requires each call to Store.GetNote to appear in a
// function listed in reviewedGetNoteCallers -- see its doc comment.
func TestGetNoteCallsiteInventory(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || len(name) < 3 || name[len(name)-3:] != ".go" {
			continue
		}
		if len(name) > 8 && name[len(name)-8:] == "_test.go" {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Body == nil {
				continue
			}
			fname := fn.Name.Name
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "GetNote" {
					return true
				}
				allowed := reviewedGetNoteCallers[name]
				if allowed == nil || !allowed[fname] {
					t.Errorf("unreviewed Store.GetNote call in %s:%s -- add it to reviewedGetNoteCallers (owner-only) or switch to GetReadableNote (viewer-safe) and update the other allowlist", name, fname)
				}
				return true
			})
		}
	}
}
