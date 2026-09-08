package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestParseLimit(t *testing.T) {
	cases := []struct {
		name   string
		raw    string
		def    int
		max    int
		want   int
		wantOK bool
	}{
		{"absent uses default", "", 50, 100, 50, true},
		{"minimum", "1", 50, 100, 1, true},
		{"maximum", "100", 50, 100, 100, true},
		{"mid range", "37", 50, 100, 37, true},
		{"zero rejected", "0", 50, 100, 0, false},
		{"negative rejected", "-1", 50, 100, 0, false},
		{"over max rejected", "101", 50, 100, 0, false},
		{"non-numeric rejected", "abc", 50, 100, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := url.Values{}
			if tc.raw != "" {
				q.Set("limit", tc.raw)
			}
			got, ok := parseLimit(q, tc.def, tc.max)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && got != tc.want {
				t.Fatalf("limit = %d, want %d", got, tc.want)
			}
		})
	}
}

type testCursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        string    `json:"id"`
}

func TestCursorRoundTrip(t *testing.T) {
	want := testCursor{CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), ID: "abc-123"}
	tok := encodeCursor("things", want)
	if tok == "" {
		t.Fatal("expected non-empty cursor")
	}
	var got testCursor
	if err := decodeCursor("things", tok, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) || got.ID != want.ID {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestCursorWrongKindRejected(t *testing.T) {
	tok := encodeCursor("things", testCursor{CreatedAt: time.Now(), ID: "x"})
	var got testCursor
	if err := decodeCursor("other-things", tok, &got); err == nil {
		t.Fatal("expected wrong-kind cursor to be rejected")
	}
}

func TestCursorBadBase64Rejected(t *testing.T) {
	var got testCursor
	if err := decodeCursor("things", "not base64!!!", &got); err == nil {
		t.Fatal("expected bad base64 to be rejected")
	}
}

func TestCursorBadJSONRejected(t *testing.T) {
	raw := base64.RawURLEncoding.EncodeToString([]byte("{not json"))
	var got testCursor
	if err := decodeCursor("things", raw, &got); err == nil {
		t.Fatal("expected bad JSON to be rejected")
	}
}

func TestCursorMissingTupleValuesRejected(t *testing.T) {
	// A structurally valid envelope whose payload is missing the id field.
	env := cursorEnvelope{Kind: "things", Data: json.RawMessage(`{"created_at":"2026-01-02T03:04:05Z"}`)}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	tok := base64.RawURLEncoding.EncodeToString(raw)
	var got testCursor
	if err := decodeCursor("things", tok, &got); err != nil {
		t.Fatalf("decode should succeed at the codec level: %v", err)
	}
	if got.ID != "" {
		t.Fatal("expected missing id to decode as zero value")
	}
	// Callers reject this zero-value tuple themselves (see decodeUserCursor
	// etc.), which is exercised by their own endpoint tests below.
}

func TestCursorTrailingInputRejected(t *testing.T) {
	tok := encodeCursor("things", testCursor{CreatedAt: time.Now(), ID: "x"})
	raw, err := base64.RawURLEncoding.DecodeString(tok)
	if err != nil {
		t.Fatalf("decode token: %v", err)
	}
	trailing := base64.RawURLEncoding.EncodeToString(append(raw, []byte(`{}`)...))
	var got testCursor
	if err := decodeCursor("things", trailing, &got); err == nil {
		t.Fatal("expected trailing input after the JSON value to be rejected")
	}
}

func TestParsePageRequestDefaultsAndCursor(t *testing.T) {
	// No cursor: limit only.
	rec := httptest.NewRecorder()
	q := url.Values{}
	limit, hasCursor, ok := parsePageRequest(rec, q, 50, 100, "things", &testCursor{})
	if !ok || hasCursor || limit != 50 {
		t.Fatalf("limit=%d hasCursor=%v ok=%v", limit, hasCursor, ok)
	}

	// Valid cursor.
	tok := encodeCursor("things", testCursor{CreatedAt: time.Now(), ID: "x"})
	q = url.Values{"cursor": []string{tok}}
	var dst testCursor
	limit, hasCursor, ok = parsePageRequest(rec, q, 50, 100, "things", &dst)
	if !ok || !hasCursor || limit != 50 || dst.ID != "x" {
		t.Fatalf("limit=%d hasCursor=%v ok=%v dst=%+v", limit, hasCursor, ok, dst)
	}

	// Malformed cursor -> 400 via ok=false.
	q = url.Values{"cursor": []string{"!!!not-a-cursor"}}
	_, _, ok = parsePageRequest(rec, q, 50, 100, "things", &testCursor{})
	if ok {
		t.Fatal("expected malformed cursor to be rejected")
	}

	// Malformed limit -> 400 via ok=false.
	q = url.Values{"limit": []string{"0"}}
	_, _, ok = parsePageRequest(rec, q, 50, 100, "things", &testCursor{})
	if ok {
		t.Fatal("expected malformed limit to be rejected")
	}
}
