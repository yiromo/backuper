package destination

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"backuper/internal/config"
)

type LocalDestination struct {
	cfg *config.DestinationConfig
}

func newLocal(cfg *config.DestinationConfig) *LocalDestination {
	return &LocalDestination{cfg: cfg}
}

func (d *LocalDestination) Name() string { return d.cfg.Name }
func (d *LocalDestination) Type() string { return "local" }

func (d *LocalDestination) Transfer(_ context.Context, localPath string) error {
	destDir := expandHome(d.cfg.Path)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("creating destination dir %q: %w", destDir, err)
	}
	destPath := filepath.Join(destDir, filepath.Base(localPath))

	src, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("opening source %q: %w", localPath, err)
	}
	defer src.Close()

	dst, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("creating destination %q: %w", destPath, err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("copying to %q: %w", destPath, err)
	}
	return nil
}

func (d *LocalDestination) ListFiles(_ context.Context, targetName string) ([]string, error) {
	dir := expandHome(d.cfg.Path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading dir %q: %w", dir, err)
	}
	prefix := targetName + "_"
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), prefix) {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(files)
	return files, nil
}

func (d *LocalDestination) DeleteFile(_ context.Context, filename string) error {
	if err := os.Remove(filename); err != nil {
		return fmt.Errorf("deleting %q: %w", filename, err)
	}
	return nil
}

func expandHome(path string) string {
	if len(path) > 1 && path[:2] == "~/" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}
