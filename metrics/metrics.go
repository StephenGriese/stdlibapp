package metrics

import (
	"database/sql"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/StephenGriese/stdlibapp/kitmetrics"
	"github.com/StephenGriese/stdlibapp/kitprometheus"
	stdprometheus "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	methodField = "method"
)

var fieldKeys = []string{methodField}

// A Factory is used to create Prometheus metrics
type Factory struct {
	namespace string
	Registry  *stdprometheus.Registry
}

// NewFactory will return a new Factory whose metrics will all be created under the given namespace. A new Prometheus
// Registry will be created for the factory as well. This will also register collectors to export process and Go GC
// metrics
func NewFactory(namespace string) Factory {
	r := stdprometheus.NewRegistry()
	r.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	r.MustRegister(collectors.NewGoCollector())

	return Factory{namespace: CanonicalLabel(namespace), Registry: r}
}

// NewCounter creates and registers a Prometheus CounterVec, and returns a Counter object.
func (f Factory) NewCounter(subsystem, name, help string, labelNames []string) *kitprometheus.Counter {
	cv := stdprometheus.NewCounterVec(stdprometheus.CounterOpts{
		Namespace: f.namespace,
		Subsystem: CanonicalLabel(subsystem),
		Name:      CanonicalLabel(name),
		Help:      help,
	}, CanonicalLabels(labelNames))
	f.Registry.MustRegister(cv)
	return kitprometheus.NewCounter(cv)
}

// NewGauge creates and registers a Prometheus GaugeVec, and returns a Gauge object.
func (f Factory) NewGauge(subsystem, name, help string, labelNames []string) *kitprometheus.Gauge {
	gv := stdprometheus.NewGaugeVec(stdprometheus.GaugeOpts{
		Namespace: f.namespace,
		Subsystem: CanonicalLabel(subsystem),
		Name:      CanonicalLabel(name),
		Help:      help,
	}, CanonicalLabels(labelNames))
	f.Registry.MustRegister(gv)
	return kitprometheus.NewGauge(gv)
}

