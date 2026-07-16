package controlplane

import (
	"context"
	"database/sql"
	"errors"
	"sort"
)

func (r *MemoryRepository) ListWorkspaceUsers(context.Context) ([]WorkspaceUser, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]WorkspaceUser, 0, len(r.workspaceUsers))
	for _, user := range r.workspaceUsers {
		out = append(out, user)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Status == out[j].Status {
			return out[i].Email < out[j].Email
		}
		return out[i].Status < out[j].Status
	})
	return out, nil
}

func (r *MemoryRepository) SaveWorkspaceUser(_ context.Context, user WorkspaceUser) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.workspaceUsers[user.ID] = user
	return nil
}

func (r *MemoryRepository) ListAuthIdentities(_ context.Context, userID string) ([]AuthIdentity, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]AuthIdentity, 0)
	for _, identity := range r.authIdentities {
		if identity.UserID == userID {
			out = append(out, identity)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Issuer < out[j].Issuer })
	return out, nil
}

func (r *MemoryRepository) FindAuthIdentity(_ context.Context, issuer, subject string) (AuthIdentity, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	identity, ok := r.authIdentities[issuer+"\x00"+subject]
	return identity, ok, nil
}

func (r *MemoryRepository) SaveAuthIdentity(_ context.Context, identity AuthIdentity) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := identity.Issuer + "\x00" + identity.Subject
	if current, exists := r.authIdentities[key]; exists && current.UserID != identity.UserID {
		return errors.New("external identity is already bound to another user")
	}
	for currentKey, current := range r.authIdentities {
		if current.UserID == identity.UserID && current.Issuer == identity.Issuer && currentKey != key {
			return errors.New("user already has an identity for this issuer")
		}
	}
	r.authIdentities[key] = identity
	return nil
}

func (r *MemoryRepository) DeleteAuthIdentity(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, identity := range r.authIdentities {
		if identity.ID == id {
			delete(r.authIdentities, key)
			return nil
		}
	}
	return sql.ErrNoRows
}

func (r *MemoryRepository) ListRoleBindings(context.Context) ([]RoleBinding, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]RoleBinding, 0, len(r.roleBindings))
	for _, binding := range r.roleBindings {
		out = append(out, binding)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UserID == out[j].UserID {
			if out[i].ScopeType == out[j].ScopeType {
				return out[i].ScopeID < out[j].ScopeID
			}
			return out[i].ScopeType < out[j].ScopeType
		}
		return out[i].UserID < out[j].UserID
	})
	return out, nil
}

func (r *MemoryRepository) SaveRoleBinding(_ context.Context, binding RoleBinding) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.roleBindings[binding.ID] = binding
	return nil
}

func (r *MemoryRepository) DeleteRoleBinding(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.roleBindings, id)
	return nil
}

