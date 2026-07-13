package auth

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"
	texttemplate "text/template"
)

type EmailTemplateData struct {
	SiteName, UserName, ActionURL, Title, Amount, Limit, Period, Message string
}

type EmailTemplateDefinition struct{ Event, Locale, Subject, HTML string }

func DefaultEmailTemplates() []EmailTemplateDefinition {
	return []EmailTemplateDefinition{
		{Event: "email_verification", Locale: "zh-CN", Subject: "验证您的 {{.SiteName}} 账户", HTML: `<h2>验证邮箱</h2><p>{{.UserName}}，请点击以下链接完成验证：</p><p><a href="{{.ActionURL}}">验证邮箱</a></p>`},
		{Event: "email_verification", Locale: "en-US", Subject: "Verify your {{.SiteName}} account", HTML: `<h2>Verify email</h2><p>Hello {{.UserName}}, verify your account:</p><p><a href="{{.ActionURL}}">Verify email</a></p>`},
		{Event: "password_reset", Locale: "zh-CN", Subject: "重置您的 {{.SiteName}} 密码", HTML: `<h2>重置密码</h2><p><a href="{{.ActionURL}}">设置新密码</a></p>`},
		{Event: "password_reset", Locale: "en-US", Subject: "Reset your {{.SiteName}} password", HTML: `<h2>Reset password</h2><p><a href="{{.ActionURL}}">Set a new password</a></p>`},
		{Event: "balance_low", Locale: "zh-CN", Subject: "{{.SiteName}} 余额提醒", HTML: `<h2>余额提醒</h2><p>当前余额：{{.Amount}}</p>`},
		{Event: "balance_low", Locale: "en-US", Subject: "{{.SiteName}} balance alert", HTML: `<h2>Balance alert</h2><p>Current balance: {{.Amount}}</p>`},
		{Event: "quota_limit", Locale: "zh-CN", Subject: "{{.SiteName}} 额度通知", HTML: `<h2>额度通知</h2><p>{{.Period}} 使用额度：{{.Limit}}</p>`},
		{Event: "quota_limit", Locale: "en-US", Subject: "{{.SiteName}} quota notification", HTML: `<h2>Quota notification</h2><p>{{.Period}} limit: {{.Limit}}</p>`},
		{Event: "subscription_expiry", Locale: "zh-CN", Subject: "{{.SiteName}} 访问授权即将到期", HTML: `<h2>授权到期提醒</h2><p>{{.Message}}</p>`},
		{Event: "subscription_expiry", Locale: "en-US", Subject: "{{.SiteName}} access expires soon", HTML: `<h2>Expiration notice</h2><p>{{.Message}}</p>`},
		{Event: "customer_notification", Locale: "zh-CN", Subject: "{{.SiteName}} 通知：{{.Title}}", HTML: `<h2>{{.Title}}</h2><p>{{.Message}}</p>{{if .ActionURL}}<p><a href="{{.ActionURL}}">查看详情</a></p>{{end}}`},
		{Event: "customer_notification", Locale: "en-US", Subject: "{{.SiteName}} notification: {{.Title}}", HTML: `<h2>{{.Title}}</h2><p>{{.Message}}</p>{{if .ActionURL}}<p><a href="{{.ActionURL}}">View details</a></p>{{end}}`},
	}
}

func RenderEmailTemplate(subject, htmlBody string, data EmailTemplateData) (string, string, error) {
	renderSubject := func(source string) (string, error) {
		parsed, err := texttemplate.New("subject").Option("missingkey=error").Parse(source)
		if err != nil {
			return "", fmt.Errorf("parse subject template: %w", err)
		}
		var output bytes.Buffer
		if err := parsed.Execute(&output, data); err != nil {
			return "", fmt.Errorf("render subject template: %w", err)
		}
		if strings.ContainsAny(output.String(), "\r\n") {
			return "", fmt.Errorf("email subject must be a single line")
		}
		return output.String(), nil
	}
	renderedSubject, err := renderSubject(subject)
	if err != nil {
		return "", "", err
	}
	parsedHTML, err := template.New("html").Option("missingkey=error").Parse(htmlBody)
	if err != nil {
		return "", "", fmt.Errorf("parse html template: %w", err)
	}
	var htmlOutput bytes.Buffer
	if err := parsedHTML.Execute(&htmlOutput, data); err != nil {
		return "", "", fmt.Errorf("render html template: %w", err)
	}
	renderedHTML := htmlOutput.String()
	return renderedSubject, renderedHTML, nil
}
