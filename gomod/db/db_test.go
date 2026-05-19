package db

import (
	"context"
	"testing"
	"time"

	"github.com/a-h/depot/store"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	s, closer, err := store.New(context.Background(), "sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	t.Cleanup(func() { closer() })
	if err := s.Init(context.Background()); err != nil {
		t.Fatalf("failed to init store: %v", err)
	}
	return New(s)
}

func TestPutAndGetModuleVersion(t *testing.T) {
	tests := []struct {
		name       string
		modulePath string
		version    string
		mv         ModuleVersion
	}{
		{
			name:       "simple module path is stored and retrieved",
			modulePath: "github.com/foo/bar",
			version:    "v1.0.0",
			mv: ModuleVersion{
				Info:  VersionInfo{Version: "v1.0.0", Time: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
				GoMod: "module github.com/foo/bar\n\ngo 1.21\n",
			},
		},
		{
			name:       "module path with capitals is encoded correctly",
			modulePath: "github.com/Azure/go-autorest",
			version:    "v14.2.0+incompatible",
			mv: ModuleVersion{
				Info:  VersionInfo{Version: "v14.2.0+incompatible", Time: time.Date(2023, 6, 15, 0, 0, 0, 0, time.UTC)},
				GoMod: "module github.com/Azure/go-autorest\n\ngo 1.15\n",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newTestDB(t)
			ctx := context.Background()

			if err := db.PutModuleVersion(ctx, tt.modulePath, tt.version, tt.mv); err != nil {
				t.Fatalf("unexpected error putting module version: %v", err)
			}

			got, ok, err := db.GetModuleVersion(ctx, tt.modulePath, tt.version)
			if err != nil {
				t.Fatalf("unexpected error getting module version: %v", err)
			}
			if !ok {
				t.Fatal("expected module version to exist")
			}
			if got.Info.Version != tt.mv.Info.Version {
				t.Errorf("got version %q, expected %q", got.Info.Version, tt.mv.Info.Version)
			}
			if !got.Info.Time.Equal(tt.mv.Info.Time) {
				t.Errorf("got time %v, expected %v", got.Info.Time, tt.mv.Info.Time)
			}
			if got.GoMod != tt.mv.GoMod {
				t.Errorf("got gomod %q, expected %q", got.GoMod, tt.mv.GoMod)
			}
		})
	}
}

func TestGetModuleVersionReturnsNotFoundForMissingModule(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	_, ok, err := db.GetModuleVersion(ctx, "github.com/nonexistent/mod", "v1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected module version to not exist")
	}
}

func TestListVersions(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	modulePath := "github.com/foo/bar"
	versions := []string{"v1.0.0", "v1.1.0", "v2.0.0"}

	for _, v := range versions {
		mv := ModuleVersion{
			Info:  VersionInfo{Version: v, Time: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
			GoMod: "module github.com/foo/bar\n",
		}
		if err := db.PutModuleVersion(ctx, modulePath, v, mv); err != nil {
			t.Fatalf("unexpected error putting %s: %v", v, err)
		}
	}

	got, err := db.ListVersions(ctx, modulePath)
	if err != nil {
		t.Fatalf("unexpected error listing versions: %v", err)
	}
	if len(got) != len(versions) {
		t.Fatalf("got %d versions, expected %d", len(got), len(versions))
	}
	for _, v := range versions {
		var found bool
		for _, g := range got {
			if g == v {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("version %s not found in list", v)
		}
	}
}

func TestListVersionsReturnsEmptyForMissingModule(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	got, err := db.ListVersions(ctx, "github.com/nonexistent/mod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d versions, expected 0", len(got))
	}
}

func TestGetLatestVersion(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	modulePath := "github.com/foo/bar"
	mvs := []ModuleVersion{
		{Info: VersionInfo{Version: "v1.0.0", Time: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}, GoMod: "module github.com/foo/bar\n"},
		{Info: VersionInfo{Version: "v1.1.0", Time: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)}, GoMod: "module github.com/foo/bar\n"},
		{Info: VersionInfo{Version: "v1.0.1", Time: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)}, GoMod: "module github.com/foo/bar\n"},
	}
	for _, mv := range mvs {
		if err := db.PutModuleVersion(ctx, modulePath, mv.Info.Version, mv); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	got, ok, err := db.GetLatestVersion(ctx, modulePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected a latest version to exist")
	}
	if got.Info.Version != "v1.1.0" {
		t.Errorf("got version %q, expected %q", got.Info.Version, "v1.1.0")
	}
}

func TestGetLatestVersionReturnsNotFoundForMissingModule(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	_, ok, err := db.GetLatestVersion(ctx, "github.com/nonexistent/mod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected latest version to not exist")
	}
}
