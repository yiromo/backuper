package destination

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sort"
	"strings"

	"backuper/internal/config"
	"backuper/internal/secrets"

	"golang.org/x/crypto/ssh"
)

// RsyncDestination transfers files using the rsync binary over SSH.
// For key auth it passes -e "ssh -i <key>"; for password auth it uses sshpass.
type RsyncDestination struct {
	cfg   *config.DestinationConfig
	store secrets.Store
}

func newRsync(cfg *config.DestinationConfig, store secrets.Store) *RsyncDestination {
	return &RsyncDestination{cfg: cfg, store: store}
}

func (d *RsyncDestination) Name() string { return d.cfg.Name }
func (d *RsyncDestination) Type() string { return "rsync" }

func (d *RsyncDestination) Transfer(ctx context.Context, localPath string, targetDir string) error {
	remotePath := d.cfg.RemotePath
	if targetDir != "" {
		remotePath = remotePath + "/" + targetDir
	}
	remote := fmt.Sprintf("%s@%s:%s", d.cfg.User, d.cfg.Host, remotePath)

	var cmd *exec.Cmd
	switch d.cfg.Auth {
	case "password":
		pass, err := d.store.Get(d.cfg.SecretRef)
		if err != nil {
			return fmt.Errorf("getting ssh password for rsync: %w", err)
		}
		cmd = exec.CommandContext(ctx,
			"sshpass", "-p", pass,
			"rsync", "-az", "-e", "ssh -o StrictHostKeyChecking=no",
			localPath, remote,
		)
	default: // "key" or empty
		keyPath := expandHome(d.cfg.SSHKeyPath)
		sshOpt := fmt.Sprintf("ssh -i %s -o StrictHostKeyChecking=no", keyPath)
		cmd = exec.CommandContext(ctx,
			"rsync", "-az", "-e", sshOpt,
			localPath, remote,
		)
	}

	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rsync: %w (stderr: %s)", err, errBuf.String())
	}
	return nil
}

func (d *RsyncDestination) sshClientForListing() (*ssh.Client, error) {
	var authMethods []ssh.AuthMethod
	switch d.cfg.Auth {
	case "password":
		pass, err := d.store.Get(d.cfg.SecretRef)
		if err != nil {
			return nil, fmt.Errorf("getting password: %w", err)
		}
		authMethods = append(authMethods, ssh.Password(pass))
	default:
		keyPath := expandHome(d.cfg.SSHKeyPath)
		keyData, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("reading ssh key %q: %w", keyPath, err)
		}
		signer, err := ssh.ParsePrivateKey(keyData)
		if err != nil {
			return nil, fmt.Errorf("parsing ssh key: %w", err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}
	cfg := &ssh.ClientConfig{
		User:            d.cfg.User,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec
	}
	addr := net.JoinHostPort(d.cfg.Host, "22")
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, fmt.Errorf("ssh dial %s: %w", addr, err)
	}
	return client, nil
}

func (d *RsyncDestination) ListFiles(_ context.Context, targetName string) ([]string, error) {
	client, err := d.sshClientForListing()
	if err != nil {
		return nil, err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("new ssh session: %w", err)
	}
	defer session.Close()

	pattern := fmt.Sprintf("%s/%s_*.sql.gz", d.cfg.RemotePath, targetName)
	out, err := session.Output(fmt.Sprintf("ls %s 2>/dev/null || true", pattern))
	if err != nil {
		return nil, nil
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil, nil
	}
	files := strings.Split(raw, "\n")
	sort.Strings(files)
	return files, nil
}

func (d *RsyncDestination) DeleteFile(_ context.Context, filename string) error {
	client, err := d.sshClientForListing()
	if err != nil {
		return err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("new ssh session: %w", err)
	}
	defer session.Close()

	if err := session.Run(fmt.Sprintf("rm -f %s", filename)); err != nil {
		return fmt.Errorf("deleting remote file %q: %w", filename, err)
	}
	return nil
}
