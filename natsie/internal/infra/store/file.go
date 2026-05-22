package store

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/1995parham/natsie/internal/manifest"
)

// validID restricts manifest IDs to a safe shape: lowercase letters, digits,
// dashes, underscores, and dots. No path separators, no leading dots
// (avoids hidden files and parent traversal).
var validID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

const manifestExt = ".yaml"

// File stores manifests as <dir>/<id>.yaml.
type File struct {
	Dir string
	mu  sync.Mutex
}

// NewFile creates the directory if it does not exist.
func NewFile(dir string) (*File, error) {
	if dir == "" {
		return nil, errors.New("file store: empty directory")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", dir, err)
	}
	return &File{Dir: dir}, nil
}

func (f *File) Name() string { return "file://" + f.Dir }

func (f *File) path(id string) (string, error) {
	if !validID.MatchString(id) {
		return "", fmt.Errorf("invalid manifest id %q (allowed: [a-zA-Z0-9][a-zA-Z0-9._-]+)", id)
	}
	return filepath.Join(f.Dir, id+manifestExt), nil
}

func (f *File) Put(_ context.Context, id string, m *manifest.Manifest) error {
	path, err := f.path(id)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return m.Write(path, true)
}

func (f *File) Get(_ context.Context, id string) (*manifest.Manifest, error) {
	path, err := f.path(id)
	if err != nil {
		return nil, err
	}
	return manifest.Read(path)
}

func (f *File) Delete(_ context.Context, id string) error {
	path, err := f.path(id)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}

func (f *File) List(_ context.Context) ([]string, error) {
	entries, err := os.ReadDir(f.Dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", f.Dir, err)
	}
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, manifestExt) {
			continue
		}
		ids = append(ids, strings.TrimSuffix(name, manifestExt))
	}
	sort.Strings(ids)
	return ids, nil
}
