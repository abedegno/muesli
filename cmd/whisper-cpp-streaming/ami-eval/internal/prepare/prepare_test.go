package prepare

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/acquire"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/audio"
	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/manifest"
)

func writeWAV(t *testing.T, path string, samples []float32, sampleRate int) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := audio.Encode(f, samples, sampleRate); err != nil {
		t.Fatal(err)
	}
}

func writeAnnotation(t *testing.T, path string, doc string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func testAcquired(t *testing.T, dir string, samples []float32) acquire.AcquiredRecording {
	t.Helper()
	audioPath := filepath.Join(dir, "audio-src.wav")
	writeWAV(t, audioPath, samples, SampleRate)

	annPath := filepath.Join(dir, "A.words.xml")
	writeAnnotation(t, annPath, `<nite:root xmlns:nite="http://nite.sourceforge.net/">
		<w starttime="0.0" endtime="0.5">hi</w>
	</nite:root>`)

	sel0 := 0
	return acquire.AcquiredRecording{
		Recording: manifest.Recording{
			ID:        "ES2002a-headset",
			MeetingID: "ES2002a",
			Class:     manifest.ClassScenario,
			Mic:       manifest.MicHeadset,
			Audio: manifest.AudioObject{
				URL: "https://example.test/audio.wav", SizeBytes: 1, SHA256: "audio-sha",
				Channels: 1, ChannelPolicy: manifest.ChannelPolicyExplicit, SelectChannel: &sel0,
			},
			Annotations: []manifest.AnnotationObject{
				{ParticipantID: "A", Member: "words/ES2002a.A.words.xml", SizeBytes: 1, SHA256: "ann-sha-A"},
			},
		},
		AudioPath:       audioPath,
		AnnotationPaths: map[string]string{"A": annPath},
	}
}

func TestPrepareProducesCanonicalOutputs(t *testing.T) {
	root := t.TempDir()
	cache := acquire.Cache{Root: root}
	samples := make([]float32, SampleRate*2) // 2s of digital silence
	acq := testAcquired(t, t.TempDir(), samples)

	p, err := Prepare(cache, "digest1", acq)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if len(p.Audio) != len(samples) {
		t.Fatalf("expected %d samples, got %d", len(samples), len(p.Audio))
	}
	if len(p.Reference.Intervals) != 1 {
		t.Fatalf("expected 1 reference interval, got %+v", p.Reference.Intervals)
	}
	for _, name := range []string{"audio.wav", "reference.json", "provenance.json"} {
		if _, err := os.Stat(filepath.Join(p.Dir, name)); err != nil {
			t.Fatalf("expected %s to exist: %v", name, err)
		}
	}
}

func TestPrepareReusesExactMatch(t *testing.T) {
	root := t.TempDir()
	cache := acquire.Cache{Root: root}
	samples := make([]float32, SampleRate)
	acq := testAcquired(t, t.TempDir(), samples)

	first, err := Prepare(cache, "digest1", acq)
	if err != nil {
		t.Fatal(err)
	}
	// Mutate the on-disk audio.wav directly (bypassing Prepare) to prove
	// the second call reuses without recomputing.
	marker := []byte("PROOF-OF-REUSE-MARKER")
	provPath := filepath.Join(first.Dir, "reference.json")
	orig, err := os.ReadFile(provPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(provPath+".sentinel", marker, 0o644); err != nil {
		t.Fatal(err)
	}

	second, err := Prepare(cache, "digest1", acq)
	if err != nil {
		t.Fatal(err)
	}
	if second.Dir != first.Dir {
		t.Fatalf("expected same dir, got %q vs %q", second.Dir, first.Dir)
	}
	// The sentinel file must still be present: Prepare did not wipe and
	// regenerate the directory on the reuse path.
	if _, err := os.Stat(provPath + ".sentinel"); err != nil {
		t.Fatal("expected reuse path to leave the prepared directory untouched")
	}
	if !bytes.Equal(orig, mustRead(t, provPath)) {
		t.Fatal("expected reference.json unchanged on reuse")
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestPrepareRecomputesWhenSourceHashChanges(t *testing.T) {
	root := t.TempDir()
	cache := acquire.Cache{Root: root}
	samples := make([]float32, SampleRate)
	acq := testAcquired(t, t.TempDir(), samples)

	first, err := Prepare(cache, "digest1", acq)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(first.Dir, "reference.json.sentinel"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	acq.Recording.Audio.SHA256 = "different-sha"
	second, err := Prepare(cache, "digest1", acq)
	if err != nil {
		t.Fatal(err)
	}
	if second.Dir != first.Dir {
		t.Fatalf("expected same dir, got %q vs %q", second.Dir, first.Dir)
	}
	if _, err := os.Stat(filepath.Join(first.Dir, "reference.json.sentinel")); !os.IsNotExist(err) {
		t.Fatal("expected recompute to replace the prepared directory, clearing the sentinel")
	}
}

func TestPreparePreservesPriorGoodDataOnFailure(t *testing.T) {
	root := t.TempDir()
	cache := acquire.Cache{Root: root}
	samples := make([]float32, SampleRate)
	acq := testAcquired(t, t.TempDir(), samples)

	first, err := Prepare(cache, "digest1", acq)
	if err != nil {
		t.Fatal(err)
	}

	// Force a failure by pointing the annotation path at a malformed file.
	broken := acq
	badPath := filepath.Join(t.TempDir(), "bad.xml")
	writeAnnotation(t, badPath, `<nite:root xmlns:nite="http://nite.sourceforge.net/">
		<w starttime="2.0" endtime="1.0">bad</w>
	</nite:root>`)
	broken.AnnotationPaths = map[string]string{"A": badPath}
	broken.Recording.Audio.SHA256 = "yet-another-sha" // force a recompute attempt, not reuse

	if _, err := Prepare(cache, "digest1", broken); err == nil {
		t.Fatal("expected failure for malformed annotation")
	}

	// The original good prepared directory must be untouched.
	for _, name := range []string{"audio.wav", "reference.json", "provenance.json"} {
		if _, err := os.Stat(filepath.Join(first.Dir, name)); err != nil {
			t.Fatalf("expected prior good %s preserved: %v", name, err)
		}
	}
}

func TestPrepareAtomicNoLeftoverTempDirs(t *testing.T) {
	root := t.TempDir()
	cache := acquire.Cache{Root: root}
	samples := make([]float32, SampleRate)
	acq := testAcquired(t, t.TempDir(), samples)

	p, err := Prepare(cache, "digest1", acq)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Dir(p.Dir))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != filepath.Base(p.Dir) {
			t.Fatalf("unexpected leftover entry %q", e.Name())
		}
	}
}
