package accesslog

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/a-h/kv"
)

func New(store kv.Store) *AccessLog {
	return &AccessLog{
		store: store,
		now:   time.Now,
	}
}

type AccessLog struct {
	store kv.Store
	now   func() time.Time
}

func (m *AccessLog) Read(ctx context.Context, filename string) (err error) {
	day := m.now().UTC().Truncate(24 * time.Hour).Format("2006-01-02")
	encodedFilename := url.PathEscape(filename)
	key := path.Join("/accesslog", encodedFilename, day, "r")
	// Every time we upsert a key with Put, the version number is incremented.
	return m.store.Put(ctx, key, -1, "")
}

func (m *AccessLog) Write(ctx context.Context, filename string) (err error) {
	day := m.now().UTC().Truncate(24 * time.Hour).Format("2006-01-02")
	encodedFilename := url.PathEscape(filename)
	key := path.Join("/accesslog", encodedFilename, day, "w")
	return m.store.Put(ctx, key, -1, "")
}

func (m *AccessLog) Delete(ctx context.Context, filename string) (err error) {
	day := m.now().UTC().Truncate(24 * time.Hour).Format("2006-01-02")
	encodedFilename := url.PathEscape(filename)
	key := path.Join("/accesslog", encodedFilename, day, "d")
	return m.store.Put(ctx, key, -1, "")
}

func (m *AccessLog) Get(ctx context.Context, filename string) (stats Stats, ok bool, err error) {
	stats.Filename = filename
	prefix := path.Join("/accesslog", url.PathEscape(filename)) + "/"

	rows, err := m.store.GetPrefix(ctx, prefix, 0, -1)
	if err != nil {
		return stats, false, err
	}

	for _, row := range rows {
		parts := strings.Split(strings.TrimPrefix(row.Key, "/"), "/")
		if len(parts) != 4 {
			return stats, false, fmt.Errorf("invalid key format: %s", row.Key)
		}
		var count Count
		count.Date, err = time.Parse("2006-01-02", parts[2])
		if err != nil {
			return stats, false, fmt.Errorf("failed to parse date in key %q: %w", row.Key, err)
		}
		count.Count = row.Version

		switch parts[3] {
		case "r":
			stats.Reads = append(stats.Reads, count)
		case "w":
			stats.Writes = append(stats.Writes, count)
		case "d":
			stats.Deletes = append(stats.Deletes, count)
		default:
			return stats, false, fmt.Errorf("invalid action in key: %s", row.Key)
		}

		ok = true
	}

	return stats, ok, nil
}

type Stats struct {
	Filename string
	Reads    []Count
	Writes   []Count
	Deletes  []Count
}

func (s Stats) Created() time.Time {
	if len(s.Writes) == 0 {
		return time.Time{}
	}
	return s.Writes[0].Date
}

func (s Stats) TotalWrites() (total int) {
	for _, c := range s.Writes {
		total += c.Count
	}
	return total
}

func (s Stats) LastUpdated() time.Time {
	if len(s.Reads) == 0 {
		return time.Time{}
	}
	return s.Reads[len(s.Reads)-1].Date
}

func (s Stats) TotalReads() (total int) {
	for _, c := range s.Reads {
		total += c.Count
	}
	return total
}

func (s Stats) LastRead() time.Time {
	if len(s.Reads) == 0 {
		return time.Time{}
	}
	return s.Reads[len(s.Reads)-1].Date
}

type Count struct {
	Date  time.Time
	Count int
}
