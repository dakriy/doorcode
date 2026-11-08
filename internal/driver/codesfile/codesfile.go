// Package codesfile implements connector.Codes over a file on disk.
package codesfile

import "os"

// File reads the door-code list from Path.
type File struct {
	Path string
}

// Load returns the file's contents as the filesystem reports them.
func (f File) Load() ([]byte, error) {
	return os.ReadFile(f.Path)
}
