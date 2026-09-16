// Package telemetry initialises OpenTelemetry with an OTLP/HTTP exporter
// that pushes metrics and traces directly to Grafana Cloud.
// Uses OTel v1.21.x (Go 1.22 compatible).
//
// Required env vars (all optional — telemetry is silently skipped when absent):
//
//	GRAFANA_OTLP_ENDPOINT  – e.g. https://otlp-gateway-prod-us-east-0.grafana.net/otlp
//	GRAFANA_INSTANCE_ID    – numeric Prometheus instance ID shown in Grafana Cloud
//	GRAFANA_API_KEY        – Grafana Cloud API key / token
package telemetry

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
)

// ShutdownFunc flushes and shuts down all OTel providers.
type ShutdownFunc func(context.Context) error

// Init sets up the global OTel MeterProvider and TracerProvider.
// It returns a ShutdownFunc that must be deferred in main().
// If the required env vars are absent it is a no-op and returns a nil ShutdownFunc.
func Init(ctx context.Context, serviceName string) (ShutdownFunc, error) {
	rawEndpoint := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	if rawEndpoint == "" {
		rawEndpoint = strings.TrimSpace(os.Getenv("GRAFANA_OTLP_ENDPOINT"))
	}
	instanceID := strings.TrimSpace(os.Getenv("GRAFANA_INSTANCE_ID"))
	apiKey := strings.TrimSpace(os.Getenv("GRAFANA_API_KEY"))
	otlpHeaders := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_HEADERS"))

	var authHeader string
	if instanceID != "" && apiKey != "" {
		authHeader = "Basic " + base64.StdEncoding.EncodeToString(
			[]byte(fmt.Sprintf("%s:%s", instanceID, apiKey)),
		)
	} else if otlpHeaders != "" {
		if strings.HasPrefix(otlpHeaders, "Authorization=Basic ") {
			authHeader = strings.TrimPrefix(otlpHeaders, "Authorization=")
		} else if strings.HasPrefix(otlpHeaders, "Basic ") {
			authHeader = otlpHeaders
		} else {
			authHeader = "Basic " + otlpHeaders
		}
	}

	if rawEndpoint == "" || authHeader == "" {
		log.Println("[telemetry] GRAFANA / OTEL env vars not set — OTel push disabled (local mode)")
		return func(context.Context) error { return nil }, nil
	}

	// OTel WithEndpoint requires only "host[:port]", without scheme or path
	insecure := false
	endpointHost := rawEndpoint
	if strings.HasPrefix(endpointHost, "http://") {
		insecure = true
		endpointHost = strings.TrimPrefix(endpointHost, "http://")
	} else if strings.HasPrefix(endpointHost, "https://") {
		endpointHost = strings.TrimPrefix(endpointHost, "https://")
	}
	endpointHost = strings.TrimRight(strings.Split(endpointHost, "/")[0], "/")

	headers := map[string]string{"Authorization": authHeader}

	// Shared resource attributes (show up on every metric/trace in Grafana)
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion("1.0.0"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry: build resource: %w", err)
	}

	// ── Metrics ────────────────────────────────────────────────────────────────
	metricOpts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint(endpointHost),
		otlpmetrichttp.WithURLPath("/otlp/v1/metrics"),
		otlpmetrichttp.WithHeaders(headers),
	}
	if insecure {
		metricOpts = append(metricOpts, otlpmetrichttp.WithInsecure())
	}

	metricExp, err := otlpmetrichttp.New(ctx, metricOpts...)
	if err != nil {
		return nil, fmt.Errorf("telemetry: create metric exporter: %w", err)
	}

	mp := metric.NewMeterProvider(
		metric.WithResource(res),
		// Push every 15 s — fine for free tier limits
		metric.WithReader(metric.NewPeriodicReader(metricExp, metric.WithInterval(15*time.Second))),
	)
	otel.SetMeterProvider(mp)

	// ── Traces ─────────────────────────────────────────────────────────────────
	traceOpts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(endpointHost),
		otlptracehttp.WithURLPath("/otlp/v1/traces"),
		otlptracehttp.WithHeaders(headers),
	}
	if insecure {
		traceOpts = append(traceOpts, otlptracehttp.WithInsecure())
	}

	traceExp, err := otlptracehttp.New(ctx, traceOpts...)
	if err != nil {
		return nil, fmt.Errorf("telemetry: create trace exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		// Sample 100% of requests — lower if you hit Tempo limits
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithBatcher(traceExp),
	)
	otel.SetTracerProvider(tp)

	log.Printf("[telemetry] OTel push enabled → %s (service: %s)", endpointHost, serviceName)

	return func(shutdownCtx context.Context) error {
		if err := mp.Shutdown(shutdownCtx); err != nil {
			log.Printf("[telemetry] metric provider shutdown error: %v", err)
		}
		if err := tp.Shutdown(shutdownCtx); err != nil {
			log.Printf("[telemetry] trace provider shutdown error: %v", err)
		}
		return nil
	}, nil
}
