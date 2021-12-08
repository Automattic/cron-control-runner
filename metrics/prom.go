package metrics

import (
	"log"
	"net/http"
	"time"

	"github.com/Automattic/cron-control-runner/locker"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var _ Manager = &Prom{}

// Prom tracks metrics via prometheus
// Initialize with NewProm()
type Prom struct {
	histGetSitesLatency           *prometheus.HistogramVec
	histGetSiteEventsLatency      *prometheus.HistogramVec
	ctrGetSiteEventsReceivedTotal *prometheus.CounterVec
	histRunEventLatency           *prometheus.HistogramVec
	gaugeOldestEventTs            *prometheus.GaugeVec
	gaugeOldestEventAge           *prometheus.GaugeVec
	ctrLockerEventsTotal          *prometheus.CounterVec
	histWpcliStatMaxRSS           *prometheus.HistogramVec
	histWpcliStatCPUTime          *prometheus.HistogramVec
	histFpmCallDurationSeconds    *prometheus.HistogramVec
	gaugeRunWorkerStateCount      *prometheus.GaugeVec
	gaugeRunWorkerBusyPct         prometheus.Gauge
	ctrRunWorkersAllBusyHits      prometheus.Counter
}

// NewProm configures the Prom metrics manager.
func NewProm() *Prom {
	promMetrics := &Prom{}
	promMetrics.initializeMetrics()
	return promMetrics
}

// StartPromServer starts up metrics-related endpoints and listeners.
func StartPromServer(metricsAddress string) {
	http.Handle("/metrics", promhttp.Handler())

	log.Printf("Listening for metrics on %q", metricsAddress)
	err := http.ListenAndServe(metricsAddress, nil)
	log.Printf("Metrics server terminated: %v", err)
}

// RecordGetSites tracks successful performer.GetSites() calls.
func (p *Prom) RecordGetSites(isSuccess bool, elapsed time.Duration) {
	p.histGetSitesLatency.With(makeLabels(isSuccess)).Observe(elapsed.Seconds())
}

// RecordGetSiteEvents tracks successful performer.GetEvents() calls.
func (p *Prom) RecordGetSiteEvents(isSuccess bool, elapsed time.Duration, siteURL string, numEvents int) {
	siteLabel := prometheus.Labels{"site": siteURL}
	p.histGetSiteEventsLatency.With(makeLabels(isSuccess, siteLabel)).Observe(elapsed.Seconds())
	if numEvents > 0 {
		p.ctrGetSiteEventsReceivedTotal.With(siteLabel).Add(float64(numEvents))
	}
}

// RecordRunEvent tracks successful performer.runEvent() calls.
func (p *Prom) RecordRunEvent(isSuccess bool, elapsed time.Duration, siteURL string, reason string) {
	if siteURL == "" {
		siteURL = "unknown"
	}
	if reason == "" {
		reason = "unknown"
	}
	p.histRunEventLatency.With(makeLabels(isSuccess, prometheus.Labels{
		"site_url": siteURL,
		"reason":   reason,
	})).Observe(elapsed.Seconds())
}

func (p *Prom) RecordLockEvent(group locker.LockGroup, status string) {
	p.ctrLockerEventsTotal.With(prometheus.Labels{
		"group":  string(group),
		"status": status,
	}).Inc()
}

// RecordRunWorkerStats keeps track of runWorker activity.
func (p *Prom) RecordRunWorkerStats(currBusy int32, max int32) {
	p.gaugeRunWorkerStateCount.With(prometheus.Labels{"state": "max"}).Set(float64(max))
	p.gaugeRunWorkerStateCount.With(prometheus.Labels{"state": "busy"}).Set(float64(currBusy))
	p.gaugeRunWorkerStateCount.With(prometheus.Labels{"state": "idle"}).Set(float64(max - currBusy))
	if max > 0 {
		p.gaugeRunWorkerBusyPct.Set(float64(currBusy) / float64(max))
	}
	if currBusy >= max {
		// all workers are busy right now. increment:
		p.ctrRunWorkersAllBusyHits.Inc()
	}
}

// RecordFpmTiming track FPM CLI calls.
func (p *Prom) RecordFpmTiming(isSuccess bool, elapsed time.Duration) {
	var labels prometheus.Labels
	if isSuccess {
		labels = prometheus.Labels{"status": "success"}
	} else {
		labels = prometheus.Labels{"status": "error"}
	}
	p.histFpmCallDurationSeconds.With(labels).Observe(elapsed.Seconds())
}

func (p *Prom) RecordSiteEventLag(url string, oldestEventTs time.Time) {
	if url == "" {
		url = "unknown"
	}
	ageMs := time.Since(oldestEventTs).Milliseconds()
	if ageMs < 0 {
		ageMs = 0
	}
	labels := prometheus.Labels{"site_url": url}
	p.gaugeOldestEventTs.With(labels).Set(float64(oldestEventTs.Unix()))
	p.gaugeOldestEventAge.With(labels).Set(float64(ageMs))
}

func (p *Prom) initializeMetrics() {
	const metricNamespace = "cron_control_runner"

	log.Printf("Initializing metrics")

	p.histGetSitesLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metricNamespace,
		Subsystem: "get_sites",
		Name:      "latency_seconds",
		Help:      "Histogram of time taken to enumerate sites",
		Buckets:   []float64{.1, .25, .5, 1, 2.5, 5, 10},
	}, []string{"status"})

	p.histGetSiteEventsLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metricNamespace,
		Subsystem: "get_site_events",
		Name:      "latency_seconds",
		Help:      "Histogram of time taken to enumerate events for a site",
		Buckets:   []float64{.1, .25, .5, 1, 2.5, 5, 10},
	}, []string{"site", "status"})

	p.ctrGetSiteEventsReceivedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricNamespace,
		Subsystem: "get_site_events",
		Name:      "events_received_total",
		Help:      "Number of events retrieved by site",
	}, []string{"site"})

	p.gaugeOldestEventTs = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricNamespace,
		Subsystem: "get_site_events",
		Name:      "oldest_event_timestamp",
		Help:      "Timestamp of the oldest event received in the last batch for this site",
	}, []string{"site_url"})

	p.gaugeOldestEventAge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricNamespace,
		Subsystem: "get_site_events",
		Name:      "oldest_event_age_ms",
		Help:      "Age in milliseconds of the oldest event received in the last batch for this site",
	}, []string{"site_url"})

	p.histRunEventLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metricNamespace,
		Subsystem: "run_event",
		Name:      "latency_seconds",
		Help:      "Histogram of time taken to run events",
		Buckets:   []float64{.1, .25, .5, 1, 2.5, 5, 10, 30, 60, 90},
	}, []string{"site_url", "status", "reason"})

	p.ctrLockerEventsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricNamespace,
		Subsystem: "locker",
		Name:      "events_total",
		Help:      "Number of events locked",
	}, []string{"group", "status"})

	p.histWpcliStatMaxRSS = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metricNamespace,
		Subsystem: "wpcli_stat",
		Name:      "maxrss_mb",
		Help:      "MaxRSS (in MiB) of invoked wp-cli commands",
		Buckets:   []float64{2, 4, 8, 16, 32, 64, 128, 256, 512, 768, 1024},
	}, []string{"status"})

	p.histWpcliStatCPUTime = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metricNamespace,
		Subsystem: "wpcli_stat",
		Name:      "cputime_seconds",
		Help:      "CPU time (in seconds) of invoked wp-cli commands",
		Buckets:   []float64{.1, .25, .5, 1, 2.5, 5, 10, 30, 60, 90},
	}, []string{"cpu_mode", "status"})

	p.histFpmCallDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metricNamespace,
		Subsystem: "fpm_client",
		Name:      "call_duration_seconds",
		Help:      "Wall time of full call to backend FPM",
		Buckets:   prometheus.DefBuckets,
	}, []string{"status"})

	p.gaugeRunWorkerStateCount = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricNamespace,
		Subsystem: "run_worker",
		Name:      "state_count",
		Help:      "Breakdown of run-workers by state",
	}, []string{"state"})

	p.gaugeRunWorkerBusyPct = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: metricNamespace,
		Subsystem: "run_worker",
		Name:      "busy_pct",
		Help:      "Instantaneous percentage of busy workers",
	})

	p.ctrRunWorkersAllBusyHits = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricNamespace,
		Subsystem: "run_worker",
		Name:      "all_busy_hits",
		Help:      "Number of times all workers have been instantaneously saturated",
	})
}

func makeLabels(isSuccess bool, labels ...prometheus.Labels) prometheus.Labels {
	lenSum := 1
	for _, ls := range labels {
		lenSum += len(ls)
	}
	r := make(prometheus.Labels, lenSum)
	for _, ls := range labels {
		for k, v := range ls {
			r[k] = v
		}
	}
	if isSuccess {
		r["status"] = "success"
	} else {
		r["status"] = "failure"
	}
	return r
}
