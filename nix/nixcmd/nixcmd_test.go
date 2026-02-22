package nixcmd

import (
	"testing"
)

func TestGetInputDrvsReturnsOnlyNixStorePaths(t *testing.T) {
	tests := []struct {
		name      string
		json      string
		wantDrvs  []string
		wantSrcs  []string
		wantError bool
	}{
		{
			name: "basename store paths are normalised and non-store paths are excluded",
			json: `{
				"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-example.drv": {
					"inputDrvs": {
						"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb-foo.drv": [],
						"/private/tmp/local-builder.sh": []
					},
					"inputSrcs": [
						"cccccccccccccccccccccccccccccccc-source",
						"/Users/adrian/work/default-builder.sh"
					]
				}
			}`,
			wantDrvs: []string{"/nix/store/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb-foo.drv"},
			wantSrcs: []string{"/nix/store/cccccccccccccccccccccccccccccccc-source"},
		},
		{
			name: "absolute store paths are preserved",
			json: `{
				"/nix/store/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-example.drv": {
					"inputDrvs": {
						"/nix/store/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb-foo.drv": []
					},
					"inputSrcs": [
						"/nix/store/cccccccccccccccccccccccccccccccc-source"
					]
				}
			}`,
			wantDrvs: []string{"/nix/store/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb-foo.drv"},
			wantSrcs: []string{"/nix/store/cccccccccccccccccccccccccccccccc-source"},
		},
		{
			name:      "invalid json returns error",
			json:      `not-json`,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotDrvs, gotSrcs, err := getInputDrvs([]byte(tt.json))
			if tt.wantError {
				if err == nil {
					t.Fatalf("expected an error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(gotDrvs) != len(tt.wantDrvs) {
				t.Fatalf("expected %d derivations, got %d", len(tt.wantDrvs), len(gotDrvs))
			}
			for i := range tt.wantDrvs {
				if gotDrvs[i] != tt.wantDrvs[i] {
					t.Fatalf("expected derivation %s at index %d, got %s", tt.wantDrvs[i], i, gotDrvs[i])
				}
			}
			if len(gotSrcs) != len(tt.wantSrcs) {
				t.Fatalf("expected %d sources, got %d", len(tt.wantSrcs), len(gotSrcs))
			}
			for i := range tt.wantSrcs {
				if gotSrcs[i] != tt.wantSrcs[i] {
					t.Fatalf("expected source %s at index %d, got %s", tt.wantSrcs[i], i, gotSrcs[i])
				}
			}
		})
	}
}
