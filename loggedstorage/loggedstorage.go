package loggedstorage

import (
	"context"
	"io"
	"log/slog"
	"time"

	"github.com/a-h/depot/accesslog"
	"github.com/a-h/depot/metrics"
	"github.com/a-h/depot/storage"
)

func New(ctx context.Context, log *slog.Logger, wrapped storage.Storage, accessLog *accesslog.AccessLog, m metrics.Metrics) (s *LoggedStorage, shutdown func(timeout time.Duration) error) {
	s = &LoggedStorage{
		wrapped: wrapped,
	}
	s.c, shutdown = newBufferedAccessLog(ctx, log, accessLog, m, 2048)
	return s, shutdown
}

var _ storage.Storage = &LoggedStorage{}

type LoggedStorage struct {
	wrapped storage.Storage
	c       chan event
}

func (ls *LoggedStorage) Stat(ctx context.Context, filename string) (size int64, exists bool, err error) {
	size, exists, err = ls.wrapped.Stat(ctx, filename)
	if err != nil {
		return size, exists, err
	}
	ls.c <- newEvent(filename, eventTypeRead)
	return size, exists, err
}

func (ls *LoggedStorage) Get(ctx context.Context, filename string) (r io.ReadCloser, exists bool, err error) {
	r, exists, err = ls.wrapped.Get(ctx, filename)
	if err != nil {
		return r, exists, err
	}
	ls.c <- newEvent(filename, eventTypeRead)
	return r, exists, err
}

func (ls *LoggedStorage) Put(ctx context.Context, filename string) (w io.WriteCloser, err error) {
	w, err = ls.wrapped.Put(ctx, filename)
	if err != nil {
		return w, err
	}
	ls.c <- newEvent(filename, eventTypeWrite)
	return w, err
}
