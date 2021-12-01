package locker

import (
	"io"
	"time"
)

type LockGroup string

const (
	GroupRunEvent  LockGroup = "run_event"
	GroupGetEvents LockGroup = "get_events"
)

type Locker interface {
	io.Closer
	Lock(LockGroup, string, time.Duration) (Lock, error)
}

type Lock interface {
	Unlock() error
}
