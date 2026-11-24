//go:build !windows

package market

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func tryProjectLock(f *os.File) (bool, error) {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EINTR) {
		return false, nil
	}
	return false, err
}

func unlockProjectFile(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_UN)
}
