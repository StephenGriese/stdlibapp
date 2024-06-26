package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/StephenGriese/stdlibapp/dictionary"
	"github.com/StephenGriese/stdlibapp/kitmetrics"
	"github.com/StephenGriese/stdlibapp/logs"
	"github.com/StephenGriese/stdlibapp/metrics"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"io"
	"strconv"

	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"
)

const (
	labelEndpoint = "endpoint"
	labelStatus   = "status"
)

func main() {
	ctx := context.Background()
	if err := run(ctx, os.Getenv); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}

type Config struct {
	AppName       string
	Port          string
	DownstreamURL string
}

func run(
	ctx context.Context,
	getenv func(string) string,
) error {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt)
	defer func() {
		log.Println("cancelling run's notify context")
		cancel()
	}()

	config := createConfig(getenv)

	logger := logs.NewLogger()

	tracer := otel.Tracer(config.AppName)
	logger.Info(ctx, "got tracer", "tracer", tracer)

	metricsFactory := metrics.NewFactory(config.AppName)

	srv := NewServer(ctx, config, logger, metricsFactory, tracer)
	httpServer := &http.Server{
		Addr:    net.JoinHostPort("localhost", config.Port),
		Handler: srv,
	}
	go func() {
		logger.Info(ctx, "starting http server", "addr", httpServer.Addr)
		log.Printf("listening on %s", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "error listening and serving: %s\n", err)
		}
	}()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		// Make a new context for the Shutdown (thanks Alessandro Rosetti)
		shutdownCtx := context.Background()
		shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "error shutting down http server: %s\n", err)
		} else {
			fmt.Fprintf(os.Stdout, "http server shutdown\n")
		}
	}()
	wg.Wait()
	return nil
}

func NewServer(
	ctx context.Context,
	config Config,
	logger dictionary.Logger,
	metricsFactory metrics.Factory,
	tracer trace.Tracer,
) http.Handler {
	mux := http.NewServeMux()
	addRoutes(ctx, mux, config, logger, metricsFactory, tracer)
	var handler http.Handler = mux
	return handler
}

func addRoutes(
	ctx context.Context,
	mux *http.ServeMux,
	config Config,
	logger dictionary.Logger,
	metricsFactory metrics.Factory,
	tracer trace.Tracer,
) {
	histogram := newRequestLatencyHistogram(metricsFactory)

	mux.Handle("/lookup", withMetrics(histogram, "lookup", handleLookup(ctx, logger, config.DownstreamURL, metricsFactory.NewServiceStatistics("lookup"), tracer)))
	mux.Handle("/metrics", handleGetMetrics(ctx, logger, metricsFactory))
}

func handleGetMetrics(ctx context.Context, logger dictionary.Logger, metricsFactory metrics.Factory) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.Info(ctx, "handleGetMetrics called")
		metricsFactory.HTTPHandlerFor().ServeHTTP(w, r)
	})
}

func handleLookup(ctx context.Context, logger dictionary.Logger, downstreamURL string, serviceStatistics metrics.ServiceStatistics, tracer trace.Tracer) http.Handler {
	if downstreamURL == "" {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, span := tracer.Start(ctx, "handleLookup")
			defer span.End()
			logger.Info(ctx, "handleLookup called", "downstreamURL", downstreamURL)
			w.Write([]byte("hey!! lookup"))
		})
	} else {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, span := tracer.Start(ctx, "handleLookup")
			defer span.End()
			logger.Info(ctx, "handleLookup called", "downstreamURL", downstreamURL)
			// Define the URL of the external server
			externalURL := downstreamURL + r.URL.Path

			// Create a new request to the external server
			req, err := http.NewRequest("GET", externalURL, nil)
			if err != nil {
				http.Error(w, "Failed to create request", http.StatusInternalServerError)
				return
			}

			// Optionally, you can copy headers from the original request to the new request
			for key, values := range r.Header {
				for _, value := range values {
					req.Header.Add(key, value)
				}
			}

			// Create an HTTP client
			client := &http.Client{
				Timeout:   10 * time.Second,
				Transport: dictionary.LoggingRoundTripper{Proxied: http.DefaultTransport, Statistic: serviceStatistics},
			}

			// Make the request to the external server
			resp, err := client.Do(req)
			if err != nil {
				http.Error(w, "Failed to get response from external server", http.StatusInternalServerError)
				return
			}
			defer resp.Body.Close()

			// Read the response body
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				http.Error(w, "Failed to read response body", http.StatusInternalServerError)
				return
			}

			// Write the response from the external server back to the client
			for key, values := range resp.Header {
				for _, value := range values {
					w.Header().Add(key, value)
				}
			}
			w.WriteHeader(resp.StatusCode)
			w.Write(body)
		})
	}
}

func createConfig(getenv func(string) string) Config {
	port := getenv("PORT")
	downstreamURL := getenv("DOWNSTREAM_URL")
	return Config{
		Port:          port,
		DownstreamURL: downstreamURL,
	}
}

func withMetrics(histogram kitmetrics.Histogram, label string, handler http.Handler) http.Handler {
	return metricHandler{
		histogram: histogram,
		label:     metrics.CanonicalLabel(label),
		handler:   handler,
	}
}

type metricHandler struct {
	handler   http.Handler
	label     string
	histogram kitmetrics.Histogram
}

// ServeHTTP implements the http.Handler interface.
func (mh metricHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	sw := newStatusCapturingResponseWriter(rw)

	defer func(start time.Time) {
		mh.histogram.With(labelEndpoint, mh.label, labelStatus, strconv.Itoa(sw.status)).Observe(float64(time.Since(start).Milliseconds()))
	}(time.Now())

	mh.handler.ServeHTTP(sw, r)
}

// statusWriter implements http.ResponseWriter to capture the http response status code.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func newStatusCapturingResponseWriter(rw http.ResponseWriter) *statusWriter {
	return &statusWriter{ResponseWriter: rw}
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	return w.ResponseWriter.Write(b)
}

func newRequestLatencyHistogram(mf metrics.Factory) kitmetrics.Histogram {
	buckets := []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000}
	return mf.NewHistogram("http_server", "request_latency_milliseconds", "Total duration of http requests in milliseconds",
		buckets, []string{labelEndpoint, labelStatus})
}
