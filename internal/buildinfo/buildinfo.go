package buildinfo

import "time"

var Version = "dev"
var Commit = "unknown"
var BuildTime = "unknown"
var startedAt = time.Now()

type Info struct {
	Version   string    `json:"version"`
	Commit    string    `json:"commit"`
	BuildTime string    `json:"build_time"`
	StartedAt time.Time `json:"started_at"`
	Uptime    string    `json:"uptime"`
}

func Current() Info {
	return Info{Version: Version, Commit: Commit, BuildTime: BuildTime, StartedAt: startedAt, Uptime: time.Since(startedAt).Round(time.Second).String()}
}
