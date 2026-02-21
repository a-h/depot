package downloadcounter

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/a-h/kv"
)

func New(store kv.Store) *Counter {
	return &Counter{
		store: store,
		now:   time.Now,
	}
}

type Counter struct {
	store kv.Store
	now   func() time.Time
}

func (m *Counter) buildCounterKey(group, name string, date time.Time) string {
	encodedGroup := url.PathEscape(group)
	encodedName := url.PathEscape(name)
	encodedDate := date.Format("2006-01-02")
	return path.Join("/downloadcounter", encodedGroup, encodedName, encodedDate)
}

func (m *Counter) buildCounterPrefix(group, name string) string {
	encodedGroup := url.PathEscape(group)
	encodedName := url.PathEscape(name)
	return path.Join("/downloadcounter", encodedGroup, encodedName) + "/"
}

func (m *Counter) Increment(ctx context.Context, group, name string) (err error) {
	day := m.now().Truncate(24 * time.Hour)
	key := m.buildCounterKey(group, name, day)
	// Every time we upsert a key with Put, the version number is incremented.
	return m.store.Put(ctx, key, -1, "")
}

func (m *Counter) Get(ctx context.Context, group, name string) (count Counts, err error) {
	rows, err := m.store.GetPrefix(ctx, m.buildCounterPrefix(group, name), 0, -1)
	if err != nil {
		return nil, err
	}

	counts := make([]Count, len(rows))
	for i, row := range rows {
		parts := strings.Split(row.Key, "/")
		if len(parts) != 5 {
			return counts, fmt.Errorf("invalid key format: %s", row.Key)
		}
		if counts[i].Date, err = time.Parse("2006-01-02", parts[4]); err != nil {
			return nil, fmt.Errorf("failed to parse key: %w", err)
		}
		counts[i].Count = row.Version
	}

	return counts, nil
}

type Counts []Count

func (c Counts) Total() (total int) {
	for _, count := range c {
		total += count.Count
	}
	return total
}

// Range returns the date range covered by the counts. It assumes the counts are sorted by date.
func (c Counts) Range() (from time.Time, to time.Time) {
	if len(c) == 0 {
		return time.Time{}, time.Time{}
	}
	return c[0].Date, c[len(c)-1].Date
}

// Values provides just the count values, including zeros for days with no counts.
func (c Counts) Values() (values []int) {
	from, to := c.Range()
	hours := to.Sub(from).Hours()
	days := int(hours / 24)
	values = make([]int, days+1)
	for _, count := range c {
		index := int(count.Date.Sub(from).Hours() / 24)
		values[index] = count.Count
	}
	return values
}

type Count struct {
	Date  time.Time
	Count int
}
