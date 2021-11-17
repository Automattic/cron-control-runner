package locker

import (
	"errors"
	"io"
)

var ErrAlreadyLocked = errors.New("already locked")

type Locker interface {
	io.Closer
	Lock(id string) (Lock, error)
}

type Lock interface {
	Unlock()
}
