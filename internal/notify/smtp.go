package notify

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"backuper/internal/backup"
	"backuper/internal/config"
	"backuper/internal/secrets"
)

type smtpNotifier struct {
	name        string
	host        string
	port        int
	from        string
	to          []string
	username    string
	password    string
	useTLS      bool
	insecureTLS bool
	onSuccess   bool
	onFailure   bool
}

func newSMTP(cfg config.NotificationConfig, store secrets.Store) (*smtpNotifier, error) {
	if cfg.SMTPHost == "" {
		return nil, fmt.Errorf("smtp: smtp_host is required")
	}
	if len(cfg.To) == 0 {
		return nil, fmt.Errorf("smtp: to is required")
	}
	if cfg.PasswordRef == "" {
		return nil, fmt.Errorf("smtp: password_ref is required")
	}
	password, err := store.Get(cfg.PasswordRef)
	if err != nil {
		return nil, fmt.Errorf("smtp: resolving password %q: %w", cfg.PasswordRef, err)
	}

	port := cfg.SMTPPort
	if port == 0 {
		port = 587
	}

	username := cfg.Username
	if username == "" {
		username = cfg.From
	}

	// Default useTLS to true for standard SMTP ports.
	useTLS := cfg.UseTLS
	if !cfg.UseTLS && port == 587 {
		useTLS = true
	}

	return &smtpNotifier{
		name:        cfg.Name,
		host:        cfg.SMTPHost,
		port:        port,
		from:        cfg.From,
		to:          cfg.To,
		username:    username,
		password:    password,
		useTLS:      useTLS,
		insecureTLS: cfg.InsecureTLS,
		onSuccess:   cfg.OnSuccess,
		onFailure:   cfg.OnFailure,
	}, nil
}

func (s *smtpNotifier) Name() string { return s.name }

func (s *smtpNotifier) ShouldSend(rec *backup.Record) bool {
	if rec.Status == "success" {
		return s.onSuccess
	}
	return s.onFailure
}

func (s *smtpNotifier) Send(ctx context.Context, rec *backup.Record) error {
	subject, body := s.buildMessage(rec)
	msg, err := s.buildMIME(s.from, s.to, subject, body)
	if err != nil {
		return fmt.Errorf("smtp: build message: %w", err)
	}

	addr := fmt.Sprintf("%s:%d", s.host, s.port)

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("smtp: connect to %s: %w", addr, err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		return fmt.Errorf("smtp: create client: %w", err)
	}

	// STARTTLS upgrade.
	if s.useTLS {
		tlsCfg := &tls.Config{
			ServerName:         s.host,
			InsecureSkipVerify: s.insecureTLS,
		}
		if err := client.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("smtp: STARTTLS: %w", err)
		}
	}

	// Authenticate if credentials provided.
	if s.username != "" && s.password != "" {
		auth := smtp.PlainAuth("", s.username, s.password, s.host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp: auth: %w", err)
		}
	}

	if err := client.Mail(s.from); err != nil {
		return fmt.Errorf("smtp: MAIL FROM: %w", err)
	}
	for _, rcpt := range s.to {
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("smtp: RCPT TO %q: %w", rcpt, err)
		}
	}

	wc, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp: DATA: %w", err)
	}
	if _, err := wc.Write([]byte(msg)); err != nil {
		return fmt.Errorf("smtp: write body: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("smtp: close body: %w", err)
	}

	return client.Quit()
}

func (s *smtpNotifier) buildMessage(rec *backup.Record) (string, string) {
	status := "Success"
	icon := "\u2705" // white checkmark
	if rec.Status != "success" {
		status = "Failure"
		icon = "\u274C" // red cross
	}

	size := formatBytes(rec.SizeBytes)
	duration := formatDuration(rec.DurationMs)
	ts := rec.CreatedAt.Format("2006-01-02 15:04:05")

	subject := fmt.Sprintf("[%s] Backup %s: %s \u2192 %s", status, rec.Target, rec.Destination, ts)

	body := fmt.Sprintf(
		"%s Backup %s\n\n"+
			"Target:      %s\n"+
			"Destination: %s\n"+
			"Size:        %s\n"+
			"Duration:    %s\n"+
			"Time:        %s",
		icon, status,
		rec.Target,
		rec.Destination,
		size,
		duration,
		ts,
	)

	if rec.ErrorMsg != "" {
		body += fmt.Sprintf("\n\nError:\n%s", rec.ErrorMsg)
	}

	return subject, body
}

func (s *smtpNotifier) buildMIME(from string, to []string, subject, body string) (string, error) {
	var b strings.Builder

	fromAddr, err := mail.ParseAddress(from)
	if err != nil {
		return "", fmt.Errorf("invalid from address %q: %w", from, err)
	}

	b.WriteString("From: ")
	b.WriteString(fromAddr.String())
	b.WriteString("\r\n")

	b.WriteString("To: ")
	for i, addr := range to {
		parsed, err := mail.ParseAddress(addr)
		if err != nil {
			return "", fmt.Errorf("invalid to address %q: %w", addr, err)
		}
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(parsed.String())
	}
	b.WriteString("\r\n")

	b.WriteString("Subject: =?utf-8?B?")
	b.WriteString(base64.StdEncoding.EncodeToString([]byte(subject)))
	b.WriteString("?=\r\n")

	b.WriteString("Date: ")
	b.WriteString(time.Now().Format(time.RFC1123Z))
	b.WriteString("\r\n")

	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("Content-Transfer-Encoding: base64\r\n")
	b.WriteString("\r\n")
	b.WriteString(base64.StdEncoding.EncodeToString([]byte(body)))
	b.WriteString("\r\n")

	return b.String(), nil
}
