//go:build !windows

package autoupdate

import (
	"syscall"
)

// getAvailableDiskSpaceMB returns the available disk space in MB for the given path.
func getAvailableDiskSpaceMB(path string) (int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	// Available blocks * block size / 1MB
	return int64(stat.Bavail) * int64(stat.Bsize) / (1024 * 1024), nil
}
