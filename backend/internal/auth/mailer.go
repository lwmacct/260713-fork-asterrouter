package auth

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
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
	fromAddress, err := parseSMTPAddress(cfg.From)
	if err != nil {
		return fmt.Errorf("invalid SMTP sender: %w", err)
	}
	toAddress, err := parseSMTPAddress(to)
	if err != nil {
		return fmt.Errorf("invalid SMTP recipient: %w", err)
	}
	message, err := buildSMTPMessage(fromAddress, toAddress, subject, contentType, body)
	if err != nil {
		return err
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
	if err := client.Mail(fromAddress.Address); err != nil {
		return err
	}
	if err := client.Rcpt(toAddress.Address); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	// Headers are parsed or RFC 2047 encoded, and the body is Base64 encoded.
	// lgtm[go/email-injection]
	// codeql[go/email-injection]
	if _, err := writer.Write(message); err != nil {
		_ = writer.Close()
		return err
	}
	return writer.Close()
}

func parseSMTPAddress(value string) (*mail.Address, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "\r\n") {
		return nil, errors.New("mailbox must be a single line")
	}
	address, err := mail.ParseAddress(value)
	if err != nil || strings.TrimSpace(address.Address) == "" {
		return nil, errors.New("mailbox is invalid")
	}
	return address, nil
}

func buildSMTPMessage(from, to *mail.Address, subject, contentType, body string) ([]byte, error) {
	if from == nil || to == nil {
		return nil, errors.New("sender and recipient are required")
	}
	if strings.ContainsAny(subject, "\r\n") {
		return nil, errors.New("email subject must be a single line")
	}
	if contentType != "text/plain" && contentType != "text/html" {
		return nil, errors.New("unsupported email content type")
	}

	encodedBody := base64.StdEncoding.EncodeToString([]byte(body))
	var message bytes.Buffer
	fmt.Fprintf(&message, "From: %s\r\n", from.String())
	fmt.Fprintf(&message, "To: %s\r\n", to.String())
	fmt.Fprintf(&message, "Subject: %s\r\n", mime.QEncoding.Encode("UTF-8", subject))
	fmt.Fprint(&message, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&message, "Content-Type: %s; charset=UTF-8\r\n", contentType)
	fmt.Fprint(&message, "Content-Transfer-Encoding: base64\r\n\r\n")
	for len(encodedBody) > 76 {
		fmt.Fprintf(&message, "%s\r\n", encodedBody[:76])
		encodedBody = encodedBody[76:]
	}
	fmt.Fprintf(&message, "%s\r\n", encodedBody)
	return message.Bytes(), nil
}
