package controlplane

import "time"

const (
	CustomerNotificationBalanceLow      = "balance_low"
	CustomerNotificationErrorRate       = "abuse_5xx"
	CustomerNotificationPayment         = "payment"
	CustomerNotificationMonthlyBill     = "monthly_bill"
	CustomerNotificationAnnouncement    = "announcement"
	CustomerNotificationModelUpdate     = "model_update"
	CustomerNotificationAccountSecurity = "account_security"
	CustomerNotificationMarketing       = "marketing"
	CustomerNotificationProductUpdate   = "product_update"

	CustomerNotificationChannelInApp = "inapp"
	CustomerNotificationChannelEmail = "email"

	CustomerNotificationDeliveryPending = "pending"
	CustomerNotificationDeliverySent    = "sent"
	CustomerNotificationDeliveryFailed  = "failed"
)

type CustomerNotificationPreference struct {
	EventType string     `json:"event_type"`
	Enabled   bool       `json:"enabled"`
	Channels  []string   `json:"channels"`
	Threshold *float64   `json:"threshold,omitempty"`
	Marketing bool       `json:"marketing"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

type CustomerNotificationSettings struct {
	Preferences []CustomerNotificationPreference `json:"preferences"`
}

type CustomerNotificationSettingsRequest struct {
	Preferences []CustomerNotificationPreference `json:"preferences"`
}

type CustomerNotification struct {
	ID           string     `json:"id"`
	UserID       string     `json:"-"`
	EventType    string     `json:"type"`
	Title        string     `json:"title"`
	Content      string     `json:"content"`
	Link         string     `json:"link,omitempty"`
	DedupeKey    string     `json:"-"`
	VisibleInApp bool       `json:"-"`
	IsRead       bool       `json:"is_read"`
	ReadAt       *time.Time `json:"read_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

type CustomerNotificationQuery struct {
	UserID string
	Limit  int
	Offset int
}

type CustomerNotificationList struct {
	Items  []CustomerNotification `json:"items"`
	Total  int                    `json:"total"`
	Unread int                    `json:"unread"`
	Limit  int                    `json:"limit"`
	Offset int                    `json:"offset"`
}

type CustomerNotificationDelivery struct {
	ID             string
	NotificationID string
	UserID         string
	EventType      string
	Channel        string
	Status         string
	Error          string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type customerNotificationInput struct {
	UserID    string
	EventType string
	Title     string
	Content   string
	Link      string
	DedupeKey string
}
