package embedded

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestParentDeathIntegration(t *testing.T) {
	if os.Getenv("MUESLI_EMBEDDED_IT") == "" || os.Getenv("MUESLI_SERVER_BIN") == "" {
		t.Skip("set MUESLI_EMBEDDED_IT and MUESLI_SERVER_BIN to run parent-death integration test")
	}

	serverBin := os.Getenv("MUESLI_SERVER_BIN")
	if info, err := os.Stat(serverBin); err != nil || info.IsDir() {
		t.Skipf("MUESLI_SERVER_BIN must point at a built muesli binary: %q", serverBin)
	}

	root := t.TempDir()
	appDataDir := filepath.Join(root, "appdata")
	serverPIDFile := filepath.Join(root, "server.pid")
	masterKey := make([]byte, 32)
	if _, err := rand.Read(masterKey); err != nil {
		t.Fatalf("generate master key: %v", err)
	}

	// Keep the shell alive as the throwaway parent while its server child runs.
	// The sleep is deliberately the shell's foreground command so killing the
	// shell reparents the server and exercises the real parent-death watch.
	parent := exec.Command("sh", "-c", `export MUESLI_PARENT_PID=$$; "$1" --embedded & echo $! > "$2"; sleep 300`, "sh", serverBin, serverPIDFile)
	parent.Env = append(os.Environ(),
		"DATABASE_URL=postgres://placeholder?sslmode=disable",
		"MUESLI_ADDR=127.0.0.1:0",
		"MUESLI_APPDATA="+appDataDir,
		"MUESLI_MASTER_KEY="+base64.StdEncoding.EncodeToString(masterKey),
		"MUESLI_PUBLIC_URL=http://127.0.0.1:0",
		"MUESLI_STORAGE_DIR="+filepath.Join(root, "storage"),
	)
	parentLog, err := os.Create(filepath.Join(root, "parent.log"))
	if err != nil {
		t.Fatalf("create parent log: %v", err)
	}
	defer parentLog.Close()
	parent.Stdout = parentLog
	parent.Stderr = parentLog
	if err := parent.Start(); err != nil {
		t.Fatalf("start throwaway parent: %v", err)
	}

	serverPID, err := waitForPIDFile(serverPIDFile, 5*time.Second)
	if err != nil {
		_ = parent.Process.Kill()
		_ = parent.Wait()
		t.Fatalf("server pid: %v\noutput:\n%s", err, readLog(parentLog))
	}

	var childPIDs []int
	var postgresPID int
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		childPIDs, err = directChildPIDs(serverPID)
		// pg_ctl daemonizes Postgres, so its final PID is no longer a direct
		// child even though the server owns and shuts down that process.
		postgresPID, _ = readPostmasterPID(filepath.Join(appDataDir, "postgres", "data"))
		if err == nil && len(childPIDs) >= 2 && postgresPID > 0 {
			childPIDs = append(childPIDs, postgresPID)
			break
		}
		time.Sleep(time.Second)
	}
	if len(childPIDs) == 0 {
		cleanupProcesses(parent, serverPID, childPIDs)
		t.Fatalf("server %d never spawned a child\noutput:\n%s", serverPID, readLog(parentLog))
	}
	if len(childPIDs) < 3 || postgresPID == 0 {
		cleanupProcesses(parent, serverPID, childPIDs)
		t.Fatalf("server %d did not expose postgres and both whisper children (pids %v)\noutput:\n%s", serverPID, childPIDs, readLog(parentLog))
	}

	if err := parent.Process.Kill(); err != nil {
		cleanupProcesses(parent, serverPID, childPIDs)
		t.Fatalf("kill throwaway parent: %v", err)
	}
	_ = parent.Wait()

	if err := waitForProcessesGone(20*time.Second, append([]int{serverPID}, childPIDs...)); err != nil {
		cleanupProcesses(nil, serverPID, childPIDs)
		t.Fatalf("processes survived parent death: %v\noutput:\n%s", err, readLog(parentLog))
	}
}

