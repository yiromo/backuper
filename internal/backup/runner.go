package backup

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"backuper/internal/config"
	"backuper/internal/destination"
	"backuper/internal/secrets"
	"backuper/internal/target"
)

type Runner struct {
	cfg     *config.Config
	secrets secrets.Store
	histDB  *HistoryDB
	logger  *slog.Logger
}

func NewRunner(cfg *config.Config, store secrets.Store, histDB *HistoryDB, logger *slog.Logger) *Runner {
	return &Runner{cfg: cfg, secrets: store, histDB: histDB, logger: logger}
}

type RunOptions struct {
	Target      string
	Destination string
	Compress    string // "gzip" | "none" | ""
	TmpDir      string
	Retention   config.RetentionConfig
	// ScheduleType is the schedule type (daily, weekly, monthly, yearly).
	// Used to determine the subdirectory for the backup file.
	ScheduleType config.ScheduleType
}

// Run executes a full backup run and returns the history record.
// logW receives human-readable progress lines during the run.
func (r *Runner) Run(ctx context.Context, opts RunOptions, logW io.Writer) (*Record, error) {
	log := func(format string, args ...any) {
		line := fmt.Sprintf(format, args...)
		fmt.Fprintln(logW, line)
		r.logger.Info(line, "target", opts.Target, "destination", opts.Destination)
	}

	start := time.Now()
	rec := &Record{
		CreatedAt:   start,
		Target:      opts.Target,
		Destination: opts.Destination,
		Status:      "failure",
	}

	tgtCfg, err := r.cfg.FindTarget(opts.Target)
	if err != nil {
		return rec, r.fail(ctx, rec, err, logW)
	}

	dstCfg, err := r.cfg.FindDestination(opts.Destination)
	if err != nil {
		return rec, r.fail(ctx, rec, err, logW)
	}

	tgt, err := target.New(tgtCfg)
	if err != nil {
		return rec, r.fail(ctx, rec, fmt.Errorf("creating target: %w", err), logW)
	}

	dst, err := destination.New(dstCfg, r.secrets)
	if err != nil {
		return rec, r.fail(ctx, rec, fmt.Errorf("creating destination: %w", err), logW)
	}

	log("Fetching credentials for %s...", opts.Target)
	password, err := tgt.GetPassword(ctx, r.secrets)
	if err != nil {
		return rec, r.fail(ctx, rec, fmt.Errorf("fetching password: %w", err), logW)
	}

	tmpDir := opts.TmpDir
	if tmpDir == "" {
		tmpDir = os.TempDir()
	}
	ts := start.Format("2006-01-02_15-04")
	ext := ".sql"
	compress := strings.ToLower(opts.Compress)
	if compress == "" {
		compress = "gzip"
	}
	if compress == "gzip" {
		ext += ".gz"
	}
	tmpName := fmt.Sprintf("%s_%s%s", opts.Target, ts, ext)
	tmpPath := filepath.Join(tmpDir, tmpName)
	rec.Filename = tmpName

	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return rec, r.fail(ctx, rec, fmt.Errorf("creating temp file: %w", err), logW)
	}
	// Clean up temp file on failure; keep it on success.
	deleteTmp := true
	defer func() {
		if deleteTmp {
			os.Remove(tmpPath)
		}
	}()

	// Stream dump → compress → temp file.
	log("Starting dump of %s...", opts.Target)
	var dumpW io.Writer = tmpFile
	var gzW *gzip.Writer
	if compress == "gzip" {
		gzW = gzip.NewWriter(tmpFile)
		dumpW = gzW
	}

	var logBuf bytes.Buffer
	logTee := io.MultiWriter(logW, &logBuf)
	progressW := &progressWriter{w: dumpW, logW: logTee, label: "dump"}

	if err := tgt.Dump(ctx, progressW, password); err != nil {
		return rec, r.fail(ctx, rec, fmt.Errorf("dump: %w", err), logW)
	}

	if gzW != nil {
		if err := gzW.Close(); err != nil {
			return rec, r.fail(ctx, rec, fmt.Errorf("closing gzip: %w", err), logW)
		}
	}
	if err := tmpFile.Close(); err != nil {
		return rec, r.fail(ctx, rec, fmt.Errorf("closing temp file: %w", err), logW)
	}

	fi, err := os.Stat(tmpPath)
	if err != nil {
		return rec, r.fail(ctx, rec, fmt.Errorf("stat temp file: %w", err), logW)
	}
	rec.SizeBytes = fi.Size()
	log("Dump complete: %s (%.1f MB)", tmpName, float64(rec.SizeBytes)/1e6)

	// Compute the target directory based on schedule type.
	targetDir := string(opts.ScheduleType)

	log("Transferring to %s (%s)...", opts.Destination, dst.Type())
	if err := dst.Transfer(ctx, tmpPath, targetDir); err != nil {
		return rec, r.fail(ctx, rec, fmt.Errorf("transfer: %w", err), logW)
	}
	log("Transfer complete.")

	// Success — delete temp file.
	deleteTmp = false
	os.Remove(tmpPath)

	if opts.Retention.KeepLast > 0 {
		log("Applying retention (keep_last=%d)...", opts.Retention.KeepLast)
		if err := r.applyRetention(ctx, dst, opts.Target, opts.Retention.KeepLast, logW); err != nil {
			log("Retention warning: %v", err)
		}
	}

	rec.Status = "success"
	rec.DurationMs = time.Since(start).Milliseconds()
	rec.LogOutput = logBuf.String()

	log("Backup completed in %dms.", rec.DurationMs)

	if r.histDB != nil {
		if _, err := r.histDB.Insert(ctx, rec); err != nil {
			r.logger.Error("writing history", "error", err)
		}
	}
	return rec, nil
}

func (r *Runner) fail(ctx context.Context, rec *Record, err error, logW io.Writer) error {
	fmt.Fprintf(logW, "ERROR: %v\n", err)
	rec.Status = "failure"
	rec.ErrorMsg = err.Error()
	rec.DurationMs = time.Since(rec.CreatedAt).Milliseconds()
	if r.histDB != nil {
		if _, dbErr := r.histDB.Insert(ctx, rec); dbErr != nil {
			r.logger.Error("writing failure history", "error", dbErr)
		}
	}
	return err
}

func (r *Runner) applyRetention(ctx context.Context, dst destination.Destination, targetName string, keepLast int, logW io.Writer) error {
	files, err := dst.ListFiles(ctx, targetName)
	if err != nil {
		return fmt.Errorf("listing files on destination: %w", err)
	}
	sort.Strings(files) // oldest first (filename has date embedded)
	excess := len(files) - keepLast
	for i := 0; i < excess; i++ {
		fmt.Fprintf(logW, "  Deleting old backup: %s\n", files[i])
		if err := dst.DeleteFile(ctx, files[i]); err != nil {
			r.logger.Error("deleting old backup", "file", files[i], "error", err)
		}
	}
	return nil
}

// progressWriter counts bytes written and periodically logs progress.
type progressWriter struct {
	w     io.Writer
	logW  io.Writer
	label string
	n     int64
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	n, err := pw.w.Write(p)
	pw.n += int64(n)
	if pw.n%(50*1024*1024) == 0 && pw.n > 0 {
		fmt.Fprintf(pw.logW, "  %s: %.0f MB written\n", pw.label, float64(pw.n)/1e6)
	}
	return n, err
}
