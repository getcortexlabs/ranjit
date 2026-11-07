//go:build !windows

package filelock

import (
	"golang.org/x/sys/unix"
	"os"
)

func lock(f *os.File, exclusive bool) error {
	mode := unix.LOCK_SH
	if exclusive {
		mode = unix.LOCK_EX
	}
	return unix.Flock(int(f.Fd()), mode|unix.LOCK_NB)
}
