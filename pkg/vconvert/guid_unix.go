// +build linux darwin

package vconvert

import (
	"os"
	"syscall"
)

func fetchUIDandGID() (int, int) {

	// we need to check 3, because on mac it is the only of the std files
	// with the gid set to the user. the rest is owned by tty
	s, err := os.Lstat("/dev/fd/3")
	if err == nil {
		if stat, ok := s.Sys().(*syscall.Stat_t); ok {
			return int(stat.Uid), int(stat.Gid)
		}
	}

	return 1000, 1000

}
