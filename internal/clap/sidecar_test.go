package clap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSidecarStopNil(t *testing.T) {
	var s *SidecarProcess
	s.Stop()
}

func TestSidecarStopEmpty(t *testing.T) {
	s := &SidecarProcess{}
	s.Stop()
}

func TestEnsureSidecarMissingScript(t *testing.T) {
	tmpDir := t.TempDir()
	_, _, err := EnsureSidecar("127.0.0.1", 59998, tmpDir, tmpDir, func(string) {})
	if err == nil {
		t.Fatal("expected error for missing server.py, got nil")
	}
	if !strings.Contains(err.Error(), "sidecar script not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureSidecarNoPython(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "server.py"), []byte("pass"), 0o644); err != nil {
		t.Fatal(err)
	}

	origPath := os.Getenv("PATH")
	os.Setenv("PATH", "")
	defer os.Setenv("PATH", origPath)

	_, _, err := EnsureSidecar("127.0.0.1", 59998, tmpDir, tmpDir, func(string) {})
	if err == nil {
		t.Fatal("expected error for missing python, got nil")
	}
	if !strings.Contains(err.Error(), "no python interpreter") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureSidecarStatusCallback(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "server.py"), []byte("pass"), 0o644); err != nil {
		t.Fatal(err)
	}
	if findPython(tmpDir) == "" {
		t.Skip("no python interpreter available")
	}
	var messages []string
	_, _, _ = EnsureSidecar("127.0.0.1", 59998, tmpDir, tmpDir, func(msg string) {
		messages = append(messages, msg)
	})
	if len(messages) == 0 {
		t.Fatal("expected at least one status message")
	}
	if !strings.Contains(messages[0], "Starting CLAP sidecar") {
		t.Fatalf("expected starting message, got: %s", messages[0])
	}
}
