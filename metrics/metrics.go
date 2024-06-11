package metrics

import (
	"github.com/StephenGriese/stdlibapp/kitmetrics"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"net/http"
	"strings"
	"time"
)

const (
	methodField = "method"
)

var fieldKeys = []string{methodField}

// A Factory is used to create Prometheus metrics
type Factory struct {
	namespace string
	Registry  *prometheus.Registry
}

// NewFactory will return a new Factory whose metrics will all be created under the given namespace. A new Prometheus
// Registry will be created for the factory as well. This will also register collectors to export process and Go GC
// metrics
func NewFactory(namespace string) Factory {
	r := prometheus.NewRegistry()
	r.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	r.MustRegister(collectors.NewGoCollector())

	return Factory{namespace: CanonicalLabel(namespace), Registry: r}
}

func CanonicalLabel(s string) string {
	var result strings.Builder
	for i := 0; i < len(s); i++ {
		b := s[i]
		if ('a' <= b && b <= 'z') ||
			('A' <= b && b <= 'Z') ||
			('0' <= b && b <= '9') ||
			b == '_' {
			result.WriteByte(b)
		} else if b == ' ' || b == '-' {
			result.WriteByte('_')
		}
	}

	return strings.ToLower(result.String())
}

// ServiceStatistics are meant to be used as middle-ware to measure "service-level" metrics broken out by method. It will
// include the number of requests
type ServiceStatistics interface {
	Update(methodName string, begin time.Time, err error)
}

type serviceStats struct {
	requestCount   kitmetrics.Counter
	errorCount     kitmetrics.Counter
	requestLatency kitmetrics.Histogram
}

// HTTPHandlerFor will return a new http.Handler for exporting the metrics created via this Factory over HTTP
func (f Factory) HTTPHandlerFor() http.Handler {
	return promhttp.InstrumentMetricHandler(f.Registry, promhttp.HandlerFor(f.Registry, promhttp.HandlerOpts{}))
}

// NewServiceStatistics creates and registers all of the metrics associated with a ServiceStatistics
func (f Factory) NewServiceStatistics(subsystem string) ServiceStatistics {
	requestCount := f.NewCounter(subsystem, "request_count", "Number of requests received", fieldKeys)
	errorCount := f.NewCounter(subsystem, "error_count", "Number of errors encountered", fieldKeys)
	requestLatency := f.NewSummary(subsystem, "request_latency_milliseconds", "Total duration of requests in milliseconds", fieldKeys)

	return NewServiceStatistics(requestCount, errorCount, requestLatency)
}

// NewServiceStatistics returns a new ServiceStatistics
func NewServiceStatistics(requestCount, errorCount kitmetrics.Counter, requestLatency kitmetrics.Histogram) ServiceStatistics {
	return &serviceStats{requestCount, errorCount, requestLatency}
}

func (s *serviceStats) Update(methodName string, begin time.Time, err error) {
	s.requestCount.With(methodField, methodName).Add(1)
	s.requestLatency.With(methodField, methodName).Observe(computeDuration(begin))
	if err != nil {
		s.errorCount.With(methodField, methodName).Add(1)
	}
}

// NewSummary creates and registers a Prometheus SummaryVec, and returns a Summary object.
func (f Factory) NewSummary(subsystem, name, help string, labelNames []string) *kitmetrics.Summary {
	sv := prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Namespace: f.namespace,
		Subsystem: CanonicalLabel(subsystem),
		Name:      CanonicalLabel(name),
		Help:      help,
		// See https://prometheus.io/docs/practices/histograms
		// keys are the quantiles, i.e percentile, and value is the error quantile
		// The φ-quantile is the observation value that ranks at number φ*N among the N observations.
		// Examples for φ-quantiles: The 0.5-quantile is known as the median. The 0.95-quantile is the 95th percentile.
		// The error of the quantile in a summary is configured in the dimension of φ. In our case we have
		// configured 0.95±0.01, i.e. the calculated value will be between the 94th and 96th percentile
		Objectives: map[float64]float64{0.5: 0.01, 0.75: 0.01, 0.95: 0.01, 0.99: 0.001, 0.999: 0.0001},
	}, CanonicalLabels(labelNames))
	f.Registry.MustRegister(sv)
	return kitmetrics.NewSummary(sv)
}

// NewCounter creates and registers a Prometheus CounterVec, and returns a Counter object.
func (f Factory) NewCounter(subsystem, name, help string, labelNames []string) kitmetrics.Counter {
	cv := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: f.namespace,
		Subsystem: CanonicalLabel(subsystem),
		Name:      CanonicalLabel(name),
		Help:      help,
	}, CanonicalLabels(labelNames))
	f.Registry.MustRegister(cv)
	return kitmetrics.NewCounter(cv)
}

func CanonicalLabels(labels []string) []string {
	formattedLabels := make([]string, 0, len(labels))
	for _, label := range labels {
		formattedLabels = append(formattedLabels, CanonicalLabel(label))
	}

	return formattedLabels
}

func computeDuration(begin time.Time) float64 {
	d := float64(time.Since(begin).Nanoseconds()) / float64(time.Millisecond)
	if d < 0 {
		d = 0
	}
	return d
}
