//go:build !unix

package system

func freeSpace(string) (int64, bool) { return 0, false }
