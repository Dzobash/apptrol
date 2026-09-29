package config

import (
	"os"
	"syscall"
)

// inode returns the file's inode number, so replacing a file by renaming a
// new one over it counts as a change even when size and time are equal.
func inode(fi os.FileInfo) uint64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return st.Ino
	}
	return 0
}
