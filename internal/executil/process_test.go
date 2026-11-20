package executil

import (
	"context"
	"testing"
)

func TestCommand(t *testing.T) {
	t.Parallel()
	command := Command("example", "--version")
	if command.Path == "" {
		t.Fatal("Command() returned an empty executable path")
	}
	if len(command.Args) != 2 || command.Args[1] != "--version" {
		t.Fatalf("Command() args = %#v", command.Args)
	}
}

func TestCommandContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	command := CommandContext(ctx, "example", "--version")
	if command.Path == "" {
		t.Fatal("CommandContext() returned an empty executable path")
	}
	if command.Args[0] != "example" {
		t.Fatalf("CommandContext() args = %#v", command.Args)
	}
}
