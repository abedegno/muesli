package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/plugin"
	"github.com/abedegno/muesli/internal/pluginkit"
	"github.com/gorilla/websocket"
)

// TestWhisperCppStreamingConformance starts the actual binary and exercises
// its HTTP and WebSocket wire contracts. It has no opt-in gate so the normal
// server `go test ./...` CI job always runs it.
func TestWhisperCppStreamingConformance(t *testing.T) {
	binPath := filepath.Join(t.TempDir(), "whisper-cpp-streaming")
	build := exec.Command(goTool(), "build", "-o", binPath, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	cmd := exec.Command(binPath, "--token=tok", "--addr=127.0.0.1:0", "--model=tiny.en", "--model-url=")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}()

	startup := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(stdout).ReadString('\n')
		startup <- strings.TrimSpace(line)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var baseURL string
	select {
	case baseURL = <-startup:
	case <-ctx.Done():
		t.Fatalf("startup timeout: %s", stderr.String())
	}
	if !strings.HasPrefix(baseURL, "http://") {
		t.Fatalf("startup URL = %q", baseURL)
	}

	assertStreamingInfo(t, baseURL)
	assertDownloadingEvent(t, baseURL)
	assertReadyStatus(t, baseURL)
	assertInvalidStartEvent(t, baseURL)
	assertStreamingSegments(t, baseURL)

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("shutdown: %v: %s", err, stderr.String())
	}
	cmd.Process = nil
}

func assertStreamingInfo(t *testing.T, baseURL string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, baseURL+"/info", nil)
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
	if resp.StatusCode != http.StatusOK || info.Kind != model.PluginStreamingTranscriber || info.PluginAPI != 1 {
		t.Fatalf("/info status=%d body=%+v", resp.StatusCode, info)
	}
}

func assertDownloadingEvent(t *testing.T, baseURL string) {
	t.Helper()
	conn := dialStreaming(t, baseURL)
	defer conn.Close()
	start := pluginkit.StreamingStartRequest{Type: "start", SampleRate: 16_000, Channels: 1}
	if err := conn.WriteJSON(start); err != nil {
		t.Fatal(err)
	}
	event := readWireEvent(t, conn)
	if event.Type != "error" || !strings.Contains(event.Message, "downloading") {
		t.Fatalf("initial event = %+v, want downloading error", event)
	}
}

func assertReadyStatus(t *testing.T, baseURL string) {
	t.Helper()
	for attempt := 0; attempt < 100; attempt++ {
		resp, err := http.Get(baseURL + "/status")
		if err != nil {
			t.Fatal(err)
		}
		var status struct {
			Status  string `json:"status"`
			Model   string `json:"model"`
			Percent int    `json:"percent"`
		}
		err = json.NewDecoder(resp.Body).Decode(&status)
		_ = resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if status.Status == "ready" {
			if status.Model != "tiny.en" || status.Percent != 100 {
				t.Fatalf("ready status = %+v", status)
			}
			return
		}
	}
	t.Fatal("model did not reach ready status")
}

func assertInvalidStartEvent(t *testing.T, baseURL string) {
	t.Helper()
	conn := dialStreaming(t, baseURL)
	defer conn.Close()
	if err := conn.WriteJSON(pluginkit.StreamingStartRequest{Type: "start", SampleRate: 0, Channels: 1}); err != nil {
		t.Fatal(err)
	}
	event := readWireEvent(t, conn)
	if event.Type != "error" || event.Message == "" {
		t.Fatalf("invalid-start event = %+v", event)
	}
}

func assertStreamingSegments(t *testing.T, baseURL string) {
	t.Helper()
	session, err := plugin.NewStreaming(baseURL, "tok").Open(context.Background(), plugin.StreamingStartRequest{SampleRate: 16_000, Channels: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if err := session.WriteAudio(pcm16Frame(24_000, .25)); err != nil {
		t.Fatal(err)
	}
	partial, err := session.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if partial.Type != "segment" || partial.Final || partial.Text == "" || partial.T1 <= partial.T0 || partial.Speaker != nil {
		t.Fatalf("partial event = %+v", partial)
	}
	if err := session.WriteAudio(pcm16Frame(12_000, 0)); err != nil {
		t.Fatal(err)
	}
	final, err := session.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if final.Type != "segment" || !final.Final || final.Text == "" || final.T1 <= final.T0 || final.Speaker != nil {
		t.Fatalf("final event = %+v", final)
	}
	if err := session.Stop(); err != nil {
		t.Fatal(err)
	}
}

func dialStreaming(t *testing.T, baseURL string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(baseURL, "http") + "/stream"
	header := http.Header{}
	header.Set("Authorization", "Bearer tok")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

func readWireEvent(t *testing.T, conn *websocket.Conn) pluginkit.StreamingEvent {
	t.Helper()
	var event pluginkit.StreamingEvent
	if err := conn.ReadJSON(&event); err != nil {
		t.Fatal(err)
	}
	return event
}

func pcm16Frame(samples int, value float32) []byte {
	frame := make([]byte, samples*2)
	sample := int16(value * 32767)
	for offset := 0; offset < len(frame); offset += 2 {
		binary.LittleEndian.PutUint16(frame[offset:], uint16(sample))
	}
	return frame
}

func goTool() string {
	if root := runtime.GOROOT(); root != "" {
		path := filepath.Join(root, "bin", "go")
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return "go"
}
