// Package observability exposes Prometheus metrics and OpenTelemetry
// tracing (M8).
package observability

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/trace"
)

// Metrics is the server's registry.
type Metrics struct {
	reg      *prometheus.Registry
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	builds   prometheus.Histogram
}

// Sources are read when Prometheus scrapes, so nothing has to push updates.
type Sources struct {
	IndexEntries   func() float64 // entries in the current snapshot
	IndexRhymable  func() float64
	IndexGen       func() float64
	CacheHits      func() float64
	CacheMisses    func() float64
	EventsRecorded func() float64
	EventsDropped  func() float64 // spec: events_dropped_total
	EventsFailed   func() float64
}

// New registers the standard Go/process collectors and Playword's metrics.
func New(src Sources) *Metrics {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	m := &Metrics{
		reg: reg,
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "playword_http_requests_total",
			Help: "HTTP requests by route pattern, method and status."}, []string{"route", "method", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "playword_http_request_duration_seconds",
			Help:    "HTTP request latency by route pattern.",
			Buckets: []float64{.0005, .001, .0025, .005, .01, .025, .05, .1, .25, .5, 1, 2.5}}, []string{"route"}),
		builds: prometheus.NewHistogram(prometheus.HistogramOpts{Name: "playword_index_build_seconds",
			Help: "Time to build an index snapshot.", Buckets: prometheus.ExponentialBuckets(0.5, 2, 8)}),
	}
	reg.MustRegister(m.requests, m.duration, m.builds)
	gauge := func(name, help string, f func() float64) {
		if f != nil {
			reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: name, Help: help}, f))
		}
	}
	counter := func(name, help string, f func() float64) {
		if f != nil {
			reg.MustRegister(prometheus.NewCounterFunc(prometheus.CounterOpts{Name: name, Help: help}, f))
		}
	}
	gauge("playword_index_entries", "Entries in the current index snapshot.", src.IndexEntries)
	gauge("playword_index_rhymable_entries", "Entries with rhyme keys in the current snapshot.", src.IndexRhymable)
	gauge("playword_index_generation", "Generation of the current index snapshot.", src.IndexGen)
	counter("playword_cache_hits_total", "Rhyme result cache hits.", src.CacheHits)
	counter("playword_cache_misses_total", "Rhyme result cache misses.", src.CacheMisses)
	counter("events_recorded_total", "Analytics events written.", src.EventsRecorded)
	counter("events_dropped_total", "Analytics events dropped because the buffer was full.", src.EventsDropped)
	counter("events_failed_total", "Analytics events lost to storage errors.", src.EventsFailed)
	return m
}

// ObserveBuild records how long an index build took.
func (m *Metrics) ObserveBuild(d time.Duration) { m.builds.Observe(d.Seconds()) }

// Handler serves /metrics.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{Registry: m.reg})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

type routeKey struct{}

// RouteCapture records the ServeMux pattern that matched, for Instrument and
// for the trace span name. Wrap the mux itself with it: middlewares between
// Instrument and the mux copy the request, so r.Pattern would not reach it.
func RouteCapture(mux http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r)
		if p, ok := r.Context().Value(routeKey{}).(*string); ok {
			*p = r.Pattern
		}
		if r.Pattern != "" {
			trace.SpanFromContext(r.Context()).SetName(r.Pattern)
		}
	})
}

// Instrument counts and times requests. The route label is the ServeMux
// pattern ("GET /word/{lang}/{text}"), never the raw path, so cardinality
// stays bounded.
func (m *Metrics) Instrument(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusRecorder{ResponseWriter: w}
		route := ""
		next.ServeHTTP(sw, r.WithContext(context.WithValue(r.Context(), routeKey{}, &route)))
		if route == "" {
			route = "unmatched"
		}
		if sw.status == 0 {
			sw.status = http.StatusOK
		}
		m.requests.WithLabelValues(route, r.Method, strconv.Itoa(sw.status)).Inc()
		m.duration.WithLabelValues(route).Observe(time.Since(start).Seconds())
	})
}
