package config

import "time"

type PingTarget struct {
	Label  string
	Target string
}

const (
	RunDuration       = 30 * time.Minute
	PingInterval      = 1 * time.Second
	SpeedtestInterval = 5 * time.Minute
	SpeedtestTarget   = "https://github.com/asheroto/speedtest"
)

func DefaultTargets(gateway, firstHop string) []PingTarget {
	return []PingTarget{
		{Label: "gateway", Target: gateway},
		{Label: "first-hop", Target: firstHop},
		{Label: "google", Target: "google.com"},
		{Label: "atnetplus", Target: "atnetplus.com"},
		{Label: "cloudflare", Target: "cloudflare.com"},
	}
}
