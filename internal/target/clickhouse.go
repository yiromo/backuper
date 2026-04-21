package target

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"backuper/internal/config"
	"backuper/internal/secrets"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
)

type ClickHouseTarget struct {
	cfg        *config.TargetConfig
	restConfig *rest.Config
	clientset  *kubernetes.Clientset
}

func newClickHouse(cfg *config.TargetConfig) (*ClickHouseTarget, error) {
	t := &ClickHouseTarget{cfg: cfg}
	if cfg.Runtime == "kubernetes" {
		loadRules := clientcmd.NewDefaultClientConfigLoadingRules()
		kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			loadRules, &clientcmd.ConfigOverrides{},
		)
		restCfg, err := kubeConfig.ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("loading kubeconfig: %w", err)
		}
		cs, err := kubernetes.NewForConfig(restCfg)
		if err != nil {
			return nil, fmt.Errorf("creating k8s client: %w", err)
		}
		t.restConfig = restCfg
		t.clientset = cs
	}
	return t, nil
}

func (t *ClickHouseTarget) Name() string     { return t.cfg.Name }
func (t *ClickHouseTarget) Engine() string   { return "clickhouse" }
func (t *ClickHouseTarget) Runtime() string  { return t.cfg.Runtime }
func (t *ClickHouseTarget) FileExt() string  { return ".tar" }

// isLocal returns true for local and remote runtimes (both run client binary locally).
func (t *ClickHouseTarget) isLocal() bool {
	return t.cfg.Runtime != "kubernetes"
}

func (t *ClickHouseTarget) GetPassword(ctx context.Context, store secrets.Store) (string, error) {
	if t.cfg.K8sSecret != nil {
		secret, err := t.clientset.CoreV1().Secrets(t.cfg.Namespace).Get(
			ctx, t.cfg.K8sSecret.Name, metav1.GetOptions{},
		)
		if err != nil {
			return "", fmt.Errorf("fetching k8s secret %q: %w", t.cfg.K8sSecret.Name, err)
		}
		val, ok := secret.Data[t.cfg.K8sSecret.Key]
		if !ok {
			return "", fmt.Errorf("key %q not found in k8s secret %q", t.cfg.K8sSecret.Key, t.cfg.K8sSecret.Name)
		}
		return string(val), nil
	}
	if t.cfg.SecretRef == "" {
		return "", fmt.Errorf("no secret_ref or k8s_secret configured for target %q", t.cfg.Name)
	}
	return store.Get(t.cfg.SecretRef)
}

func (t *ClickHouseTarget) Dump(ctx context.Context, w io.Writer, password string) error {
	if t.cfg.Runtime == "kubernetes" {
		return t.dumpK8s(ctx, w, password)
	}
	if t.cfg.Runtime == "docker" {
		return t.dumpDocker(ctx, w, password)
	}
	return t.dumpLocal(ctx, w, password)
}

