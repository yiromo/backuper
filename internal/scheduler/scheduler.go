// Package scheduler wraps robfig/cron to schedule backup runs.
package scheduler

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"backuper/internal/backup"
	"backuper/internal/config"

	"github.com/robfig/cron/v3"
)

type Scheduler struct {
	c      *cron.Cron
	runner *backup.Runner
	cfg    *config.Config
	logger *slog.Logger
	mu     sync.Mutex
	jobs   map[cron.EntryID]config.ScheduleConfig
}

func New(cfg *config.Config, runner *backup.Runner, logger *slog.Logger) *Scheduler {
	return &Scheduler{
		c:      cron.New(),
		runner: runner,
		cfg:    cfg,
		logger: logger,
		jobs:   make(map[cron.EntryID]config.ScheduleConfig),
	}
}

func (s *Scheduler) RegisterAll() error {
	for _, sched := range s.cfg.Schedules {
		if err := s.register(sched); err != nil {
			return fmt.Errorf("registering schedule %s→%s: %w", sched.Target, sched.Destination, err)
		}
	}
	return nil
}

func (s *Scheduler) register(sched config.ScheduleConfig) error {
	opts := backup.RunOptions{
		Target:       sched.Target,
		Destination:  sched.Destination,
		Compress:     sched.Compress,
		TmpDir:       sched.TmpDir,
		Retention:    sched.Retention,
		ScheduleType: sched.ScheduleType(),
	}
	id, err := s.c.AddFunc(sched.Cron, func() {
		s.logger.Info("cron job starting", "target", sched.Target, "dest", sched.Destination)
		var logBuf bytes.Buffer
		ctx := context.Background()
		if _, err := s.runner.Run(ctx, opts, &logBuf); err != nil {
			s.logger.Error("cron job failed",
				"target", sched.Target,
				"dest", sched.Destination,
				"error", err,
			)
		} else {
			s.logger.Info("cron job succeeded", "target", sched.Target, "dest", sched.Destination)
		}
	})
	if err != nil {
		return fmt.Errorf("adding cron %q: %w", sched.Cron, err)
	}
	s.mu.Lock()
	s.jobs[id] = sched
	s.mu.Unlock()
	return nil
}

func (s *Scheduler) Start() { s.c.Start() }

func (s *Scheduler) Stop() context.Context { return s.c.Stop() }

func (s *Scheduler) NextRun(targetName, destName string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, sched := range s.jobs {
		if sched.Target == targetName && sched.Destination == destName {
			entry := s.c.Entry(id)
			if entry.ID != 0 {
				return entry.Next.Format("2006-01-02 15:04")
			}
		}
	}
	return "-"
}

func (s *Scheduler) Entries() []JobEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	var entries []JobEntry
	for id, sched := range s.jobs {
		entry := s.c.Entry(id)
		var next string
		if entry.ID != 0 {
			next = entry.Next.Format("2006-01-02 15:04")
		}
		entries = append(entries, JobEntry{
			Schedule: sched,
			Next:     next,
		})
	}
	return entries
}

type JobEntry struct {
	Schedule config.ScheduleConfig
	Next     string
}

func (s *Scheduler) RunNow(ctx context.Context, targetName, destName string, logW io.Writer) (*backup.Record, error) {
	tgtCfg, err := s.cfg.FindTarget(targetName)
	if err != nil {
		return nil, err
	}
	dstCfg, err := s.cfg.FindDestination(destName)
	if err != nil {
		return nil, err
	}

	// Try to match a schedule for options (compress, retention, etc.).
	opts := backup.RunOptions{
		Target:       targetName,
		Destination:  destName,
		Compress:     "gzip",
		ScheduleType: config.ScheduleTypeCustom,
	}
	for _, sched := range s.cfg.Schedules {
		if sched.Target == tgtCfg.Name && sched.Destination == dstCfg.Name {
			opts.Compress = sched.Compress
			opts.TmpDir = sched.TmpDir
			opts.Retention = sched.Retention
			opts.ScheduleType = sched.ScheduleType()
			break
		}
	}
	return s.runner.Run(ctx, opts, logW)
}
