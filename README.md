# Hermes

Hermes is a small Go-based CLI for basic network troubleshooting on Windows hosts.

At startup, it:

- detects the default gateway
- runs a traceroute and captures hop 2 as the first hop past the gateway
- starts five ongoing ping checks
- schedules a speed test every 5 minutes
- stops automatically after 30 minutes unless you stop it earlier with `Ctrl+C`

The five ping targets are:

- the local gateway
- the first hop past the gateway
- `google.com`
- `atnetplus.com`
- `cloudflare.com`

## Requirements

- Go 1.22 or newer to build from source
- Windows 10 or Windows 11 for the intended runtime behavior
- PowerShell available on the host
- Standard Windows networking tools available in `PATH`
  - `route`
  - `tracert`
  - `ping`

## Build For 64-Bit Windows

If you are building directly on a 64-bit Windows machine:

```powershell
go build -o hermes.exe .
```

If you are cross-building from another OS for 64-bit Windows:

```bash
GOOS=windows GOARCH=amd64 go build -o hermes.exe .
```

The important part is that the output must be a 64-bit Windows executable. If you build for the wrong platform or architecture, Windows may report that the app is not a valid 64-bit application.

## Run

From PowerShell:

```powershell
.\hermes.exe
```

What happens when it starts:

- prints a startup summary to the console
- shows the detected gateway and first hop
- shows when the session will auto-stop
- creates a timestamped log directory under `logs\`
- writes all monitoring output to files only

To stop it early:

```text
Ctrl+C
```

Otherwise it exits automatically after 30 minutes.

## Logging

Each run creates a new directory under `logs/` named with the session start time:

```text
logs/YYYYMMDD-HHMMSS/
```

Each log line is timestamped in this format:

```text
YYYY-MM-DD HH:MM:SS
```

The directory contains:

- `session.log`
  - startup summary
  - major session events
  - early shutdown or normal completion
- `gateway.log`
  - one ping result per second to the detected default gateway
- `first-hop.log`
  - one ping result per second to hop 2 from the startup traceroute
- `google.log`
  - one ping result per second to `google.com`
- `atnetplus.log`
  - one ping result per second to `atnetplus.com`
- `cloudflare.log`
  - one ping result per second to `cloudflare.com`
- `speedtest.log`
  - scheduled speed test runs and captured output

Ping failures are logged continuously. If a host stops responding or cannot be resolved, the tool keeps writing failure entries instead of exiting.

## Speed Test Behavior

Every 5 minutes, Hermes runs this PowerShell command on Windows:

```powershell
irm asheroto.com/speedtest | iex
```

Reference:

- <https://github.com/asheroto/speedtest>

The command output is written to `speedtest.log`.

## Notes

- Hermes is written in Go and can be built cross-platform, but the current workflow is designed primarily for Windows 10 and 11.
- The speed test feature is Windows-specific in the current implementation because it shells out to PowerShell.
- The first hop is determined once at startup by running a traceroute and taking hop 2.
- The tool currently has no configuration flags and no interactive prompts.

## Basic Validation

Compile check:

```bash
go test ./...
```

Run locally after building:

```powershell
.\hermes.exe
```
