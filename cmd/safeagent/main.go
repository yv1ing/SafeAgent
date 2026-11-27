package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/yv1ing/SafeAgent/internal/app"
	"github.com/yv1ing/SafeAgent/internal/evidence"
	"github.com/yv1ing/SafeAgent/internal/platform/config"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
	"github.com/yv1ing/SafeAgent/internal/trajectory"
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
	case "verify-trajectory":
		key, err := configuration.Key()
		if err != nil {
			return err
		}
		return verifyTrajectory(ctx, configuration.DatabaseURL, key, flag.Args()[1:])
	case "export-bundle":
		key, err := configuration.Key()
		if err != nil {
			return err
		}
		return exportBundle(ctx, configuration.DatabaseURL, key, flag.Args()[1:])
	case "verify-bundle":
		key, err := configuration.Key()
		if err != nil {
			return err
		}
		return verifyBundle(key, flag.Args()[1:])
	default:
		return fmt.Errorf("unknown command %q", command)
	}
}

func verifyTrajectory(ctx context.Context, databaseURL string, instanceKey []byte, args []string) error {
	flags := flag.NewFlagSet("verify-trajectory", flag.ContinueOnError)
	projectID := flags.String("project", "", "verify one project")
	trajectoryID := flags.String("trajectory", "", "verify one trajectory id")
	limit := flags.Int("limit", 0, "maximum trajectories to verify; 0 verifies every trajectory")
	jsonOutput := flags.Bool("json", false, "print a JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *limit < 0 {
		return fmt.Errorf("-limit must be zero or positive")
	}
	database := repository.Open(databaseURL, instanceKey)
	defer database.Close()
	if err := database.VerifySchema(ctx); err != nil {
		return err
	}
	reports, err := database.VerifyTrajectories(ctx, *projectID, *trajectoryID, *limit)
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
			line := fmt.Sprintf("trajectory %s: %s events=%d head=%s", report.TrajectoryID, report.Report.Status, report.Report.Events, report.Report.Head)
			if report.Report.Error != "" {
				line += " error=" + report.Report.Error
			}
			fmt.Println(line)
		}
	}
	for _, report := range reports {
		if report.Report.Status != trajectory.StatusVerified && report.Report.Status != trajectory.StatusInProgress {
			return fmt.Errorf("trajectory verification failed for %s", report.TrajectoryID)
		}
	}
	return nil
}

func exportBundle(ctx context.Context, databaseURL string, instanceKey []byte, args []string) error {
	flags := flag.NewFlagSet("export-bundle", flag.ContinueOnError)
	projectID := flags.String("project", "", "project id")
	trajectoryID := flags.String("trajectory", "", "trajectory id")
	output := flags.String("output", "", "output file; stdout when empty")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *projectID == "" || *trajectoryID == "" {
		return fmt.Errorf("-project and -trajectory are required")
	}
	database := repository.Open(databaseURL, instanceKey)
	defer database.Close()
	if err := database.VerifySchema(ctx); err != nil {
		return err
	}
	bundle, err := database.EvidenceBundle(ctx, *projectID, *trajectoryID)
	if err != nil {
		return err
	}
	payload, err := evidence.Marshal(bundle)
	if err != nil {
		return err
	}
	return write(*output, payload)
}

func verifyBundle(instanceKey []byte, args []string) error {
	flags := flag.NewFlagSet("verify-bundle", flag.ContinueOnError)
	path := flags.String("file", "", "bundle file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *path == "" {
		return fmt.Errorf("-file is required")
	}
	payload, err := os.ReadFile(*path)
	if err != nil {
		return err
	}
	bundle, err := evidence.Unmarshal(payload)
	if err != nil {
		return err
	}
	report := evidence.Verify(bundle, instanceKey)
	encoded, _ := json.MarshalIndent(report, "", "  ")
	fmt.Println(string(encoded))
	if !report.Valid {
		return errors.New("bundle verification failed")
	}
	return nil
}

func write(path string, payload []byte) error {
	if path == "" {
		_, err := os.Stdout.Write(payload)
		return err
	}
	return os.WriteFile(path, payload, 0o644)
}
