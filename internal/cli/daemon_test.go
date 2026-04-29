package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/esignoretti/ds3backup/internal/api"
)

// TestDaemonPIDFile_CreateAndRemove verifies PID file creation, reading, and
// removal with proper permissions.

func TestDaemonPIDFile_CreateAndRemove(t *testing.T) {
	// Use a temp directory instead of real ~/.ds3backup
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "daemon.pid")

	// Write PID file manually with 0600 permissions
	pid := "12345\n"
	if err := os.WriteFile(pidPath, []byte(pid), 0600); err != nil {
		t.Fatalf("failed to write PID file: %v", err)
	}

	// Verify it's readable
	data, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("failed to read PID file: %v", err)
	}
	if string(data) != pid {
		t.Errorf("expected PID content %q, got %q", pid, string(data))
	}

	// Verify permissions
	info, err := os.Stat(pidPath)
	if err != nil {
		t.Fatalf("failed to stat PID file: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("expected 0600 permissions, got %o", perm)
	}

	// Remove PID file
	if err := os.Remove(pidPath); err != nil {
		t.Fatalf("failed to remove PID file: %v", err)
	}

	// Verify it's gone
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Errorf("expected PID file to be removed, got: %v", err)
	}
}

// TestDaemonAdapter_RunnerInterface verifies daemonRunnerAdapter implements
// api.BackupRunner via compile-time type assertion.

func TestDaemonAdapter_RunnerInterface(t *testing.T) {
	// Compile-time check that daemonRunnerAdapter satisfies api.BackupRunner
	var _ api.BackupRunner = (*daemonRunnerAdapter)(nil)
}

// TestDaemonAdapter_JobManagerInterface verifies daemonJobManagerAdapter
// implements api.JobManager via compile-time type assertion.

func TestDaemonAdapter_JobManagerInterface(t *testing.T) {
	// Compile-time check that daemonJobManagerAdapter satisfies api.JobManager
	var _ api.JobManager = (*daemonJobManagerAdapter)(nil)
}

// TestDaemonRunCommand_NoAPIFlag verifies the --no-api flag is parsed correctly.
// We test this by verifying the flag exists on the command and has the correct
// default value.

func TestDaemonRunCommand_NoAPIFlag(t *testing.T) {
	// Check that the --no-api flag exists
	flag := daemonRunCmd.Flags().Lookup("no-api")
	if flag == nil {
		t.Fatal("expected --no-api flag to exist on daemon run command")
	}
	if flag.DefValue != "false" {
		t.Errorf("expected default value false, got %s", flag.DefValue)
	}

	// Verify --no-tray flag exists
	flag = daemonRunCmd.Flags().Lookup("no-tray")
	if flag == nil {
		t.Fatal("expected --no-tray flag to exist on daemon run command")
	}
	if flag.DefValue != "false" {
		t.Errorf("expected default value false, got %s", flag.DefValue)
	}

	// Verify --port flag exists
	flag = daemonRunCmd.Flags().Lookup("port")
	if flag == nil {
		t.Fatal("expected --port flag to exist on daemon run command")
	}
}

// TestDaemonStopCmd_PortFlag verifies the --port flag exists on daemon stop.

func TestDaemonStopCmd_PortFlag(t *testing.T) {
	flag := daemonStopCmd.Flags().Lookup("port")
	if flag == nil {
		t.Fatal("expected --port flag to exist on daemon stop command")
	}
}

// TestDaemonStatusCmd_PortFlag verifies the --port flag exists on daemon status.

func TestDaemonStatusCmd_PortFlag(t *testing.T) {
	flag := daemonStatusCmd.Flags().Lookup("port")
	if flag == nil {
		t.Fatal("expected --port flag to exist on daemon status command")
	}
}
