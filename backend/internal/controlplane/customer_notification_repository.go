package controlplane

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"time"
)

func (r *MemoryRepository) GetCustomerNotificationPreferences(_ context.Context, userID string) ([]CustomerNotificationPreference, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	stored := r.customerNotificationPreferences[userID]
	out := make([]CustomerNotificationPreference, 0, len(stored))
	for _, preference := range stored {
		preference.Channels = append([]string(nil), preference.Channels...)
		out = append(out, preference)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EventType < out[j].EventType })
	return out, nil
}

func (r *MemoryRepository) SaveCustomerNotificationPreferences(_ context.Context, userID string, preferences []CustomerNotificationPreference, updatedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored := make(map[string]CustomerNotificationPreference, len(preferences))
	for _, preference := range preferences {
		preference.Channels = append([]string(nil), preference.Channels...)
		preference.UpdatedAt = timePointer(updatedAt)
		stored[preference.EventType] = preference
	}
	r.customerNotificationPreferences[userID] = stored
	return nil
}

func (r *MemoryRepository) CreateCustomerNotification(_ context.Context, notification CustomerNotification) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if notification.DedupeKey != "" {
		for _, current := range r.customerNotifications {
			if current.UserID == notification.UserID && current.DedupeKey == notification.DedupeKey {
				return false, nil
			}
		}
	}
	r.customerNotifications[notification.ID] = notification
	return true, nil
}

