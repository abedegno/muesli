package embedded

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildWhisperCppStreamingCmd(t *testing.T) {
	t.Setenv("MUESLI_WHISPER_LIVE_MODEL_DIR", "/bundle/models")
	t.Setenv("MUESLI_WHISPER_LIVE_MODEL_URL", "file:///bundle/models/live.bin")
	t.Setenv("MUESLI_WHISPER_LIVE_MODEL", "live.bin")
	t.Setenv("MUESLI_WHISPER_LIVE_LANGUAGE", "fr")

	cmd := buildWhisperCppStreamingCmd("/bin/whisper-cpp-streaming", "127.0.0.1:42125", "secret-token")
	wantArgs := []string{
		"/bin/whisper-cpp-streaming",
		"--addr", "127.0.0.1:42125",
		"--token", "secret-token",
		"--name", DefaultWhisperStreamingName,
		"--version", DefaultWhisperStreamingVersion,
	}
	if strings.Join(cmd.Args, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("args = %q, want %q", cmd.Args, wantArgs)
	}
	for _, want := range []string{
		"MUESLI_WHISPER_LIVE_ADDR=127.0.0.1:42125",
		"MUESLI_WHISPER_LIVE_TOKEN=secret-token",
		"MUESLI_WHISPER_LIVE_NAME=" + DefaultWhisperStreamingName,
		"MUESLI_WHISPER_LIVE_VERSION=" + DefaultWhisperStreamingVersion,
		"MUESLI_WHISPER_LIVE_MODEL_DIR=/bundle/models",
		"MUESLI_WHISPER_LIVE_MODEL_URL=file:///bundle/models/live.bin",
		"MUESLI_WHISPER_LIVE_MODEL=live.bin",
		"MUESLI_WHISPER_LIVE_LANGUAGE=fr",
	} {
		if !hasEnvEntry(cmd.Env, want) {
			t.Fatalf("env missing %q", want)
		}
	}
}

func TestLocateWhisperCppStreamingBinary(t *testing.T) {
	tests := []struct {
		name      string
		override  string
		createBin bool
		wantErr   bool
	}{
		{name: "override present", override: "whisper-cpp-streaming", createBin: true},
		{name: "override missing", override: "does-not-exist", wantErr: true},
		{name: "override blank uses candidate", createBin: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			binDir := tmpDir
			if tt.override == "" {
				binDir = filepath.Join(tmpDir, "bin")
				if err := os.MkdirAll(binDir, 0o755); err != nil {
					t.Fatalf("mkdir bin: %v", err)
				}
			}
			bin := filepath.Join(binDir, tt.override)
			if tt.override == "" {
				bin = filepath.Join(binDir, "whisper-cpp-streaming")
			}
			if tt.createBin {
				if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
					t.Fatalf("write bin: %v", err)
				}
			}

			if tt.override != "" {
				t.Setenv("MUESLI_WHISPER_CPP_STREAMING_BIN", bin)
			} else {
				t.Setenv("MUESLI_WHISPER_CPP_STREAMING_BIN", "")
				oldWd, err := os.Getwd()
				if err != nil {
					t.Fatalf("getwd: %v", err)
				}
				if err := os.Chdir(tmpDir); err != nil {
					t.Fatalf("chdir: %v", err)
				}
				t.Cleanup(func() { _ = os.Chdir(oldWd) })
			}

			got, err := locateWhisperCppStreamingBinary()
			if tt.wantErr {
				if err == nil {
					t.Fatal("locateWhisperCppStreamingBinary() error = nil, want non-nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("locate binary: %v", err)
			}
			if got != bin {
				t.Fatalf("binary = %q, want %q", got, bin)
			}
		})
	}
}
