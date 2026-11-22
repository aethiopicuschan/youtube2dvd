//go:build darwin

package main

import (
	"bytes"
	stdcontext "context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// runDrutil runs drutil with a terminal of its own. drutil shows its
// progress only when it has a terminal to measure, which an app started from
// the Finder does not give it.
func runDrutil(ctx stdcontext.Context, args []string, progress dvdExportProgressReporter) error {
	terminal, device, err := openPseudoTerminal()
	if err != nil {
		return fmt.Errorf("DVD書き込みを開始できません: %w", err)
	}
	defer terminal.Close()

	cmd := exec.CommandContext(ctx, "drutil", args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = device, device, device
	// The terminal becomes the controlling terminal of drutil: that is the
	// one drutil opens as /dev/tty.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	err = cmd.Start()
	_ = device.Close()
	if err != nil {
		return fmt.Errorf("DVD書き込みを開始できません: %w", err)
	}

	output := &drutilOutput{onUpdate: progress}
	buffer := make([]byte, 4096)
	for {
		n, readErr := terminal.Read(buffer)
		if n > 0 {
			_, _ = output.Write(buffer[:n])
		}
		if readErr != nil {
			// The read ends when drutil has closed the terminal.
			break
		}
	}
	waitErr := cmd.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	if waitErr != nil {
		message := output.failureMessage()
		if message == "" {
			message = waitErr.Error()
		}
		return fmt.Errorf("%s", message)
	}
	if progress != nil {
		progress(dvdExportProgressUpdate{hasPercent: true, progressPercent: 99})
	}
	return nil
}

// openPseudoTerminal returns both ends of a new pseudo-terminal, 80 columns
// wide: the end to read the program's output from, and the device to give to
// the program.
func openPseudoTerminal() (terminal, device *os.File, err error) {
	terminal, err = os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if err != nil {
			_ = terminal.Close()
		}
	}()
	fd := int(terminal.Fd())
	if err := unix.IoctlSetInt(fd, unix.TIOCPTYGRANT, 0); err != nil {
		return nil, nil, err
	}
	if err := unix.IoctlSetInt(fd, unix.TIOCPTYUNLK, 0); err != nil {
		return nil, nil, err
	}
	var name [128]byte
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(unix.TIOCPTYGNAME), uintptr(unsafe.Pointer(&name[0]))); errno != 0 {
		return nil, nil, errno
	}
	end := bytes.IndexByte(name[:], 0)
	if end <= 0 {
		return nil, nil, fmt.Errorf("pseudo-terminal has no device name")
	}
	device, err = os.OpenFile(string(name[:end]), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, err
	}
	if err := unix.IoctlSetWinsize(int(device.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 24, Col: 80}); err != nil {
		_ = device.Close()
		return nil, nil, err
	}
	return terminal, device, nil
}
