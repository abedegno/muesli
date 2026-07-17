package pluginkit

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/abedegno/muesli/internal/model"
)

func TestDecodeFFmpegF32LE(t *testing.T) {
	t.Run("round_trip", func(t *testing.T) {
		want := []float32{1.0, -1.0, 0.5, 0}
		buf := make([]byte, len(want)*4)
		for i, v := range want {
			binary.LittleEndian.PutUint32(buf[i*4:(i+1)*4], math.Float32bits(v))
		}

		got, err := decodeFFmpegF32LE(buf)
		if err != nil {
			t.Fatalf("decodeFFmpegF32LE returned error: %v", err)
		}
		if len(got) != len(want) {
			t.Fatalf("decodeFFmpegF32LE returned %d samples, want %d", len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("sample %d = %v, want %v", i, got[i], want[i])
			}
		}
	})

	t.Run("empty_input", func(t *testing.T) {
		got, err := decodeFFmpegF32LE(nil)
		if err != nil {
			t.Fatalf("decodeFFmpegF32LE returned error: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("decodeFFmpegF32LE returned %d samples, want 0", len(got))
		}
	})

	t.Run("truncated_input", func(t *testing.T) {
		_, err := decodeFFmpegF32LE([]byte{0x01, 0x02, 0x03})
		if err == nil {
			t.Fatal("decodeFFmpegF32LE returned nil error for truncated input")
		}
		if got := err.Error(); got != "ffmpeg returned truncated float32 pcm" {
			t.Fatalf("decodeFFmpegF32LE error = %q, want %q", got, "ffmpeg returned truncated float32 pcm")
		}
	})
}

func TestSegmentsText(t *testing.T) {
	tests := []struct {
		name string
		segs []model.Segment
		want string
	}{
		{
			name: "simple_join",
			segs: []model.Segment{{Text: "alpha"}, {Text: "beta"}},
			want: "alpha beta",
		},
		{
			name: "leading_and_trailing_empty",
			segs: []model.Segment{{Text: ""}, {Text: "alpha"}, {Text: ""}},
			want: "alpha",
		},
		{
			name: "interior_empty",
			segs: []model.Segment{{Text: "alpha"}, {Text: ""}, {Text: "beta"}},
			want: "alpha beta",
		},
		{
			name: "empty_input",
			segs: nil,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := segmentsText(tt.segs); got != tt.want {
				t.Fatalf("segmentsText() = %q, want %q", got, tt.want)
			}
		})
	}
}
