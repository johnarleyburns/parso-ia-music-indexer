package clap

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
)

type SidecarProcess struct {
	cmd     *exec.Cmd
	logFile *os.File
	exited  chan error
}

func EnsureSidecar(host string, port int, sidecarDir, logDir string, statusFn func(string)) (*SidecarProcess, CLAPClient, error) {
	var err error
	sidecarDir, err = filepath.Abs(sidecarDir)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve sidecar dir: %w", err)
	}
	logDir, err = filepath.Abs(logDir)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve log dir: %w", err)
	}

	statusFn(fmt.Sprintf("Checking CLAP sidecar on %s:%d...", host, port))

	client, err := NewGRPCClient(host, port)
	if err == nil {
		statusFn("CLAP sidecar already running")
		return nil, client, nil
	}

	serverScript := filepath.Join(sidecarDir, "server.py")
	if _, err := os.Stat(serverScript); err != nil {
		return nil, nil, fmt.Errorf("sidecar script not found at %s: %w", serverScript, err)
	}

	pythonBin := findPython(sidecarDir)
	if pythonBin == "" {
		return nil, nil, fmt.Errorf("no python interpreter found; create a venv at %s/.venv or install python3", sidecarDir)
	}

	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("create log dir %s: %w", logDir, err)
	}
	logFile, err := os.OpenFile(filepath.Join(logDir, "clap-sidecar.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("create sidecar log: %w", err)
	}

	statusFn(fmt.Sprintf("Starting CLAP sidecar (%s server.py)...", pythonBin))

	cmd := exec.Command(pythonBin, "server.py")
	cmd.Dir = sidecarDir
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return nil, nil, fmt.Errorf("start CLAP sidecar: %w", err)
	}

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	statusFn("Waiting for CLAP model to load (this may take a minute on first run)...")

	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	deadline := time.Now().Add(120 * time.Second)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		select {
		case waitErr := <-exited:
			logFile.Close()
			return nil, nil, fmt.Errorf("CLAP sidecar exited prematurely: %v (check %s/clap-sidecar.log)", waitErr, logDir)
		case <-ticker.C:
			conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
			if err == nil {
				conn.Close()
				statusFn("CLAP sidecar port open, connecting gRPC client...")
				c, err := NewGRPCClient(host, port)
				if err == nil {
					statusFn(fmt.Sprintf("CLAP sidecar connected on %s:%d", host, port))
					return &SidecarProcess{cmd: cmd, logFile: logFile, exited: exited}, c, nil
				}
				statusFn(fmt.Sprintf("gRPC probe failed (retrying): %v", err))
			}
		}
	}

	cmd.Process.Kill()
	logFile.Close()
	return nil, nil, fmt.Errorf("CLAP sidecar failed to become ready within 120s (check %s/clap-sidecar.log)", logDir)
}

func (s *SidecarProcess) Stop() {
	if s == nil || s.cmd == nil || s.cmd.Process == nil {
		return
	}

	s.cmd.Process.Signal(syscall.SIGTERM)

	select {
	case <-s.exited:
	case <-time.After(5 * time.Second):
		s.cmd.Process.Kill()
		<-s.exited
	}

	if s.logFile != nil {
		s.logFile.Close()
	}
}

func findPython(sidecarDir string) string {
	venvBin := "bin"
	if runtime.GOOS == "windows" {
		venvBin = "Scripts"
	}
	venvPython := filepath.Join(sidecarDir, ".venv", venvBin, "python")
	if _, err := os.Stat(venvPython); err == nil {
		return venvPython
	}

	if p, err := exec.LookPath("python3"); err == nil {
		return p
	}
	if p, err := exec.LookPath("python"); err == nil {
		return p
	}
	return ""
}
