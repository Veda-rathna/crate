//go:build !windows

package api

import "syscall"

func diskStats(path string) (free, total uint64) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 100_000_000_000, 500_000_000_000
	}
	return stat.Bavail * uint64(stat.Bsize), stat.Blocks * uint64(stat.Bsize)
}

