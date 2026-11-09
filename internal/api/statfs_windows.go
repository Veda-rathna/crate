//go:build windows

package api

func diskStats(path string) (free, total uint64) {
	return 100_000_000_000, 500_000_000_000
}

