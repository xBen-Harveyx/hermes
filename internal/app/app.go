package app

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"hermes/internal/config"
	"hermes/internal/diag"
	"hermes/internal/logx"
	"hermes/internal/monitor"
)

type sessionInfo struct {
	LogDir       string
	SessionLog   string
	Gateway      string
	FirstHop     string
	Targets      []config.PingTarget
	ResolvedIPs  map[string]string
	StartedAt    time.Time
	StopsAt      time.Time
	SpeedtestLog string
}

func Run() error {
	startedAt := time.Now()
	stopsAt := startedAt.Add(config.RunDuration)

	logDir, err := createLogDir(startedAt)
	if err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	sessionLog, err := logx.New(filepath.Join(logDir, "session.log"))
	if err != nil {
		return fmt.Errorf("failed to open session log: %w", err)
	}
	defer sessionLog.Close()

	gateway, err := diag.DetectGateway()
	if err != nil {
		sessionLog.Writef("startup failure: unable to detect gateway: %v", err)
		return fmt.Errorf("unable to detect gateway: %w", err)
	}

	firstHop, err := diag.DetectFirstHop()
	if err != nil {
		sessionLog.Writef("startup failure: unable to detect first hop: %v", err)
		return fmt.Errorf("unable to detect first hop: %w", err)
	}

	targets := config.DefaultTargets(gateway, firstHop)
	resolvedIPs := make(map[string]string, len(targets))
	for _, target := range targets {
		resolvedIPs[target.Label] = diag.ResolveTarget(target.Target)
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
			monitor.RunPingMonitor(ctx, logDir, target, sessionLog)
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		monitor.RunSpeedtestLoop(ctx, info.SpeedtestLog, sessionLog)
	}()

	<-ctx.Done()
	wg.Wait()
	sessionLog.Writef("session finished: %v", context.Cause(ctx))
	return nil
}

func createLogDir(startedAt time.Time) (string, error) {
	dir := filepath.Join("logs", startedAt.Format("20060102-150405"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func buildSummary(info sessionInfo) string {
	summary := "Hermes network monitor\n"
	summary += fmt.Sprintf("Started: %s\n", info.StartedAt.Format(time.RFC1123))
	summary += fmt.Sprintf("Auto-stop: %s\n", info.StopsAt.Format(time.RFC1123))
	summary += "Stop early: Ctrl+C\n"
	summary += fmt.Sprintf("Log directory: %s\n", info.LogDir)
	summary += fmt.Sprintf("Session log: %s\n", info.SessionLog)
	summary += fmt.Sprintf("Gateway: %s\n", info.Gateway)
	summary += fmt.Sprintf("First hop: %s\n", info.FirstHop)
	summary += "Targets:\n"
	for _, target := range info.Targets {
		summary += fmt.Sprintf("  - %s => %s", target.Label, target.Target)
		if ip := info.ResolvedIPs[target.Label]; ip != "" {
			summary += fmt.Sprintf(" (%s)", ip)
		}
		summary += "\n"
	}
	summary += fmt.Sprintf("Speed test: every %s using PowerShell on Windows (%s)\n", config.SpeedtestInterval, config.SpeedtestTarget)
	return summary
}
