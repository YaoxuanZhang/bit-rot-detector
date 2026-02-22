//go:build !windows

package coordinator

import (
	"fmt"
	"syscall"
)

type syscallStatFS struct {
	Total uint64
	Used  uint64
	Free  uint64
}

func statFS(path string, out *syscallStatFS) error {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return fmt.Errorf("statfs %s: %w", path, err)
	}
	out.Total = stat.Blocks * uint64(stat.Bsize)
	out.Free = stat.Bfree * uint64(stat.Bsize)
	out.Used = out.Total - out.Free
	return nil
}