// dumpLocal runs clickhouse-client locally, dumps schema + data into temp files,
// then streams a tar archive to w.
func (t *ClickHouseTarget) dumpLocal(ctx context.Context, w io.Writer, password string) error {
	tmpDir, err := os.MkdirTemp("", "ch-backup-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Get table list.
	tables, err := t.localQuery(ctx, password,
		fmt.Sprintf("SELECT name FROM system.tables WHERE database = '%s'", t.cfg.DBName))
	if err != nil {
		return fmt.Errorf("listing tables: %w", err)
	}
	tableNames := strings.Split(strings.TrimSpace(tables), "\n")

	// Dump schema — query each table individually to preserve multi-line DDL.
	var schemaBuf bytes.Buffer
	for _, tbl := range tableNames {
		tbl = strings.TrimSpace(tbl)
		if tbl == "" {
			continue
		}
		ddl, err := t.localQuery(ctx, password,
			fmt.Sprintf("SHOW CREATE TABLE %s.%s", t.cfg.DBName, tbl))
		if err != nil {
			return fmt.Errorf("dumping schema for %s: %w", tbl, err)
		}
		schemaBuf.WriteString(ddl)
		if !strings.HasSuffix(strings.TrimSpace(ddl), ";") {
			schemaBuf.WriteByte(';')
		}
		schemaBuf.WriteString("\n\n")
	}
	schemaPath := filepath.Join(tmpDir, "schema.sql")
	if err := os.WriteFile(schemaPath, schemaBuf.Bytes(), 0644); err != nil {
		return fmt.Errorf("writing schema: %w", err)
	}

	// Dump each table.
	for _, tbl := range tableNames {
		tbl = strings.TrimSpace(tbl)
		if tbl == "" {
			continue
		}
		dataPath := filepath.Join(tmpDir, tbl+".native")
		f, err := os.Create(dataPath)
		if err != nil {
			return fmt.Errorf("creating data file for %s: %w", tbl, err)
		}
		if err := t.localQueryToWriter(ctx, password,
			fmt.Sprintf("SELECT * FROM %s.%s FORMAT Native", t.cfg.DBName, tbl), f); err != nil {
			f.Close()
			return fmt.Errorf("dumping table %s: %w", tbl, err)
		}
		f.Close()
	}

	// Stream tar archive.
	return writeTarDir(tmpDir, w)
}

// dumpK8s executes a script inside a Kubernetes pod that dumps schema + data
// and streams a tar archive back via pod exec stdout.
func (t *ClickHouseTarget) dumpK8s(ctx context.Context, w io.Writer, password string) error {
	podName, err := t.findPod(ctx)
	if err != nil {
		return fmt.Errorf("finding pod: %w", err)
	}

	host := t.cfg.Host
	if host == "" {
		host = "localhost"
	}

	// Build a single bash script that dumps everything and outputs a tar.
	script := fmt.Sprintf(
		`cd /tmp && rm -rf chdump && mkdir -p chdump && `+
			`clickhouse-client --host=%s --user=%s --password=%s --port=%s `+
			`--query="SELECT create_table_query FROM system.tables WHERE database = '%s'" > chdump/schema.sql && `+
			`for t in $(clickhouse-client --host=%s --user=%s --password=%s --port=%s `+
			`--query="SELECT name FROM system.tables WHERE database = '%s'"); do `+
			`clickhouse-client --host=%s --user=%s --password=%s --port=%s `+
			`--query="SELECT * FROM %s.$t FORMAT Native" > "chdump/$t.native"; `+
			`done && tar cf - -C /tmp/chdump . && rm -rf /tmp/chdump`,
		host, t.cfg.DBUser, password, t.portArg(),
		t.cfg.DBName,
		host, t.cfg.DBUser, password, t.portArg(),
		t.cfg.DBName,
		host, t.cfg.DBUser, password, t.portArg(),
		t.cfg.DBName,
	)

	req := t.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(t.cfg.Namespace).
		SubResource("exec")

	req.VersionedParams(&corev1.PodExecOptions{
		Command: []string{"bash", "-c", script},
		Stdin:   false,
		Stdout:  true,
		Stderr:  true,
		TTY:     false,
	}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(t.restConfig, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("creating SPDY executor: %w", err)
	}

	var errBuf bytes.Buffer
	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: w,
		Stderr: &errBuf,
	})
	if err != nil {
		return fmt.Errorf("clickhouse k8s exec (stderr=%s): %w", errBuf.String(), err)
	}
	return nil
}

