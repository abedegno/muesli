package main

import (
	"flag"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/abedegno/muesli/cmd/whisper-cpp-transcriber/internal/engine"
)

func TestLoadConfigUsesEnvAndFlags(t *testing.T) {
	t.Setenv("MUESLI_WHISPER_ADDR", "127.0.0.1:9999")
	t.Setenv("MUESLI_WHISPER_TOKEN", "env-token")
	t.Setenv("MUESLI_WHISPER_NAME", "env-name")
	t.Setenv("MUESLI_WHISPER_VERSION", "1.2.3")
	t.Setenv("MUESLI_WHISPER_MODEL_DIR", "/tmp/env-models")
	t.Setenv("MUESLI_WHISPER_MODEL_URL", "https://example.invalid/model.bin")
	t.Setenv("MUESLI_WHISPER_MODEL", "env-model")
	t.Setenv("MUESLI_WHISPER_LANGUAGE", "fr")

	oldFlagSet := flag.CommandLine
	oldArgs := os.Args
	t.Cleanup(func() {
		flag.CommandLine = oldFlagSet
		os.Args = oldArgs
	})

	flag.CommandLine = flag.NewFlagSet(filepath.Base(oldArgs[0]), flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)
	os.Args = []string{
		filepath.Base(oldArgs[0]),
		"-addr=127.0.0.1:1234",
		"-token=flag-token",
		"-name=flag-name",
		"-version=9.9.9",
		"-model-dir=/tmp/flag-models",
		"-model-url=http://127.0.0.1/model.bin",
		"-model=flag-model",
		"-language=es",
	}

	plugCfg, engCfg := loadConfig()
	if plugCfg.Kind != "transcriber" {
		t.Fatalf("kind = %q, want transcriber", plugCfg.Kind)
	}
	if plugCfg.Addr != "127.0.0.1:1234" || plugCfg.Token != "flag-token" || plugCfg.Name != "flag-name" {
		t.Fatalf("plugCfg = %+v", plugCfg)
	}
	if plugCfg.Version != "9.9.9" || plugCfg.ModelDir != "/tmp/flag-models" {
		t.Fatalf("plugCfg = %+v", plugCfg)
	}
	if engCfg.ModelDir != "/tmp/flag-models" || engCfg.ModelURL != "http://127.0.0.1/model.bin" {
		t.Fatalf("engCfg = %+v", engCfg)
	}
	if engCfg.Model != "flag-model" || engCfg.Language != "es" {
		t.Fatalf("engCfg = %+v", engCfg)
	}
	if string(plugCfg.ConfigSchema) != string(engine.ConfigSchema) {
		t.Fatalf("config schema mismatch: %s", plugCfg.ConfigSchema)
	}
}
