package metrics

import (
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

// NewFactory will return a new Factory whose metrics will all be created under the given namespace. A new Prometheus
// Registry will be created for the factory as well. This will also register collectors to export process and Go GC
// metrics
func NewFactory(namespace string) Factory {
	r := prometheus.NewRegistry()
	r.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	r.MustRegister(collectors.NewGoCollector())

	return Factory{namespace: canonicalLabel(namespace), Registry: r}
}

// A Factory is used to create Prometheus metrics
type Factory struct {
	namespace string
	Registry  *prometheus.Registry
}

// HTTPHandlerFor will return a new http.Handler for exporting the metrics created via this Factory over HTTP
func (f Factory) HTTPHandlerFor() http.Handler {
	return promhttp.InstrumentMetricHandler(f.Registry, promhttp.HandlerFor(f.Registry, promhttp.HandlerOpts{}))
}

// NewServiceStatistics creates and registers all of the metrics associated with a ServiceStatistics
func (f Factory) NewServiceStatistics(subsystem string) ServiceStatistics {
	requestCount := f.newCounter(subsystem, "request_count", "Number of requests received", fieldKeys)
	errorCount := f.newCounter(subsystem, "error_count", "Number of errors encountered", fieldKeys)
	requestLatency := f.newSummary(subsystem, "request_latency_milliseconds", "Total duration of requests in milliseconds", fieldKeys)

	return NewServiceStatistics(requestCount, errorCount, requestLatency)
}

// newSummary creates and registers a Prometheus SummaryVec, and returns a summary object.
func (f Factory) newSummary(subsystem, name, help string, labelNames []string) *summary {
	sv := prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Namespace: f.namespace,
		Subsystem: canonicalLabel(subsystem),
		Name:      canonicalLabel(name),
		Help:      help,
		// See https://prometheus.io/docs/practices/histograms
		// keys are the quantiles, i.e percentile, and value is the error quantile
		// The φ-quantile is the observation value that ranks at number φ*N among the N observations.
		// Examples for φ-quantiles: The 0.5-quantile is known as the median. The 0.95-quantile is the 95th percentile.
		// The error of the quantile in a summary is configured in the dimension of φ. In our case we have
		// configured 0.95±0.01, i.e. the calculated value will be between the 94th and 96th percentile
		Objectives: map[float64]float64{0.5: 0.01, 0.75: 0.01, 0.95: 0.01, 0.99: 0.001, 0.999: 0.0001},
	}, canonicalLabels(labelNames))
	f.Registry.MustRegister(sv)
	return newSummary(sv)
}

// newPrometheusBasedCounter creates and registers a Prometheus CounterVec, and returns a counter object.
func (f Factory) newCounter(subsystem, name, help string, labelNames []string) counter {
	cv := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: f.namespace,
		Subsystem: canonicalLabel(subsystem),
		Name:      canonicalLabel(name),
		Help:      help,
	}, canonicalLabels(labelNames))
	f.Registry.MustRegister(cv)
	return newPrometheusBasedCounter(cv)
}

// ServiceStatistics are meant to be used as middle-ware to measure "service-level" metrics broken out by method. It will
// include the number of requests
type ServiceStatistics interface {
	Update(methodName string, begin time.Time, err error)
}

// NewServiceStatistics returns a new ServiceStatistics
func NewServiceStatistics(requestCount, errorCount counter, requestLatency histogram) ServiceStatistics {
	return &serviceStats{requestCount, errorCount, requestLatency}
}

type serviceStats struct {
	requestCount   counter
	errorCount     counter
	requestLatency histogram
}

func (s *serviceStats) Update(methodName string, begin time.Time, err error) {
	s.requestCount.With(methodField, methodName).Add(1)
	s.requestLatency.With(methodField, methodName).Observe(computeDuration(begin))
	if err != nil {
		s.errorCount.With(methodField, methodName).Add(1)
	}
}

// counter describes a metric that accumulates values monotonically.
// An example of a counter is the number of received HTTP requests.
type counter interface {
	With(labelValues ...string) counter
	Add(delta float64)
}

// newPrometheusBasedCounter wraps the CounterVec and returns a usable counter object.
func newPrometheusBasedCounter(cv *prometheus.CounterVec) *prometheusBasedCounter {
	return &prometheusBasedCounter{
		cv: cv,
	}
}

// counter implements counter, via a Prometheus CounterVec.
type prometheusBasedCounter struct {
	cv  *prometheus.CounterVec
	lvs labelValues
}

// With implements counter.
func (c *prometheusBasedCounter) With(labelValues ...string) counter {
	return &prometheusBasedCounter{
		cv:  c.cv,
		lvs: c.lvs.with(labelValues...),
	}
}

// Add implements counter.
func (c *prometheusBasedCounter) Add(delta float64) {
	c.cv.With(makeLabels(c.lvs...)).Add(delta)
}

// newSummary wraps the SummaryVec and returns a usable summary object.
func newSummary(sv *prometheus.SummaryVec) *summary {
	return &summary{
		sv: sv,
	}
}

// histogram describes a metric that takes repeated observations of the same
// kind of thing, and produces a statistical summary of those observations,
// typically expressed as quantiles or buckets. An example of a histogram is
// HTTP request latencies.
type histogram interface {
	With(labelValues ...string) histogram
	Observe(value float64)
}

// summary implements histogram, via a Prometheus SummaryVec. The difference
// between a summary and a histogram is that Summaries don't require predefined
// quantile buckets, but cannot be statistically aggregated.
type summary struct {
	sv  *prometheus.SummaryVec
	lvs labelValues
}

// With implements histogram.
func (s *summary) With(labelValues ...string) histogram {
	return &summary{
		sv:  s.sv,
		lvs: s.lvs.with(labelValues...),
	}
}

// Observe implements histogram.
func (s *summary) Observe(value float64) {
	s.sv.With(makeLabels(s.lvs...)).Observe(value)
}

func makeLabels(labelValues ...string) prometheus.Labels {
	labels := prometheus.Labels{}
	for i := 0; i < len(labelValues); i += 2 {
		labels[labelValues[i]] = labelValues[i+1]
	}
	return labels
}

// labelValues is a type alias that provides validation on its with method.
// Metrics may include it as a member to help them satisfy with semantics and
// save some code duplication.
type labelValues []string

// with validates the input, and returns a new aggregate labelValues.
func (lvs labelValues) with(labelValues ...string) labelValues {
	if len(labelValues)%2 != 0 {
		labelValues = append(labelValues, "unknown")
	}
	return append(lvs, labelValues...)
}

func canonicalLabels(labels []string) []string {
	formattedLabels := make([]string, 0, len(labels))
	for _, label := range labels {
		formattedLabels = append(formattedLabels, canonicalLabel(label))
	}

	return formattedLabels
}

func canonicalLabel(s string) string {
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

func computeDuration(begin time.Time) float64 {
	d := float64(time.Since(begin).Nanoseconds()) / float64(time.Millisecond)
	if d < 0 {
		d = 0
	}
	return d
}
