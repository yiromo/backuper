// Package destination defines the Destination interface and its implementations.
package destination

import (
	"context"
	"fmt"

	"backuper/internal/config"
	"backuper/internal/secrets"
)

type Destination interface {
	Name() string
	Type() string
	Transfer(ctx context.Context, localPath string) error
	ListFiles(ctx context.Context, targetName string) ([]string, error)
	DeleteFile(ctx context.Context, filename string) error
}

func New(cfg *config.DestinationConfig, store secrets.Store) (Destination, error) {
	switch cfg.Type {
	case "local":
		return newLocal(cfg), nil
	case "scp":
		return newSCP(cfg, store)
	case "rsync":
		return newRsync(cfg, store), nil
	default:
		return nil, fmt.Errorf("unknown destination type %q", cfg.Type)
	}
}
