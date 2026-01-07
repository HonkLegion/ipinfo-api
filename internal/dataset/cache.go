package dataset

import (
	"os"
	"time"
)

func CacheFresh(path string, maxAge time.Duration) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) <= maxAge
}
