package vm

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"

	"vm-runner/internal/storage"
)

// QEMUManager is responsible for managing a single QEMU VM instance.
type QEMUManager struct {
	Config     storage.VMConfig
	Cmd        *exec.Cmd
	PTY        *os.File
	OutputChan chan []byte
	ErrorChan  chan error
	stopChan   chan struct{}
	wg         sync.WaitGroup
	VNCPort    int
}

// NewQEMUManager creates a new manager for a QEMU VM.
func NewQEMUManager(config storage.VMConfig) *QEMUManager {
	return &QEMUManager{
		Config:     config,
		OutputChan: make(chan []byte, 1024),
		ErrorChan:  make(chan error, 1),
		stopChan:   make(chan struct{}),
	}
}

// Start launches the QEMU VM.
func (qm *QEMUManager) Start() error {

	commonArgs := []string{
		"-m", fmt.Sprintf("%d", qm.Config.MemoryMB),
		"-smp", "2", "-cpu", "host", "-enable-kvm",
	}

	if qm.Config.DisplayType == "vnc" {
		commonArgs = append(commonArgs,
			"-vnc", ":1,websocket=5701",
			"-vga", "std",
			"-serial", "mon:stdio",
			"-usb",
			"-device", "usb-tablet", // Absolute pointer for perfect mouse sync
			"-device", "virtio-serial-pci",
			"-device", "virtserialport,chardev=ch0,name=com.redhat.spice.0",
			"-chardev", "qemu-vdagent,id=ch0,name=vdagent,clipboard=on",
		)
	} else {
		commonArgs = append(commonArgs, "-nographic")
	}

	if qm.Config.ImageFormat == "none" {
		args := append(commonArgs,
			"-drive", "if=pflash,format=raw,file=/home/soham/Documents/projects/hobbyOS/bootloader/uefi/OVMF/OVMF_CODE.4m.fd,readonly=on",
			"-drive", "if=pflash,format=raw,file=/home/soham/Documents/projects/hobbyOS/bootloader/uefi/OVMF/OVMF_VARS.4m.fd",
			"-drive", "format=raw,file=fat:rw:/home/soham/Documents/projects/hobbyOS/bootloader/uefi/diskimg",
			"-machine", "q35",
			"-net", "none",
		)
		qm.Cmd = exec.Command("qemu-system-x86_64", args...)
	} else {
		args := commonArgs
		if qm.Config.ImagePath != "" {
			args = append(args, "-hda", qm.Config.ImagePath)
		}

		qm.Cmd = exec.Command("qemu-system-x86_64", args...)
	}
	// Create a pty for the QEMU process so the guest sees a real tty.
	ptmx, err := pty.Start(qm.Cmd)
	if err != nil {
		return fmt.Errorf("failed to start qemu with pty: %w", err)
	}
	qm.PTY = ptmx

	if qm.Config.DisplayType == "vnc" {
		// Give QEMU a moment to initialize the VNC/WebSocket server
		time.Sleep(500 * time.Millisecond)
	}

	qm.wg.Add(1)
	go qm.streamOutput(qm.PTY)

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
	if qm.PTY != nil {
		_ = qm.PTY.Close()
	}
	if qm.Cmd.Process != nil {
		return qm.Cmd.Process.Kill()
	}
	return nil
}

// SendInput sends a string to the VM's standard input.
func (qm *QEMUManager) SendInput(input string) (int, error) {
	if qm.PTY == nil {
		return 0, fmt.Errorf("pty not initialized")
	}
	// Map DEL (0x7f) to BS (0x08) which many guest ttys expect for backspace.
	b := []byte(input)
	for i := range b {
		if b[i] == 0x7f {
			b[i] = 0x08
		}
	}
	return qm.PTY.Write(b)
}

// streamOutput reads from a reader and sends lines to the output channel.
func (qm *QEMUManager) streamOutput(reader io.Reader) {
	defer qm.wg.Done()
	buf := make([]byte, 4096)
	for {
		select {
		case <-qm.stopChan:
			return
		default:
		}
		n, err := reader.Read(buf)
		if n > 0 {
			// copy bytes because buf is reused
			out := make([]byte, n)
			copy(out, buf[:n])
			select {
			case qm.OutputChan <- out:
			case <-qm.stopChan:
				return
			}
		}
		if err != nil {
			if err == io.EOF {
				return
			}
			qm.ErrorChan <- err
			return
		}
	}
}
