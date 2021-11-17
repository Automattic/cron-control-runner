package metrics

import (
	"time"
)

// Manager is the contract that metric implementations must follow.
type Manager interface {
	RecordGetSites(isSuccess bool, elapsed time.Duration)
	RecordGetSiteEvents(isSuccess bool, elapsed time.Duration, siteURL string, numEvents int)
	RecordRunEvent(isSuccess bool, elapsed time.Duration, siteURL string, reason string)
	RecordLockEvent(status string)
	RecordRunWorkerStats(currBusy int32, max int32)
	RecordFpmTiming(isSuccess bool, elapsed time.Duration)
}
