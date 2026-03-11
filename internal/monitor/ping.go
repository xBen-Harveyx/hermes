package monitor

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"hermes/internal/config"
	"hermes/internal/diag"
	"hermes/internal/logx"
)

var timePattern = regexp.MustCompile(`time[=<]?\s*([0-9]+(?:\.[0-9]+)?)\s*ms`)

func RunPingMonitor(ctx context.Context, logDir string, target config.PingTarget, sessionLog *logx.LineLogger) {
	logPath := filepath.Join(logDir, fmt.Sprintf("%s.log", target.Label))
	logger, err := logx.New(logPath)
	if err != nil {
		sessionLog.Writef("unable to open ping log for %s: %v", target.Label, err)
		return
	}
	defer logger.Close()

	logger.Writef("starting ping monitor for %s (%s)", target.Label, target.Target)

	ticker := time.NewTicker(config.PingInterval)
	defer ticker.Stop()

	seq := 0
	for {
		select {
		case <-ctx.Done():
			logger.Writef("stopping ping monitor for %s", target.Label)
			return
		default:
		}

		seq++
		status, detail := pingOnce(ctx, target.Target)
		logger.Writef("seq=%d status=%s target=%s detail=%s", seq, status, target.Target, detail)

		select {
		case <-ctx.Done():
			logger.Writef("stopping ping monitor for %s", target.Label)
			return
		case <-ticker.C:
		}
	}
}

func pingOnce(parent context.Context, target string) (string, string) {
	var args []string
	switch runtime.GOOS {
	case "windows":
		args = []string{"-n", "1", "-w", "1000", target}
	default:
		args = []string{"-c", "1", "-W", "1", target}
	}

	output, err := diag.RunCommand(parent, 3*time.Second, "ping", args...)
	detail := summarizePingOutput(output)
	if detail == "" {
		detail = "no ping output"
	}

	if err != nil {
		return "FAIL", sanitize(detail)
	}

	if match := timePattern.FindStringSubmatch(output); len(match) > 1 {
		return "OK", sanitize(fmt.Sprintf("%sms %s", match[1], detail))
	}

	return "OK", sanitize(detail)
}

func summarizePingOutput(output string) string {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if trimmed == "" {
			continue
		}
		if strings.Contains(lower, "reply from") ||
			strings.Contains(lower, "bytes from") ||
			strings.Contains(lower, "timed out") ||
			strings.Contains(lower, "could not find host") ||
			strings.Contains(lower, "could not resolve") ||
			strings.Contains(lower, "destination host unreachable") ||
			strings.Contains(lower, "general failure") {
			return trimmed
		}
	}
	return strings.TrimSpace(output)
}

func sanitize(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
