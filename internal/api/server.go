package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"backuper/internal/agent"
	"backuper/internal/config"
)

type Server struct {
	agent  *agent.Agent
	cfg    *config.APIConfig
	logger *slog.Logger
	srv    *http.Server
}

func NewServer(ag *agent.Agent, cfg *config.APIConfig, logger *slog.Logger) *Server {
	return &Server{
		agent:  ag,
		cfg:    cfg,
		logger: logger,
	}
}

func (s *Server) ListenAndServe() error {
	h := &handlers{agent: s.agent}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.handleHealthz)
	mux.HandleFunc("GET /livez", h.handleLivez)
	mux.HandleFunc("GET /api/targets", h.handleTargets)
	mux.HandleFunc("GET /api/schedules", h.handleSchedules)
	mux.HandleFunc("GET /api/history", h.handleHistory)
	mux.HandleFunc("GET /api/runs/{id}/log", h.handleRunLog)
	mux.HandleFunc("GET /api/runs/{id}/log/stream", h.handleRunLogStream)
	mux.HandleFunc("POST /api/run", h.handleRun)
	mux.HandleFunc("POST /api/stop", h.handleStop)

	var handler http.Handler = mux
	handler = recoveryMiddleware(s.logger, handler)
	handler = loggingMiddleware(s.logger, handler)

	addr := s.cfg.ListenAddr
	if addr == "" {
		addr = "0.0.0.0:8080"
	}

	s.srv = &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	s.logger.Info("api server starting", "addr", addr)
	if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("api server: %w", err)
	}
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.srv == nil {
		return nil
	}
	s.logger.Info("api server shutting down")
	return s.srv.Shutdown(ctx)
}
