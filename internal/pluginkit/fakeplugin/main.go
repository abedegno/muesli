package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/pluginkit"
)

type fakeTranscriber struct{}

func (fakeTranscriber) Transcribe(_ context.Context, pcm []float32, _ pluginkit.TranscribeRequest) (pluginkit.TranscribeResult, error) {
	_ = pcm
	return pluginkit.TranscribeResult{
		Segments: []model.Segment{
			{StartMS: 0, EndMS: 1000, Text: scriptedTranscript(), Source: "mic"},
		},
		Language:   "en",
		Model:      "fake-transcriber",
		DurationMS: 1000,
	}, nil
}

func kindFromBinaryName(path string) string {
	name := filepath.Base(path)
	if strings.HasSuffix(name, "-agent") {
		return "agent"
	}
	if strings.HasSuffix(name, "-transcriber") {
		return "transcriber"
	}
	return ""
}

func scriptedTranscript() string {
	if transcript := strings.TrimSpace(os.Getenv("MUESLI_FAKE_TRANSCRIPT")); transcript != "" {
		return transcript
	}
	return "hello from fakeplugin"
}

type fakeAgent struct{}

func (fakeAgent) Generate(_ context.Context, req pluginkit.GenerateRequest) (pluginkit.GenerateResponse, error) {
	sections := make([]model.SummarySection, 0, len(req.Template.Sections))
	for _, sec := range req.Template.Sections {
		sections = append(sections, model.SummarySection{
			Heading:         sec.Heading,
			ContentMarkdown: "Summary for " + sec.Heading + ".",
		})
	}
	return pluginkit.GenerateResponse{
		Summary: pluginkit.SummaryPayload{Sections: sections},
		Model:   "fake-agent",
	}, nil
}

func main() {
	var (
		kind    = flag.String("kind", "", "plugin kind: transcriber or agent")
		token   = flag.String("token", "fake-token", "bearer token")
		addr    = flag.String("addr", "127.0.0.1:0", "listen address")
		name    = flag.String("name", "fakeplugin", "plugin name")
		version = flag.String("version", "dev", "plugin version")
	)
	flag.Parse()
	if *kind == "" {
		*kind = kindFromBinaryName(os.Args[0])
	}

	if *kind != "transcriber" && *kind != "agent" {
		fmt.Fprintln(os.Stderr, "--kind must be transcriber or agent")
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := pluginkit.Config{
		Name:         *name,
		Version:      *version,
		Kind:         *kind,
		Token:        *token,
		Addr:         *addr,
		ConfigSchema: json.RawMessage(`{}`),
	}

	var (
		srv *http.Server
		ln  net.Listener
		err error
	)
	if *kind == "transcriber" {
		srv, ln, err = pluginkit.NewServer(cfg, pluginkit.TranscriberHandler(cfg, fakeTranscriber{}))
	} else {
		srv, ln, err = pluginkit.NewServer(cfg, pluginkit.AgentHandler(cfg, fakeAgent{}))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Printf("http://%s\n", ln.Addr().String())

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
