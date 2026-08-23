package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/KarmaXP/mcp-gateway/internal/app"
	"github.com/KarmaXP/mcp-gateway/internal/config"
	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/telemetry"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdownTelemetry, err := telemetry.Init(ctx, defaults.DefaultTelemetryServiceName)
	if err != nil {
		return err
	}
	defer func() {
		sctx, cancel := context.WithTimeout(context.Background(), defaults.TelemetryShutdownTimeout)
		defer cancel()
		if err := shutdownTelemetry(sctx); err != nil {
			slog.Error("telemetry shutdown", "err", err)
		}
	}()

	baseLog := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	slog.SetDefault(slog.New(telemetry.TraceHandler(baseLog)))

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	gateway, err := app.New(ctx, app.Options{
		ServiceName: defaults.DefaultTelemetryServiceName,
		Config:      cfg,
	})
	if err != nil {
		return err
	}
	defer gateway.Close()

	return gateway.Run(ctx)
}
