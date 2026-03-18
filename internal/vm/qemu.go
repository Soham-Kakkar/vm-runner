package vm

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"sync"

	"vm-runner/internal/storage"
)

// QEMUManager is responsible for managing a single QEMU VM instance.
type QEMUManager struct {
	Config      storage.VMConfig
	Cmd         *exec.Cmd
	Stdin       io.WriteCloser
	Stdout      io.ReadCloser
	Stderr      io.ReadCloser
	OutputChan  chan string
	ErrorChan   chan error
	stopChan    chan struct{}
	wg          sync.WaitGroup
}

// NewQEMUManager creates a new manager for a QEMU VM.
func NewQEMUManager(config storage.VMConfig) *QEMUManager {
	return &QEMUManager{
		Config:     config,
		OutputChan: make(chan string, 100),
		ErrorChan:  make(chan error, 1),
		stopChan:   make(chan struct{}),
	}
}

// Start launches the QEMU VM.
func (qm *QEMUManager) Start() error {
	args := []string{
		"-m", fmt.Sprintf("%d", qm.Config.MemoryMB),
		"-smp", "2", "-cpu", "host", "-enable-kvm", "-nographic",
	}
	if qm.Config.ImagePath != "" {
		args = append(args, "-hda", qm.Config.ImagePath)
	}

	qm.Cmd = exec.Command("qemu-system-x86_64", args...)

	var err error
	qm.Stdin, err = qm.Cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdin pipe: %w", err)
	}
	qm.Stdout, err = qm.Cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout pipe: %w", err)
	}
	qm.Stderr, err = qm.Cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	if err := qm.Cmd.Start(); err != nil {
		return fmt.Errorf("failed to start qemu process: %w", err)
	}

	qm.wg.Add(2)
	go qm.streamOutput(qm.Stdout)
	go qm.streamOutput(qm.Stderr)

	go func() {
		qm.wg.Wait()
		close(qm.OutputChan)
		if err := qm.Cmd.Wait(); err != nil {
			qm.ErrorChan <- fmt.Errorf("vm process exited with error: %w", err)
		}
		close(qm.ErrorChan)
	}()

	return nil
}

// Stop terminates the QEMU VM.
func (qm *QEMUManager) Stop() error {
	close(qm.stopChan)
	if qm.Cmd.Process != nil {
		return qm.Cmd.Process.Kill()
	}
	return nil
}

// SendInput sends a string to the VM's standard input.
func (qm *QEMUManager) SendInput(input string) (int, error) {
	return qm.Stdin.Write([]byte(input))
}

// streamOutput reads from a reader and sends lines to the output channel.
func (qm *QEMUManager) streamOutput(reader io.Reader) {
	defer qm.wg.Done()
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		select {
		case <-qm.stopChan:
			return
		case qm.OutputChan <- scanner.Text():
		}
	}
}
