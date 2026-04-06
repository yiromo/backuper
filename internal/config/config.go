package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Targets      []TargetConfig      `yaml:"targets"`
	Destinations []DestinationConfig `yaml:"destinations"`
	Schedules    []ScheduleConfig    `yaml:"schedules"`
}

type TargetConfig struct {
	Name        string        `yaml:"name"`
	Type        string        `yaml:"type"`         // "kubernetes" | "local"
	Namespace   string        `yaml:"namespace"`    // k8s only
	PodSelector string        `yaml:"pod_selector"` // k8s only, regex
	DBUser      string        `yaml:"db_user"`
	DBName      string        `yaml:"db_name"`    // local only; empty = pg_dumpall
	SecretRef   string        `yaml:"secret_ref"` // key in secrets store
	K8sSecret   *K8sSecretRef `yaml:"k8s_secret,omitempty"`
}

type K8sSecretRef struct {
	Name string `yaml:"name"`
	Key  string `yaml:"key"`
}

type DestinationConfig struct {
	Name       string `yaml:"name"`
	Type       string `yaml:"type"`        // "local" | "scp" | "rsync"
	Host       string `yaml:"host"`        // scp/rsync
	User       string `yaml:"user"`        // scp/rsync
	RemotePath string `yaml:"remote_path"` // scp/rsync
	Path       string `yaml:"path"`        // local
	Auth       string `yaml:"auth"`        // "key" | "password"
	SSHKeyPath string `yaml:"ssh_key_path"`
	SecretRef  string `yaml:"secret_ref"` // password if auth=password
}

type ScheduleConfig struct {
	Target      string          `yaml:"target"`
	Destination string          `yaml:"destination"`
	Cron        string          `yaml:"cron"`
	Compress    string          `yaml:"compress"`  // "gzip" | "none"
	TmpDir      string          `yaml:"tmp_dir"`
	Retention   RetentionConfig `yaml:"retention"`
}

type RetentionConfig struct {
	KeepLast int `yaml:"keep_last"`
}

func DefaultConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "backuper", "config.yaml")
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %q: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return &cfg, nil
}

func Save(cfg *Config, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshalling config: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}

func (c *Config) Validate() error {
	targetNames := make(map[string]bool)
	for i, t := range c.Targets {
		if t.Name == "" {
			return fmt.Errorf("targets[%d]: name is required", i)
		}
		if targetNames[t.Name] {
			return fmt.Errorf("targets[%d]: duplicate name %q", i, t.Name)
		}
		targetNames[t.Name] = true
		switch t.Type {
		case "kubernetes":
			if t.Namespace == "" {
				return fmt.Errorf("target %q: namespace required for kubernetes type", t.Name)
			}
			if t.PodSelector == "" {
				return fmt.Errorf("target %q: pod_selector required for kubernetes type", t.Name)
			}
		case "local":
		default:
			return fmt.Errorf("target %q: unknown type %q (must be kubernetes or local)", t.Name, t.Type)
		}
		if t.DBUser == "" {
			return fmt.Errorf("target %q: db_user is required", t.Name)
		}
		if t.SecretRef == "" && t.K8sSecret == nil {
			return fmt.Errorf("target %q: secret_ref or k8s_secret is required", t.Name)
		}
	}

	destNames := make(map[string]bool)
	for i, d := range c.Destinations {
		if d.Name == "" {
			return fmt.Errorf("destinations[%d]: name is required", i)
		}
		if destNames[d.Name] {
			return fmt.Errorf("destinations[%d]: duplicate name %q", i, d.Name)
		}
		destNames[d.Name] = true
		switch d.Type {
		case "local":
			if d.Path == "" {
				return fmt.Errorf("destination %q: path required for local type", d.Name)
			}
		case "scp", "rsync":
			if d.Host == "" {
				return fmt.Errorf("destination %q: host required for %s type", d.Name, d.Type)
			}
			if d.User == "" {
				return fmt.Errorf("destination %q: user required for %s type", d.Name, d.Type)
			}
			if d.RemotePath == "" {
				return fmt.Errorf("destination %q: remote_path required for %s type", d.Name, d.Type)
			}
		default:
			return fmt.Errorf("destination %q: unknown type %q (must be local, scp, or rsync)", d.Name, d.Type)
		}
	}

	for i, s := range c.Schedules {
		if s.Target == "" {
			return fmt.Errorf("schedules[%d]: target is required", i)
		}
		if s.Destination == "" {
			return fmt.Errorf("schedules[%d]: destination is required", i)
		}
		if s.Cron == "" {
			return fmt.Errorf("schedules[%d]: cron is required", i)
		}
		if !targetNames[s.Target] {
			return fmt.Errorf("schedule %d: unknown target %q", i, s.Target)
		}
		if !destNames[s.Destination] {
			return fmt.Errorf("schedule %d: unknown destination %q", i, s.Destination)
		}
	}
	return nil
}

func (c *Config) FindTarget(name string) (*TargetConfig, error) {
	for i := range c.Targets {
		if c.Targets[i].Name == name {
			return &c.Targets[i], nil
		}
	}
	return nil, fmt.Errorf("target %q not found", name)
}

func (c *Config) FindDestination(name string) (*DestinationConfig, error) {
	for i := range c.Destinations {
		if c.Destinations[i].Name == name {
			return &c.Destinations[i], nil
		}
	}
	return nil, fmt.Errorf("destination %q not found", name)
}

func (c *Config) SchedulesForTarget(targetName string) []ScheduleConfig {
	var out []ScheduleConfig
	for _, s := range c.Schedules {
		if s.Target == targetName {
			out = append(out, s)
		}
	}
	return out
}