func TestParentDeathPipeSIGPIPEIntegration(t *testing.T) {
	if os.Getenv("MUESLI_EMBEDDED_IT") == "" || os.Getenv("MUESLI_SERVER_BIN") == "" {
		t.Skip("set MUESLI_EMBEDDED_IT and MUESLI_SERVER_BIN to run parent-death integration test")
	}

	serverBin := os.Getenv("MUESLI_SERVER_BIN")
	if info, err := os.Stat(serverBin); err != nil || info.IsDir() {
		t.Skipf("MUESLI_SERVER_BIN must point at a built muesli binary: %q", serverBin)
	}

	root := t.TempDir()
	appDataDir := filepath.Join(root, "appdata")
	serverPIDFile := filepath.Join(root, "server.pid")
	masterKey := make([]byte, 32)
	if _, err := rand.Read(masterKey); err != nil {
		t.Fatalf("generate master key: %v", err)
	}

	// The server inherits this pipe from its throwaway parent, matching the
	// desktop launch. Closing the read end below makes every subsequent server
	// log write encounter a broken pipe.
	pipeReader, pipeWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("create parent output pipe: %v", err)
	}
	defer pipeReader.Close()
	defer pipeWriter.Close()
	go func() {
		_, _ = io.Copy(io.Discard, pipeReader)
	}()

	parent := exec.Command("sh", "-c", `export MUESLI_PARENT_PID=$$; "$1" --embedded & echo $! > "$2"; sleep 300`, "sh", serverBin, serverPIDFile)
	parent.Env = append(os.Environ(),
		"DATABASE_URL=postgres://placeholder?sslmode=disable",
		"MUESLI_ADDR=127.0.0.1:0",
		"MUESLI_APPDATA="+appDataDir,
		"MUESLI_MASTER_KEY="+base64.StdEncoding.EncodeToString(masterKey),
		"MUESLI_PUBLIC_URL=http://127.0.0.1:0",
		"MUESLI_STORAGE_DIR="+filepath.Join(root, "storage"),
	)
	parent.Stdout = pipeWriter
	parent.Stderr = pipeWriter
	if err := parent.Start(); err != nil {
		t.Fatalf("start throwaway parent: %v", err)
	}
	// Only the throwaway parent and server should retain the write end.
	if err := pipeWriter.Close(); err != nil {
		_ = parent.Process.Kill()
		_ = parent.Wait()
		t.Fatalf("close local pipe writer: %v", err)
	}

	serverPID, err := waitForPIDFile(serverPIDFile, 5*time.Second)
	if err != nil {
		_ = parent.Process.Kill()
		_ = parent.Wait()
		t.Fatalf("server pid: %v", err)
	}

	var childPIDs []int
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		childPIDs, err = directChildPIDs(serverPID)
		if err == nil && len(childPIDs) >= 2 {
			break
		}
		time.Sleep(time.Second)
	}
	if len(childPIDs) < 2 {
		cleanupProcesses(parent, serverPID, childPIDs)
		t.Fatalf("server %d did not spawn both whisper children (pids %v)", serverPID, childPIDs)
	}

	if err := pipeReader.Close(); err != nil {
		cleanupProcesses(parent, serverPID, childPIDs)
		t.Fatalf("close pipe reader: %v", err)
	}
	if err := parent.Process.Kill(); err != nil {
		cleanupProcesses(parent, serverPID, childPIDs)
		t.Fatalf("kill throwaway parent: %v", err)
	}
	_ = parent.Wait()

	if err := waitForProcessesGone(20*time.Second, append([]int{serverPID}, childPIDs...)); err != nil {
		cleanupProcesses(nil, serverPID, childPIDs)
		t.Fatalf("server or whisper children survived parent death with broken output pipe: %v", err)
	}
}

func readLog(log *os.File) string {
	if err := log.Sync(); err != nil {
		return fmt.Sprintf("sync log: %v", err)
	}
	contents, err := os.ReadFile(log.Name())
	if err != nil {
		return fmt.Sprintf("read log: %v", err)
	}
	return string(contents)
}

func waitForPIDFile(path string, timeout time.Duration) (int, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		contents, err := os.ReadFile(path)
		if err == nil {
			pid, err := strconv.Atoi(strings.TrimSpace(string(contents)))
			if err == nil && pid > 0 {
				return pid, nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return 0, fmt.Errorf("timed out waiting for %s", path)
}

func directChildPIDs(parentPID int) ([]int, error) {
	out, err := exec.Command("pgrep", "-P", strconv.Itoa(parentPID)).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		return nil, err
	}
	fields := strings.Fields(string(out))
	pids := make([]int, 0, len(fields))
	for _, field := range fields {
		pid, err := strconv.Atoi(field)
		if err != nil {
			return nil, fmt.Errorf("parse pgrep pid %q: %w", field, err)
		}
		pids = append(pids, pid)
	}
	return pids, nil
}

func waitForProcessesGone(timeout time.Duration, pids []int) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		alive := alivePIDs(pids)
		if len(alive) == 0 {
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("pids still alive: %v", alivePIDs(pids))
}

func alivePIDs(pids []int) []int {
	alive := make([]int, 0, len(pids))
	for _, pid := range pids {
		if err := syscall.Kill(pid, 0); err == nil || err == syscall.EPERM {
			alive = append(alive, pid)
		}
	}
	return alive
}

func cleanupProcesses(parent *exec.Cmd, serverPID int, childPIDs []int) {
	if parent != nil && parent.Process != nil {
		_ = parent.Process.Kill()
		_ = parent.Wait()
	}
	for _, pid := range append([]int{serverPID}, childPIDs...) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}
