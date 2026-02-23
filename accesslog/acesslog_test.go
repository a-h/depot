package accesslog

import (
	"testing"
	"time"

	"github.com/a-h/depot/store"
	"github.com/google/go-cmp/cmp"
)

func TestAccessLogs(t *testing.T) {
	s, closer, err := store.New(t.Context(), "sqlite", "file::memory:?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer closer()

	accessLog := New(s)
	now := time.Date(2000, 1, 1, 14, 0, 0, 0, time.UTC)
	accessLog.now = func() time.Time { return now }
	expectedCreationDate := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

	t.Run("stats are not returned for files that don't exist", func(t *testing.T) {
		_, ok, err := accessLog.Get(t.Context(), "file-does-not-exist.txt")
		if err != nil {
			t.Errorf("unexpected error getting access logs: %v", err)
		}
		if ok {
			t.Error("expected ok=false, got true")
		}
	})
	t.Run("the first write is assumed to be the creation", func(t *testing.T) {
		if err := accessLog.Write(t.Context(), "filea.txt"); err != nil {
			t.Fatalf("failed to log file write: %v", err)
		}
		stats, ok, err := accessLog.Get(t.Context(), "filea.txt")
		if err != nil {
			t.Fatalf("failed to get stats: %v", err)
		}
		if !ok {
			t.Error("expected access logs for file that exists, but got none")
		}
		expected := Stats{
			Filename: "filea.txt",
			Writes: []Count{
				{Date: expectedCreationDate, Count: 1},
			},
		}
		if diff := cmp.Diff(expected, stats); diff != "" {
			t.Error(diff)
		}
	})
	t.Run("reads can happen on multiple days", func(t *testing.T) {
		for range 5 {
			if err = accessLog.Read(t.Context(), "filea.txt"); err != nil {
				t.Fatalf("failed to read file: %v", err)
			}
		}
		accessLog.now = func() time.Time {
			return expectedCreationDate.Add(24 * time.Hour)
		}
		for range 7 {
			if err = accessLog.Read(t.Context(), "filea.txt"); err != nil {
				t.Fatalf("failed to read file: %v", err)
			}
		}
		stats, ok, err := accessLog.Get(t.Context(), "filea.txt")
		if err != nil {
			t.Fatalf("failed to get stats: %v", err)
		}
		if !ok {
			t.Error("expected access logs for file that exists, but got none")
		}
		expected := Stats{
			Filename: "filea.txt",
			Writes: []Count{
				{Date: expectedCreationDate, Count: 1},
			},
			Reads: []Count{
				{Date: expectedCreationDate, Count: 5},
				{Date: expectedCreationDate.Add(time.Hour * 24), Count: 7},
			},
		}
		if diff := cmp.Diff(expected, stats); diff != "" {
			t.Error(diff)
		}
	})
	t.Run("events only affect a single filename", func(t *testing.T) {
		if err := accessLog.Write(t.Context(), "fileb.txt"); err != nil {
			t.Fatalf("failed to log file write: %v", err)
		}
		for range 3 {
			if err = accessLog.Read(t.Context(), "fileb.txt"); err != nil {
				t.Fatalf("failed to read file: %v", err)
			}
		}
		stats, ok, err := accessLog.Get(t.Context(), "fileb.txt")
		if err != nil {
			t.Fatalf("failed to get stats: %v", err)
		}
		if !ok {
			t.Error("expected access logs for file that exists, but got none")
		}
		expected := Stats{
			Filename: "fileb.txt",
			Writes: []Count{
				{Date: expectedCreationDate.Add(time.Hour * 24), Count: 1},
			},
			Reads: []Count{
				{Date: expectedCreationDate.Add(time.Hour * 24), Count: 3},
			},
		}
		if diff := cmp.Diff(expected, stats); diff != "" {
			t.Error(diff)
		}
	})
	t.Run("deletions are logged", func(t *testing.T) {
		if err = accessLog.Delete(t.Context(), "fileb.txt"); err != nil {
			t.Fatalf("failed to log file deletion: %v", err)
		}
		stats, ok, err := accessLog.Get(t.Context(), "fileb.txt")
		if err != nil {
			t.Fatalf("failed to get stats: %v", err)
		}
		if !ok {
			t.Error("expected access logs for file that exists, but got none")
		}
		expected := Stats{
			Filename: "fileb.txt",
			Writes: []Count{
				{Date: expectedCreationDate.Add(time.Hour * 24), Count: 1},
			},
			Reads: []Count{
				{Date: expectedCreationDate.Add(time.Hour * 24), Count: 3},
			},
			Deletes: []Count{
				{Date: expectedCreationDate.Add(time.Hour * 24), Count: 1},
			},
		}
		if diff := cmp.Diff(expected, stats); diff != "" {
			t.Error(diff)
		}
	})
}
