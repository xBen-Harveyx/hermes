package diag

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"
)

func RunCommand(parent context.Context, timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := strings.TrimSpace(stdout.String())
	errText := strings.TrimSpace(stderr.String())
	if errText != "" {
		if output != "" {
			output += "\n"
		}
		output += errText
	}
	if ctx.Err() == context.DeadlineExceeded {
		if output != "" {
			output += "\n"
		}
		output += "command timed out"
	}

	return output, err
}
