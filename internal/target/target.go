// Package target defines the Target interface and its implementations.
package target

import (
	"context"
	"fmt"
	"io"

	"backuper/internal/config"
	"backuper/internal/secrets"
)

type Target interface {
	Name() string
	Engine() string
	Runtime() string
	FileExt() string
	GetPassword(ctx context.Context, store secrets.Store) (string, error)
	Dump(ctx context.Context, w io.Writer, password string) error
}

func New(cfg *config.TargetConfig) (Target, error) {
	switch cfg.Engine {
	case "postgres":
		if cfg.Runtime == "kubernetes" {
			return newKubernetes(cfg)
		}
		return newLocal(cfg), nil
	case "clickhouse":
		return newClickHouse(cfg)
	default:
		return nil, fmt.Errorf("unknown engine %q", cfg.Engine)
	}
}
