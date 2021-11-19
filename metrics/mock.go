package metrics

import (
	"log"
	"time"
)

var _ Manager = Mock{}
// Mock is a mock implementation of a Metrics Manager.
type Mock struct {
	Log bool
}

func (m Mock) RecordSiteEventLag(url string, oldestEventTs time.Time) {
	if m.Log {
		log.Printf("metrics: RecordSiteEventLag(url: %s, oldestEventTs: %v)", url, oldestEventTs)
	}
}

func (m Mock) RecordLockEvent(status string) {
	if m.Log {
		log.Printf("metrics: RecordLockEvent(status: %s)", status)
	}
}

// RecordGetSites tracks successful performer.GetSites() calls.
func (m Mock) RecordGetSites(isSuccess bool, elapsed time.Duration) {
	if m.Log {
		log.Printf("metrics: RecordGetSites(isSuccess: %t, elapsed: %s)", isSuccess, elapsed)
	}
}

// RecordGetSiteEvents tracks successful performer.GetEvents() calls.
func (m Mock) RecordGetSiteEvents(isSuccess bool, elapsed time.Duration, siteURL string, numEvents int) {
	if m.Log {
		log.Printf("metrics: RecordGetSiteEvents(isSuccess: %t, elapsed: %s, siteURL: %s, numEvents: %d)", isSuccess, elapsed, siteURL, numEvents)
	}
}

// RecordRunEvent tracks successful performer.runEvent() calls.
func (m Mock) RecordRunEvent(isSuccess bool, elapsed time.Duration, siteURL string, reason string) {
	if m.Log {
		log.Printf("metrics: RecordRunEvent( isSuccess: %t, elapsed: %s, siteURL: %s, reason: %s)", isSuccess, elapsed, siteURL, reason)
	}
}

// RecordRunWorkerStats keeps track of runWorker activity.
func (m Mock) RecordRunWorkerStats(currBusy int32, max int32) {
	if m.Log {
		log.Printf("metrics: RecordRunWorkerStats(currBusy: %d, max: %d)", currBusy, max)
	}
}

// RecordFpmTiming track FPM CLI calls.
func (m Mock) RecordFpmTiming(isSuccess bool, elapsed time.Duration) {
	if m.Log {
		log.Printf("metrics: RecordFpmTiming( isSuccess: %t, elapsed: %s,)", isSuccess, elapsed)
	}
}
