package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Targets       []TargetConfig       `yaml:"targets"`
	Destinations  []DestinationConfig  `yaml:"destinations"`
	Schedules     []ScheduleConfig     `yaml:"schedules"`
	Notifications []NotificationConfig `yaml:"notifications,omitempty"`
}

type NotificationConfig struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`          // "telegram" | "smtp"
	BotTokenRef string `yaml:"bot_token_ref"` // secret ref for bot token (telegram)
	ChatID      string `yaml:"chat_id"`       // group/chat ID (telegram)
	ThreadID    int    `yaml:"thread_id,omitempty"`
	OnSuccess   bool   `yaml:"on_success"`
	OnFailure   bool   `yaml:"on_failure"`

	// SMTP notification fields
	SMTPHost     string   `yaml:"smtp_host"`               // SMTP server hostname
	SMTPPort     int      `yaml:"smtp_port"`               // SMTP server port (default 587)
	From         string   `yaml:"from"`                    // sender email address
	To           []string `yaml:"to"`                      // recipient email addresses
	Username     string   `yaml:"username,omitempty"`      // SMTP auth username (defaults to from)
	PasswordRef  string   `yaml:"password_ref"`            // secret ref for SMTP password
	UseTLS       bool     `yaml:"use_tls,omitempty"`       // use STARTTLS (default true for port 587)
	InsecureTLS  bool     `yaml:"insecure_tls,omitempty"`  // skip TLS certificate verification
}

type TargetConfig struct {
	Name        string        `yaml:"name"`
	Engine      string        `yaml:"engine"`                // "postgres" | "clickhouse" | "redis"
	Runtime     string        `yaml:"runtime"`               // "local" | "kubernetes"
	Namespace   string        `yaml:"namespace,omitempty"`   // runtime=kubernetes
	PodSelector string        `yaml:"pod_selector,omitempty"` // runtime=kubernetes, regex
	DBUser      string        `yaml:"db_user"`
	DBName      string        `yaml:"db_name,omitempty"` // postgres: omit = pg_dumpall; clickhouse: required
	SecretRef   string        `yaml:"secret_ref,omitempty"`
	K8sSecret   *K8sSecretRef `yaml:"k8s_secret,omitempty"`
	Host        string        `yaml:"host,omitempty"` // clickhouse: server host
	Port        string        `yaml:"port,omitempty"` // clickhouse: server port
}

type K8sSecretRef struct {
	Name string `yaml:"name"`
	Key  string `yaml:"key"`
}

type DestinationConfig struct {
	Name       string `yaml:"name"`
	Type       string `yaml:"type"`        // "local" | "scp" | "rsync" | "s3"
	Host       string `yaml:"host"`        // scp/rsync
	User       string `yaml:"user"`        // scp/rsync
	RemotePath string `yaml:"remote_path"` // scp/rsync
	Path       string `yaml:"path"`        // local
	Auth       string `yaml:"auth"`        // "key" | "password"
	SSHKeyPath string `yaml:"ssh_key_path"`
	SecretRef  string `yaml:"secret_ref"` // password if auth=password

	// S3 specific fields (for AWS S3, Minio, and other S3-compatible storage)
	Bucket          string `yaml:"bucket"`            // S3 bucket name
	Endpoint        string `yaml:"endpoint"`          // S3 endpoint URL (for Minio/custom S3, empty for AWS)
	Region          string `yaml:"region"`            // AWS region (e.g., us-east-1), optional for Minio
	AccessKeyRef    string `yaml:"access_key_ref"`    // secret ref for access key ID
	SecretKeyRef    string `yaml:"secret_key_ref"`    // secret ref for secret access key
	SessionTokenRef string `yaml:"session_token_ref"` // secret ref for session token (optional)
	UseSSL          bool   `yaml:"use_ssl"`           // use HTTPS for S3 connections (default: true)
}

type ScheduleConfig struct {
	Target      string          `yaml:"target"`
	Destination string          `yaml:"destination"`
	Cron        string          `yaml:"cron"`
	Compress    string          `yaml:"compress"` // "gzip" | "none"
	TmpDir      string          `yaml:"tmp_dir"`
	Retention   RetentionConfig `yaml:"retention"`
}

// ScheduleType represents the type of backup schedule.
type ScheduleType string

const (
	ScheduleTypeDaily   ScheduleType = "daily"
	ScheduleTypeWeekly  ScheduleType = "weekly"
	ScheduleTypeMonthly ScheduleType = "monthly"
	ScheduleTypeYearly  ScheduleType = "yearly"
	ScheduleTypeCustom  ScheduleType = "custom"
)

// ScheduleType derives the schedule type from a cron expression.
// It inspects the cron string to determine the frequency:
//   - Yearly: "X Y 1 1 *" (specific day of month = 1 and specific month)
//   - Monthly: "X Y 1 * *" (day of month = 1, any month)
//   - Weekly: "X Y * * 0-6" (day of week specified, any day of month)
//   - Daily: "X Y * * *" (any day, any month, any day of week)
//   - Custom: anything else (multiple times per day, complex patterns)
func (s ScheduleConfig) ScheduleType() ScheduleType {
	cron := strings.TrimSpace(s.Cron)
	fields := strings.Fields(cron)
	if len(fields) < 5 {
		return ScheduleTypeCustom
	}

	minute, hour, dom, month, dow := fields[0], fields[1], fields[2], fields[3], fields[4]

	// Yearly: specific day (1) and specific month (not *), any day of week
	if dom == "1" && month != "*" && dow == "*" {
		return ScheduleTypeYearly
	}

	// Monthly: day of month = 1, any month, any day of week
	if dom == "1" && month == "*" && dow == "*" {
		return ScheduleTypeMonthly
	}

	// Weekly: day of week is specified (not *), any day of month
	if dow != "*" && dom == "*" {
		return ScheduleTypeWeekly
	}

	// Daily: any day, any month, any day of week (all *), specific hour/minute (not *, not */N)
	if dom == "*" && month == "*" && dow == "*" &&
		!strings.HasPrefix(minute, "*/") && !strings.HasPrefix(hour, "*/") {
		return ScheduleTypeDaily
	}

	return ScheduleTypeCustom
}

// ScheduleDir returns the subdirectory name for a backup based on schedule type and time.
// Examples: "daily", "weekly/2026-W14", "monthly/2026-04", "yearly/2026"
func (s ScheduleConfig) ScheduleDir(t time.Time) string {
	st := s.ScheduleType()
	switch st {
	case ScheduleTypeDaily:
		return "daily"
	case ScheduleTypeWeekly:
		_, week := t.ISOWeek()
		return fmt.Sprintf("weekly/%d-W%02d", t.Year(), week)
	case ScheduleTypeMonthly:
		return fmt.Sprintf("monthly/%s", t.Format("2006-01"))
	case ScheduleTypeYearly:
		return fmt.Sprintf("yearly/%d", t.Year())
	default:
		return ""
	}
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
		// Validate engine.
		switch t.Engine {
		case "postgres", "clickhouse", "redis":
		default:
			return fmt.Errorf("target %q: unknown engine %q (must be postgres, clickhouse, or redis)", t.Name, t.Engine)
		}
		// Validate runtime.
		switch t.Runtime {
		case "local", "remote", "kubernetes":
		default:
			return fmt.Errorf("target %q: unknown runtime %q (must be local, remote, or kubernetes)", t.Name, t.Runtime)
		}
		// Runtime-specific.
		if t.Runtime == "kubernetes" {
			if t.Namespace == "" {
				return fmt.Errorf("target %q: namespace required for kubernetes runtime", t.Name)
			}
			if t.PodSelector == "" {
				return fmt.Errorf("target %q: pod_selector required for kubernetes runtime", t.Name)
			}
		}
		// Engine-specific.
		if t.Engine == "clickhouse" {
			if t.DBName == "" {
				return fmt.Errorf("target %q: db_name is required for clickhouse engine", t.Name)
			}
			if t.Host == "" && t.Runtime != "kubernetes" {
				return fmt.Errorf("target %q: host is required for clickhouse with %s runtime", t.Name, t.Runtime)
			}
		}
		if t.DBUser == "" && t.Engine != "redis" {
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
		case "s3":
			if d.Bucket == "" {
				return fmt.Errorf("destination %q: bucket required for s3 type", d.Name)
			}
			if d.AccessKeyRef == "" && d.SecretKeyRef == "" {
				return fmt.Errorf("destination %q: at least one of access_key_ref or secret_key_ref required for s3 type", d.Name)
			}
		default:
			return fmt.Errorf("destination %q: unknown type %q (must be local, scp, rsync, or s3)", d.Name, d.Type)
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
