package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/lihongjie0209/audit-service/internal/config"
	"github.com/lihongjie0209/audit-service/internal/migration"
)

func main() {
	configPath := flag.String("config", "config/config.yaml", "configuration file path")
	profile := flag.String("env", "", "active environment profile (overrides APP_ENV and config)")
	direction := flag.String("direction", "up", "migration direction: up or down")
	steps := flag.Int("steps", 0, "number of steps; negative values migrate down")
	flag.Parse()
	if *direction != "up" && *direction != "down" {
		fmt.Fprintln(os.Stderr, "direction must be up or down")
		os.Exit(2)
	}
	cfg, err := config.LoadWithProfile(*configPath, *profile)
	if err == nil {
		err = migration.Run(cfg.Migration, *direction, *steps)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "migration failed: %v\n", err)
		os.Exit(1)
	}
}
