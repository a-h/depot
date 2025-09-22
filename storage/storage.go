package storage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Storage interface abstracts file storage operations for reading and writing.
type Storage interface {
	Stat(filename string) (size int64, exists bool, err error)
	Get(filename string) (r io.ReadCloser, exists bool, err error)
	Put(filename string) (w io.WriteCloser, err error)
}

var _ Storage = (*FileSystem)(nil)

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

func (fs *FileSystem) Stat(filename string) (size int64, exists bool, err error) {
	fullPath := filepath.Join(fs.basePath, filename)
	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, false, nil
		}
		return 0, false, err
	}
	return info.Size(), true, nil
}

func (fs *FileSystem) Get(filename string) (r io.ReadCloser, exists bool, err error) {
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

func (fs *FileSystem) Put(filename string) (w io.WriteCloser, err error) {
	fullPath := filepath.Join(fs.basePath, filename)

	// Create directory structure.
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	// Create.
	file, err := os.Create(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create file: %w", err)
	}

	return file, nil
}
