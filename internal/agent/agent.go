package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"backuper/internal/backup"
	"backuper/internal/config"
	"backuper/internal/scheduler"

	"github.com/google/uuid"
)

type Agent struct {
	cfg    *config.Config
	sched  *scheduler.Scheduler
	histDB *backup.HistoryDB
	logger *slog.Logger

	mu   sync.Mutex
	runs map[string]*ActiveRun // UUID → active run
}

func New(cfg *config.Config, sched *scheduler.Scheduler, histDB *backup.HistoryDB, logger *slog.Logger) *Agent {
	return &Agent{
		cfg:    cfg,
		sched:  sched,
		histDB: histDB,
		logger: logger,
		runs:   make(map[string]*ActiveRun),
	}
}

// RunBackup starts an asynchronous backup run and returns the run UUID immediately.
func (a *Agent) RunBackup(targetName, destName string) (string, error) {
	if _, err := a.cfg.FindTarget(targetName); err != nil {
		return "", fmt.Errorf("target: %w", err)
	}
	if _, err := a.cfg.FindDestination(destName); err != nil {
		return "", fmt.Errorf("destination: %w", err)
	}

	runID := uuid.NewString()
	ctx, cancel := context.WithCancel(context.Background())
	logBuf := newThreadSafeBuffer()

	ar := &ActiveRun{
		ID:          runID,
		Target:      targetName,
		Destination: destName,
		StartedAt:   time.Now(),
		Cancel:      cancel,
		LogBuf:      logBuf,
		Done:        make(chan struct{}),
	}

	a.mu.Lock()
	a.runs[runID] = ar
	a.mu.Unlock()

	go func() {
		defer close(ar.Done)
		defer cancel()

		a.logger.Info("api run started", "run_id", runID, "target", targetName, "dest", destName)

		rec, err := a.sched.RunNow(ctx, targetName, destName, logBuf)

		a.mu.Lock()
		ar.Record = rec
		ar.Err = err
		
		go func() {
			time.Sleep(30 * time.Second)
			a.mu.Lock()
			delete(a.runs, runID)
			a.mu.Unlock()
		}()
		a.mu.Unlock()

		if err != nil {
			a.logger.Error("api run failed", "run_id", runID, "error", err)
		} else {
			a.logger.Info("api run completed", "run_id", runID, "status", rec.Status)
		}
	}()

	return runID, nil
}

// StopRun cancels a running backup by its run ID.
func (a *Agent) StopRun(runID string) error {
	a.mu.Lock()
	ar, ok := a.runs[runID]
	a.mu.Unlock()
	if !ok {
		return fmt.Errorf("run %q not found", runID)
	}
	ar.Cancel()
	return nil
}

// ActiveRuns returns summaries of all in-progress runs.
func (a *Agent) ActiveRuns() []ActiveRunInfo {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]ActiveRunInfo, 0, len(a.runs))
	for _, ar := range a.runs {
		out = append(out, ar.Info())
	}
	return out
}

// StreamLog writes real-time log lines for an active run via the onLine callback.
// Blocks until the run completes or the context is cancelled.
func (a *Agent) StreamLog(ctx context.Context, runID string, onLine func(line string)) error {
	a.mu.Lock()
	ar, ok := a.runs[runID]
	a.mu.Unlock()
	if !ok {
		return fmt.Errorf("run %q not found", runID)
	}

	var offset int64
	// Send existing buffered content.
	for {
		content := ar.LogBuf.ReadFrom(offset)
		if content != "" {
			for _, line := range splitLines(content) {
				onLine(line)
			}
			offset = ar.LogBuf.Len()
		}
		// Check if done.
		select {
		case <-ar.Done:
			// Send any remaining content.
			content = ar.LogBuf.ReadFrom(offset)
			if content != "" {
				for _, line := range splitLines(content) {
					onLine(line)
				}
			}
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-ar.LogBuf.Notify():
			// New data available, loop to read it.
		}
	}
}

// GetRunLog returns the full log for a run. Checks active runs first, then history DB.
func (a *Agent) GetRunLog(ctx context.Context, runID string) (string, error) {
	a.mu.Lock()
	ar, ok := a.runs[runID]
	a.mu.Unlock()
	if ok {
		return ar.LogBuf.String(), nil
	}

	if a.histDB != nil {
		rec, err := a.histDB.GetByRunID(ctx, runID)
		if err != nil {
			return "", fmt.Errorf("querying history: %w", err)
		}
		if rec != nil {
			return rec.LogOutput, nil
		}
	}
	return "", fmt.Errorf("run %q not found", runID)
}

// Targets returns the configured target list.
func (a *Agent) Targets() []config.TargetConfig {
	return a.cfg.Targets
}

// Schedules returns schedule entries with next-run times.
func (a *Agent) Schedules() []scheduler.JobEntry {
	return a.sched.Entries()
}

// History queries the backup history.
func (a *Agent) History(ctx context.Context, targetFilter string, limit int) ([]*backup.Record, error) {
	if a.histDB == nil {
		return nil, nil
	}
	return a.histDB.Query(ctx, targetFilter, limit)
}

// Close cancels all active runs.
func (a *Agent) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, ar := range a.runs {
		ar.Cancel()
	}
}

// splitLines splits content into lines, preserving line endings.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.SplitAfter(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
