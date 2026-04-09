package notify

import (
	"fmt"

	"backuper/internal/backup"
	"backuper/internal/config"
	"backuper/internal/secrets"
)

func New(cfg config.NotificationConfig, store secrets.Store) (backup.Notifier, error) {
	switch cfg.Type {
	case "telegram":
		return newTelegram(cfg, store)
	case "smtp":
		return newSMTP(cfg, store)
	default:
		return nil, fmt.Errorf("unknown notification type %q", cfg.Type)
	}
}
