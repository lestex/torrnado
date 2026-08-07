//go:build linux || darwin

package engine

import "golang.org/x/sys/unix"

func diskUsage(path string) (free, total int64, err error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, 0, err
	}
	free = int64(stat.Bavail) * int64(stat.Bsize)
	total = int64(stat.Blocks) * int64(stat.Bsize)
	return free, total, nil
}
