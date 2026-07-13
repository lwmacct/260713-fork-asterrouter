package auth

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"
)

type SMTPConfig struct {
	Host                     string
	Port                     int
	Username, Password, From string
}
type SMTPMailer struct {
	Config  SMTPConfig
	Timeout time.Duration
}

func (m SMTPMailer) Send(ctx context.Context, to, subject, text string) error {
	return m.send(ctx, to, subject, "text/plain", text)
}

func (m SMTPMailer) SendHTML(ctx context.Context, to, subject, html string) error {
	return m.send(ctx, to, subject, "text/html", html)
}

func (m SMTPMailer) send(ctx context.Context, to, subject, contentType, body string) error {
	cfg := m.Config
	if strings.TrimSpace(cfg.Host) == "" || cfg.Port < 1 || strings.TrimSpace(cfg.From) == "" {
		return errors.New("SMTP is not configured")
	}
	timeout := m.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	address := net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.Port))
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return err
	}
	defer conn.Close()
	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return err
	}
	defer client.Close()
	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	}
	if cfg.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)); err != nil {
			return err
		}
	}
	if err := client.Mail(cfg.From); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	message := "From: " + cfg.From + "\r\nTo: " + to + "\r\nSubject: " + subject + "\r\nMIME-Version: 1.0\r\nContent-Type: " + contentType + "; charset=UTF-8\r\n\r\n" + body
	if _, err := writer.Write([]byte(message)); err != nil {
		_ = writer.Close()
		return err
	}
	return writer.Close()
}
