//go:build !windows

package main

import (
	"os/exec"
	"path/filepath"
	"runtime"
)

// revealInFileManager opens the folder of path in the file manager, with the
// file selected where the file manager can be told to: Finder can, the
// file managers that xdg-open starts on Linux cannot.
func revealInFileManager(path string) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("open", "-R", path)
	} else {
		cmd = exec.Command("xdg-open", filepath.Dir(path))
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
