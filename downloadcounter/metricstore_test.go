package downloadcounter

import (
	"context"
	"testing"
	"time"

	"github.com/a-h/depot/store"
	"github.com/google/go-cmp/cmp"
)

func TestCounter(t *testing.T) {
	ctx := context.Background()
	s, closer, err := store.New(ctx, "sqlite", "file::memory:?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer closer()

	t.Run("counter can increment a value within a group", func(t *testing.T) {
		counter := New(s)
		now := time.Date(2026, 2, 20, 14, 0, 0, 0, time.UTC)
		counter.now = func() time.Time { return now }

		err := counter.Increment(ctx, "nix", "package-a")
		if err != nil {
			t.Fatalf("failed to increment: %v", err)
		}

		counts, err := counter.Get(ctx, "nix", "package-a")
		if err != nil {
			t.Fatalf("failed to get counts: %v", err)
		}

		expected := Counts{
			{Date: time.Date(2026, 2, 20, 0, 0, 0, 0, time.UTC), Count: 1},
		}
		if diff := cmp.Diff(expected, counts); diff != "" {
			t.Error(diff)
		}
	})
	t.Run("counts are distinct per group", func(t *testing.T) {
		counter := New(s)
		now := time.Date(2026, 2, 20, 14, 0, 0, 0, time.UTC)
		counter.now = func() time.Time { return now }

		err := counter.Increment(ctx, "npm", "shared-package")
		if err != nil {
			t.Fatalf("failed to increment npm group: %v", err)
		}

		err = counter.Increment(ctx, "python", "shared-package")
		if err != nil {
			t.Fatalf("failed to increment python group: %v", err)
		}

		npmCounts, err := counter.Get(ctx, "npm", "shared-package")
		if err != nil {
			t.Fatalf("failed to get npm counts: %v", err)
		}

		pythonCounts, err := counter.Get(ctx, "python", "shared-package")
		if err != nil {
			t.Fatalf("failed to get python counts: %v", err)
		}

		expected := Counts{
			{Date: time.Date(2026, 2, 20, 0, 0, 0, 0, time.UTC), Count: 1},
		}
		if diff := cmp.Diff(expected, npmCounts); diff != "" {
			t.Error(diff)
		}
		if diff := cmp.Diff(expected, pythonCounts); diff != "" {
			t.Error(diff)
		}

		if actual := npmCounts.Total(); actual != 1 {
			t.Errorf("expected 1, got %d", actual)
		}
		if actual := pythonCounts.Total(); actual != 1 {
			t.Errorf("expected 1, got %d", actual)
		}
	})
	t.Run("multiple increments on the same day increase the count", func(t *testing.T) {
		counter := New(s)
		now := time.Date(2026, 2, 21, 10, 30, 0, 0, time.UTC)
		counter.now = func() time.Time { return now }

		for range 5 {
			err := counter.Increment(ctx, "nix", "popular-package")
			if err != nil {
				t.Fatalf("failed to increment: %v", err)
			}
		}

		counts, err := counter.Get(ctx, "nix", "popular-package")
		if err != nil {
			t.Fatalf("failed to get counts: %v", err)
		}

		expected := Counts{
			{Date: time.Date(2026, 2, 21, 0, 0, 0, 0, time.UTC), Count: 5},
		}
		if diff := cmp.Diff(expected, counts); diff != "" {
			t.Error(diff)
		}
	})
	t.Run("counts are distinct per day", func(t *testing.T) {
		counter := New(s)

		day1 := time.Date(2026, 2, 15, 10, 0, 0, 0, time.UTC)
		counter.now = func() time.Time { return day1 }
		err := counter.Increment(ctx, "nix", "multi-day-package")
		if err != nil {
			t.Fatalf("failed to increment on day 1: %v", err)
		}

		day2 := time.Date(2026, 2, 16, 15, 0, 0, 0, time.UTC)
		counter.now = func() time.Time { return day2 }
		err = counter.Increment(ctx, "nix", "multi-day-package")
		if err != nil {
			t.Fatalf("failed to increment on day 2: %v", err)
		}
		err = counter.Increment(ctx, "nix", "multi-day-package")
		if err != nil {
			t.Fatalf("failed to increment on day 2 again: %v", err)
		}

		day3 := time.Date(2026, 2, 17, 12, 0, 0, 0, time.UTC)
		counter.now = func() time.Time { return day3 }
		err = counter.Increment(ctx, "nix", "multi-day-package")
		if err != nil {
			t.Fatalf("failed to increment on day 3: %v", err)
		}

		counts, err := counter.Get(ctx, "nix", "multi-day-package")
		if err != nil {
			t.Fatalf("failed to get counts: %v", err)
		}

		expected := Counts{
			{Date: time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC), Count: 1},
			{Date: time.Date(2026, 2, 16, 0, 0, 0, 0, time.UTC), Count: 2},
			{Date: time.Date(2026, 2, 17, 0, 0, 0, 0, time.UTC), Count: 1},
		}
		if diff := cmp.Diff(expected, counts); diff != "" {
			t.Error(diff)
		}
	})
	t.Run("counts are distinct per group and day", func(t *testing.T) {
		counter := New(s)

		day1 := time.Date(2026, 2, 18, 10, 0, 0, 0, time.UTC)
		counter.now = func() time.Time { return day1 }
		for range 3 {
			err := counter.Increment(ctx, "nix", "combined-test")
			if err != nil {
				t.Fatalf("failed to increment nix on day 1: %v", err)
			}
		}
		for range 2 {
			err := counter.Increment(ctx, "npm", "combined-test")
			if err != nil {
				t.Fatalf("failed to increment npm on day 1: %v", err)
			}
		}

		day2 := time.Date(2026, 2, 19, 10, 0, 0, 0, time.UTC)
		counter.now = func() time.Time { return day2 }
		for range 5 {
			err := counter.Increment(ctx, "nix", "combined-test")
			if err != nil {
				t.Fatalf("failed to increment nix on day 2: %v", err)
			}
		}
		for range 1 {
			err := counter.Increment(ctx, "npm", "combined-test")
			if err != nil {
				t.Fatalf("failed to increment npm on day 2: %v", err)
			}
		}

		nixCounts, err := counter.Get(ctx, "nix", "combined-test")
		if err != nil {
			t.Fatalf("failed to get nix counts: %v", err)
		}

		npmCounts, err := counter.Get(ctx, "npm", "combined-test")
		if err != nil {
			t.Fatalf("failed to get npm counts: %v", err)
		}

		expectedNix := Counts{
			{Date: time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC), Count: 3},
			{Date: time.Date(2026, 2, 19, 0, 0, 0, 0, time.UTC), Count: 5},
		}
		if diff := cmp.Diff(expectedNix, nixCounts); diff != "" {
			t.Error(diff)
		}

		expectedNpm := Counts{
			{Date: time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC), Count: 2},
			{Date: time.Date(2026, 2, 19, 0, 0, 0, 0, time.UTC), Count: 1},
		}
		if diff := cmp.Diff(expectedNpm, npmCounts); diff != "" {
			t.Error(diff)
		}
	})
	t.Run("the count returns a total", func(t *testing.T) {
		counter := New(s)

		day1 := time.Date(2026, 2, 22, 10, 0, 0, 0, time.UTC)
		counter.now = func() time.Time { return day1 }
		for range 10 {
			err := counter.Increment(ctx, "nix", "total-test-package")
			if err != nil {
				t.Fatalf("failed to increment on day 1: %v", err)
			}
		}

		day2 := time.Date(2026, 2, 23, 10, 0, 0, 0, time.UTC)
		counter.now = func() time.Time { return day2 }
		for range 25 {
			err := counter.Increment(ctx, "nix", "total-test-package")
			if err != nil {
				t.Fatalf("failed to increment on day 2: %v", err)
			}
		}

		day3 := time.Date(2026, 2, 24, 10, 0, 0, 0, time.UTC)
		counter.now = func() time.Time { return day3 }
		for range 5 {
			err := counter.Increment(ctx, "nix", "total-test-package")
			if err != nil {
				t.Fatalf("failed to increment on day 3: %v", err)
			}
		}

		counts, err := counter.Get(ctx, "nix", "total-test-package")
		if err != nil {
			t.Fatalf("failed to get counts: %v", err)
		}

		if actual := counts.Total(); actual != 40 {
			t.Errorf("expected 40, got %d", actual)
		}
	})
	t.Run("values returns an item in the slice for each day, including days with zero counts", func(t *testing.T) {
		counter := New(s)

		day1 := time.Date(2026, 2, 25, 10, 0, 0, 0, time.UTC)
		counter.now = func() time.Time { return day1 }
		for range 10 {
			err := counter.Increment(ctx, "nix", "values-test-package")
			if err != nil {
				t.Fatalf("failed to increment on day 1: %v", err)
			}
		}

		day3 := time.Date(2026, 2, 27, 10, 0, 0, 0, time.UTC)
		counter.now = func() time.Time { return day3 }
		for range 5 {
			err := counter.Increment(ctx, "nix", "values-test-package")
			if err != nil {
				t.Fatalf("failed to increment on day 3: %v", err)
			}
		}

		counts, err := counter.Get(ctx, "nix", "values-test-package")
		if err != nil {
			t.Fatalf("failed to get counts: %v", err)
		}

		expected := []int{10, 0, 5}
		actual := counts.Values()
		if diff := cmp.Diff(expected, actual); diff != "" {
			t.Error(diff)
		}
	})
	t.Run("Counts.Range returns the date range", func(t *testing.T) {
		counts := Counts{
			{Date: time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC), Count: 10},
			{Date: time.Date(2026, 2, 16, 0, 0, 0, 0, time.UTC), Count: 25},
			{Date: time.Date(2026, 2, 17, 0, 0, 0, 0, time.UTC), Count: 5},
		}

		actualFrom, actualTo := counts.Range()
		expectedFrom := time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)
		expectedTo := time.Date(2026, 2, 17, 0, 0, 0, 0, time.UTC)

		if diff := cmp.Diff(expectedFrom, actualFrom); diff != "" {
			t.Error(diff)
		}
		if diff := cmp.Diff(expectedTo, actualTo); diff != "" {
			t.Error(diff)
		}
	})
	t.Run("get returns empty slice for non-existent group and name", func(t *testing.T) {
		counter := New(s)

		counts, err := counter.Get(ctx, "non-existent-group", "non-existent-name")
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}

		if len(counts) != 0 {
			t.Errorf("expected 0 counts, got %d", len(counts))
		}
	})
}
