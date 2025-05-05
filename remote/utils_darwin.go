//go:build darwin

package remote

import (
	"golang.org/x/sys/unix"
)

const (
	TCGETS = 0x40487413
	TCSETS = 0x80487414
)

func setPtyToIgnoreCR(fd int) error {
	termios, err := unix.IoctlGetTermios(fd, TCGETS)
	if err == nil {
		termios.Iflag |= unix.IGNCR
		return unix.IoctlSetTermios(fd, TCSETS, termios)
	}
	return err
}
