package api

import (
	"encoding/json"
	"net/http"
)

// APIResponse is the standard envelope for all API responses.
type APIResponse struct {
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

// TargetResponse is the JSON shape for a target.
type TargetResponse struct {
	Name        string `json:"name"`
	Engine      string `json:"engine"`
	Runtime     string `json:"runtime"`
	Namespace   string `json:"namespace,omitempty"`
	DBName      string `json:"db_name,omitempty"`
	Host        string `json:"host,omitempty"`
	Port        string `json:"port,omitempty"`
	SecretRef   string `json:"secret_ref,omitempty"`
}

// ScheduleResponse is the JSON shape for a schedule entry.
type ScheduleResponse struct {
	Target       string `json:"target"`
	Destination  string `json:"destination"`
	Cron         string `json:"cron"`
	Compress     string `json:"compress"`
	KeepLast     int    `json:"keep_last"`
	ScheduleType string `json:"schedule_type"`
	NextRun      string `json:"next_run"`
}

// HistoryResponse is the JSON shape for a history record.
type HistoryResponse struct {
	ID          int64  `json:"id"`
	RunID       string `json:"run_id"`
	CreatedAt   string `json:"created_at"`
	Target      string `json:"target"`
	Destination string `json:"destination"`
	Status      string `json:"status"`
	SizeBytes   int64  `json:"size_bytes"`
	DurationMs  int64  `json:"duration_ms"`
	ErrorMsg    string `json:"error_msg,omitempty"`
	Filename    string `json:"filename,omitempty"`
}

// RunRequest is the JSON body for POST /api/run.
type RunRequest struct {
	Target      string `json:"target"`
	Destination string `json:"destination"`
}

// RunResponse is the JSON body for a successful run trigger.
type RunResponse struct {
	RunID       string `json:"run_id"`
	Target      string `json:"target"`
	Destination string `json:"destination"`
	Status      string `json:"status"`
}

// StopRequest is the JSON body for POST /api/stop.
type StopRequest struct {
	RunID string `json:"run_id"`
}

// StopResponse is the JSON body for a successful stop.
type StopResponse struct {
	RunID  string `json:"run_id"`
	Status string `json:"status"`
}

// HealthResponse is the JSON body for health checks.
type HealthResponse struct {
	Scheduler string `json:"scheduler"`
	DB        string `json:"db"`
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(APIResponse{OK: true, Data: data})
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(APIResponse{OK: false, Error: msg})
}
