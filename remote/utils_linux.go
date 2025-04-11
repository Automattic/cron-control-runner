//go:build linux

package remote

import (
	"golang.org/x/sys/unix"
)

func setPtyToIgnoreCR(fd int) error {
	termios, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err == nil {
		termios.Iflag |= unix.IGNCR
		return unix.IoctlSetTermios(fd, unix.TCSETS, termios)
	}
	return err
}
