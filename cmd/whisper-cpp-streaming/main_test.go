package main

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/pluginkit"
)

func TestSelectLiveModel(t *testing.T) {
	tests := []struct {
		name, batch, explicit, want string
	}{
		{"tiny", "tiny", "", "tiny"},
		{"tiny en case insensitive", "GGML-TINY.EN", "", "GGML-TINY.EN"},
		{"base", "base", "", "base"},
		{"base en file", "ggml-base.en.bin", "", "ggml-base.en.bin"},
		{"large fallback", "large-v3", "", "tiny.en"},
		{"explicit beats reuse", "base", "custom-live", "custom-live"},
		{"explicit beats fallback", "large-v3", "tiny", "tiny"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lookup := func(string) (string, bool) { return tc.explicit, tc.explicit != "" }
			got, _ := selectLiveModel(tc.batch, lookup)
			if got != tc.want {
				t.Fatalf("selectLiveModel(%q) = %q, want %q", tc.batch, got, tc.want)
			}
		})
	}
}

func TestLoadConfigLiveOverrides(t *testing.T) {
	t.Setenv("MUESLI_WHISPER_MODEL", "large-v3")
	t.Setenv("MUESLI_WHISPER_LIVE_MODEL", "base.en")
	t.Setenv("MUESLI_WHISPER_LIVE_ADDR", "127.0.0.1:9876")
	t.Setenv("MUESLI_WHISPER_LIVE_TOKEN", "token")
	t.Setenv("MUESLI_WHISPER_LIVE_MODEL_DIR", "/tmp/live-models")
	t.Setenv("MUESLI_WHISPER_LIVE_MODEL_URL", "https://example.invalid/live.bin")
	t.Setenv("MUESLI_WHISPER_LIVE_LANGUAGE", "fr")
	oldFlags, oldArgs := flag.CommandLine, os.Args
	t.Cleanup(func() { flag.CommandLine, os.Args = oldFlags, oldArgs })
	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)
	os.Args = []string{"test"}
	pluginCfg, engineCfg := loadConfig()
	if pluginCfg.Kind != model.PluginStreamingTranscriber || pluginCfg.Addr != "127.0.0.1:9876" || pluginCfg.Token != "token" {
		t.Fatalf("plugin config = %+v", pluginCfg)
	}
	if engineCfg.Model != "base.en" || engineCfg.ModelDir != "/tmp/live-models" || engineCfg.ModelURL != "https://example.invalid/live.bin" || engineCfg.Language != "fr" {
		t.Fatalf("engine config = %+v", engineCfg)
	}
}

type infoEngine struct{}

func (infoEngine) StartStream(context.Context, pluginkit.StreamingStartRequest) (pluginkit.StreamingEngineSession, error) {
	return nil, context.Canceled
}

func TestInfoReportsStreamingKindAndPluginAPI(t *testing.T) {
	srv := httptest.NewServer(pluginkit.StreamingTranscriberHandler(pluginkit.Config{Name: "live", Version: "1", Token: "tok"}, infoEngine{}))
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/info", nil)
	req.Header.Set("Authorization", "Bearer tok")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var info pluginkit.Info
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatal(err)
	}
	if info.Kind != model.PluginStreamingTranscriber || info.PluginAPI != 1 {
		t.Fatalf("info = %+v", info)
	}
}