// NewSummary creates and registers a Prometheus SummaryVec, and returns a Summary object.
func (f Factory) NewSummary(subsystem, name, help string, labelNames []string) *kitprometheus.Summary {
	sv := stdprometheus.NewSummaryVec(stdprometheus.SummaryOpts{
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
	return kitprometheus.NewSummary(sv)
}

// NewHistogram creates and registers a Prometheus HistogramVec and returns a Histogram object.
func (f Factory) NewHistogram(subsystem, name, help string, buckets []float64, labelNames []string) *kitprometheus.Histogram {
	hv := stdprometheus.NewHistogramVec(stdprometheus.HistogramOpts{
		Namespace: f.namespace,
		Subsystem: CanonicalLabel(subsystem),
		Name:      CanonicalLabel(name),
		Buckets:   buckets,
		Help:      help,
	}, CanonicalLabels(labelNames))
	f.Registry.MustRegister(hv)
	return kitprometheus.NewHistogram(hv)
}

// NewServiceStatistics creates and registers all of the metrics associated with a ServiceStatistics
func (f Factory) NewServiceStatistics(subsystem string) ServiceStatistics {
	requestCount := f.NewCounter(subsystem, "request_count", "Number of requests received", fieldKeys)
	errorCount := f.NewCounter(subsystem, "error_count", "Number of errors encountered", fieldKeys)
	requestLatency := f.NewSummary(subsystem, "request_latency_milliseconds", "Total duration of requests in milliseconds", fieldKeys)

	return NewServiceStatistics(requestCount, errorCount, requestLatency)
}

// HTTPHandlerFor will return a new http.Handler for exporting the metrics created via this Factory over HTTP
func (f Factory) HTTPHandlerFor() http.Handler {
	return promhttp.InstrumentMetricHandler(f.Registry, promhttp.HandlerFor(f.Registry, promhttp.HandlerOpts{}))
}

// A Timer is used to measure the duration of some activity. It will wrap a go-kit Summary
type Timer interface {
	Time() TimerContext
}
type timer struct {
	histogram kitmetrics.Histogram
}

// A TimerContext is used to measure one activity. They are created via the time method on a Timer
type TimerContext struct {
	begin     time.Time
	histogram kitmetrics.Histogram
}

// Time will return a new TimerContext that is used for timing the duration of some action. Users should call the
// Stop method on the context when the action is completed
func (t timer) Time() TimerContext {
	return TimerContext{begin: time.Now(), histogram: t.histogram}
}

// Stop will stop the timing of the action and record the observation with the histogram
func (tc TimerContext) Stop() {
	tc.histogram.Observe(computeDuration(tc.begin))
}

// NewSummaryBasedTimer will create and register new a Timer that is backed by a Prometheus Summary
func (f Factory) NewSummaryBasedTimer(subsystem, name, help string, labelNames []string) Timer {
	scrubbedName := name
	if !strings.HasSuffix(scrubbedName, "_milliseconds") {
		scrubbedName += "_milliseconds"
	}
	return timer{f.NewSummary(subsystem, scrubbedName, help, labelNames)}
}

// NewHistogramBasedTimer will create and register new a Timer that is backed by a Prometheus Histogram
func (f Factory) NewHistogramBasedTimer(subsystem, name, help string, buckets []float64, labelNames []string) Timer {
	scrubbedName := name
	if !strings.HasSuffix(scrubbedName, "_milliseconds") {
		scrubbedName += "_milliseconds"
	}
	return timer{f.NewHistogram(subsystem, scrubbedName, help, buckets, labelNames)}
}

func computeDuration(begin time.Time) float64 {
	d := float64(time.Since(begin).Nanoseconds()) / float64(time.Millisecond)
	if d < 0 {
		d = 0
	}
	return d
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

func (s *serviceStats) Update(methodName string, begin time.Time, err error) {
	s.requestCount.With(methodField, methodName).Add(1)
	s.requestLatency.With(methodField, methodName).Observe(computeDuration(begin))
	if err != nil {
		s.errorCount.With(methodField, methodName).Add(1)
	}
}

// NewServiceStatistics returns a new ServiceStatistics
func NewServiceStatistics(requestCount, errorCount kitmetrics.Counter, requestLatency kitmetrics.Histogram) ServiceStatistics {
	return &serviceStats{requestCount, errorCount, requestLatency}
}

// TrackDBStatistics tracks all of the metrics associated with a database
func (f Factory) TrackDBStatistics(db *sql.DB) {
	stats := newDBStatsCache(db)
	const subsystem = "database"

	inUse := f.newDBInUseGaugeFunc(subsystem, stats)
	idle := f.newDBIdleGaugeFunc(subsystem, stats)
	oc := f.newDBOpenConnectionsGaugeFunc(subsystem, stats)
	moc := f.newDBMaxOpenConnectionsCounterFunc(subsystem, stats)
	wc := f.newDBWaitCountCounterFunc(subsystem, stats)
	wd := f.newDBWaitDurationCounterFunc(subsystem, stats)
	mic := f.newDBMaxIdleClosedCounterFunc(subsystem, stats)
	mlc := f.newDBMaxLifetimeClosedCounterFunc(subsystem, stats)

	f.Registry.MustRegister(inUse, idle, oc, moc, wc, wd, mic, mlc)
}

func (f Factory) newDBInUseGaugeFunc(subsystem string, dbStats Statser) stdprometheus.GaugeFunc {
	return stdprometheus.NewGaugeFunc(stdprometheus.GaugeOpts{
		Namespace: f.namespace,
		Subsystem: subsystem,
		Name:      "in_use",
		Help:      "The number of connections currently in use.",
	}, func() float64 {
		return float64(dbStats.Stats().InUse)
	})
}

func (f Factory) newDBIdleGaugeFunc(subsystem string, dbStats Statser) stdprometheus.GaugeFunc {
	return stdprometheus.NewGaugeFunc(stdprometheus.GaugeOpts{
		Namespace: f.namespace,
		Subsystem: subsystem,
		Name:      "idle",
		Help:      "The number of idle connections.",
	}, func() float64 {
		return float64(dbStats.Stats().Idle)
	})
}

func (f Factory) newDBOpenConnectionsGaugeFunc(subsystem string, dbStats Statser) stdprometheus.GaugeFunc {
	return stdprometheus.NewGaugeFunc(stdprometheus.GaugeOpts{
		Namespace: f.namespace,
		Subsystem: subsystem,
		Name:      "open_connections",
		Help:      "The number of established connections both in use and idle.",
	}, func() float64 {
		return float64(dbStats.Stats().OpenConnections)
	})
}

func (f Factory) newDBMaxOpenConnectionsCounterFunc(subsystem string, dbStats Statser) stdprometheus.CounterFunc {
	return stdprometheus.NewCounterFunc(stdprometheus.CounterOpts{
		Namespace: f.namespace,
		Subsystem: subsystem,
		Name:      "max_open_connections",
		Help:      "The maximum number of open connections to the database.",
	}, func() float64 {
		return float64(dbStats.Stats().MaxOpenConnections)
	})
}

func (f Factory) newDBWaitCountCounterFunc(subsystem string, dbStats Statser) stdprometheus.CounterFunc {
	return stdprometheus.NewCounterFunc(stdprometheus.CounterOpts{
		Namespace: f.namespace,
		Subsystem: subsystem,
		Name:      "wait_count",
		Help:      "The total number of connections waited for.",
	}, func() float64 {
		return float64(dbStats.Stats().WaitCount)
	})
}

func (f Factory) newDBWaitDurationCounterFunc(subsystem string, dbStats Statser) stdprometheus.CounterFunc {
	return stdprometheus.NewCounterFunc(stdprometheus.CounterOpts{
		Namespace: f.namespace,
		Subsystem: subsystem,
		Name:      "wait_duration",
		Help:      "The total time blocked waiting for a new connection.",
	}, func() float64 {
		return float64(dbStats.Stats().WaitDuration)
	})
}

func (f Factory) newDBMaxIdleClosedCounterFunc(subsystem string, dbStats Statser) stdprometheus.CounterFunc {
	return stdprometheus.NewCounterFunc(stdprometheus.CounterOpts{
		Namespace: f.namespace,
		Subsystem: subsystem,
		Name:      "max_idle_closed",
		Help:      "The total number of connections closed due to SetMaxIdleConns.",
	}, func() float64 {
		return float64(dbStats.Stats().MaxIdleClosed)
	})
}

func (f Factory) newDBMaxLifetimeClosedCounterFunc(subsystem string, dbStats Statser) stdprometheus.CounterFunc {
	return stdprometheus.NewCounterFunc(stdprometheus.CounterOpts{
		Namespace: f.namespace,
		Subsystem: subsystem,
		Name:      "max_lifetime_closed",
		Help:      "The total number of connections closed due to SetConnMaxLifetime.",
	}, func() float64 {
		return float64(dbStats.Stats().MaxLifetimeClosed)
	})
}

// A Statser returns statistical information regarding a database
type Statser interface {
	Stats() sql.DBStats
}

type dbStatsCache struct {
	db              *sql.DB
	stats           sql.DBStats
	lastRefreshTime time.Time
	rw              *sync.RWMutex
}

func newDBStatsCache(db *sql.DB) *dbStatsCache {
	return &dbStatsCache{
		db:              db,
		rw:              &sync.RWMutex{},
		stats:           db.Stats(),
		lastRefreshTime: time.Now(),
	}
}

// Stats caches sql.DBStats for the specified refreshInterval
func (d *dbStatsCache) Stats() sql.DBStats {
	const refreshInterval = 1 * time.Second

	localStats, lrt := d.getCachedStatsAndLastRefreshTime()
	if time.Since(lrt) > refreshInterval {
		localStats = d.updateCachedStats()
	}

	return localStats
}

func (d *dbStatsCache) getCachedStatsAndLastRefreshTime() (sql.DBStats, time.Time) {
	d.rw.RLock()
	defer d.rw.RUnlock()

	return d.stats, d.lastRefreshTime
}

func (d *dbStatsCache) updateCachedStats() sql.DBStats {
	d.rw.Lock()
	defer d.rw.Unlock()

	d.stats = d.db.Stats()
	d.lastRefreshTime = time.Now()

	return d.stats
}
