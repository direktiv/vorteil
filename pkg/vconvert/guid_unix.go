// +build linux darwin

package vconvert

import (
	"os"
	"runtime"
	"syscall"
)

func fetchUIDandGID() (int, int) {

	if runtime.GOOS == "darwin" {
		return 501, 20
	}

	s, err := os.Lstat("/dev/fd/3")
	if err == nil {
		if stat, ok := s.Sys().(*syscall.Stat_t); ok {
			return int(stat.Uid), int(stat.Gid)
		}
	}

	return 1000, 1000

}
