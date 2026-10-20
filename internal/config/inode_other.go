//go:build !linux

package config

import "os"

// inode leaves file identity out of the stamp on other systems; the watcher
// compares size and modification time instead (CFG-06, NFR-11, ADR 0013).
func inode(os.FileInfo) uint64 {
	return 0
}
