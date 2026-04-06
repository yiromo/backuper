package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"filippo.io/age"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"backuper/internal/backup"
	"backuper/internal/config"
	"backuper/internal/scheduler"
	"backuper/internal/secrets"
	"backuper/internal/tui"
)

var (
	cfgPath        string
	passphraseFile string
	savePassphrase bool
	globalCfg      *config.Config
	globalLog      *slog.Logger
	globalHist     *backup.HistoryDB
	globalStor     secrets.Store
)

func main() {
	root := &cobra.Command{
		Use:   "backuper",
		Short: "TUI-based PostgreSQL backup manager for Kubernetes and local Postgres",
		Long: `backuper is a k9s-style TUI for managing PostgreSQL backups.
Run without sub-commands to open the interactive TUI.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI()
		},
		SilenceUsage: true,
	}
	root.PersistentFlags().StringVar(&cfgPath, "config", config.DefaultConfigPath(), "path to config file")
	root.PersistentFlags().StringVar(&passphraseFile, "passphrase-file", "", "path to file containing secrets store passphrase (for daemon mode)")
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		// Skip initialisation for config validate (reads its own cfg).
		if cmd.Name() == "validate" {
			return nil
		}
		return initGlobals()
	}

	daemonCmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run the backup scheduler headlessly (no TUI)",
		Long: `Run the backup scheduler headlessly.

For first-time setup, use --save-passphrase to encrypt and store the
secrets passphrase so the daemon can start unattended on reboot.`,
		RunE: runDaemon,
	}
	daemonCmd.Flags().BoolVar(&savePassphrase, "save-passphrase", false, "encrypt and save the secrets passphrase for unattended daemon startup")

	runCmd := &cobra.Command{
		Use:   "run <target>",
		Short: "Run a one-shot backup for the given target",
		Args:  cobra.ExactArgs(1),
		RunE:  runOneShot,
	}
	runCmd.Flags().StringP("dest", "d", "", "destination name (interactive picker if omitted)")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List resources",
	}
	listCmd.AddCommand(
		&cobra.Command{
			Use:   "targets",
			Short: "List configured targets",
			RunE:  runListTargets,
		},
		&cobra.Command{
			Use:   "schedules",
			Short: "List configured schedules",
			RunE:  runListSchedules,
		},
	)
	historyCmd := &cobra.Command{
		Use:   "history",
		Short: "List backup history",
		RunE:  runListHistory,
	}
	historyCmd.Flags().StringP("target", "t", "", "filter by target name")
	historyCmd.Flags().IntP("limit", "l", 20, "number of records to show")
	listCmd.AddCommand(historyCmd)

	secretsCmd := &cobra.Command{
		Use:   "secrets",
		Short: "Manage the encrypted secrets store",
	}
	secretsCmd.AddCommand(
		&cobra.Command{
			Use:   "set <ref>",
			Short: "Set (or update) a secret",
			Args:  cobra.ExactArgs(1),
			RunE:  runSecretsSet,
		},
		&cobra.Command{
			Use:   "delete <ref>",
			Short: "Delete a secret",
			Args:  cobra.ExactArgs(1),
			RunE:  runSecretsDelete,
		},
		&cobra.Command{
			Use:   "list",
			Short: "List known secret refs",
			RunE:  runSecretsList,
		},
	)

	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Config management",
	}
	configCmd.AddCommand(
		&cobra.Command{
			Use:   "validate",
			Short: "Validate the config file",
			RunE: func(cmd *cobra.Command, args []string) error {
				_, err := config.Load(cfgPath)
				if err != nil {
					return err
				}
				fmt.Println("Config is valid.")
				return nil
			},
		},
	)

	root.AddCommand(daemonCmd, runCmd, listCmd, secretsCmd, configCmd)
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func initGlobals() error {
	// Logger — write to file, not stdout (stdout belongs to TUI).
	home, _ := os.UserHomeDir()
	logPath := filepath.Join(home, ".config", "backuper", "backuper.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0700); err != nil {
		return fmt.Errorf("creating log dir: %w", err)
	}
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("opening log file: %w", err)
	}
	globalLog = slog.New(slog.NewJSONHandler(logFile, &slog.HandlerOptions{Level: slog.LevelInfo}))

	globalCfg, err = config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	globalHist, err = backup.OpenHistoryDB(backup.DefaultHistoryPath())
	if err != nil {
		return fmt.Errorf("opening history db: %w", err)
	}

	storePath := secrets.DefaultStorePath()
	passphrase, err := promptPassphrase(storePath)
	if err != nil {
		return fmt.Errorf("secrets passphrase: %w", err)
	}

	// If --save-passphrase was given, encrypt and save it for future unattended starts.
	if savePassphrase && !secrets.Exists(savedPassphrasePath()) {
		if err := savePassphraseEncrypted(passphrase); err != nil {
			return fmt.Errorf("saving passphrase: %w", err)
		}
	}

	globalStor, err = secrets.NewAgeStore(storePath, passphrase)
	if err != nil {
		return fmt.Errorf("opening secrets store: %w", err)
	}

	return nil
}

func promptPassphrase(storePath string) (string, error) {
	// 0. Saved (encrypted) passphrase file — auto-detected for daemon mode.
	saved, err := loadSavedPassphrase()
	if err != nil {
		return "", err
	}
	if saved != "" {
		return saved, nil
	}

	// 1. Environment variable.
	if pass := os.Getenv("BACKUPER_PASSPHRASE"); pass != "" {
		return pass, nil
	}

	// 2. Explicit passphrase file (--passphrase-file flag).
	if passphraseFile != "" {
		data, err := os.ReadFile(passphraseFile)
		if err != nil {
			return "", fmt.Errorf("reading passphrase file %q: %w", passphraseFile, err)
		}
		return strings.TrimSpace(string(data)), nil
	}

	// 3. Interactive prompt.
	prompt := "Enter secrets passphrase"
	if !secrets.Exists(storePath) {
		prompt = "Create secrets passphrase (new store)"
	}
	fmt.Fprintf(os.Stderr, "%s: ", prompt)
	pass, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("reading passphrase: %w", err)
	}
	return string(pass), nil
}

// Saved passphrase file paths.
func savedPassphrasePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "backuper", ".backuper_passphrase")
}

func savedKeyPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "backuper", ".backuper_key")
}

// savePassphraseEncrypted generates an age X25519 keypair, encrypts the
// passphrase with the public key, and writes both to disk.
func savePassphraseEncrypted(passphrase string) error {
	// Generate a new X25519 key pair.
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return fmt.Errorf("creating age identity: %w", err)
	}

	// Encrypt the passphrase.
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, identity.Recipient())
	if err != nil {
		return fmt.Errorf("creating encryptor: %w", err)
	}
	if _, err := w.Write([]byte(passphrase)); err != nil {
		return fmt.Errorf("encrypting passphrase: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("finalizing encryption: %w", err)
	}

	// Write the encrypted passphrase and the identity (private key).
	dir := filepath.Dir(savedPassphrasePath())
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating dir: %w", err)
	}
	if err := os.WriteFile(savedPassphrasePath(), buf.Bytes(), 0600); err != nil {
		return fmt.Errorf("writing encrypted passphrase: %w", err)
	}

	// Write identity in the standard age format:
	// # created: <timestamp>
	// # public key: <pubkey>
	// AGE-SECRET-KEY-1...
	recipient := identity.Recipient().String()
	var idBuf bytes.Buffer
	idBuf.WriteString(fmt.Sprintf("# created: %s\n", "now"))
	idBuf.WriteString(fmt.Sprintf("# public key: %s\n", recipient))
	idBuf.WriteString(identity.String())
	if err := os.WriteFile(savedKeyPath(), idBuf.Bytes(), 0600); err != nil {
		return fmt.Errorf("writing identity key: %w", err)
	}

	fmt.Fprintln(os.Stderr, "Passphrase saved encrypted to:", savedPassphrasePath())
	fmt.Fprintln(os.Stderr, "Private key saved to:", savedKeyPath())
	fmt.Fprintln(os.Stderr, "Keep the private key secure. Daemon will auto-decrypt on future starts.")
	return nil
}

// loadSavedPassphrase reads and decrypts the saved passphrase.
// Returns empty string if no saved passphrase exists.
func loadSavedPassphrase() (string, error) {
	encPath := savedPassphrasePath()
	keyPath := savedKeyPath()
	if !secrets.Exists(encPath) || !secrets.Exists(keyPath) {
		return "", nil
	}

	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return "", fmt.Errorf("reading saved key: %w", err)
	}
	identities, err := age.ParseIdentities(bytes.NewReader(keyData))
	if err != nil {
		return "", fmt.Errorf("parsing saved key: %w", err)
	}

	encData, err := os.ReadFile(encPath)
	if err != nil {
		return "", fmt.Errorf("reading encrypted passphrase: %w", err)
	}
	r, err := age.Decrypt(bytes.NewReader(encData), identities...)
	if err != nil {
		return "", fmt.Errorf("decrypting saved passphrase: %w", err)
	}
	var passBuf bytes.Buffer
	if _, err := passBuf.ReadFrom(r); err != nil {
		return "", fmt.Errorf("reading decrypted data: %w", err)
	}

	return strings.TrimSpace(passBuf.String()), nil
}

func runTUI() error {
	runner := backup.NewRunner(globalCfg, globalStor, globalHist, globalLog)
	sched := scheduler.New(globalCfg, runner, globalLog)
	if err := sched.RegisterAll(); err != nil {
		return fmt.Errorf("registering schedules: %w", err)
	}
	sched.Start()
	defer sched.Stop()

	app := tui.New(globalCfg, globalStor, sched, globalHist, globalLog)
	return tui.Run(app)
}

func runDaemon(cmd *cobra.Command, args []string) error {
	runner := backup.NewRunner(globalCfg, globalStor, globalHist, globalLog)
	sched := scheduler.New(globalCfg, runner, globalLog)
	if err := sched.RegisterAll(); err != nil {
		return fmt.Errorf("registering schedules: %w", err)
	}
	sched.Start()
	fmt.Fprintln(os.Stderr, "backuper daemon running. Press Ctrl+C to stop.")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	fmt.Fprintln(os.Stderr, "Shutting down (waiting for running backups to finish)...")
	ctx := sched.Stop()
	<-ctx.Done()
	fmt.Fprintln(os.Stderr, "Done.")
	return nil
}

func runOneShot(cmd *cobra.Command, args []string) error {
	targetName := args[0]
	destName, _ := cmd.Flags().GetString("dest")

	if destName == "" {
		fmt.Println("Available destinations:")
		for i, d := range globalCfg.Destinations {
			fmt.Printf("  %d) %s (%s)\n", i+1, d.Name, d.Type)
		}
		fmt.Print("Select destination number: ")
		var idx int
		if _, err := fmt.Scan(&idx); err != nil || idx < 1 || idx > len(globalCfg.Destinations) {
			return fmt.Errorf("invalid selection")
		}
		destName = globalCfg.Destinations[idx-1].Name
	}

	runner := backup.NewRunner(globalCfg, globalStor, globalHist, globalLog)
	sched := scheduler.New(globalCfg, runner, globalLog)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	fmt.Fprintf(os.Stderr, "Starting backup: %s → %s\n", targetName, destName)
	_, err := sched.RunNow(ctx, targetName, destName, os.Stdout)
	return err
}

func runListTargets(cmd *cobra.Command, args []string) error {
	fmt.Printf("%-20s  %-12s  %-18s  %s\n", "NAME", "TYPE", "NAMESPACE/DB", "SECRET_REF")
	fmt.Println(strings.Repeat("-", 70))
	for _, t := range globalCfg.Targets {
		ns := t.Namespace
		if ns == "" {
			ns = t.DBName
		}
		fmt.Printf("%-20s  %-12s  %-18s  %s\n", t.Name, t.Type, ns, t.SecretRef)
	}
	return nil
}

func runListSchedules(cmd *cobra.Command, args []string) error {
	fmt.Printf("%-18s  %-18s  %-18s  %s\n", "TARGET", "DESTINATION", "CRON", "KEEP_LAST")
	fmt.Println(strings.Repeat("-", 70))
	for _, s := range globalCfg.Schedules {
		fmt.Printf("%-18s  %-18s  %-18s  %d\n", s.Target, s.Destination, s.Cron, s.Retention.KeepLast)
	}
	return nil
}

func runListHistory(cmd *cobra.Command, args []string) error {
	targetFilter, _ := cmd.Flags().GetString("target")
	limit, _ := cmd.Flags().GetInt("limit")

	recs, err := globalHist.Query(context.Background(), targetFilter, limit)
	if err != nil {
		return err
	}
	fmt.Printf("%-18s  %-18s  %-18s  %-8s  %-10s  %s\n",
		"TIMESTAMP", "TARGET", "DESTINATION", "STATUS", "SIZE", "DURATION")
	fmt.Println(strings.Repeat("-", 80))
	for _, r := range recs {
		size := humanBytes(r.SizeBytes)
		dur := fmt.Sprintf("%dms", r.DurationMs)
		fmt.Printf("%-18s  %-18s  %-18s  %-8s  %-10s  %s\n",
			r.CreatedAt.Format("2006-01-02 15:04"),
			r.Target, r.Destination, r.Status, size, dur)
	}
	return nil
}

func runSecretsSet(cmd *cobra.Command, args []string) error {
	ref := args[0]
	fmt.Fprintf(os.Stderr, "Value for %q: ", ref)
	val, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return fmt.Errorf("reading value: %w", err)
	}
	if err := globalStor.Set(ref, string(val)); err != nil {
		return fmt.Errorf("setting secret: %w", err)
	}
	fmt.Printf("Secret %q saved.\n", ref)
	return nil
}

func runSecretsDelete(cmd *cobra.Command, args []string) error {
	ref := args[0]
	fmt.Printf("Delete secret %q? [y/N]: ", ref)
	var confirm string
	fmt.Scan(&confirm)
	if confirm != "y" && confirm != "Y" {
		fmt.Println("Aborted.")
		return nil
	}
	if err := globalStor.Delete(ref); err != nil {
		return fmt.Errorf("deleting secret: %w", err)
	}
	fmt.Printf("Secret %q deleted.\n", ref)
	return nil
}

func runSecretsList(cmd *cobra.Command, args []string) error {
	refs, err := globalStor.List()
	if err != nil {
		return err
	}
	if len(refs) == 0 {
		fmt.Println("No secrets stored.")
		return nil
	}
	for _, r := range refs {
		fmt.Printf("  %s\n", r)
	}
	return nil
}

// humanBytes is duplicated from tui package to keep cmd self-contained.
func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
