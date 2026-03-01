package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	runDuration       = 30 * time.Minute
	pingInterval      = 1 * time.Second
	speedtestInterval = 5 * time.Minute
	speedtestTarget   = "https://github.com/asheroto/speedtest"
)

var timePattern = regexp.MustCompile(`time[=<]?\s*([0-9]+(?:\.[0-9]+)?)\s*ms`)

type pingTarget struct {
	Label  string
	Target string
}

type sessionInfo struct {
	LogDir       string
	SessionLog   string
	Gateway      string
	FirstHop     string
	Targets      []pingTarget
	ResolvedIPs  map[string]string
	StartedAt    time.Time
	StopsAt      time.Time
	SpeedtestLog string
}

type lineLogger struct {
	mu   sync.Mutex
	file *os.File
}

func main() {
	startedAt := time.Now()
	stopsAt := startedAt.Add(runDuration)

	logDir, err := createLogDir(startedAt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create log directory: %v\n", err)
		os.Exit(1)
	}

	sessionLog, err := newLineLogger(filepath.Join(logDir, "session.log"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open session log: %v\n", err)
		os.Exit(1)
	}
	defer sessionLog.Close()

	gateway, err := detectGateway()
	if err != nil {
		sessionLog.Writef("startup failure: unable to detect gateway: %v", err)
		fmt.Fprintf(os.Stderr, "unable to detect gateway: %v\n", err)
		os.Exit(1)
	}

	firstHop, err := detectFirstHop()
	if err != nil {
		sessionLog.Writef("startup failure: unable to detect first hop: %v", err)
		fmt.Fprintf(os.Stderr, "unable to detect first hop: %v\n", err)
		os.Exit(1)
	}

	targets := []pingTarget{
		{Label: "gateway", Target: gateway},
		{Label: "first-hop", Target: firstHop},
		{Label: "google", Target: "google.com"},
		{Label: "atnetplus", Target: "atnetplus.com"},
		{Label: "cloudflare", Target: "cloudflare.com"},
	}

	resolvedIPs := make(map[string]string, len(targets))
	for _, target := range targets {
		resolvedIPs[target.Label] = resolveTarget(target.Target)
	}

	info := sessionInfo{
		LogDir:       logDir,
		SessionLog:   filepath.Join(logDir, "session.log"),
		Gateway:      gateway,
		FirstHop:     firstHop,
		Targets:      targets,
		ResolvedIPs:  resolvedIPs,
		StartedAt:    startedAt,
		StopsAt:      stopsAt,
		SpeedtestLog: filepath.Join(logDir, "speedtest.log"),
	}

	summary := buildSummary(info)
	fmt.Println(summary)
	sessionLog.Write(summary)

	ctx, cancel := context.WithDeadline(context.Background(), stopsAt)
	defer cancel()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)

	go func() {
		sig := <-signals
		sessionLog.Writef("received signal %s, stopping early", sig.String())
		cancel()
	}()

	var wg sync.WaitGroup

	for _, target := range targets {
		target := target
		wg.Add(1)
		go func() {
			defer wg.Done()
			runPingMonitor(ctx, logDir, target, sessionLog)
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		runSpeedtestLoop(ctx, info.SpeedtestLog, sessionLog)
	}()

	<-ctx.Done()
	wg.Wait()
	sessionLog.Writef("session finished: %v", context.Cause(ctx))
}

func createLogDir(startedAt time.Time) (string, error) {
	dir := filepath.Join("logs", startedAt.Format("20060102-150405"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func newLineLogger(path string) (*lineLogger, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &lineLogger{file: file}, nil
}

func (l *lineLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.file.Close()
}

func (l *lineLogger) Write(text string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range splitLogLines(text) {
		_, _ = l.file.WriteString(fmt.Sprintf("%s %s\n", time.Now().Format("2006-01-02 15:04:05"), line))
	}
}

func (l *lineLogger) Writef(format string, args ...any) {
	l.Write(fmt.Sprintf(format, args...))
}

func buildSummary(info sessionInfo) string {
	var builder strings.Builder

	builder.WriteString("Hermes network monitor\n")
	builder.WriteString(fmt.Sprintf("Started: %s\n", info.StartedAt.Format(time.RFC1123)))
	builder.WriteString(fmt.Sprintf("Auto-stop: %s\n", info.StopsAt.Format(time.RFC1123)))
	builder.WriteString("Stop early: Ctrl+C\n")
	builder.WriteString(fmt.Sprintf("Log directory: %s\n", info.LogDir))
	builder.WriteString(fmt.Sprintf("Session log: %s\n", info.SessionLog))
	builder.WriteString(fmt.Sprintf("Gateway: %s\n", info.Gateway))
	builder.WriteString(fmt.Sprintf("First hop: %s\n", info.FirstHop))
	builder.WriteString("Targets:\n")
	for _, target := range info.Targets {
		builder.WriteString(fmt.Sprintf("  - %s => %s", target.Label, target.Target))
		if ip := info.ResolvedIPs[target.Label]; ip != "" {
			builder.WriteString(fmt.Sprintf(" (%s)", ip))
		}
		builder.WriteString("\n")
	}
	builder.WriteString(fmt.Sprintf("Speed test: every %s using PowerShell on Windows (%s)\n", speedtestInterval, speedtestTarget))
	return builder.String()
}

func resolveTarget(target string) string {
	if ip := net.ParseIP(target); ip != nil {
		return ip.String()
	}

	ips, err := net.LookupIP(target)
	if err != nil || len(ips) == 0 {
		return ""
	}

	for _, ip := range ips {
		if ipv4 := ip.To4(); ipv4 != nil {
			return ipv4.String()
		}
	}
	return ips[0].String()
}

func detectGateway() (string, error) {
	switch runtime.GOOS {
	case "windows":
		output, err := runCommand(context.Background(), 5*time.Second, "route", "print", "-4")
		if err != nil {
			return "", err
		}
		for _, line := range strings.Split(output, "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 4 && fields[0] == "0.0.0.0" && fields[1] == "0.0.0.0" {
				return fields[2], nil
			}
		}
	case "linux":
		output, err := runCommand(context.Background(), 5*time.Second, "ip", "route", "show", "default")
		if err != nil {
			return "", err
		}
		for _, line := range strings.Split(output, "\n") {
			fields := strings.Fields(line)
			for idx := range fields {
				if fields[idx] == "via" && idx+1 < len(fields) {
					return fields[idx+1], nil
				}
			}
		}
	case "darwin":
		output, err := runCommand(context.Background(), 5*time.Second, "route", "-n", "get", "default")
		if err != nil {
			return "", err
		}
		for _, line := range strings.Split(output, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "gateway:") {
				return strings.TrimSpace(strings.TrimPrefix(line, "gateway:")), nil
			}
		}
	}

	return "", fmt.Errorf("unsupported platform or gateway not found on %s", runtime.GOOS)
}

func detectFirstHop() (string, error) {
	switch runtime.GOOS {
	case "windows":
		output, err := runCommand(context.Background(), 15*time.Second, "tracert", "-d", "-h", "2", "-w", "1000", "1.1.1.1")
		if err != nil {
			return "", err
		}
		return parseSecondTraceHop(output)
	case "linux":
		output, err := runCommand(context.Background(), 15*time.Second, "traceroute", "-n", "-m", "2", "-w", "1", "1.1.1.1")
		if err != nil {
			return "", err
		}
		return parseSecondTraceHop(output)
	case "darwin":
		output, err := runCommand(context.Background(), 15*time.Second, "traceroute", "-n", "-m", "2", "-w", "1000", "1.1.1.1")
		if err != nil {
			return "", err
		}
		return parseSecondTraceHop(output)
	default:
		return "", fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

func parseSecondTraceHop(output string) (string, error) {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 || fields[0] != "2" {
			continue
		}
		for _, field := range fields[1:] {
			candidate := strings.Trim(field, "[]()")
			if ip := net.ParseIP(candidate); ip != nil {
				return ip.String(), nil
			}
		}
	}
	return "", errors.New("second traceroute hop not found")
}

func runPingMonitor(ctx context.Context, logDir string, target pingTarget, sessionLog *lineLogger) {
	logPath := filepath.Join(logDir, fmt.Sprintf("%s.log", target.Label))
	logger, err := newLineLogger(logPath)
	if err != nil {
		sessionLog.Writef("unable to open ping log for %s: %v", target.Label, err)
		return
	}
	defer logger.Close()

	logger.Writef("starting ping monitor for %s (%s)", target.Label, target.Target)

	ticker := time.NewTicker(pingInterval)
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

	output, err := runCommand(parent, 3*time.Second, "ping", args...)
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

func splitLogLines(text string) []string {
	trimmed := strings.TrimRight(text, "\n")
	if trimmed == "" {
		return []string{""}
	}
	return strings.Split(trimmed, "\n")
}

func runSpeedtestLoop(ctx context.Context, logPath string, sessionLog *lineLogger) {
	logger, err := newLineLogger(logPath)
	if err != nil {
		sessionLog.Writef("unable to open speed test log: %v", err)
		return
	}
	defer logger.Close()

	logger.Writef("starting speed test loop")

	ticker := time.NewTicker(speedtestInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Writef("stopping speed test loop")
			return
		case <-ticker.C:
			runSpeedtestOnce(ctx, logger)
		}
	}
}

func runSpeedtestOnce(parent context.Context, logger *lineLogger) {
	logger.Writef("speed test begin")

	if runtime.GOOS != "windows" {
		logger.Writef("speed test skipped: PowerShell command is only enabled on Windows")
		return
	}

	output, err := runCommand(parent, 4*time.Minute, "powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", "irm asheroto.com/speedtest | iex")
	if err != nil {
		logger.Writef("speed test failed: %v", err)
	}

	logger.Writef("speed test output start")
	logger.Write(output)
	logger.Writef("speed test output end")
}

func runCommand(parent context.Context, timeout time.Duration, name string, args ...string) (string, error) {
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
