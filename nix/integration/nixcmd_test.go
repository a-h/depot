package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// getEnv returns the environment variables needed for nix commands.
func getEnv() []string {
	env := os.Environ()
	// Ensure HOME is set for git operations.
	if os.Getenv("HOME") == "" {
		env = append(env, "HOME=/root")
	}
	return env
}

// ErrorBuffer creates a combined writer that captures both stdout and stderr
// for better error reporting.
func ErrorBuffer(stdout, stderr io.Writer) (io.Writer, func(error) error) {
	var buf bytes.Buffer
	w := io.MultiWriter(&buf, stderr)
	return w, func(err error) error {
		if err != nil {
			// Include command output in error for debugging.
			output := strings.TrimSpace(buf.String())
			if output != "" {
				return fmt.Errorf("%w: %s", err, output)
			}
		}
		return err
	}
}

// CopyTo copies Nix store paths to a target store.
func CopyTo(stdout, stderr io.Writer, codeDir, targetStore string, derivation bool, paths ...string) (err error) {
	nixPath, err := exec.LookPath("nix")
	if err != nil {
		return fmt.Errorf("failed to find nix on path: %v", err)
	}

	args := []string{"copy", "--to", targetStore, "--refresh"}
	if derivation {
		args = append(args, "--derivation")
	}
	args = append(args, paths...)
	cmd := exec.Command(nixPath, args...)
	cmd.Dir = codeDir

	w, closer := ErrorBuffer(stdout, stderr)
	cmd.Stderr = w
	cmd.Stdout = w
	return closer(cmd.Run())
}

// FlakeArchive archives a flake to a target store.
func FlakeArchive(stdout, stderr io.Writer, targetStore, flakeRef string) error {
	nixPath, err := exec.LookPath("nix")
	if err != nil {
		return fmt.Errorf("failed to find nix on path: %v", err)
	}

	// Inside the Docker container, the export is hard coded to /nix-export/nix-store/
	// So, the targetStore would be file:///nix-export/nix-store/
	cmd := exec.Command(nixPath, "flake", "archive", "--to", targetStore, "--refresh", flakeRef)

	w, closer := ErrorBuffer(stdout, stderr)
	cmd.Stderr = w
	cmd.Stdout = w
	return closer(cmd.Run())
}

// CopyFrom copies store paths from a source store to the local store.
func CopyFrom(stdout, stderr io.Writer, toStore, fromStore string, paths ...string) error {
	if len(paths) == 0 {
		return nil
	}

	nixPath, err := exec.LookPath("nix")
	if err != nil {
		return fmt.Errorf("failed to find nix on path: %w", err)
	}

	args := []string{"copy", "--from", fromStore, "--to", toStore}
	args = append(args, paths...)

	cmd := exec.Command(nixPath, args...)
	cmd.Env = getEnv()

	w, closer := ErrorBuffer(stdout, stderr)
	cmd.Stderr = w
	cmd.Stdout = w

	return closer(cmd.Run())
}

// Eval evaluates a Nix expression and returns the store path.
func Eval(stdout, stderr io.Writer, expr string) (string, error) {
	nixPath, err := exec.LookPath("nix")
	if err != nil {
		return "", fmt.Errorf("failed to find nix on path: %w", err)
	}

	cmd := exec.Command(nixPath, "eval", expr, "--raw")
	cmd.Env = getEnv()

	w, closer := ErrorBuffer(stdout, stderr)
	cmd.Stderr = w

	output, err := cmd.Output()
	if err != nil {
		return "", closer(err)
	}

	return string(output), nil
}

// DerivationShow runs `nix derivation show` on a given derivation reference.
func DerivationShow(stdout, stderr io.Writer, codeDir, ref string) (inputDrvs []string, srcs []string, err error) {
	nixPath, err := exec.LookPath("nix")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to find nix on path: %v", err)
	}

	stdoutBuffer := new(bytes.Buffer)

	cmd := exec.Command(nixPath, "derivation", "show", ref)
	cmd.Dir = codeDir

	w, closer := ErrorBuffer(stdout, stderr)
	cmd.Stdout = io.MultiWriter(stdoutBuffer, w)
	cmd.Stderr = w
	if err = closer(cmd.Run()); err != nil {
		return nil, nil, fmt.Errorf("failed to run nix derivation show: %v", err)
	}

	return getInputDrvs(stdoutBuffer.Bytes())
}

type Derivation struct {
	InputDrvs map[string]any `json:"inputDrvs"`
	InputSrcs []string       `json:"inputSrcs"`
}

func normalizeNixStorePath(p string) (string, bool) {
	if strings.HasPrefix(p, "/nix/store/") {
		return p, true
	}
	// Reject empty strings and absolute paths (which indicate non-store paths).
	if p == "" || filepath.IsAbs(p) {
		return "", false
	}
	// v3 format: basename only (hash-name style).
	return filepath.Join("/nix/store", p), true
}

func getInputDrvs(input []byte) (drvs []string, srcs []string, err error) {
	var m map[string]Derivation
	err = json.Unmarshal(input, &m)
	if err != nil {
		return drvs, srcs, fmt.Errorf("failed to unmarshal derivation: %v", err)
	}
	var drvKeys []string
	for k := range m {
		drvKeys = append(drvKeys, k)
	}
	if len(drvKeys) != 1 {
		return drvs, srcs, fmt.Errorf("expected exactly one key in the map, got %d", len(drvKeys))
	}
	drv := m[drvKeys[0]]
	for k := range drv.InputDrvs {
		if path, ok := normalizeNixStorePath(k); ok {
			drvs = append(drvs, path)
		}
	}
	for _, src := range drv.InputSrcs {
		if path, ok := normalizeNixStorePath(src); ok {
			srcs = append(srcs, path)
		}
	}
	slices.Sort(drvs)
	return drvs, srcs, nil
}

// RealiseStorePaths realises store paths and returns their output paths.
func RealiseStorePaths(stdout, stderr io.Writer, paths ...string) ([]string, error) {
	if len(paths) == 0 {
		return []string{}, nil
	}

	nixStorePath, err := exec.LookPath("nix-store")
	if err != nil {
		return nil, fmt.Errorf("failed to find nix-store on path: %w", err)
	}

	args := []string{"--realise"}
	args = append(args, paths...)

	cmd := exec.Command(nixStorePath, args...)
	cmd.Env = getEnv()

	w, closer := ErrorBuffer(stdout, stderr)
	cmd.Stderr = w

	output, err := cmd.Output()
	if err != nil {
		return nil, closer(err)
	}

	// nix-store --realise outputs paths separated by newlines
	result := strings.Split(strings.TrimSpace(string(output)), "\n")
	var realisedPaths []string
	for _, path := range result {
		path = strings.TrimSpace(path)
		if path != "" {
			realisedPaths = append(realisedPaths, path)
		}
	}

	return realisedPaths, nil
}
