//go:build !windows

package embedded

import (
	"crypto/rand"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestEmbeddedSigkillGroupIntegration(t *testing.T) {
	if os.Getenv("MUESLI_EMBEDDED_IT") == "" || os.Getenv("MUESLI_SERVER_BIN") == "" {
		t.Skip("set MUESLI_EMBEDDED_IT and MUESLI_SERVER_BIN to run process-group integration test")
	}

	serverBin := os.Getenv("MUESLI_SERVER_BIN")
	if info, err := os.Stat(serverBin); err != nil || info.IsDir() {
		t.Skipf("MUESLI_SERVER_BIN must point at a built muesli binary: %q", serverBin)
	}

	root := t.TempDir()
	appDataDir := filepath.Join(root, "appdata")
	masterKey := make([]byte, 32)
	if _, err := rand.Read(masterKey); err != nil {
		t.Fatalf("generate master key: %v", err)
	}

	server := exec.Command(serverBin, "--embedded")
	server.Env = append(os.Environ(),
		"DATABASE_URL=postgres://placeholder?sslmode=disable",
		"MUESLI_ADDR=127.0.0.1:0",
		"MUESLI_APPDATA="+appDataDir,
		"MUESLI_MASTER_KEY="+base64.StdEncoding.EncodeToString(masterKey),
		"MUESLI_PARENT_PID="+strconv.Itoa(os.Getpid()),
		"MUESLI_PUBLIC_URL=http://127.0.0.1:0",
		"MUESLI_STORAGE_DIR="+filepath.Join(root, "storage"),
	)
	serverLog, err := os.Create(filepath.Join(root, "server.log"))
	if err != nil {
		t.Fatalf("create server log: %v", err)
	}
	defer serverLog.Close()
	server.Stdout = serverLog
	server.Stderr = serverLog
	if err := server.Start(); err != nil {
		t.Fatalf("start embedded server: %v", err)
	}
	serverPID := server.Process.Pid

	var whisperPIDs []int
	var postgresPID int
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		whisperPIDs, err = directChildPIDs(serverPID)
		postgresPID, _ = readPostmasterPID(filepath.Join(appDataDir, "postgres", "data"))
		if err == nil && len(whisperPIDs) >= 2 && postgresPID > 0 {
			break
		}
		time.Sleep(time.Second)
	}
	childPIDs := append(append([]int(nil), whisperPIDs...), postgresPID)
	if len(whisperPIDs) < 2 || postgresPID <= 0 {
		_ = syscall.Kill(-serverPID, syscall.SIGKILL)
		_ = server.Wait()
		t.Fatalf("server %d did not expose postgres and both whisper children (pids %v)\noutput:\n%s", serverPID, childPIDs, readLog(serverLog))
	}
	postgresPGID, err := syscall.Getpgid(postgresPID)
	if err != nil {
		t.Fatalf("get Postgres process group: %v", err)
	}
	if postgresPGID == serverPID {
		t.Fatalf("Postgres pid %d unexpectedly remained in embedded server process group %d", postgresPID, serverPID)
	}

	if err := syscall.Kill(serverPID, syscall.SIGKILL); err != nil {
		_ = syscall.Kill(-serverPID, syscall.SIGKILL)
		_ = server.Wait()
		t.Fatalf("SIGKILL embedded server: %v", err)
	}
	_ = server.Wait()
	if err := syscall.Kill(-serverPID, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
		t.Fatalf("SIGKILL embedded process group: %v", err)
	}
	if err := waitForProcessesGone(5*time.Second, whisperPIDs); err != nil {
		_ = syscall.Kill(postgresPID, syscall.SIGKILL)
		t.Fatalf("whisper processes survived group sweep: %v", err)
	}
	// pg_ctl detaches the postmaster into a different process group. Mirror the
	// supervisor's additional sweep using the PID recorded by Postgres itself.
	if err := syscall.Kill(postgresPID, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
		t.Fatalf("SIGKILL embedded Postgres: %v", err)
	}

	if err := waitForProcessesGone(20*time.Second, append([]int{serverPID}, childPIDs...)); err != nil {
		_ = syscall.Kill(-serverPID, syscall.SIGKILL)
		t.Fatalf("processes survived group sweep: %v\noutput:\n%s", err, readLog(serverLog))
	}
}
