package core

import "golang.org/x/sys/windows"

func nativeFreeSpace(path string) int64 {
	value, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return -1
	}
	var available, total, free uint64
	if windows.GetDiskFreeSpaceEx(value, &available, &total, &free) != nil {
		return -1
	}
	return int64(available)
}
