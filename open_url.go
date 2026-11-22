package main

import (
	"os/exec"
	"runtime"
)

// openURL opens url in the user's browser.
func openURL(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	hideConsoleWindow(cmd)
	_ = cmd.Start()
}
