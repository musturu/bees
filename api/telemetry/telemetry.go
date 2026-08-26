package telemetry

import (
	"context"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
)

// Config defines the configuration for Logging and OTEL
type Config struct {
	LogLevel    string `json:"log_level"`
	ServiceName string `json:"service_name"`
	// UseJSON toggle to log output in structured JSON (Loki/Promtail compatible)
	UseJSON     bool   `json:"use_json"`
	EnableOTEL  bool   `json:"enable_otel"`
}

// Setup initializes the global slog and optional OpenTelemetry SDK
func Setup(ctx context.Context, cfg Config) (*sdktrace.TracerProvider, error) {
	// Parse log level
	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: level,
	}

	var handler slog.Handler
	if cfg.UseJSON {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	// Apply default attributes (like service name) to every log
	if cfg.ServiceName != "" {
		handler = handler.WithAttrs([]slog.Attr{slog.String("service", cfg.ServiceName)})
	}

	slog.SetDefault(slog.New(handler))

	// Setup OpenTelemetry Tracing
	if cfg.EnableOTEL {
		res, err := resource.New(ctx,
			resource.WithAttributes(
				semconv.ServiceName(cfg.ServiceName),
			),
		)
		if err != nil {
			return nil, err
		}

		// Usually, an OTLP exporter would be attached here based on environment vars
		// e.g. using otelExporter := otlptracegrpc.New(ctx)
		// For framework simplicity without hard dependencies on OTLP grpc out of the box,
		// we set up the provider without an exporter - which means tracing happens in memory.
		// Exporters can be injected additionally or set via OTEL_EXPORTER_OTLP_ENDPOINT env variable.
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithResource(res),
		)
		otel.SetTracerProvider(tp)
		return tp, nil
	}

	return nil, nil
}
