//go:build !windows

package core

import "golang.org/x/sys/unix"

func nativeFreeSpace(path string) int64 {
	var value unix.Statfs_t
	if unix.Statfs(path, &value) != nil {
		return -1
	}
	return int64(value.Bavail) * int64(value.Bsize)
}
