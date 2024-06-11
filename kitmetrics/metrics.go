package kitmetrics

import (
	"github.com/StephenGriese/stdlibapp/kitmetrics/internal/lv"
	"github.com/prometheus/client_golang/prometheus"
)

// Counter describes a metric that accumulates values monotonically.
// An example of a counter is the number of received HTTP requests.
type Counter interface {
	With(labelValues ...string) Counter
	Add(delta float64)
}

// Histogram describes a metric that takes repeated observations of the same
// kind of thing, and produces a statistical summary of those observations,
// typically expressed as quantiles or buckets. An example of a histogram is
// HTTP request latencies.
type Histogram interface {
	With(labelValues ...string) Histogram
	Observe(value float64)
}

// counter implements Counter, via a Prometheus CounterVec.
type counter struct {
	cv  *prometheus.CounterVec
	lvs lv.LabelValues
}

// With implements Counter.
func (c *counter) With(labelValues ...string) Counter {
	return &counter{
		cv:  c.cv,
		lvs: c.lvs.With(labelValues...),
	}
}

// Add implements Counter.
func (c *counter) Add(delta float64) {
	c.cv.With(makeLabels(c.lvs...)).Add(delta)
}

func makeLabels(labelValues ...string) prometheus.Labels {
	labels := prometheus.Labels{}
	for i := 0; i < len(labelValues); i += 2 {
		labels[labelValues[i]] = labelValues[i+1]
	}
	return labels
}

// Summary implements Histogram, via a Prometheus SummaryVec. The difference
// between a Summary and a Histogram is that Summaries don't require predefined
// quantile buckets, but cannot be statistically aggregated.
type Summary struct {
	sv  *prometheus.SummaryVec
	lvs lv.LabelValues
}

// With implements Histogram.
func (s *Summary) With(labelValues ...string) Histogram {
	return &Summary{
		sv:  s.sv,
		lvs: s.lvs.With(labelValues...),
	}
}

// Observe implements Histogram.
func (s *Summary) Observe(value float64) {
	s.sv.With(makeLabels(s.lvs...)).Observe(value)
}

// NewSummary wraps the SummaryVec and returns a usable Summary object.
func NewSummary(sv *prometheus.SummaryVec) *Summary {
	return &Summary{
		sv: sv,
	}
}

// NewCounter wraps the CounterVec and returns a usable Counter object.
func NewCounter(cv *prometheus.CounterVec) *counter {
	return &counter{
		cv: cv,
	}
}

var _ Counter = &counter{}
