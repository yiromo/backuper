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
	Type() string
	GetPassword(ctx context.Context, store secrets.Store) (string, error)
	Dump(ctx context.Context, w io.Writer, password string) error
}

func New(cfg *config.TargetConfig) (Target, error) {
	switch cfg.Type {
	case "kubernetes":
		return newKubernetes(cfg)
	case "local":
		return newLocal(cfg), nil
	default:
		return nil, fmt.Errorf("unknown target type %q", cfg.Type)
	}
}
