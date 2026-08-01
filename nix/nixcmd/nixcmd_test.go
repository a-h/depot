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
			name: "version 4 format: basename paths are normalised and non-store paths are excluded",
			json: `{
				"version": 4,
				"derivations": {
					"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-example.drv": {
						"inputs": {
							"drvs": {
								"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb-foo.drv": {"outputs": ["out"], "dynamicOutputs": {}},
								"/private/tmp/local-builder.sh": {"outputs": [], "dynamicOutputs": {}}
							},
							"srcs": [
								"cccccccccccccccccccccccccccccccc-source",
								"/Users/adrian/work/default-builder.sh"
							]
						}
					}
				}
			}`,
			wantDrvs: []string{"/nix/store/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb-foo.drv"},
			wantSrcs: []string{"/nix/store/cccccccccccccccccccccccccccccccc-source"},
		},
		{
			// Nix < 2.33 (nixos-25.11 ships Nix 2.31) produces an unversioned top-level map.
			name: "legacy format (Nix < 2.33): basename paths are normalised and non-store paths are excluded",
			json: `{
				"/nix/store/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-example.drv": {
					"inputDrvs": {
						"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb-foo.drv": ["out"],
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
			name:      "unsupported version returns an error",
			json:      `{"version": 3, "derivations": {}}`,
			wantError: true,
		},
		{
			name:      "invalid json returns an error",
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
