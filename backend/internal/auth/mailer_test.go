package auth

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
)

func TestSMTPMailerRequiresConfiguration(t *testing.T) {
	if err := (SMTPMailer{}).Send(context.Background(), "user@example.test", "subject", "body"); err == nil {
		t.Fatal("unconfigured SMTP must fail")
	}
}

func TestSMTPMailerRejectsInjectedHeadersBeforeDial(t *testing.T) {
	mailer := SMTPMailer{Config: SMTPConfig{Host: "127.0.0.1", Port: 1, From: "sender@example.test"}}
	for _, test := range []struct {
		name    string
		from    string
		to      string
		subject string
	}{
		{name: "sender", from: "sender@example.test\r\nBcc: victim@example.test", to: "user@example.test", subject: "subject"},
		{name: "recipient", from: "sender@example.test", to: "user@example.test\r\nBcc: victim@example.test", subject: "subject"},
		{name: "subject", from: "sender@example.test", to: "user@example.test", subject: "subject\r\nBcc: victim@example.test"},
	} {
		t.Run(test.name, func(t *testing.T) {
			mailer.Config.From = test.from
			if err := mailer.Send(context.Background(), test.to, test.subject, "body"); err == nil || !strings.Contains(err.Error(), "invalid") && !strings.Contains(err.Error(), "single line") {
				t.Fatalf("Send() error = %v, want header validation error", err)
			}
		})
	}
}

func TestBuildSMTPMessageEncodesUntrustedContent(t *testing.T) {
	from, err := parseSMTPAddress("Aster Router <sender@example.test>")
	if err != nil {
		t.Fatal(err)
	}
	to, err := parseSMTPAddress("user@example.test")
	if err != nil {
		t.Fatal(err)
	}
	body := "hello\r\nBcc: victim@example.test"
	message, err := buildSMTPMessage(from, to, "verification subject", "text/html", body)
	if err != nil {
		t.Fatal(err)
	}
	encodedBody := base64.StdEncoding.EncodeToString([]byte(body))
	if strings.Contains(string(message), body) || !strings.Contains(string(message), encodedBody) {
		t.Fatalf("message body was not safely encoded: %s", message)
	}
	if !strings.Contains(string(message), "Content-Transfer-Encoding: base64") {
		t.Fatalf("message is missing base64 transfer encoding: %s", message)
	}
}
