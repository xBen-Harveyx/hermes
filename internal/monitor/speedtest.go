package monitor

import (
	"context"
	"runtime"
	"time"

	"hermes/internal/config"
	"hermes/internal/diag"
	"hermes/internal/logx"
)

func RunSpeedtestLoop(ctx context.Context, logPath string, sessionLog *logx.LineLogger) {
	logger, err := logx.New(logPath)
	if err != nil {
		sessionLog.Writef("unable to open speed test log: %v", err)
		return
	}
	defer logger.Close()

	logger.Writef("starting speed test loop")
	runSpeedtestOnce(ctx, logger)

	ticker := time.NewTicker(config.SpeedtestInterval)
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

func runSpeedtestOnce(parent context.Context, logger *logx.LineLogger) {
	logger.Writef("speed test begin")

	if runtime.GOOS != "windows" {
		logger.Writef("speed test skipped: PowerShell command is only enabled on Windows")
		return
	}

	output, err := diag.RunCommand(parent, 4*time.Minute, "powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", "irm asheroto.com/speedtest | iex")
	if err != nil {
		logger.Writef("speed test failed: %v", err)
	}

	logger.Writef("speed test output start")
	logger.Write(output)
	logger.Writef("speed test output end")
}
