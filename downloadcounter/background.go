package downloadcounter

import (
	"context"
	"log/slog"
	"sync"

	"github.com/a-h/depot/metrics"
	"github.com/a-h/kv"
)

type DownloadEvent struct {
	Group string
	Name  string
}

func NewBufferedCounter(ctx context.Context, log *slog.Logger, store kv.Store, metrics metrics.Metrics, bufferSize int) (counter chan DownloadEvent, shutdown func()) {
	counter = make(chan DownloadEvent, bufferSize)

	var wg sync.WaitGroup
	wg.Go(func() {
		c := New(store)
		for event := range counter {
			log.Debug("recording download", "group", event.Group, "name", event.Name)
			if err := c.Increment(ctx, event.Group, event.Name); err != nil {
				log.Error("failed to record download", slog.String("group", event.Group), slog.String("name", event.Name), slog.Any("error", err))
				metrics.IncrementDownloadCounterErrors(ctx, event.Group)
			}
		}
	})

	shutdown = func() {
		close(counter)
		wg.Wait()
	}

	return counter, shutdown
}
