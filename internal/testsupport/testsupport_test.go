package testsupport

import (
	"fmt"
	"testing"
)

type fakeT struct {
	helperCalls int
	skipCalls   []string
	fatalCalls  []string
}

type fatalPanic struct{}

func (f *fakeT) Helper() {
	f.helperCalls++
}

func (f *fakeT) Skip(args ...any) {
	f.skipCalls = append(f.skipCalls, fmt.Sprint(args...))
}

func (f *fakeT) Fatalf(format string, args ...any) {
	f.fatalCalls = append(f.fatalCalls, fmt.Sprintf(format, args...))
	panic(fatalPanic{})
}

func TestRequireDependencySkipsWhenCIUnset(t *testing.T) {
	t.Setenv("CI", "")

	fake := &fakeT{}
	requireDependency(fake, false, "ffmpeg not available on PATH", false)

	if len(fake.skipCalls) != 1 {
		t.Fatalf("Skip calls = %d, want 1", len(fake.skipCalls))
	}
	if len(fake.fatalCalls) != 0 {
		t.Fatalf("Fatalf calls = %d, want 0", len(fake.fatalCalls))
	}
}

func TestRequireDependencyFailsWhenCISet(t *testing.T) {
	t.Setenv("CI", "1")

	fake := &fakeT{}
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected Fatalf panic")
		} else if _, ok := r.(fatalPanic); !ok {
			t.Fatalf("unexpected panic type %T", r)
		}
	}()
	requireDependency(fake, false, "ffmpeg not available on PATH", true)

	if len(fake.skipCalls) != 0 {
		t.Fatalf("Skip calls = %d, want 0", len(fake.skipCalls))
	}
	if len(fake.fatalCalls) != 1 {
		t.Fatalf("Fatalf calls = %d, want 1", len(fake.fatalCalls))
	}
}

func TestRequireDependencyNoopsWhenAvailable(t *testing.T) {
	for _, ci := range []string{"", "1"} {
		t.Run(ci, func(t *testing.T) {
			t.Setenv("CI", ci)

			fake := &fakeT{}
			requireDependency(fake, true, "ffmpeg not available on PATH", ci != "")

			if len(fake.skipCalls) != 0 {
				t.Fatalf("Skip calls = %d, want 0", len(fake.skipCalls))
			}
			if len(fake.fatalCalls) != 0 {
				t.Fatalf("Fatalf calls = %d, want 0", len(fake.fatalCalls))
			}
		})
	}
}
