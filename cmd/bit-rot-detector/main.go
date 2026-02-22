// Command bit-rot-detector detects silent file corruption (bit rot) using
// BLAKE3 hashing with an atomic Shadow-DB swap for crash safety.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	"github.com/YaoxuanZhang/bit-rot-detector/internal/config"
	"github.com/YaoxuanZhang/bit-rot-detector/internal/coordinator"
	"github.com/YaoxuanZhang/bit-rot-detector/internal/mailer"
)

func main() {
	os.Exit(run())
}

func run() int {
	// Load .env if present (errors are silently ignored – env vars take precedence).
	_ = godotenv.Load()

	// ── Flags ────────────────────────────────────────────────────────────────
	syncOnly      := flag.Bool("sync", false, "run sync operation only")
	scrubOnly     := flag.Bool("scrub", false, "run scrub operation only")
	testEmail     := flag.Bool("test-email", false, "send test email and exit")
	flag.Parse()

	// ── Logging ──────────────────────────────────────────────────────────────
	setupLogging("INFO")

	// ── Configuration ────────────────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		slog.Error("configuration error", "err", err)
		return 1
	}
	setupLogging(cfg.LogLevel)

	slog.Info("bit-rot-detector starting",
		"targets", cfg.TargetPaths,
		"scrub_pct", cfg.ScrubPercentage,
		"scrub_freq", cfg.ScrubFrequency,
		"max_workers", cfg.MaxWorkers,
	)

	// ── Graceful shutdown ────────────────────────────────────────────────────
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	m := mailer.New(cfg.Email)

	// ── Test email mode ───────────────────────────────────────────────────────
	if *testEmail {
		slog.Info("sending test email")
		if err := m.SendTestEmail(); err != nil {
			slog.Error("test email failed", "err", err)
			return 1
		}
		slog.Info("test email sent successfully")
		return 0
	}

	// ── Determine operations ─────────────────────────────────────────────────
	runSync  := *syncOnly || !*scrubOnly
	runScrub := *scrubOnly || !*syncOnly

	opts := coordinator.Options{
		RunSync:         runSync,
		RunScrub:        runScrub,
		ScrubPercentage: cfg.ScrubPercentage,
		ScrubFrequency:  cfg.ScrubFrequency,
		MaxWorkers:      cfg.MaxWorkers,
	}

	// ── Run ───────────────────────────────────────────────────────────────────
	results, duration := coordinator.Run(ctx, cfg.TargetPaths, opts)

	// ── Report ────────────────────────────────────────────────────────────────
	errors := coordinator.CollectErrors(results)

	m.SendUnifiedReport(
		coordinator.ToMailerSyncEntries(results),
		coordinator.ToMailerScrubEntries(results),
		coordinator.ToHealthSlice(results),
		errors,
		duration,
	)

	// ── Exit code ─────────────────────────────────────────────────────────────
	if len(errors) > 0 {
		slog.Warn("completed with errors", "count", len(errors))
		return 1
	}

	hasBitRot := false
	for _, r := range coordinator.ToMailerScrubEntries(results) {
		if len(r.Result.FilesCorrupted) > 0 {
			hasBitRot = true
			break
		}
	}
	if hasBitRot {
		slog.Error("BIT ROT DETECTED")
		return 1
	}

	slog.Info("all operations completed successfully", "duration", duration.String())
	return 0
}

// setupLogging configures the global slog logger with structured JSON output.
func setupLogging(level string) {
	var lvl slog.Level
	switch level {
	case "DEBUG":
		lvl = slog.LevelDebug
	case "WARN", "WARNING":
		lvl = slog.LevelWarn
	case "ERROR":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	handler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: lvl,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			// Use "level" instead of the default key for compatibility.
			if a.Key == slog.LevelKey {
				a.Key = "level"
				lv := a.Value.Any().(slog.Level)
				a.Value = slog.StringValue(fmt.Sprintf("%s", lv))
			}
			return a
		},
	})
	slog.SetDefault(slog.New(handler))
}
