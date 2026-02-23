package loggedstorage

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/a-h/depot/accesslog"
	"github.com/a-h/depot/metrics"
)

func newEvent(filename string, t eventType) event {
	return event{
		Filename: filename,
		Type:     t,
	}
}

type event struct {
	Filename string
	Type     eventType
}

type eventType string

const (
	eventTypeRead   eventType = "read"
	eventTypeWrite  eventType = "write"
	eventTypeDelete eventType = "delete"
)

func newBufferedAccessLog(ctx context.Context, log *slog.Logger, accessLog *accesslog.AccessLog, metrics metrics.Metrics, bufferSize int) (c chan event, shutdown func(timeout time.Duration) error) {
	c = make(chan event, bufferSize)
	shutdownComplete := make(chan struct{}, 1)

	go func() {
		defer func() {
			shutdownComplete <- struct{}{}
		}()
		for event := range c {
			log.Debug("logging access", slog.Any("event", event))
			var err error
			switch event.Type {
			case eventTypeRead:
				err = accessLog.Read(ctx, event.Filename)
			case eventTypeWrite:
				err = accessLog.Write(ctx, event.Filename)
			case eventTypeDelete:
				err = accessLog.Write(ctx, event.Filename)
			default:
				err = fmt.Errorf("unknown event type: %v", event.Type)
			}
			if err != nil {
				log.Error("failed to log access", slog.Any("event", event), slog.Any("error", err))
				metrics.IncrementAccessLogErrors(ctx)
			}
		}
	}()

	shutdown = func(timeout time.Duration) error {
		close(c)
		select {
		case <-time.Tick(timeout):
			return fmt.Errorf("timed out waiting for events to complete")
		case <-shutdownComplete:
			return nil
		}
	}

	return c, shutdown
}
