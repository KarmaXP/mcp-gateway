package telemetry

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
)

var ActiveSessions atomic.Int64

func Init(ctx context.Context, serviceName string) (shutdown func(context.Context) error, err error) {
	if serviceName == "" {
		serviceName = defaults.DefaultTelemetryServiceName
	}
	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithAttributes(semconv.ServiceName(serviceName)),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry: resource: %w", err)
	}

	ep := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	if ep != "" {
		ep = normalizeOTLP(ep)
		_ = os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", ep)
	}
	var tp *sdktrace.TracerProvider
	var mp *sdkmetric.MeterProvider

	if ep != "" {
		texp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(normalizeOTLP(ep)))
		if err != nil {
			return nil, fmt.Errorf("telemetry: trace exporter: %w", err)
		}
		tp = sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(texp),
			sdktrace.WithResource(res),
		)

		mexp, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(normalizeOTLP(ep)))
		if err != nil {
			return nil, fmt.Errorf("telemetry: metric exporter: %w", err)
		}
		reader := sdkmetric.NewPeriodicReader(mexp, sdkmetric.WithInterval(defaults.OTLPMetricExportInterval))
		mp = sdkmetric.NewMeterProvider(
			sdkmetric.WithReader(reader),
			sdkmetric.WithResource(res),
		)
	} else {
		tp = sdktrace.NewTracerProvider(sdktrace.WithResource(res))
		mp = sdkmetric.NewMeterProvider(sdkmetric.WithResource(res))
	}

	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	if err := registerInstruments(); err != nil {
		return nil, err
	}

	return func(c context.Context) error {
		var errs []error
		if err := mp.Shutdown(c); err != nil {
			errs = append(errs, fmt.Errorf("telemetry: meter shutdown: %w", err))
		}
		if err := tp.Shutdown(c); err != nil {
			errs = append(errs, fmt.Errorf("telemetry: tracer shutdown: %w", err))
		}
		return errors.Join(errs...)
	}, nil
}

func normalizeOTLP(ep string) string {
	if strings.HasPrefix(ep, "http://") || strings.HasPrefix(ep, "https://") {
		return ep
	}
	return "http://" + ep
}
