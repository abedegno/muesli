package audiohash

import "testing"

func TestFfmpegBinHonorsEnv(t *testing.T) {
	t.Setenv("MUESLI_FFMPEG_BIN", "  /opt/x/ffmpeg  ")
	if got := ffmpegBin(); got != "/opt/x/ffmpeg" {
		t.Fatalf("ffmpegBin() = %q, want %q", got, "/opt/x/ffmpeg")
	}

	t.Setenv("MUESLI_FFMPEG_BIN", "")
	if got := ffmpegBin(); got != "ffmpeg" {
		t.Fatalf("ffmpegBin() = %q, want %q", got, "ffmpeg")
	}
}
