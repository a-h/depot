package storage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Storage interface abstracts file storage operations for reading and writing.
type Storage interface {
	// Read opens a file for reading and returns a ReadCloser and whether the file exists.
	Read(filename string) (io.ReadCloser, bool, error)

	// Write creates or overwrites a file with content from the ReadCloser.
	Write(filename string, data io.ReadCloser) error
}

// FileSystem implements Storage using the local filesystem.
type FileSystem struct {
	basePath string
}

// NewFileSystem creates a new FileSystem storage backend.
func NewFileSystem(basePath string) *FileSystem {
	return &FileSystem{
		basePath: basePath,
	}
}

func (fs *FileSystem) Read(filename string) (io.ReadCloser, bool, error) {
	fullPath := filepath.Join(fs.basePath, filename)
	file, err := os.Open(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return file, true, nil
}

func (fs *FileSystem) Write(filename string, data io.ReadCloser) error {
	defer data.Close()

	fullPath := filepath.Join(fs.basePath, filename)

	// Create directory structure.
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Create and write file.
	file, err := os.Create(fullPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	_, err = io.Copy(file, data)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}
