package metrics

import (
	"context"
	"fmt"
	"net/http"

	promclient "github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func New() (m Metrics, err error) {
	exporter, err := prometheus.New()
	if err != nil {
		return Metrics{}, fmt.Errorf("failed to create prometheus exporter: %w", err)
	}

	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	otel.SetMeterProvider(provider)

	meter := provider.Meter("github.com/a-h/depot")

	if m.TotalDownloads, err = meter.Int64Counter("total_downloads", metric.WithDescription("Total number of successful downloads served")); err != nil {
		return Metrics{}, fmt.Errorf("failed to create total_downloads counter: %w", err)
	}
	if m.DownloadedBytesTotal, err = meter.Int64Counter("downloaded_bytes_total", metric.WithDescription("Total bytes downloaded from depot")); err != nil {
		return Metrics{}, fmt.Errorf("failed to create downloaded_bytes_total counter: %w", err)
	}
	if m.AccessLogErrorsTotal, err = meter.Int64Counter("access_log_errors_total", metric.WithDescription("Total number of access log processing errors")); err != nil {
		return Metrics{}, fmt.Errorf("failed to create access_log_errors_total counter: %w", err)
	}
	if m.PackageUploadsTotal, err = meter.Int64Counter("package_uploads_total", metric.WithDescription("Total number of successfully uploaded package files")); err != nil {
		return Metrics{}, fmt.Errorf("failed to create package_uploads_total counter: %w", err)
	}
	if m.UploadedBytesTotal, err = meter.Int64Counter("uploaded_bytes_total", metric.WithDescription("Total bytes uploaded into depot")); err != nil {
		return Metrics{}, fmt.Errorf("failed to create uploaded_bytes_total counter: %w", err)
	}

	return m, nil
}

type Metrics struct {
	TotalDownloads       metric.Int64Counter
	DownloadedBytesTotal metric.Int64Counter
	AccessLogErrorsTotal metric.Int64Counter
	PackageUploadsTotal  metric.Int64Counter
	UploadedBytesTotal   metric.Int64Counter
}

func ListenAndServe(addr string) error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promclient.Handler())
	return http.ListenAndServe(addr, mux)
}

func (m Metrics) IncrementDownloadMetrics(ctx context.Context, ecosystem string, bytes int64) {
	if m.TotalDownloads == nil || m.DownloadedBytesTotal == nil {
		return
	}
	m.TotalDownloads.Add(ctx, 1, metric.WithAttributes(attribute.String("ecosystem", ecosystem)))
	m.DownloadedBytesTotal.Add(ctx, bytes, metric.WithAttributes(attribute.String("ecosystem", ecosystem)))
}

func (m Metrics) IncrementAccessLogErrors(ctx context.Context) {
	if m.AccessLogErrorsTotal == nil {
		return
	}
	m.AccessLogErrorsTotal.Add(ctx, 1)
}

func (m Metrics) IncrementUploadMetrics(ctx context.Context, ecosystem string, bytes int64) {
	if m.PackageUploadsTotal == nil || m.UploadedBytesTotal == nil {
		return
	}
	m.PackageUploadsTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("ecosystem", ecosystem)))
	m.UploadedBytesTotal.Add(ctx, bytes, metric.WithAttributes(attribute.String("ecosystem", ecosystem)))
}
