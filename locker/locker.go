package locker

import (
	"io"
	"time"
)

type Locker interface {
	io.Closer
	Lock(string, time.Duration) (Lock, error)
}

type Lock interface {
	Unlock() error
}
