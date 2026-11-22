package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yv1ing/SafeAgent/internal/app"
	"github.com/yv1ing/SafeAgent/internal/platform/config"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
	"github.com/yv1ing/SafeAgent/internal/transport/console"
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
		database := repository.Open(dsn, nil)
		defer database.Close()
		baseline, err := database.Baseline()
		if err != nil {
			return err
		}
		return write(*output, []byte(baseline))
	case "serve":
		key, err := configuration.Key()
		if err != nil {
			return err
		}
		database := repository.Open(configuration.DatabaseURL, key)
		defer database.Close()
		frontend, err := web.Assets()
		if err != nil {
			return err
		}
		return app.Run(ctx, configuration, database, frontend)
	case "verify-audit":
		key, err := configuration.Key()
		if err != nil {
			return err
		}
		return verifyAudit(ctx, configuration.DatabaseURL, key, flag.Args()[1:])
	default:
		return fmt.Errorf("unknown command %q", command)
	}
}

func verifyAudit(ctx context.Context, databaseURL string, instanceKey []byte, args []string) error {
	flags := flag.NewFlagSet("verify-audit", flag.ContinueOnError)
	callID := flags.String("call", "", "verify one call id")
	projectID := flags.String("project", "", "verify one project")
	sinceText := flags.String("since", "", "only calls created after a duration (24h) or date (2026-01-02)")
	limit := flags.Int("limit", 0, "maximum calls to verify; 0 verifies every call")
	jsonOutput := flags.Bool("json", false, "print a JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *limit < 0 {
		return fmt.Errorf("-limit must be zero or positive")
	}
	scope := repository.AuditScope{ProjectID: *projectID, CallID: *callID, Limit: *limit}
	if *sinceText != "" {
		since, err := parseSince(*sinceText)
		if err != nil {
			return err
		}
		scope.Since = since
	}
	database := repository.Open(databaseURL, instanceKey)
	defer database.Close()
	if err := database.VerifySchema(ctx); err != nil {
		return err
	}
	reports, err := database.VerifyAudit(ctx, scope)
	if err != nil {
		return err
	}
	if *jsonOutput {
		payload, err := json.MarshalIndent(reports, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(payload))
	} else {
		for _, report := range reports {
			line := fmt.Sprintf("call %s: %s events=%d head=%s", report.CallID, report.Report.Status, report.Report.Events, report.Report.Head)
			if report.Report.Error != "" {
				line += " error=" + report.Report.Error
			}
			fmt.Println(line)
		}
	}
	for _, report := range reports {
		if report.Report.Status != "verified" && report.Report.Status != "in_progress" {
			return fmt.Errorf("audit verification failed for %s", report.CallID)
		}
	}
	return nil
}

func parseSince(value string) (time.Time, error) {
	if duration, err := time.ParseDuration(value); err == nil {
		return time.Now().UTC().Add(-duration), nil
	}
	date, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid -since value %q", value)
	}
	return date.UTC(), nil
}

func write(path string, payload []byte) error {
	if path == "" {
		_, err := os.Stdout.Write(payload)
		return err
	}
	return os.WriteFile(path, payload, 0o644)
}
