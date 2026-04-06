package destination

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"backuper/internal/config"
	"backuper/internal/secrets"

	"golang.org/x/crypto/ssh"
)

type SCPDestination struct {
	cfg   *config.DestinationConfig
	store secrets.Store
}

func newSCP(cfg *config.DestinationConfig, store secrets.Store) (*SCPDestination, error) {
	return &SCPDestination{cfg: cfg, store: store}, nil
}

func (d *SCPDestination) Name() string { return d.cfg.Name }
func (d *SCPDestination) Type() string { return "scp" }

func (d *SCPDestination) sshClient() (*ssh.Client, error) {
	sshCfg, err := d.buildSSHConfig()
	if err != nil {
		return nil, err
	}
	addr := net.JoinHostPort(d.cfg.Host, "22")
	client, err := ssh.Dial("tcp", addr, sshCfg)
	if err != nil {
		return nil, fmt.Errorf("ssh dial %s: %w", addr, err)
	}
	return client, nil
}

func (d *SCPDestination) buildSSHConfig() (*ssh.ClientConfig, error) {
	var authMethods []ssh.AuthMethod

	switch d.cfg.Auth {
	case "key", "":
		keyPath := expandHome(d.cfg.SSHKeyPath)
		if keyPath == "" {
			home, _ := os.UserHomeDir()
			keyPath = filepath.Join(home, ".ssh", "id_rsa")
		}
		keyData, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("reading ssh key %q: %w", keyPath, err)
		}
		signer, err := ssh.ParsePrivateKey(keyData)
		if err != nil {
			return nil, fmt.Errorf("parsing ssh key: %w", err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	case "password":
		pass, err := d.store.Get(d.cfg.SecretRef)
		if err != nil {
			return nil, fmt.Errorf("getting ssh password: %w", err)
		}
		authMethods = append(authMethods, ssh.Password(pass))
	default:
		return nil, fmt.Errorf("unknown auth type %q", d.cfg.Auth)
	}

	return &ssh.ClientConfig{
		User:            d.cfg.User,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // configurable in future
	}, nil
}

func (d *SCPDestination) Transfer(_ context.Context, localPath string) error {
	client, err := d.sshClient()
	if err != nil {
		return err
	}
	defer client.Close()

	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("opening %q: %w", localPath, err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat %q: %w", localPath, err)
	}

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("new ssh session: %w", err)
	}
	defer session.Close()

	// Use scp sink mode on the remote end.
	remoteDir := d.cfg.RemotePath
	stdin, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}

	if err := session.Start(fmt.Sprintf("scp -qt %s", remoteDir)); err != nil {
		return fmt.Errorf("starting remote scp: %w", err)
	}

	readAck := func() error {
		buf := make([]byte, 1)
		if _, err := io.ReadFull(stdout, buf); err != nil {
			return fmt.Errorf("reading scp ack: %w", err)
		}
		if buf[0] != 0 {
			msg, _ := io.ReadAll(stdout)
			return fmt.Errorf("scp error (code %d): %s", buf[0], strings.TrimSpace(string(msg)))
		}
		return nil
	}

	if err := readAck(); err != nil {
		return fmt.Errorf("initial ack: %w", err)
	}

	// Send file header.
	header := fmt.Sprintf("C0644 %d %s\n", stat.Size(), filepath.Base(localPath))
	if _, err := fmt.Fprint(stdin, header); err != nil {
		return fmt.Errorf("sending scp header: %w", err)
	}
	if err := readAck(); err != nil {
		return fmt.Errorf("header ack: %w", err)
	}

	// Send file content.
	if _, err := io.Copy(stdin, f); err != nil {
		return fmt.Errorf("sending file data: %w", err)
	}

	// Send completion marker.
	if _, err := stdin.Write([]byte{0}); err != nil {
		return fmt.Errorf("sending scp terminator: %w", err)
	}
	if err := readAck(); err != nil {
		return fmt.Errorf("final ack: %w", err)
	}

	stdin.Close()
	return session.Wait()
}

func (d *SCPDestination) ListFiles(_ context.Context, targetName string) ([]string, error) {
	client, err := d.sshClient()
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

func (d *SCPDestination) DeleteFile(_ context.Context, filename string) error {
	client, err := d.sshClient()
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
