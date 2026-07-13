package auth

import (
	"strings"
	"testing"
)

func TestRenderEmailTemplateEscapesHTMLAndRejectsUnknownFields(t *testing.T) {
	subject, htmlBody, err := RenderEmailTemplate("Hello {{.UserName}}", `<p>{{.UserName}}</p>`, EmailTemplateData{UserName: `<script>alert(1)</script>`})
	if err != nil {
		t.Fatalf("RenderEmailTemplate() error = %v", err)
	}
	if subject != "Hello <script>alert(1)</script>" {
		t.Fatalf("subject = %q", subject)
	}
	if strings.Contains(htmlBody, "<script>") || !strings.Contains(htmlBody, "&lt;script") {
		t.Fatalf("HTML was not escaped: %q", htmlBody)
	}
	if _, _, err := RenderEmailTemplate("{{.Unknown}}", "body", EmailTemplateData{}); err == nil {
		t.Fatal("unknown field error = nil")
	}
}
