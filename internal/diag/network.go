package diag

import (
	"context"
	"errors"
	"fmt"
	"net"
	"runtime"
	"strings"
	"time"
)

func ResolveTarget(target string) string {
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

func DetectGateway() (string, error) {
	switch runtime.GOOS {
	case "windows":
		output, err := RunCommand(context.Background(), 5*time.Second, "route", "print", "-4")
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
		output, err := RunCommand(context.Background(), 5*time.Second, "ip", "route", "show", "default")
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
		output, err := RunCommand(context.Background(), 5*time.Second, "route", "-n", "get", "default")
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

func DetectFirstHop() (string, error) {
	switch runtime.GOOS {
	case "windows":
		output, err := RunCommand(context.Background(), 15*time.Second, "tracert", "-d", "-h", "2", "-w", "1000", "1.1.1.1")
		if err != nil {
			return "", err
		}
		return parseSecondTraceHop(output)
	case "linux":
		output, err := RunCommand(context.Background(), 15*time.Second, "traceroute", "-n", "-m", "2", "-w", "1", "1.1.1.1")
		if err != nil {
			return "", err
		}
		return parseSecondTraceHop(output)
	case "darwin":
		output, err := RunCommand(context.Background(), 15*time.Second, "traceroute", "-n", "-m", "2", "-w", "1000", "1.1.1.1")
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
