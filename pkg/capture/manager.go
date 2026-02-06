package capture

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
)

type Manager struct {
	mu       sync.Mutex
	captures map[string]*exec.Cmd
}

func NewManager() *Manager {
	return &Manager{
		captures: make(map[string]*exec.Cmd),
	}
}

func (m *Manager) StartCapture(podName string, maxFiles int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.captures[podName]; exists {
		return
	}

	// Files stored in /capture directory, named as capture-<Pod name>.pcap
	// This matches task spec "/capture-<Pod name>.pcap" pattern
	captureFile := fmt.Sprintf("/capture/capture-%s.pcap", podName)
	cmd := exec.Command("tcpdump",
		"-C", "1", // Rotate at 1MB (tcpdump uses millions of bytes)
		"-W", fmt.Sprintf("%d", maxFiles), // Max files
		"-w", captureFile, // Output file
		"-i", "any", // Capture on all interfaces
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		slog.Error("Failed to start tcpdump", "error", err, "pod", podName)
		return
	}

	m.captures[podName] = cmd
	slog.Info("Capture started", "pod", podName, "pid", cmd.Process.Pid, "file", captureFile)
}

func (m *Manager) StopCapture(podName string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.stopCaptureLocked(podName)
}

func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for podName := range m.captures {
		m.stopCaptureLocked(podName)
	}
}

func (m *Manager) IsCapturing(podName string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, exists := m.captures[podName]
	return exists
}

func (m *Manager) stopCaptureLocked(podName string) {
	cmd, exists := m.captures[podName]
	if !exists {
		return
	}

	slog.Info("Stopping capture", "pod", podName, "pid", cmd.Process.Pid)

	// Send SIGTERM for graceful shutdown
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		slog.Error("Failed to signal tcpdump", "error", err)
	}
	cmd.Wait()

	delete(m.captures, podName)

	// Files stored in /capture directory
	pattern := fmt.Sprintf("/capture/capture-%s.pcap*", podName)
	files, _ := filepath.Glob(pattern)
	for _, f := range files {
		os.Remove(f)
		slog.Info("Removed capture file", "file", f)
	}
}
