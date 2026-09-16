// Package telemetry initialises OpenTelemetry with an OTLP/HTTP exporter
// that pushes metrics and traces directly to Grafana Cloud, and also exposes
// a Prometheus reader that serves metrics via /metrics.
// Uses OTel v1.21.x (Go 1.22 compatible).
//
// Required env vars (all optional — OTLP push is silently skipped when absent;
// the Prometheus /metrics endpoint still works):
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
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
)

// ShutdownFunc flushes and shuts down all OTel providers.
type ShutdownFunc func(context.Context) error

// Handler returns the HTTP handler that exposes the OTel metrics registry in
// Prometheus text format. Mount it in main.go at "/metrics".
func Handler() http.Handler {
	return promhttp.Handler()
}

// Init sets up the global OTel MeterProvider and TracerProvider.
// It returns a ShutdownFunc that must be deferred in main().
// If the required env vars are absent, OTLP push is skipped but the
// Prometheus /metrics endpoint remains active.
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

	// ── Prometheus reader (always installed, no env vars required) ────────────
	promExp, err := otelprom.New()
	if err != nil {
		return nil, fmt.Errorf("telemetry: create prometheus exporter: %w", err)
	}

	// ── Local mode: no OTLP push, but still serve /metrics ────────────────────
	if rawEndpoint == "" || authHeader == "" {
		log.Println("[telemetry] GRAFANA / OTEL env vars not set — OTel push disabled (local mode); Prometheus /metrics enabled")
		mp := metric.NewMeterProvider(
			metric.WithResource(res),
			metric.WithReader(promExp),
		)
		otel.SetMeterProvider(mp)
		return func(shutdownCtx context.Context) error {
			return mp.Shutdown(shutdownCtx)
		}, nil
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

	// Two readers on one MeterProvider: periodic OTLP push + on-scrape Prometheus pull.
	mp := metric.NewMeterProvider(
		metric.WithResource(res),
		metric.WithReader(metric.NewPeriodicReader(metricExp, metric.WithInterval(15*time.Second))),
		metric.WithReader(promExp),
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
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithBatcher(traceExp),
	)
	otel.SetTracerProvider(tp)

	log.Printf("[telemetry] OTel push enabled → %s (service: %s); Prometheus /metrics enabled", endpointHost, serviceName)

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