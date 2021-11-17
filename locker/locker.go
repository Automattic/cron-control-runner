package locker

import (
	"errors"
	"io"
)

var ErrAlreadyLocked = errors.New("already locked")

type Lockable interface {
	LockKey() string
}

type Locker interface {
	io.Closer
	Lock(Lockable) (Lock, error)
}

type Lock interface {
	Unlock()
}
