package target

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"

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

type RedisTarget struct {
	cfg        *config.TargetConfig
	restConfig *rest.Config
	clientset  *kubernetes.Clientset
}

func newRedisTarget(cfg *config.TargetConfig) (*RedisTarget, error) {
	t := &RedisTarget{cfg: cfg}
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

func (t *RedisTarget) Name() string    { return t.cfg.Name }
func (t *RedisTarget) Engine() string  { return "redis" }
func (t *RedisTarget) Runtime() string { return t.cfg.Runtime }
func (t *RedisTarget) FileExt() string { return ".rdb" }

func (t *RedisTarget) GetPassword(ctx context.Context, store secrets.Store) (string, error) {
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

func (t *RedisTarget) Dump(ctx context.Context, w io.Writer, password string) error {
	if t.cfg.Runtime == "kubernetes" {
		return t.dumpK8s(ctx, w, password)
	}
	if t.cfg.Runtime == "docker" {
		return t.dumpDocker(ctx, w, password)
	}
	return t.dumpLocal(ctx, w, password)
}

func (t *RedisTarget) host() string {
	if t.cfg.Host != "" {
		return t.cfg.Host
	}
	return "localhost"
}

func (t *RedisTarget) port() string {
	if t.cfg.Port != "" {
		return t.cfg.Port
	}
	return "6379"
}

// dumpLocal runs redis-cli --rdb to produce an RDB dump and streams it to w.
func (t *RedisTarget) dumpLocal(ctx context.Context, w io.Writer, password string) error {
	tmpFile, err := os.CreateTemp("", "redis-backup-*.rdb")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	args := []string{
		"-h", t.host(),
		"-p", t.port(),
		"-a", password,
		"--no-auth-warning",
		"--rdb", tmpPath,
	}

	cmd := exec.CommandContext(ctx, "redis-cli", args...)
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("redis-cli --rdb: %w (stderr: %s)", err, errBuf.String())
	}

	f, err := os.Open(tmpPath)
	if err != nil {
		return fmt.Errorf("opening rdb file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(w, f); err != nil {
		return fmt.Errorf("streaming rdb: %w", err)
	}
	return nil
}

// dumpDocker executes redis-cli inside a Docker container via docker exec.
func (t *RedisTarget) dumpDocker(ctx context.Context, w io.Writer, password string) error {
	script := fmt.Sprintf(
		`redis-cli -h %s -p %s -a '%s' --no-auth-warning --rdb /tmp/backup.rdb && cat /tmp/backup.rdb && rm -f /tmp/backup.rdb`,
		t.host(), t.port(), password,
	)

	cmd := exec.CommandContext(ctx, "docker", "exec", t.cfg.ContainerName, "bash", "-c", script)
	cmd.Stdout = w

	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker exec redis dump (stderr=%s): %w", errBuf.String(), err)
	}
	return nil
}

// dumpK8s executes redis-cli inside a Kubernetes pod to produce an RDB dump
// and streams it back via pod exec stdout.
func (t *RedisTarget) dumpK8s(ctx context.Context, w io.Writer, password string) error {
	podName, err := t.findPod(ctx)
	if err != nil {
		return fmt.Errorf("finding pod: %w", err)
	}

	script := fmt.Sprintf(
		`redis-cli -h %s -p %s -a '%s' --no-auth-warning --rdb /tmp/backup.rdb && cat /tmp/backup.rdb && rm -f /tmp/backup.rdb`,
		t.host(), t.port(), password,
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
		return fmt.Errorf("redis k8s exec (stderr=%s): %w", errBuf.String(), err)
	}
	return nil
}

// findPod lists pods and returns the first running pod matching the selector regex.
func (t *RedisTarget) findPod(ctx context.Context) (string, error) {
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
