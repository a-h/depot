package pkglock

import (
	"context"
	_ "embed"
	"slices"
	"strings"
	"testing"
)

//go:embed example.json
var exampleLockFile string

//go:embed expected.txt
var expectedOutput string

func TestParse(t *testing.T) {
	r := strings.NewReader(exampleLockFile)
	pkgs, err := Parse(context.Background(), r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := strings.Split(strings.TrimSpace(expectedOutput), "\n")
	slices.Sort(pkgs)
	slices.Sort(expected)
	if len(pkgs) != len(expected) {
		t.Fatalf("unexpected number of packages: got %d, want %d", len(pkgs), len(expected))
	}
	for i, e := range expected {
		a := pkgs[i]
		if a != e {
			t.Fatalf("unexpected package at index %d: got %q, want %q", i, a, e)
		}
	}
}
