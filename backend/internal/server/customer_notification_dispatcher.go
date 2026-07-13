package server

import (
	"context"
	"strings"

	"github.com/astercloud/asterrouter/backend/internal/auth"
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/settings"
)

type customerEmailNotificationDispatcher struct {
	settings *settings.Service
}

func (d customerEmailNotificationDispatcher) DispatchCustomerNotification(ctx context.Context, user controlplane.WorkspaceUser, notification controlplane.CustomerNotification) error {
	actionURL := notification.Link
	if strings.HasPrefix(actionURL, "/") {
		if admin, err := d.settings.Admin(ctx); err == nil && strings.TrimSpace(admin.PublicBaseURL) != "" {
			actionURL = strings.TrimRight(admin.PublicBaseURL, "/") + actionURL
		} else {
			actionURL = ""
		}
	}
	return sendConfiguredEmailData(ctx, d.settings, "customer_notification", user.Email, auth.EmailTemplateData{
		UserName: user.DisplayName, Title: notification.Title, Message: notification.Content, ActionURL: actionURL,
	})
}
