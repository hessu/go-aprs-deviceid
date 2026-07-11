//go:build windows

package deviceid

import "io/fs"

// inode returns 0 on Windows; change detection relies on mtime alone.
func inode(fi fs.FileInfo) uint64 {
	return 0
}
