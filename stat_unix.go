//go:build unix

package deviceid

import (
	"io/fs"
	"syscall"
)

// inode returns the file's inode number, used together with mtime
// to detect database file changes.
func inode(fi fs.FileInfo) uint64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Ino)
	}
	return 0
}
