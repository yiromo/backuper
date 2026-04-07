package target

import (
	"bytes"
	"context"
	"fmt"
	"io"
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

type KubernetesTarget struct {
	cfg        *config.TargetConfig
	restConfig *rest.Config
	clientset  *kubernetes.Clientset
}

func newKubernetes(cfg *config.TargetConfig) (*KubernetesTarget, error) {
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
	return &KubernetesTarget{cfg: cfg, restConfig: restCfg, clientset: cs}, nil
}

func (t *KubernetesTarget) Name() string { return t.cfg.Name }
func (t *KubernetesTarget) Type() string { return "kubernetes" }

func (t *KubernetesTarget) GetPassword(ctx context.Context, store secrets.Store) (string, error) {
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
		return string(val), nil // client-go already base64-decodes Secret.Data
	}
	if t.cfg.SecretRef == "" {
		return "", fmt.Errorf("no secret_ref or k8s_secret configured for target %q", t.cfg.Name)
	}
	return store.Get(t.cfg.SecretRef)
}

func (t *KubernetesTarget) Dump(ctx context.Context, w io.Writer, password string) error {
	podName, err := t.findPod(ctx)
	if err != nil {
		return fmt.Errorf("finding pod: %w", err)
	}

	var dumpCmd string
	if t.cfg.DBName == "" {
		dumpCmd = fmt.Sprintf("PGPASSWORD='%s' pg_dumpall -U %s", password, t.cfg.DBUser)
	} else {
		dumpCmd = fmt.Sprintf("PGPASSWORD='%s' pg_dump -U %s %s", password, t.cfg.DBUser, t.cfg.DBName)
	}

	req := t.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(t.cfg.Namespace).
		SubResource("exec")

	req.VersionedParams(&corev1.PodExecOptions{
		Command: []string{"bash", "-c", dumpCmd},
		Stdin:   false,
		Stdout:  true,
		Stderr:  true,
		TTY:     false,
	}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(t.restConfig, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("creating SPDY executor: %w", err)
	}

	var errBuf bytes.Buffer
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: w,
		Stderr: &errBuf,
	})
	if err != nil {
		return fmt.Errorf("exec stream (stderr=%s): %w", errBuf.String(), err)
	}
	return nil
}

// findPod lists pods in the target namespace and returns the first pod name
// whose name matches cfg.PodSelector (a regex).
func (t *KubernetesTarget) findPod(ctx context.Context) (string, error) {
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
