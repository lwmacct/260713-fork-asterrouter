CREATE TABLE IF NOT EXISTS auth_identities (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES workspace_users(id) ON DELETE CASCADE,
  issuer TEXT NOT NULL,
  subject TEXT NOT NULL,
  email TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE(issuer, subject),
  UNIQUE(user_id, issuer)
);

INSERT INTO auth_identities(id,user_id,issuer,subject,email,created_at,updated_at)
SELECT 'aid_' || md5(id || ':' || external_issuer || ':' || external_subject),
       id, external_issuer, external_subject, email, created_at, updated_at
FROM workspace_users
WHERE external_issuer <> '' AND external_subject <> ''
ON CONFLICT DO NOTHING;
