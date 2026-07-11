package api

import "testing"

func TestIsKeyForNote(t *testing.T) {
	t.Parallel()
	id := "11111111-1111-1111-1111-111111111111"
	if !isKeyForNote("notes/"+id+"/audio/abc", id) {
		t.Error("valid audio key should pass")
	}
	for _, bad := range []string{
		"notes/" + id + "/other/abc",                           // wrong subdir
		"notes/22222222-2222-2222-2222-222222222222/audio/abc", // other note
		"notes/" + id + "/audio/",                              // empty leaf
		"notes/" + id + "/audio/../x",                          // traversal
	} {
		if isKeyForNote(bad, id) {
			t.Errorf("bad key passed: %q", bad)
		}
	}
}
