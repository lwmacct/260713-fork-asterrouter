CREATE TABLE IF NOT EXISTS customer_notification_preferences (
  user_id TEXT NOT NULL REFERENCES workspace_users(id) ON DELETE CASCADE,
  event_type TEXT NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  channels TEXT NOT NULL DEFAULT '[]',
  threshold DOUBLE PRECISION,
  updated_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY(user_id, event_type),
  CHECK (threshold IS NULL OR threshold >= 0)
);

CREATE TABLE IF NOT EXISTS customer_notifications (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES workspace_users(id) ON DELETE CASCADE,
  event_type TEXT NOT NULL,
  title TEXT NOT NULL,
  content TEXT NOT NULL DEFAULT '',
  link TEXT NOT NULL DEFAULT '',
  dedupe_key TEXT NOT NULL DEFAULT '',
  visible_in_app BOOLEAN NOT NULL DEFAULT TRUE,
  is_read BOOLEAN NOT NULL DEFAULT FALSE,
  read_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS customer_notifications_user_dedupe_idx
  ON customer_notifications(user_id, dedupe_key)
  WHERE dedupe_key <> '';

CREATE INDEX IF NOT EXISTS customer_notifications_user_created_idx
  ON customer_notifications(user_id, visible_in_app, created_at DESC);

CREATE INDEX IF NOT EXISTS customer_notifications_user_unread_idx
  ON customer_notifications(user_id, is_read, created_at DESC)
  WHERE visible_in_app = TRUE;

CREATE TABLE IF NOT EXISTS customer_notification_deliveries (
  id TEXT PRIMARY KEY,
  notification_id TEXT NOT NULL REFERENCES customer_notifications(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL REFERENCES workspace_users(id) ON DELETE CASCADE,
  event_type TEXT NOT NULL,
  channel TEXT NOT NULL,
  status TEXT NOT NULL,
  error_message TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS customer_notification_deliveries_notification_idx
  ON customer_notification_deliveries(notification_id, channel);
