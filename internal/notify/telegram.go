package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"backuper/internal/backup"
	"backuper/internal/config"
	"backuper/internal/secrets"
)

type telegramNotifier struct {
	name      string
	token     string
	chatID    string
	threadID  int
	onSuccess bool
	onFailure bool
}

func newTelegram(cfg config.NotificationConfig, store secrets.Store) (*telegramNotifier, error) {
	if cfg.BotTokenRef == "" {
		return nil, fmt.Errorf("telegram: bot_token_ref is required")
	}
	if cfg.ChatID == "" {
		return nil, fmt.Errorf("telegram: chat_id is required")
	}
	token, err := store.Get(cfg.BotTokenRef)
	if err != nil {
		return nil, fmt.Errorf("telegram: resolving bot token %q: %w", cfg.BotTokenRef, err)
	}
	return &telegramNotifier{
		name:      cfg.Name,
		token:     token,
		chatID:    cfg.ChatID,
		threadID:  cfg.ThreadID,
		onSuccess: cfg.OnSuccess,
		onFailure: cfg.OnFailure,
	}, nil
}

func (t *telegramNotifier) Name() string { return t.name }

func (t *telegramNotifier) ShouldSend(rec *backup.Record) bool {
	if rec.Status == "success" {
		return t.onSuccess
	}
	return t.onFailure
}

func (t *telegramNotifier) Send(ctx context.Context, rec *backup.Record) error {
	text := formatMessage(rec)
	payload := map[string]any{
		"chat_id":    t.chatID,
		"text":       text,
		"parse_mode": "HTML",
	}
	if t.threadID != 0 {
		payload["message_thread_id"] = t.threadID
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("telegram: marshal payload: %w", err)
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("telegram: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram: send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var result struct {
			Description string `json:"description"`
		}
		json.NewDecoder(resp.Body).Decode(&result)
		return fmt.Errorf("telegram: API error %d: %s", resp.StatusCode, result.Description)
	}
	return nil
}

func formatMessage(rec *backup.Record) string {
	icon := "\xe2\x9c\x85" // green checkmark
	status := "Success"
	if rec.Status != "success" {
		icon = "\xe2\x9d\x8c" // red cross
		status = "Failure"
	}

	size := formatBytes(rec.SizeBytes)
	duration := formatDuration(rec.DurationMs)
	ts := rec.CreatedAt.Format("2006-01-02 15:04:05")

	msg := fmt.Sprintf(
		"%s <b>Backup %s</b>\n\n"+
			"<b>Target:</b> %s\n"+
			"<b>Destination:</b> %s\n"+
			"<b>Size:</b> %s\n"+
			"<b>Duration:</b> %s\n"+
			"<b>Time:</b> %s",
		icon, status,
		rec.Target,
		rec.Destination,
		size,
		duration,
		ts,
	)

	if rec.ErrorMsg != "" {
		msg += fmt.Sprintf("\n\n<b>Error:</b>\n<code>%s</code>", rec.ErrorMsg)
	}

	return msg
}

func formatBytes(b int64) string {
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

func formatDuration(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	if ms < 60000 {
		return fmt.Sprintf("%.1fs", float64(ms)/1000)
	}
	return fmt.Sprintf("%.1fm", float64(ms)/60000)
}
