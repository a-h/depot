package save

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/a-h/depot/python/models"
	"github.com/a-h/depot/storage"
	version "github.com/aquasecurity/go-pep440-version"
)

func New(log *slog.Logger, storage storage.Storage) *Saver {
	return &Saver{
		log:     log,
		storage: storage,
		client:  &http.Client{},
	}
}

type Saver struct {
	log     *slog.Logger
	storage storage.Storage
	client  *http.Client
}

func (s *Saver) Save(ctx context.Context, packages []string) error {
	for _, pkg := range packages {
		if err := s.savePackage(ctx, pkg); err != nil {
			s.log.Error("failed to save package", slog.String("package", pkg), slog.Any("error", err))
			return err
		}
	}
	return nil
}

func (s *Saver) SaveFromReader(ctx context.Context, r io.Reader) error {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if err := s.savePackage(ctx, line); err != nil {
			s.log.Error("failed to save package", slog.String("package", line), slog.Any("error", err))
			return err
		}
	}
	return scanner.Err()
}

// requests [security,tests] >= 2.8.1, == 2.8.* ; python_version < "2.7"
var splitters = []string{"===", "==", "~=", ">=", "<=", ">", "<", "!="}

func firstIndexOfAny(s string, subs []string) (minIndex int) {
	var found bool
	for _, sub := range subs {
		idx := strings.Index(s, sub)
		if idx != -1 && (!found || idx < minIndex) {
			found = true
			minIndex = idx
		}
	}
	if !found {
		return -1
	}
	return minIndex
}

func splitOnFirst(s string, subs []string) (before, after string) {
	minIndex := firstIndexOfAny(s, subs)
	if minIndex == -1 {
		return s, ""
	}
	return s[:minIndex], s[minIndex:]
}

func (s *Saver) savePackage(ctx context.Context, line string) error {
	s.log.Info("saving package", slog.String("line", line))

	// Split on the lowest index of a splitter to get the package name.
	pkg, specifiers := splitOnFirst(line, splitters)

	// Parse package spec, e.g. "requests>=2.0.0,<3.0.0"
	var spec version.Specifiers
	if specifiers != "" {
		var err error
		spec, err = version.NewSpecifiers(specifiers)
		if err != nil {
			return fmt.Errorf("invalid package specifier %q: %w", line, err)
		}
	}

	s.log.Debug("fetching package index", slog.String("package", pkg))
	index, err := s.getPackageIndex(ctx, pkg)
	if err != nil {
		return fmt.Errorf("failed to get package index for %s: %w", pkg, err)
	}

	s.log.Debug("filtering package versions", slog.String("package", pkg), slog.String("spec", spec.String()), slog.Int("totalVersions", len(index.Versions)))
	filteredIndex, err := filterVersions(index, func(v string) (ok bool, err error) {
		if spec.String() == "" {
			return true, nil
		}
		version, err := version.Parse(v)
		if err != nil {
			return false, fmt.Errorf("invalid version %s: %w", v, err)
		}
		return spec.Check(version), nil
	})
	if err != nil {
		return fmt.Errorf("failed to filter versions for %s: %w", pkg, err)
	}

	s.log.Debug("saving package files", slog.String("package", pkg), slog.Int("totalFiles", len(filteredIndex.Files)))
	for _, file := range filteredIndex.Files {
		s.log.Debug("saving package file", slog.String("package", pkg), slog.String("file", file.Filename))
		if err = s.savePackageFile(ctx, pkg, file); err != nil {
			return fmt.Errorf("failed to save package file %s for %s: %w", file.Filename, pkg, err)
		}
	}

	s.log.Info("saved package", slog.String("package", pkg), slog.Int("versions", len(filteredIndex.Versions)), slog.Int("files", len(filteredIndex.Files)))
	return nil
}

func (s *Saver) savePackageFile(ctx context.Context, pkg string, file models.SimpleFileEntry) error {
	// Check the existing file size.
	fileName := fmt.Sprintf("%s/%s", pkg, file.Filename)
	size, exists, err := s.storage.Stat(ctx, fileName)
	if err != nil {
		return fmt.Errorf("failed to stat storage file %s/%s: %w", pkg, file.Filename, err)
	}
	if exists && size == file.Size {
		s.log.Debug("file already exists with matching size, skipping download", slog.String("package", pkg), slog.String("file", file.Filename))
		return nil
	}

	// Download and save the file.
	w, err := s.storage.Put(ctx, fileName)
	if err != nil {
		return fmt.Errorf("failed to create storage writer for %s/%s: %w", pkg, file.Filename, err)
	}
	defer w.Close()
	resp, err := s.client.Get(file.URL)
	if err != nil {
		return fmt.Errorf("failed to download file %s: %w", file.URL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code %d when downloading file %s", resp.StatusCode, file.URL)
	}
	_, err = io.Copy(w, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to write file %s to storage: %w", file.Filename, err)
	}

	// Save the metadata alongside the file.
	metadataName := fmt.Sprintf("%s/%s.json", pkg, file.Filename)
	metadataWriter, err := s.storage.Put(ctx, metadataName)
	if err != nil {
		return fmt.Errorf("failed to create storage writer for metadata %s: %w", metadataName, err)
	}
	defer metadataWriter.Close()
	encoder := json.NewEncoder(metadataWriter)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(file); err != nil {
		return fmt.Errorf("failed to encode metadata for %s: %w", metadataName, err)
	}

	return nil
}

func (s *Saver) getPackageIndex(ctx context.Context, name string) (index models.SimplePackageIndex, err error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://pypi.org/simple/"+url.PathEscape(name), nil)
	if err != nil {
		return index, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "Depot/0.1 (+https://github.com/a-h/depot)")
	req.Header.Set("Accept", "application/vnd.pypi.simple.v1+json")
	resp, err := s.client.Do(req)
	if err != nil {
		return index, fmt.Errorf("failed to perform request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return index, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	err = json.NewDecoder(resp.Body).Decode(&index)
	if err != nil {
		return index, fmt.Errorf("failed to decode response: %w", err)
	}
	return index, nil
}

func filterVersions(index models.SimplePackageIndex, shouldKeep func(version string) (ok bool, err error)) (filtered models.SimplePackageIndex, err error) {
	filtered = models.SimplePackageIndex{
		Meta:     index.Meta,
		Name:     index.Name,
		Files:    []models.SimpleFileEntry{},
		Versions: []string{},
	}
	for _, v := range index.Versions {
		ok, err := shouldKeep(v)
		if err != nil {
			return filtered, fmt.Errorf("failed to filter versions: %w", err)
		}
		if !ok {
			continue
		}
		filtered.Versions = append(filtered.Versions, v)
	}
	for _, f := range index.Files {
		ok, err := shouldKeep(f.Version())
		if err != nil {
			return filtered, fmt.Errorf("failed to filter file %q with version %q: %w", f.Filename, f.Version(), err)
		}
		if !ok {
			continue
		}
		filtered.Files = append(filtered.Files, f)
	}
	return filtered, nil
}
