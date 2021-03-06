package metrics

import (
	"time"

	"github.com/Automattic/cron-control-runner/locker"
)

// Manager is the contract that metric implementations must follow.
type Manager interface {
	RecordGetSites(isSuccess bool, elapsed time.Duration)
	RecordGetSiteEvents(isSuccess bool, elapsed time.Duration, siteURL string, numEvents int)
	RecordRunEvent(isSuccess bool, elapsed time.Duration, siteURL string, reason string)
	RecordLockEvent(group locker.LockGroup, status string)
	RecordRunWorkerStats(currBusy int32, max int32)
	RecordFpmTiming(isSuccess bool, elapsed time.Duration)
	RecordSiteEventLag(url string, oldestEventTs time.Time)
}