func (r *MemoryRepository) ListCustomerNotifications(_ context.Context, query CustomerNotificationQuery) ([]CustomerNotification, int, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]CustomerNotification, 0)
	unread := 0
	for _, notification := range r.customerNotifications {
		if notification.UserID != query.UserID || !notification.VisibleInApp {
			continue
		}
		items = append(items, notification)
		if !notification.IsRead {
			unread++
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	total := len(items)
	limit, offset := normalizeListWindow(query.Limit, query.Offset, 20, 100)
	if offset >= total {
		return []CustomerNotification{}, total, unread, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return append([]CustomerNotification(nil), items[offset:end]...), total, unread, nil
}

func (r *MemoryRepository) MarkCustomerNotificationRead(_ context.Context, userID, id string, readAt time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	notification, ok := r.customerNotifications[id]
	if !ok || notification.UserID != userID || !notification.VisibleInApp {
		return false, nil
	}
	if !notification.IsRead {
		notification.IsRead = true
		notification.ReadAt = timePointer(readAt)
		r.customerNotifications[id] = notification
	}
	return true, nil
}

func (r *MemoryRepository) MarkAllCustomerNotificationsRead(_ context.Context, userID string, readAt time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	updated := 0
	for id, notification := range r.customerNotifications {
		if notification.UserID != userID || !notification.VisibleInApp || notification.IsRead {
			continue
		}
		notification.IsRead = true
		notification.ReadAt = timePointer(readAt)
		r.customerNotifications[id] = notification
		updated++
	}
	return updated, nil
}

func (r *MemoryRepository) SaveCustomerNotificationDelivery(_ context.Context, delivery CustomerNotificationDelivery) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.customerNotificationDeliveries[delivery.ID] = delivery
	return nil
}

func (r *PostgresRepository) GetCustomerNotificationPreferences(ctx context.Context, userID string) ([]CustomerNotificationPreference, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT event_type, enabled, channels, threshold, updated_at
FROM customer_notification_preferences
WHERE user_id = $1
ORDER BY event_type ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]CustomerNotificationPreference, 0)
	for rows.Next() {
		var preference CustomerNotificationPreference
		var channels string
		var threshold sql.NullFloat64
		var updatedAt time.Time
		if err := rows.Scan(&preference.EventType, &preference.Enabled, &channels, &threshold, &updatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(channels), &preference.Channels); err != nil {
			return nil, err
		}
		if threshold.Valid {
			preference.Threshold = floatPointer(threshold.Float64)
		}
		preference.UpdatedAt = timePointer(updatedAt)
		out = append(out, preference)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SaveCustomerNotificationPreferences(ctx context.Context, userID string, preferences []CustomerNotificationPreference, updatedAt time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM customer_notification_preferences WHERE user_id = $1`, userID); err != nil {
		return err
	}
	for _, preference := range preferences {
		channels, err := json.Marshal(preference.Channels)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO customer_notification_preferences(user_id,event_type,enabled,channels,threshold,updated_at)
VALUES($1,$2,$3,$4,$5,$6)`, userID, preference.EventType, preference.Enabled, string(channels), preference.Threshold, updatedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *PostgresRepository) CreateCustomerNotification(ctx context.Context, notification CustomerNotification) (bool, error) {
	result, err := r.db.ExecContext(ctx, `
INSERT INTO customer_notifications(id,user_id,event_type,title,content,link,dedupe_key,visible_in_app,is_read,read_at,created_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
ON CONFLICT (user_id,dedupe_key) WHERE dedupe_key <> '' DO NOTHING`,
		notification.ID, notification.UserID, notification.EventType, notification.Title, notification.Content,
		notification.Link, notification.DedupeKey, notification.VisibleInApp, notification.IsRead, notification.ReadAt, notification.CreatedAt)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows > 0, err
}

func (r *PostgresRepository) ListCustomerNotifications(ctx context.Context, query CustomerNotificationQuery) ([]CustomerNotification, int, int, error) {
	limit, offset := normalizeListWindow(query.Limit, query.Offset, 20, 100)
	var total, unread int
	if err := r.db.QueryRowContext(ctx, `
SELECT COUNT(*), COALESCE(SUM(CASE WHEN is_read THEN 0 ELSE 1 END), 0)
FROM customer_notifications
WHERE user_id = $1 AND visible_in_app = TRUE`, query.UserID).Scan(&total, &unread); err != nil {
		return nil, 0, 0, err
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT id,event_type,title,content,link,is_read,read_at,created_at
FROM customer_notifications
WHERE user_id = $1 AND visible_in_app = TRUE
ORDER BY created_at DESC
LIMIT $2 OFFSET $3`, query.UserID, limit, offset)
	if err != nil {
		return nil, 0, 0, err
	}
	defer rows.Close()
	items := make([]CustomerNotification, 0)
	for rows.Next() {
		var notification CustomerNotification
		if err := rows.Scan(&notification.ID, &notification.EventType, &notification.Title, &notification.Content, &notification.Link, &notification.IsRead, &notification.ReadAt, &notification.CreatedAt); err != nil {
			return nil, 0, 0, err
		}
		items = append(items, notification)
	}
	return items, total, unread, rows.Err()
}

func (r *PostgresRepository) MarkCustomerNotificationRead(ctx context.Context, userID, id string, readAt time.Time) (bool, error) {
	result, err := r.db.ExecContext(ctx, `
UPDATE customer_notifications
SET is_read = TRUE, read_at = COALESCE(read_at, $1)
WHERE id = $2 AND user_id = $3 AND visible_in_app = TRUE`, readAt, id, userID)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows > 0, err
}

func (r *PostgresRepository) MarkAllCustomerNotificationsRead(ctx context.Context, userID string, readAt time.Time) (int, error) {
	result, err := r.db.ExecContext(ctx, `
UPDATE customer_notifications
SET is_read = TRUE, read_at = $1
WHERE user_id = $2 AND visible_in_app = TRUE AND is_read = FALSE`, readAt, userID)
	if err != nil {
		return 0, err
	}
	rows, err := result.RowsAffected()
	return int(rows), err
}

func (r *PostgresRepository) SaveCustomerNotificationDelivery(ctx context.Context, delivery CustomerNotificationDelivery) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO customer_notification_deliveries(id,notification_id,user_id,event_type,channel,status,error_message,created_at,updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT(id) DO UPDATE SET status = EXCLUDED.status, error_message = EXCLUDED.error_message, updated_at = EXCLUDED.updated_at`,
		delivery.ID, delivery.NotificationID, delivery.UserID, delivery.EventType, delivery.Channel,
		delivery.Status, delivery.Error, delivery.CreatedAt, delivery.UpdatedAt)
	return err
}

func timePointer(value time.Time) *time.Time {
	return &value
}

func floatPointer(value float64) *float64 {
	return &value
}
