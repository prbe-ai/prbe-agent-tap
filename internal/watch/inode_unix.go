//go:build linux || darwin

package watch

import (
	"os"
	"syscall"
)

func inodeOf(info os.FileInfo) uint64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Ino)
	}
	return 0
}
