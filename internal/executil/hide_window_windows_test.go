//go:build windows

package executil

import (
	"testing"
)

func TestHideConsoleWindow(t *testing.T) {
	t.Parallel()
	command := Command("quarto.exe", "--version")
	if command.SysProcAttr == nil {
		t.Fatal("SysProcAttr was not configured")
	}
	if !command.SysProcAttr.HideWindow {
		t.Fatal("HideWindow = false, want true")
	}
	if command.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Fatalf("CreationFlags = %#x, want CREATE_NO_WINDOW", command.SysProcAttr.CreationFlags)
	}
}
