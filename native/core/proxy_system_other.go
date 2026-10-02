//go:build !windows

package core

func platformSystemProxy() nativeSystemProxy { return nativeSystemProxy{} }
