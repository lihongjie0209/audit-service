package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/lihongjie0209/audit-service/internal/app"
	"github.com/lihongjie0209/audit-service/internal/buildinfo"
	"github.com/lihongjie0209/audit-service/internal/config"
)

// @title Go API Template
// @version 1.0
// @description Production-oriented Go Web API scaffold. Application error codes: 0 success; 10000-19999 common/input; 20000-29999 authentication/authorization; 30000-39999 business; 50000-59999 infrastructure.
// @BasePath /
// @schemes http https
// @securityDefinitions.apikey Bearer
// @in header
// @name Authorization
// @description Enter "Bearer {JWT}".
// @securityDefinitions.apikey PSK
// @in header
// @name Authorization
// @description Enter "PSK {shared-key}" for routes configured with PSK authentication.
func main() {
	configPath := flag.String("config", "config/config.yaml", "configuration file path")
	profile := flag.String("env", "", "active environment profile (overrides APP_ENV and config)")
	showVersion := flag.Bool("version", false, "print build version information and exit")
	flag.Parse()
	if *showVersion {
		fmt.Printf("version=%s commit=%s build_time=%s\n", buildinfo.Version, buildinfo.Commit, buildinfo.BuildTime)
		return
	}
	cfg, err := config.LoadWithProfile(*configPath, *profile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load configuration: %v\n", err)
		os.Exit(1)
	}
	app.New(cfg).Run()
}
