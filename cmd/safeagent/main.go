package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/yv1ing/SafeAgent/internal/api/console"
	"github.com/yv1ing/SafeAgent/internal/app"
	"github.com/yv1ing/SafeAgent/internal/platform/config"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
	"github.com/yv1ing/SafeAgent/web"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	if err := run(); err != nil {
		slog.Error("SafeAgent stopped", "reason", err.Error())
		os.Exit(1)
	}
}

func run() error {
	configurationPath := flag.String("config", "config.toml", "configuration file")
	output := flag.String("output", "", "generated artifact path")
	flag.Parse()
	command := "serve"
	if flag.NArg() > 0 {
		command = flag.Arg(0)
	}
	configuration, err := config.Load(*configurationPath)
	if err != nil && command != "openapi" && command != "schema" {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	switch command {
	case "openapi":
		payload, err := console.Specification()
		if err != nil {
			return err
		}
		return write(*output, payload)
	case "schema":
		dsn := configuration.DatabaseURL
		if dsn == "" {
			// Baseline only builds SQL strings via the query builder; it never connects.
			// A syntactically valid placeholder prevents the driver from panicking.
			dsn = "postgres://localhost/safeagent"
		}
		database := repository.Open(dsn)
		defer database.Close()
		baseline, err := database.Baseline()
		if err != nil {
			return err
		}
		return write(*output, []byte(baseline))
	case "serve":
		database := repository.Open(configuration.DatabaseURL)
		defer database.Close()
		frontend, err := web.Assets()
		if err != nil {
			return err
		}
		return app.Run(ctx, configuration, database, frontend)
	default:
		return fmt.Errorf("unknown command %q", command)
	}
}

func write(path string, payload []byte) error {
	if path == "" {
		_, err := os.Stdout.Write(payload)
		return err
	}
	return os.WriteFile(path, payload, 0o644)
}
