package locker

import (
	"io"
)

type Locker interface {
	io.Closer
	Lock(string) (Lock, error)
}

type Lock interface {
	Unlock() error
}
