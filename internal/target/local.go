package target

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"

	"backuper/internal/config"
	"backuper/internal/secrets"
)

type LocalTarget struct {
	cfg *config.TargetConfig
}

func newLocal(cfg *config.TargetConfig) *LocalTarget {
	return &LocalTarget{cfg: cfg}
}

func (t *LocalTarget) Name() string { return t.cfg.Name }
func (t *LocalTarget) Type() string    { return "local" }
func (t *LocalTarget) FileExt() string { return ".sql" }

func (t *LocalTarget) GetPassword(_ context.Context, store secrets.Store) (string, error) {
	if t.cfg.SecretRef == "" {
		return "", fmt.Errorf("no secret_ref configured for target %q", t.cfg.Name)
	}
	return store.Get(t.cfg.SecretRef)
}

// Dump runs pg_dumpall (or pg_dump if db_name is set) and streams output to w.
func (t *LocalTarget) Dump(ctx context.Context, w io.Writer, password string) error {
	var cmd *exec.Cmd
	if t.cfg.DBName == "" {
		cmd = exec.CommandContext(ctx, "pg_dumpall", "-U", t.cfg.DBUser)
	} else {
		cmd = exec.CommandContext(ctx, "pg_dump", "-U", t.cfg.DBUser, t.cfg.DBName)
	}
	cmd.Env = append(os.Environ(), "PGPASSWORD="+password)
	cmd.Stdout = w

	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pg_dump: %w (stderr: %s)", err, errBuf.String())
	}
	return nil
}
