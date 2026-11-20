package executil

import (
	"context"
	"os/exec"
)

// Command creates a command without opening a console window on Windows.
func Command(name string, arg ...string) *exec.Cmd {
	command := exec.Command(name, arg...)
	hideConsoleWindow(command)
	return command
}

// CommandContext creates a context-aware command without opening a console
// window on Windows.
func CommandContext(ctx context.Context, name string, arg ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, name, arg...)
	hideConsoleWindow(command)
	return command
}
