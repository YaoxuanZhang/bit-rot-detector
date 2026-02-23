// Command bit-rot-detector detects silent file corruption (bit rot) using
// BLAKE3 hashing with an atomic Shadow-DB swap for crash safety.
//
// Usage:
//
//	bit-rot-detector [flags]
//
// Flags:
//
//	-sync         run sync phase only
//	-scrub        run scrub phase only
//	-test-email   send a test email and exit
//	-watch        watch directories for changes and re-run on each change
//	-web          start the HTTP status/control UI
//	-addr string  HTTP listen address when -web is set (default ":8080")
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/YaoxuanZhang/bit-rot-detector/internal/api"
	"github.com/YaoxuanZhang/bit-rot-detector/internal/config"
	"github.com/YaoxuanZhang/bit-rot-detector/internal/coordinator"
	"github.com/YaoxuanZhang/bit-rot-detector/internal/mailer"
	"github.com/YaoxuanZhang/bit-rot-detector/internal/watcher"
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
	watchMode     := flag.Bool("watch", false, "watch directories for changes and re-run on each change")
	webMode       := flag.Bool("web", false, "start the HTTP status/control UI")
	listenAddr    := flag.String("addr", ":8080", "HTTP listen address (used with -web)")
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

	// ── Web UI mode ───────────────────────────────────────────────────────────
	if *webMode {
		srv := api.New(cfg.TargetPaths, opts)
		slog.Info("starting web UI", "addr", *listenAddr)
		if err := srv.ListenAndServe(ctx, *listenAddr); err != nil {
			slog.Error("web server error", "err", err)
			return 1
		}
		return 0
	}

	// ── Watch mode ────────────────────────────────────────────────────────────
	if *watchMode {
		onChange := func(watchCtx context.Context, changed []string) {
			slog.Info("watcher: resync triggered", "drives", changed)
			results, duration := coordinator.Run(watchCtx, changed, opts)
			errors := coordinator.CollectErrors(results)
			m.SendUnifiedReport(
				coordinator.ToMailerSyncEntries(results),
				coordinator.ToMailerScrubEntries(results),
				coordinator.ToHealthSlice(results),
				errors,
				duration,
			)
		}
		w, err := watcher.New(cfg.TargetPaths, 3*time.Second, onChange)
		if err != nil {
			slog.Error("watcher init failed", "err", err)
			return 1
		}
		defer w.Close()
		slog.Info("watching for changes", "paths", cfg.TargetPaths)
		w.Run(ctx)
		return 0
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
				a.Value = slog.StringValue(lv.String())
			}
			return a
		},
	})
	slog.SetDefault(slog.New(handler))
}