// dumpDocker executes the dump script inside a Docker container via docker exec.
func (t *ClickHouseTarget) dumpDocker(ctx context.Context, w io.Writer, password string) error {
	host := t.cfg.Host
	if host == "" {
		host = "localhost"
	}

	script := fmt.Sprintf(
		`cd /tmp && rm -rf chdump && mkdir -p chdump && `+
			`clickhouse-client --host=%s --user=%s --password=%s --port=%s `+
			`--query="SELECT create_table_query FROM system.tables WHERE database = '%s'" > chdump/schema.sql && `+
			`for t in $(clickhouse-client --host=%s --user=%s --password=%s --port=%s `+
			`--query="SELECT name FROM system.tables WHERE database = '%s'"); do `+
			`clickhouse-client --host=%s --user=%s --password=%s --port=%s `+
			`--query="SELECT * FROM %s.$t FORMAT Native" > "chdump/$t.native"; `+
			`done && tar cf - -C /tmp/chdump . && rm -rf /tmp/chdump`,
		host, t.cfg.DBUser, password, t.portArg(),
		t.cfg.DBName,
		host, t.cfg.DBUser, password, t.portArg(),
		t.cfg.DBName,
		host, t.cfg.DBUser, password, t.portArg(),
		t.cfg.DBName,
	)

	cmd := exec.CommandContext(ctx, "docker", "exec", t.cfg.ContainerName, "bash", "-c", script)
	cmd.Stdout = w

	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker exec clickhouse dump (stderr=%s): %w", errBuf.String(), err)
	}
	return nil
}

// localQuery runs a clickhouse-client query and returns the stdout as a string.
func (t *ClickHouseTarget) localQuery(ctx context.Context, password, query string) (string, error) {
	var buf bytes.Buffer
	err := t.localQueryToWriter(ctx, password, query, &buf)
	return buf.String(), err
}

// localQueryToWriter runs a clickhouse-client query and pipes stdout to w.
func (t *ClickHouseTarget) localQueryToWriter(ctx context.Context, password, query string, w io.Writer) error {
	args := []string{"--host", t.cfg.Host}
	if t.cfg.Port != "" {
		args = append(args, "--port", t.cfg.Port)
	}
	args = append(args,
		"--user", t.cfg.DBUser,
		"--query", query,
	)

	cmd := exec.CommandContext(ctx, "clickhouse-client", args...)
	cmd.Env = append(os.Environ(), "CLICKHOUSE_PASSWORD="+password)
	cmd.Stdout = w

	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("clickhouse-client: %w (stderr: %s)", err, errBuf.String())
	}
	return nil
}

func (t *ClickHouseTarget) portArg() string {
	if t.cfg.Port != "" {
		return t.cfg.Port
	}
	return "9000"
}

// findPod lists pods and returns the first running pod matching the selector regex.
func (t *ClickHouseTarget) findPod(ctx context.Context) (string, error) {
	re, err := regexp.Compile(t.cfg.PodSelector)
	if err != nil {
		return "", fmt.Errorf("invalid pod_selector regex %q: %w", t.cfg.PodSelector, err)
	}

	pods, err := t.clientset.CoreV1().Pods(t.cfg.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("listing pods in namespace %q: %w", t.cfg.Namespace, err)
	}

	for _, p := range pods.Items {
		if p.Status.Phase == corev1.PodRunning && re.MatchString(p.Name) {
			return p.Name, nil
		}
	}
	return "", fmt.Errorf("no running pod matching %q in namespace %q", t.cfg.PodSelector, t.cfg.Namespace)
}

// writeTarDir writes all files in dir as a tar archive to w.
func writeTarDir(dir string, w io.Writer) error {
	tw := tar.NewWriter(w)
	defer tw.Close()

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("reading temp dir: %w", err)
	}

	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if entry.IsDir() {
			continue
		}

		hdr := &tar.Header{
			Name: entry.Name(),
			Size: info.Size(),
			Mode: 0644,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("writing tar header for %s: %w", entry.Name(), err)
		}

		f, err := os.Open(filepath.Join(dir, entry.Name()))
		if err != nil {
			return fmt.Errorf("opening %s: %w", entry.Name(), err)
		}
		if _, err := io.Copy(tw, f); err != nil {
			f.Close()
			return fmt.Errorf("writing tar data for %s: %w", entry.Name(), err)
		}
		f.Close()
	}
	return nil
}