func (r *PostgresRepository) ListWorkspaceUsers(ctx context.Context) ([]WorkspaceUser, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT u.id, u.email, u.display_name, u.avatar_data_url, u.status, u.role, u.balance_cents, u.concurrency_limit, u.rpm_limit, u.external_issuer, u.external_subject, u.department_id, u.totp_enabled, u.totp_secret_ciphertext, u.totp_recovery_hashes, u.password_hash, u.email_verified, u.email_verify_hash, u.email_verify_expires_at, u.password_reset_hash, u.password_reset_expires_at, u.session_version, u.created_at, u.updated_at
FROM workspace_users u
ORDER BY u.status ASC, u.email ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]WorkspaceUser, 0)
	for rows.Next() {
		var user WorkspaceUser
		var recovery string
		if err := rows.Scan(&user.ID, &user.Email, &user.DisplayName, &user.AvatarDataURL, &user.Status, &user.Role, &user.BalanceCents, &user.ConcurrencyLimit, &user.RPMLimit, &user.ExternalIssuer, &user.ExternalSubject, &user.DepartmentID, &user.TOTPEnabled, &user.TOTPSecretCiphertext, &recovery, &user.PasswordHash, &user.EmailVerified, &user.EmailVerifyHash, &user.EmailVerifyExpiresAt, &user.PasswordResetHash, &user.PasswordResetExpiresAt, &user.SessionVersion, &user.CreatedAt, &user.UpdatedAt); err != nil {
			return nil, err
		}
		user.TOTPRecoveryHashes = parseStringList(recovery)
		out = append(out, user)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SaveWorkspaceUser(ctx context.Context, user WorkspaceUser) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO workspace_users(id, email, display_name, avatar_data_url, status, role, balance_cents, concurrency_limit, rpm_limit, external_issuer, external_subject, department_id, totp_enabled, totp_secret_ciphertext, totp_recovery_hashes, password_hash, email_verified, email_verify_hash, email_verify_expires_at, password_reset_hash, password_reset_expires_at, session_version, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24)
ON CONFLICT(id) DO UPDATE SET
  email = EXCLUDED.email,
	  display_name = EXCLUDED.display_name,
	  avatar_data_url = EXCLUDED.avatar_data_url,
  status = EXCLUDED.status,
  role = EXCLUDED.role,
  balance_cents = EXCLUDED.balance_cents,
  concurrency_limit = EXCLUDED.concurrency_limit,
  rpm_limit = EXCLUDED.rpm_limit,
  external_issuer = EXCLUDED.external_issuer,
  external_subject = EXCLUDED.external_subject,
  department_id = EXCLUDED.department_id,
  totp_enabled = EXCLUDED.totp_enabled,
  totp_secret_ciphertext = EXCLUDED.totp_secret_ciphertext,
  totp_recovery_hashes = EXCLUDED.totp_recovery_hashes,
  password_hash = EXCLUDED.password_hash,
  email_verified = EXCLUDED.email_verified,
  email_verify_hash = EXCLUDED.email_verify_hash,
  email_verify_expires_at = EXCLUDED.email_verify_expires_at,
  password_reset_hash = EXCLUDED.password_reset_hash,
  password_reset_expires_at = EXCLUDED.password_reset_expires_at,
	 session_version = EXCLUDED.session_version,
  updated_at = EXCLUDED.updated_at
	`, user.ID, user.Email, user.DisplayName, user.AvatarDataURL, user.Status, user.Role, user.BalanceCents, user.ConcurrencyLimit, user.RPMLimit, user.ExternalIssuer, user.ExternalSubject, user.DepartmentID, user.TOTPEnabled, user.TOTPSecretCiphertext, marshalStringList(user.TOTPRecoveryHashes), user.PasswordHash, user.EmailVerified, user.EmailVerifyHash, user.EmailVerifyExpiresAt, user.PasswordResetHash, user.PasswordResetExpiresAt, user.SessionVersion, user.CreatedAt, user.UpdatedAt)
	return err
}

func (r *PostgresRepository) ListAuthIdentities(ctx context.Context, userID string) ([]AuthIdentity, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,user_id,issuer,subject,email,created_at,updated_at FROM auth_identities WHERE user_id=$1 ORDER BY issuer`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AuthIdentity, 0)
	for rows.Next() {
		var identity AuthIdentity
		if err := rows.Scan(&identity.ID, &identity.UserID, &identity.Issuer, &identity.Subject, &identity.Email, &identity.CreatedAt, &identity.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, identity)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) FindAuthIdentity(ctx context.Context, issuer, subject string) (AuthIdentity, bool, error) {
	var identity AuthIdentity
	err := r.db.QueryRowContext(ctx, `SELECT id,user_id,issuer,subject,email,created_at,updated_at FROM auth_identities WHERE issuer=$1 AND subject=$2`, issuer, subject).Scan(&identity.ID, &identity.UserID, &identity.Issuer, &identity.Subject, &identity.Email, &identity.CreatedAt, &identity.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return AuthIdentity{}, false, nil
	}
	return identity, err == nil, err
}

func (r *PostgresRepository) SaveAuthIdentity(ctx context.Context, identity AuthIdentity) error {
	result, err := r.db.ExecContext(ctx, `INSERT INTO auth_identities(id,user_id,issuer,subject,email,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(issuer,subject) DO UPDATE SET email=EXCLUDED.email,updated_at=EXCLUDED.updated_at WHERE auth_identities.user_id=EXCLUDED.user_id`, identity.ID, identity.UserID, identity.Issuer, identity.Subject, identity.Email, identity.CreatedAt, identity.UpdatedAt)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err == nil && rows == 0 {
		return errors.New("external identity is already bound to another user")
	}
	return nil
}

func (r *PostgresRepository) DeleteAuthIdentity(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM auth_identities WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err == nil && rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *PostgresRepository) ListRoleBindings(ctx context.Context) ([]RoleBinding, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, user_id, role, scope_type, scope_id, created_at, updated_at
FROM role_bindings
ORDER BY user_id ASC, scope_type ASC, scope_id ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RoleBinding, 0)
	for rows.Next() {
		var binding RoleBinding
		if err := rows.Scan(&binding.ID, &binding.UserID, &binding.Role, &binding.ScopeType, &binding.ScopeID, &binding.CreatedAt, &binding.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, binding)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SaveRoleBinding(ctx context.Context, binding RoleBinding) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO role_bindings(id, user_id, role, scope_type, scope_id, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT(id) DO UPDATE SET
  user_id = EXCLUDED.user_id,
  role = EXCLUDED.role,
  scope_type = EXCLUDED.scope_type,
  scope_id = EXCLUDED.scope_id,
  updated_at = EXCLUDED.updated_at
`, binding.ID, binding.UserID, binding.Role, binding.ScopeType, binding.ScopeID, binding.CreatedAt, binding.UpdatedAt)
	return err
}

func (r *PostgresRepository) DeleteRoleBinding(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM role_bindings WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err == nil && rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}
