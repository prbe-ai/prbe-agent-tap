//go:build windows

package watch

import "os"

// Windows: no inode concept; use a constant.
// File rotation/truncation detection on Windows is best-effort via size comparison only.
func inodeOf(_ os.FileInfo) uint64 { return 0 }
