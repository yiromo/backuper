package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"backuper/internal/agent"
)

type handlers struct {
	agent *agent.Agent
}

func (h *handlers) handleHealthz(w http.ResponseWriter, r *http.Request) {
	// TODO: add actual scheduler/DB health check.
	writeJSON(w, http.StatusOK, HealthResponse{
		Scheduler: "running",
		DB:        "ok",
	})
}

func (h *handlers) handleLivez(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, nil)
}

func (h *handlers) handleTargets(w http.ResponseWriter, r *http.Request) {
	targets := h.agent.Targets()
	out := make([]TargetResponse, len(targets))
	for i, t := range targets {
		out[i] = TargetResponse{
			Name:      t.Name,
			Engine:    t.Engine,
			Runtime:   t.Runtime,
			Namespace: t.Namespace,
			DBName:    t.DBName,
			Host:      t.Host,
			Port:      t.Port,
			SecretRef: t.SecretRef,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handlers) handleSchedules(w http.ResponseWriter, r *http.Request) {
	entries := h.agent.Schedules()
	out := make([]ScheduleResponse, len(entries))
	for i, e := range entries {
		out[i] = ScheduleResponse{
			Target:       e.Schedule.Target,
			Destination:  e.Schedule.Destination,
			Cron:         e.Schedule.Cron,
			Compress:     e.Schedule.Compress,
			KeepLast:     e.Schedule.Retention.KeepLast,
			ScheduleType: string(e.Schedule.ScheduleType()),
			NextRun:      e.Next,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handlers) handleHistory(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("target")
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			limit = n
		}
	}

	records, err := h.agent.History(r.Context(), target, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	out := make([]HistoryResponse, len(records))
	for i, rec := range records {
		out[i] = HistoryResponse{
			ID:          rec.ID,
			RunID:       rec.RunID,
			CreatedAt:   rec.CreatedAt.Format(time.RFC3339),
			Target:      rec.Target,
			Destination: rec.Destination,
			Status:      rec.Status,
			SizeBytes:   rec.SizeBytes,
			DurationMs:  rec.DurationMs,
			ErrorMsg:    rec.ErrorMsg,
			Filename:    rec.Filename,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handlers) handleRun(w http.ResponseWriter, r *http.Request) {
	var req RunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Target == "" || req.Destination == "" {
		writeError(w, http.StatusBadRequest, "target and destination are required")
		return
	}

	runID, err := h.agent.RunBackup(req.Target, req.Destination)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusAccepted, RunResponse{
		RunID:       runID,
		Target:      req.Target,
		Destination: req.Destination,
		Status:      "started",
	})
}

func (h *handlers) handleStop(w http.ResponseWriter, r *http.Request) {
	var req StopRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.RunID == "" {
		writeError(w, http.StatusBadRequest, "run_id is required")
		return
	}

	if err := h.agent.StopRun(req.RunID); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, StopResponse{
		RunID:  req.RunID,
		Status: "cancelling",
	})
}

func (h *handlers) handleRunLog(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	if runID == "" {
		writeError(w, http.StatusBadRequest, "run id is required")
		return
	}

	log, err := h.agent.GetRunLog(r.Context(), runID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"log": log})
}

func (h *handlers) handleRunLogStream(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	if runID == "" {
		writeError(w, http.StatusBadRequest, "run id is required")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	notify := r.Context().Done()
	go func() {
		<-notify
		cancel()
	}()

	err := h.agent.StreamLog(ctx, runID, func(line string) {
		fmt.Fprintf(w, "data: %s\n", line)
		flusher.Flush()
	})
	if err != nil {
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
		flusher.Flush()
		return
	}

	fmt.Fprintf(w, "event: done\ndata: {}\n\n")
	flusher.Flush()
}
