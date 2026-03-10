package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const shellTimeout = 30 * time.Second
const maxOutputBytes = 8192

// Shell runs an arbitrary shell command and returns combined stdout+stderr.
type Shell struct{}

func (Shell) Name() string { return "shell" }
func (Shell) Description() string {
	return "Run a shell command and return its output. Input is the command string."
}

func (Shell) Run(input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", fmt.Errorf("no command provided")
	}

	ctx, cancel := context.WithTimeout(context.Background(), shellTimeout)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", input)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", input)
	}

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	_ = cmd.Run() // we return output regardless of exit code

	out := buf.String()
	if len(out) > maxOutputBytes {
		out = out[:maxOutputBytes] + "\n[output truncated]"
	}
	if out == "" {
		return "(no output)", nil
	}
	return out, nil
}
