package controlplane

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/auth"
	"golang.org/x/crypto/bcrypt"
)

type TOTPSetup struct {
	Secret          string `json:"secret"`
	ProvisioningURI string `json:"provisioning_uri"`
}

const maxAvatarDataURLBytes = 256 * 1024

var ErrDeploymentManagedAccount = errors.New("personal account is managed by deployment configuration")

func (s *Service) EnsureLocalAdmin(ctx context.Context, username, password string, defaults ...WorkspaceUserDefaults) (WorkspaceUser, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		username = "admin"
	}
	users, err := s.repo.ListWorkspaceUsers(ctx)
	if err != nil {
		return WorkspaceUser{}, err
	}
	for _, user := range users {
		if user.ID != username {
			continue
		}
		changed := false
		if user.Status != WorkspaceUserStatusActive {
			user.Status = WorkspaceUserStatusActive
			changed = true
		}
		if user.Role != RoleSuperAdmin {
			user.Role = RoleSuperAdmin
			changed = true
		}
		if user.DisplayName == "" {
			user.DisplayName = username
			changed = true
		}
		if user.PasswordHash == "" {
			hash, hashErr := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
			if hashErr != nil {
				return WorkspaceUser{}, hashErr
			}
			user.PasswordHash = string(hash)
			changed = true
		}
		if changed {
			user.UpdatedAt = time.Now().UTC()
			if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
				return WorkspaceUser{}, err
			}
		}
		return user, nil
	}

	email := strings.ToLower(username)
	if !strings.Contains(email, "@") {
		email += "@local.invalid"
	}
	for _, user := range users {
		if strings.EqualFold(user.Email, email) {
			return WorkspaceUser{}, fmt.Errorf("local administrator email %s already belongs to another user", email)
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return WorkspaceUser{}, err
	}
	now := time.Now().UTC()
	user := WorkspaceUser{
		ID: username, Email: email, DisplayName: username,
		Status: WorkspaceUserStatusActive, Role: RoleSuperAdmin,
		PasswordHash: string(hash), EmailVerified: true,
		CreatedAt: now, UpdatedAt: now,
	}
	applyWorkspaceUserDefaults(&user, defaults)
	if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
		return WorkspaceUser{}, err
	}
	if err := s.audit(ctx, systemActor, "bootstrap", "workspace_user", user.ID, "Provisioned local administrator account"); err != nil {
		return WorkspaceUser{}, err
	}
	return user, nil
}

func (s *Service) CurrentAccountProfile(ctx context.Context, actor string) (AccountProfile, error) {
	user, err := s.workspaceUserForActor(ctx, actor)
	if err != nil {
		return AccountProfile{}, err
	}
	profile := accountProfileFromUser(user)
	profile.AuthIdentities, err = s.repo.ListAuthIdentities(ctx, user.ID)
	return profile, err
}

func (s *Service) UpdateCurrentAccountProfile(ctx context.Context, actor string, req AccountProfileUpdateRequest) (AccountProfile, error) {
	user, err := s.workspaceUserForActor(ctx, actor)
	if err != nil {
		return AccountProfile{}, err
	}
	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		return AccountProfile{}, errors.New("display name is required")
	}
	if len([]rune(displayName)) > 80 {
		return AccountProfile{}, errors.New("display name must contain at most 80 characters")
	}
	if err := validateAvatarDataURL(req.AvatarDataURL); err != nil {
		return AccountProfile{}, err
	}
	user.DisplayName = displayName
	user.AvatarDataURL = strings.TrimSpace(req.AvatarDataURL)
	user.UpdatedAt = time.Now().UTC()
	if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
		return AccountProfile{}, err
	}
	if err := s.audit(ctx, actor, "account_profile_updated", "workspace_user", user.ID, "Updated personal account profile"); err != nil {
		return AccountProfile{}, err
	}
	return accountProfileFromUser(user), nil
}

func (s *Service) ChangeCurrentAccountPassword(ctx context.Context, actor string, req AccountPasswordUpdateRequest) error {
	user, err := s.workspaceUserForActor(ctx, actor)
	if err != nil {
		return err
	}
	if len(req.NewPassword) < 10 {
		return errors.New("new password must contain at least 10 characters")
	}
	settingPassword := user.PasswordHash == ""
	if !settingPassword {
		if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.CurrentPassword)) != nil {
			return errors.New("current password is incorrect")
		}
		if req.CurrentPassword == req.NewPassword {
			return errors.New("new password must be different from the current password")
		}
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	user.PasswordHash = string(passwordHash)
	user.PasswordResetHash = ""
	user.PasswordResetExpiresAt = nil
	user.SessionVersion++
	user.UpdatedAt = time.Now().UTC()
	if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
		return err
	}
	action, summary := "account_password_changed", "Changed personal account password"
	if settingPassword {
		action, summary = "account_password_enabled", "Enabled local password login"
	}
	if err := s.audit(ctx, actor, action, "workspace_user", user.ID, summary); err != nil {
		return err
	}
	_ = s.publishAccountSecurityNotification(ctx, user, "账户密码已更新", "您的账户密码刚刚发生变更。如非本人操作，请立即重置密码并撤销其他登录会话。", action)
	return nil
}

func (s *Service) CurrentAccountPasswordHash(ctx context.Context, actor string) (string, error) {
	user, err := s.workspaceUserForActor(ctx, actor)
	if err != nil {
		return "", err
	}
	if user.PasswordHash == "" {
		return "", errors.New("password login is not enabled for this account")
	}
	return user.PasswordHash, nil
}

func accountProfileFromUser(user WorkspaceUser) AccountProfile {
	return AccountProfile{
		ID: user.ID, Email: user.Email, DisplayName: user.DisplayName, AvatarDataURL: user.AvatarDataURL,
		Status: user.Status, Role: user.Role, BalanceMicros: user.BalanceMicros,
		ConcurrencyLimit: user.ConcurrencyLimit, RPMLimit: user.RPMLimit,
		ExternalIssuer: user.ExternalIssuer, EmailVerified: user.EmailVerified,
		PasswordEnabled: user.PasswordHash != "", TOTPEnabled: user.TOTPEnabled,
		CreatedAt: user.CreatedAt, UpdatedAt: user.UpdatedAt,
	}
}

func validateAvatarDataURL(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if len(value) > maxAvatarDataURLBytes {
		return errors.New("avatar must not exceed 256 KiB")
	}
	header, payload, ok := strings.Cut(value, ",")
	if !ok || !strings.HasSuffix(header, ";base64") || !oneOf(strings.TrimSuffix(header, ";base64"), "data:image/png", "data:image/jpeg", "data:image/webp", "data:image/gif") {
		return errors.New("avatar must be a PNG, JPEG, WebP, or GIF data URL")
	}
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return errors.New("avatar contains invalid base64 data")
	}
	detected := http.DetectContentType(decoded)
	if !oneOf(detected, "image/png", "image/jpeg", "image/webp", "image/gif") {
		return errors.New("avatar content does not match a supported image type")
	}
	return nil
}

func (s *Service) RegisterWorkspaceUser(ctx context.Context, email, password, displayName string, requireVerification bool, defaults ...WorkspaceUserDefaults) (WorkspaceUser, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || !strings.Contains(email, "@") {
		return WorkspaceUser{}, "", errors.New("valid email is required")
	}
	if len(password) < 10 {
		return WorkspaceUser{}, "", errors.New("password must contain at least 10 characters")
	}
	if err := s.ensureUniqueUserEmail(ctx, "", email); err != nil {
		return WorkspaceUser{}, "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return WorkspaceUser{}, "", err
	}
	now := time.Now().UTC()
	user := WorkspaceUser{ID: "usr_" + randomID(10), Email: email, DisplayName: strings.TrimSpace(displayName), Status: WorkspaceUserStatusActive, Role: RoleDeveloper, PasswordHash: string(hash), EmailVerified: !requireVerification, CreatedAt: now, UpdatedAt: now}
	applyWorkspaceUserDefaults(&user, defaults)
	verificationToken := ""
	if requireVerification {
		verificationToken, err = auth.RandomToken(32)
		if err != nil {
			return WorkspaceUser{}, "", err
		}
		user.EmailVerifyHash = recoveryCodeHash(verificationToken)
		expires := now.Add(30 * time.Minute)
		user.EmailVerifyExpiresAt = &expires
	}
	if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
		return WorkspaceUser{}, "", err
	}
	if err := s.audit(ctx, email, "register", "workspace_user", user.ID, "Registered workspace user"); err != nil {
		return WorkspaceUser{}, "", err
	}
	return user, verificationToken, nil
}

func (s *Service) VerifyWorkspaceUserEmail(ctx context.Context, token string) error {
	hash := recoveryCodeHash(token)
	users, err := s.repo.ListWorkspaceUsers(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, user := range users {
		if user.EmailVerifyHash == hash && user.EmailVerifyExpiresAt != nil && now.Before(*user.EmailVerifyExpiresAt) {
			user.EmailVerified = true
			user.EmailVerifyHash = ""
			user.EmailVerifyExpiresAt = nil
			user.UpdatedAt = now
			if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
				return err
			}
			return s.audit(ctx, user.Email, "email_verified", "workspace_user", user.ID, "Verified workspace user email")
		}
	}
	return errors.New("email verification token is invalid or expired")
}

func (s *Service) RenewEmailVerification(ctx context.Context, email string) (WorkspaceUser, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	users, err := s.repo.ListWorkspaceUsers(ctx)
	if err != nil {
		return WorkspaceUser{}, "", err
	}
	for _, user := range users {
		if user.Email == email && user.Status == WorkspaceUserStatusActive && !user.EmailVerified {
			token, err := auth.RandomToken(32)
			if err != nil {
				return WorkspaceUser{}, "", err
			}
			expires := time.Now().UTC().Add(30 * time.Minute)
			user.EmailVerifyHash = recoveryCodeHash(token)
			user.EmailVerifyExpiresAt = &expires
			user.UpdatedAt = time.Now().UTC()
			if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
				return WorkspaceUser{}, "", err
			}
			return user, token, nil
		}
	}
	return WorkspaceUser{}, "", errors.New("user is not awaiting email verification")
}

func (s *Service) AuthenticateWorkspaceUser(ctx context.Context, email, password string, requireVerified bool) (WorkspaceUser, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	users, err := s.repo.ListWorkspaceUsers(ctx)
	if err != nil {
		return WorkspaceUser{}, err
	}
	for _, user := range users {
		if user.Email == email && user.PasswordHash != "" {
			if user.Status != WorkspaceUserStatusActive || (requireVerified && !user.EmailVerified) || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
				break
			}
			return user, nil
		}
	}
	return WorkspaceUser{}, errors.New("invalid email or password")
}

func (s *Service) BeginPasswordReset(ctx context.Context, email string) (WorkspaceUser, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	users, err := s.repo.ListWorkspaceUsers(ctx)
	if err != nil {
		return WorkspaceUser{}, "", err
	}
	for _, user := range users {
		if user.Email == email && user.Status == WorkspaceUserStatusActive && user.PasswordHash != "" {
			token, err := auth.RandomToken(32)
			if err != nil {
				return WorkspaceUser{}, "", err
			}
			expires := time.Now().UTC().Add(30 * time.Minute)
			user.PasswordResetHash = recoveryCodeHash(token)
			user.PasswordResetExpiresAt = &expires
			user.UpdatedAt = time.Now().UTC()
			if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
				return WorkspaceUser{}, "", err
			}
			_ = s.audit(ctx, email, "password_reset_requested", "workspace_user", user.ID, "Requested password reset")
			return user, token, nil
		}
	}
	return WorkspaceUser{}, "", errors.New("user is not eligible for password reset")
}

func (s *Service) CompletePasswordReset(ctx context.Context, token, password string) error {
	if len(password) < 10 {
		return errors.New("password must contain at least 10 characters")
	}
	hashToken := recoveryCodeHash(token)
	users, err := s.repo.ListWorkspaceUsers(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, user := range users {
		if user.PasswordResetHash == hashToken && user.PasswordResetExpiresAt != nil && now.Before(*user.PasswordResetExpiresAt) {
			passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
			if err != nil {
				return err
			}
			user.PasswordHash = string(passwordHash)
			user.PasswordResetHash = ""
			user.PasswordResetExpiresAt = nil
			user.SessionVersion++
			user.UpdatedAt = now
			if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
				return err
			}
			if err := s.audit(ctx, user.Email, "password_reset_completed", "workspace_user", user.ID, "Completed password reset"); err != nil {
				return err
			}
			_ = s.publishAccountSecurityNotification(ctx, user, "账户密码已重置", "您的账户密码已通过密码找回流程重置，其他登录会话已失效。", "password_reset_completed")
			return nil
		}
	}
	return errors.New("password reset token is invalid or expired")
}

func (s *Service) BeginTOTPSetup(ctx context.Context, actor string) (TOTPSetup, error) {
	user, err := s.workspaceUserByID(ctx, actor)
	if err != nil {
		return TOTPSetup{}, err
	}
	if user.Status != WorkspaceUserStatusActive {
		return TOTPSetup{}, errors.New("workspace user is disabled")
	}
	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		return TOTPSetup{}, err
	}
	ciphertext, err := encryptSecret(s.secretKey, secret)
	if err != nil {
		return TOTPSetup{}, err
	}
	user.TOTPEnabled = false
	user.TOTPSecretCiphertext = ciphertext
	user.UpdatedAt = time.Now().UTC()
	if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
		return TOTPSetup{}, err
	}
	if err := s.audit(ctx, actor, "totp_setup_started", "workspace_user", user.ID, "Started TOTP enrollment"); err != nil {
		return TOTPSetup{}, err
	}
	return TOTPSetup{Secret: secret, ProvisioningURI: auth.TOTPProvisioningURI("AsterRouter", user.Email, secret)}, nil
}

func (s *Service) ConfirmTOTP(ctx context.Context, actor, code string) error {
	_, err := s.confirmTOTP(ctx, actor, code, false)
	return err
}

func (s *Service) ConfirmTOTPWithRecoveryCodes(ctx context.Context, actor, code string) ([]string, error) {
	return s.confirmTOTP(ctx, actor, code, true)
}

func (s *Service) confirmTOTP(ctx context.Context, actor, code string, includeRecoveryCodes bool) ([]string, error) {
	user, err := s.workspaceUserByID(ctx, actor)
	if err != nil {
		return nil, err
	}
	secret, err := decryptSecret(s.secretKey, user.TOTPSecretCiphertext)
	if err != nil {
		return nil, errors.New("TOTP enrollment has not been started")
	}
	if !auth.ValidateTOTP(secret, code, time.Now().UTC()) {
		return nil, errors.New("invalid TOTP code")
	}
	var codes []string
	if includeRecoveryCodes {
		var hashes []string
		codes, hashes, err = newTOTPRecoveryCodes()
		if err != nil {
			return nil, err
		}
		user.TOTPRecoveryHashes = hashes
	}
	user.TOTPEnabled = true
	user.SessionVersion++
	user.UpdatedAt = time.Now().UTC()
	if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
		return nil, err
	}
	if err := s.audit(ctx, actor, "totp_enabled", "workspace_user", user.ID, "Enabled TOTP authentication"); err != nil {
		return nil, err
	}
	_ = s.publishAccountSecurityNotification(ctx, user, "两步验证已启用", "您的账户已启用 TOTP 两步验证。", "totp_enabled")
	return codes, nil
}

func (s *Service) DisableTOTP(ctx context.Context, actor, code string) error {
	user, err := s.workspaceUserByID(ctx, actor)
	if err != nil {
		return err
	}
	secret, err := decryptSecret(s.secretKey, user.TOTPSecretCiphertext)
	if err != nil || !user.TOTPEnabled || !auth.ValidateTOTP(secret, code, time.Now().UTC()) {
		return errors.New("invalid TOTP code")
	}
	user.TOTPEnabled = false
	user.TOTPSecretCiphertext = ""
	user.TOTPRecoveryHashes = nil
	user.SessionVersion++
	user.UpdatedAt = time.Now().UTC()
	if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
		return err
	}
	if err := s.audit(ctx, actor, "totp_disabled", "workspace_user", user.ID, "Disabled TOTP authentication"); err != nil {
		return err
	}
	_ = s.publishAccountSecurityNotification(ctx, user, "两步验证已关闭", "您的账户已关闭 TOTP 两步验证。如非本人操作，请立即检查账户安全。", "totp_disabled")
	return nil
}

func (s *Service) VerifyUserTOTP(ctx context.Context, userID, code string) (WorkspaceUser, error) {
	user, err := s.workspaceUserByID(ctx, userID)
	if err != nil {
		return WorkspaceUser{}, err
	}
	if user.Status != WorkspaceUserStatusActive || !user.TOTPEnabled {
		return WorkspaceUser{}, errors.New("TOTP is not enabled")
	}
	secret, err := decryptSecret(s.secretKey, user.TOTPSecretCiphertext)
	if err == nil && auth.ValidateTOTP(secret, code, time.Now().UTC()) {
		return user, nil
	}
	hash := recoveryCodeHash(code)
	for index, stored := range user.TOTPRecoveryHashes {
		if stored == hash {
			user.TOTPRecoveryHashes = append(user.TOTPRecoveryHashes[:index], user.TOTPRecoveryHashes[index+1:]...)
			user.UpdatedAt = time.Now().UTC()
			if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
				return WorkspaceUser{}, err
			}
			_ = s.audit(ctx, userID, "totp_recovery_used", "workspace_user", user.ID, "Used a TOTP recovery code")
			return user, nil
		}
	}
	return WorkspaceUser{}, errors.New("invalid TOTP code")
}

func (s *Service) GenerateTOTPRecoveryCodes(ctx context.Context, actor string) ([]string, error) {
	user, err := s.workspaceUserByID(ctx, actor)
	if err != nil {
		return nil, err
	}
	if !user.TOTPEnabled {
		return nil, errors.New("TOTP is not enabled")
	}
	codes, hashes, err := newTOTPRecoveryCodes()
	if err != nil {
		return nil, err
	}
	user.TOTPRecoveryHashes = hashes
	user.SessionVersion++
	user.UpdatedAt = time.Now().UTC()
	if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
		return nil, err
	}
	if err := s.audit(ctx, actor, "totp_recovery_regenerated", "workspace_user", user.ID, "Regenerated TOTP recovery codes"); err != nil {
		return nil, err
	}
	return codes, nil
}

func newTOTPRecoveryCodes() ([]string, []string, error) {
	codes := make([]string, 10)
	hashes := make([]string, 10)
	for i := range codes {
		token, err := auth.GenerateRecoveryCode()
		if err != nil {
			return nil, nil, err
		}
		codes[i], hashes[i] = token, recoveryCodeHash(token)
	}
	return codes, hashes, nil
}

func recoveryCodeHash(code string) string {
	sum := sha256.Sum256([]byte(strings.ToUpper(strings.TrimSpace(code))))
	return hex.EncodeToString(sum[:])
}

func (s *Service) ListWorkspaceUsers(ctx context.Context) ([]WorkspaceUser, error) {
	return s.repo.ListWorkspaceUsers(ctx)
}

func (s *Service) ExternalIdentityExists(ctx context.Context, issuer, subject string) (bool, error) {
	issuer = strings.TrimSpace(issuer)
	subject = strings.TrimSpace(subject)
	if issuer == "" || subject == "" {
		return false, nil
	}
	_, found, err := s.repo.FindAuthIdentity(ctx, issuer, subject)
	if err != nil || found {
		return found, err
	}
	users, err := s.repo.ListWorkspaceUsers(ctx)
	if err != nil {
		return false, err
	}
	for _, user := range users {
		if user.ExternalIssuer == issuer && user.ExternalSubject == subject {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) UnbindCurrentAuthIdentity(ctx context.Context, actor, provider string) error {
	user, err := s.workspaceUserForActor(ctx, actor)
	if err != nil {
		return err
	}
	identities, err := s.repo.ListAuthIdentities(ctx, user.ID)
	if err != nil {
		return err
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	index := -1
	for i, identity := range identities {
		issuer := strings.ToLower(identity.Issuer)
		matches := issuer == provider || (provider == "feishu" && strings.HasPrefix(issuer, "feishu:"))
		if provider == "oidc" {
			matches = issuer != "github" && issuer != "google" && issuer != "dingtalk" && !strings.HasPrefix(issuer, "feishu:")
		}
		if matches {
			index = i
			break
		}
	}
	if index < 0 {
		return errors.New("authentication identity is not bound")
	}
	if user.PasswordHash == "" && len(identities) <= 1 {
		return errors.New("cannot remove the last available login method")
	}
	identity := identities[index]
	if err := s.repo.DeleteAuthIdentity(ctx, identity.ID); err != nil {
		return err
	}
	if user.ExternalIssuer == identity.Issuer && user.ExternalSubject == identity.Subject {
		user.ExternalIssuer, user.ExternalSubject = "", ""
		user.UpdatedAt = time.Now().UTC()
		if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
			return err
		}
	}
	return s.audit(ctx, actor, "auth_identity_unbound", "workspace_user", user.ID, "Unbound "+identity.Issuer+" authentication identity")
}

func (s *Service) BindCurrentAuthIdentity(ctx context.Context, actor, issuer, subject, email string) error {
	user, err := s.workspaceUserForActor(ctx, actor)
	if err != nil {
		return err
	}
	if user.Status != WorkspaceUserStatusActive {
		return errors.New("workspace user is disabled")
	}
	issuer, subject = strings.TrimSpace(issuer), strings.TrimSpace(subject)
	if issuer == "" || subject == "" {
		return errors.New("authentication identity is incomplete")
	}
	if existing, found, err := s.repo.FindAuthIdentity(ctx, issuer, subject); err != nil {
		return err
	} else if found {
		if existing.UserID == user.ID {
			return errors.New("authentication identity is already bound")
		}
		return errors.New("authentication identity is already bound to another user")
	}
	now := time.Now().UTC()
	identity := AuthIdentity{ID: "aid_" + randomID(10), UserID: user.ID, Issuer: issuer, Subject: subject, Email: strings.ToLower(strings.TrimSpace(email)), CreatedAt: now, UpdatedAt: now}
	if err := s.repo.SaveAuthIdentity(ctx, identity); err != nil {
		return err
	}
	return s.audit(ctx, actor, "auth_identity_bound", "workspace_user", user.ID, "Bound "+issuer+" authentication identity")
}

func (s *Service) SessionVersion(ctx context.Context, actor string) (int64, bool) {
	user, err := s.workspaceUserForActor(ctx, actor)
	if err != nil {
		return 0, false
	}
	if user.Status != WorkspaceUserStatusActive {
		return user.SessionVersion + 1, true
	}
	return user.SessionVersion, true
}

func (s *Service) RevokeAccountSessions(ctx context.Context, actor string) error {
	user, err := s.workspaceUserForActor(ctx, actor)
	if err != nil {
		return err
	}
	user.SessionVersion++
	user.UpdatedAt = time.Now().UTC()
	if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
		return err
	}
	return s.audit(ctx, actor, "account_sessions_revoked", "workspace_user", user.ID, "Revoked all account sessions")
}

func (s *Service) ProvisionOIDCUser(ctx context.Context, issuer, subject, email, displayName, departmentCode string, defaults ...WorkspaceUserDefaults) (WorkspaceUser, error) {
	issuer = strings.TrimSpace(issuer)
	subject = strings.TrimSpace(subject)
	email = strings.ToLower(strings.TrimSpace(email))
	if issuer == "" || subject == "" {
		return WorkspaceUser{}, errors.New("oidc issuer and subject are required")
	}
	if email == "" || !strings.Contains(email, "@") {
		return WorkspaceUser{}, errors.New("oidc email claim is required")
	}
	identity, found, err := s.repo.FindAuthIdentity(ctx, issuer, subject)
	if err != nil {
		return WorkspaceUser{}, err
	}
	if found {
		user, err := s.workspaceUserByID(ctx, identity.UserID)
		if err != nil {
			return WorkspaceUser{}, err
		}
		if user.Status != WorkspaceUserStatusActive {
			return WorkspaceUser{}, errors.New("workspace user is disabled")
		}
		return user, nil
	}
	users, err := s.repo.ListWorkspaceUsers(ctx)
	if err != nil {
		return WorkspaceUser{}, err
	}
	for _, user := range users {
		if user.ExternalIssuer == issuer && user.ExternalSubject == subject {
			if user.Status != WorkspaceUserStatusActive {
				return WorkspaceUser{}, errors.New("workspace user is disabled")
			}
			now := time.Now().UTC()
			_ = s.repo.SaveAuthIdentity(ctx, AuthIdentity{ID: "aid_" + randomID(10), UserID: user.ID, Issuer: issuer, Subject: subject, Email: user.Email, CreatedAt: now, UpdatedAt: now})
			return user, nil
		}
		if user.Email == email && (user.ExternalIssuer != "" || user.ExternalSubject != "") {
			return WorkspaceUser{}, errors.New("email is already bound to another external identity")
		}
	}
	departmentID := ""
	if code := strings.TrimSpace(departmentCode); code != "" {
		departments, err := s.repo.ListDepartments(ctx)
		if err != nil {
			return WorkspaceUser{}, err
		}
		for _, department := range departments {
			if strings.EqualFold(department.Code, code) && department.Status == DepartmentStatusActive {
				departmentID = department.ID
				break
			}
		}
	}
	now := time.Now().UTC()
	user := WorkspaceUser{ID: "usr_" + randomID(10), Email: email, DisplayName: strings.TrimSpace(displayName), Status: WorkspaceUserStatusActive, Role: RoleDeveloper, ExternalIssuer: issuer, ExternalSubject: subject, DepartmentID: departmentID, CreatedAt: now, UpdatedAt: now}
	applyWorkspaceUserDefaults(&user, defaults)
	if err := s.ensureUniqueUserEmail(ctx, "", email); err != nil {
		return WorkspaceUser{}, err
	}
	if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
		return WorkspaceUser{}, err
	}
	if err := s.repo.SaveAuthIdentity(ctx, AuthIdentity{ID: "aid_" + randomID(10), UserID: user.ID, Issuer: issuer, Subject: subject, Email: email, CreatedAt: now, UpdatedAt: now}); err != nil {
		return WorkspaceUser{}, err
	}
	if err := s.audit(ctx, email, "oidc_provision", "workspace_user", user.ID, fmt.Sprintf("Provisioned workspace user %s through OIDC", email)); err != nil {
		return WorkspaceUser{}, err
	}
	return user, nil
}

func applyWorkspaceUserDefaults(user *WorkspaceUser, values []WorkspaceUserDefaults) {
	if len(values) == 0 {
		return
	}
	user.BalanceMicros = max(values[0].BalanceMicros, 0)
	user.ConcurrencyLimit = max(values[0].ConcurrencyLimit, 0)
	user.RPMLimit = max(values[0].RPMLimit, 0)
}

func (s *Service) CreateWorkspaceUser(ctx context.Context, actor string, req WorkspaceUserRequest) (WorkspaceUser, error) {
	now := time.Now().UTC()
	user, err := workspaceUserFromRequest(req, now)
	if err != nil {
		return WorkspaceUser{}, err
	}
	if err := s.validateWorkspaceUserDepartment(ctx, user.DepartmentID); err != nil {
		return WorkspaceUser{}, err
	}
	if err := s.ensureUniqueUserEmail(ctx, "", user.Email); err != nil {
		return WorkspaceUser{}, err
	}
	user.ID = "usr_" + randomID(10)
	if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
		return WorkspaceUser{}, err
	}
	if err := s.audit(ctx, actor, "create", "workspace_user", user.ID, fmt.Sprintf("Created workspace user %s", user.Email)); err != nil {
		return WorkspaceUser{}, err
	}
	return user, nil
}

func (s *Service) UpdateWorkspaceUser(ctx context.Context, actor string, id string, req WorkspaceUserRequest) (WorkspaceUser, error) {
	existing, err := s.workspaceUserByID(ctx, id)
	if err != nil {
		return WorkspaceUser{}, err
	}
	user, err := workspaceUserFromRequest(req, existing.CreatedAt)
	if err != nil {
		return WorkspaceUser{}, err
	}
	if err := s.ensureUniqueUserEmail(ctx, existing.ID, user.Email); err != nil {
		return WorkspaceUser{}, err
	}
	user.ID = existing.ID
	user.AvatarDataURL = existing.AvatarDataURL
	user.ExternalIssuer = existing.ExternalIssuer
	user.ExternalSubject = existing.ExternalSubject
	if req.DepartmentID == nil {
		user.DepartmentID = existing.DepartmentID
	}
	if err := s.validateWorkspaceUserDepartment(ctx, user.DepartmentID); err != nil {
		return WorkspaceUser{}, err
	}
	user.TOTPEnabled = existing.TOTPEnabled
	user.TOTPSecretCiphertext = existing.TOTPSecretCiphertext
	user.TOTPRecoveryHashes = existing.TOTPRecoveryHashes
	user.PasswordHash = existing.PasswordHash
	user.EmailVerified = existing.EmailVerified
	user.EmailVerifyHash = existing.EmailVerifyHash
	user.EmailVerifyExpiresAt = existing.EmailVerifyExpiresAt
	user.PasswordResetHash = existing.PasswordResetHash
	user.PasswordResetExpiresAt = existing.PasswordResetExpiresAt
	user.SessionVersion = existing.SessionVersion
	if user.Status != existing.Status || user.Role != existing.Role || user.DepartmentID != existing.DepartmentID {
		user.SessionVersion++
	}
	user.CreatedAt = existing.CreatedAt
	user.UpdatedAt = time.Now().UTC()
	if err := s.repo.SaveWorkspaceUser(ctx, user); err != nil {
		return WorkspaceUser{}, err
	}
	if err := s.audit(ctx, actor, "update", "workspace_user", user.ID, fmt.Sprintf("Updated workspace user %s", user.Email)); err != nil {
		return WorkspaceUser{}, err
	}
	return user, nil
}

func (s *Service) validateWorkspaceUserDepartment(ctx context.Context, departmentID string) error {
	departmentID = strings.TrimSpace(departmentID)
	if departmentID == "" {
		return nil
	}
	departments, err := s.repo.ListDepartments(ctx)
	if err != nil {
		return err
	}
	for _, department := range departments {
		if department.ID == departmentID && department.Status == DepartmentStatusActive {
			return nil
		}
	}
	return errors.New("active department not found")
}

func (s *Service) ListRoleBindings(ctx context.Context) ([]RoleBinding, error) {
	return s.repo.ListRoleBindings(ctx)
}

func (s *Service) CreateRoleBinding(ctx context.Context, actor string, req RoleBindingRequest) (RoleBinding, error) {
	now := time.Now().UTC()
	binding, err := s.roleBindingFromRequest(ctx, req, now)
	if err != nil {
		return RoleBinding{}, err
	}
	if err := s.ensureUniqueRoleBinding(ctx, binding); err != nil {
		return RoleBinding{}, err
	}
	binding.ID = "rb_" + randomID(10)
	if err := s.repo.SaveRoleBinding(ctx, binding); err != nil {
		return RoleBinding{}, err
	}
	if err := s.audit(ctx, actor, "grant_role", "role_binding", binding.ID, fmt.Sprintf("Granted %s on %s:%s to %s", binding.Role, binding.ScopeType, binding.ScopeID, binding.UserID)); err != nil {
		return RoleBinding{}, err
	}
	return binding, nil
}

func (s *Service) DeleteRoleBinding(ctx context.Context, actor string, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("role binding id is required")
	}
	binding, err := s.roleBindingByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repo.DeleteRoleBinding(ctx, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("role binding %s not found", id)
		}
		return err
	}
	return s.audit(ctx, actor, "revoke_role", "role_binding", binding.ID, fmt.Sprintf("Revoked %s on %s:%s from %s", binding.Role, binding.ScopeType, binding.ScopeID, binding.UserID))
}

func workspaceUserFromRequest(req WorkspaceUserRequest, createdAt time.Time) (WorkspaceUser, error) {
	now := time.Now().UTC()
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" || !strings.Contains(email, "@") {
		return WorkspaceUser{}, errors.New("valid user email is required")
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = WorkspaceUserStatusActive
	}
	if status != WorkspaceUserStatusActive && status != WorkspaceUserStatusDisabled {
		return WorkspaceUser{}, errors.New("invalid user status")
	}
	role := strings.TrimSpace(req.Role)
	if role == "" {
		role = RoleDeveloper
	}
	if !validRole(role) {
		return WorkspaceUser{}, errors.New("invalid user role")
	}
	if createdAt.IsZero() {
		createdAt = now
	}
	departmentID := ""
	if req.DepartmentID != nil {
		departmentID = strings.TrimSpace(*req.DepartmentID)
	}
	return WorkspaceUser{
		Email:        email,
		DisplayName:  strings.TrimSpace(req.DisplayName),
		Status:       status,
		Role:         role,
		DepartmentID: departmentID,
		CreatedAt:    createdAt,
		UpdatedAt:    now,
	}, nil
}

func (s *Service) roleBindingFromRequest(ctx context.Context, req RoleBindingRequest, createdAt time.Time) (RoleBinding, error) {
	now := time.Now().UTC()
	userID := strings.TrimSpace(req.UserID)
	if userID == "" {
		return RoleBinding{}, errors.New("user id is required")
	}
	if _, err := s.workspaceUserByID(ctx, userID); err != nil {
		return RoleBinding{}, err
	}
	role := strings.TrimSpace(req.Role)
	if !validRole(role) {
		return RoleBinding{}, errors.New("invalid role")
	}
	scopeType := strings.TrimSpace(req.ScopeType)
	if scopeType == "" {
		scopeType = RoleScopeGlobal
	}
	if !oneOf(scopeType, RoleScopeGlobal, RoleScopeResource, RoleScopeSurface, RoleScopeDepartment) {
		return RoleBinding{}, errors.New("invalid role scope")
	}
	scopeID := strings.TrimSpace(req.ScopeID)
	if scopeType == RoleScopeGlobal {
		scopeID = ""
	} else if scopeID == "" {
		return RoleBinding{}, errors.New("scope_id is required for scoped role bindings")
	} else if scopeType == RoleScopeResource && !validRBACResource(scopeID) {
		return RoleBinding{}, errors.New("invalid RBAC resource scope")
	} else if scopeType == RoleScopeSurface && !validSurface(scopeID) {
		return RoleBinding{}, errors.New("invalid surface scope")
	} else if scopeType == RoleScopeDepartment {
		if _, err := s.departmentByID(ctx, scopeID); err != nil {
			return RoleBinding{}, errors.New("department scope does not exist")
		}
	}
	if createdAt.IsZero() {
		createdAt = now
	}
	return RoleBinding{
		UserID:    userID,
		Role:      role,
		ScopeType: scopeType,
		ScopeID:   scopeID,
		CreatedAt: createdAt,
		UpdatedAt: now,
	}, nil
}

func validRBACResource(resource string) bool {
	return oneOf(resource,
		RBACResourceDashboard, RBACResourceRouting, RBACResourceProviders, RBACResourceAPIKeys,
		RBACResourceUsage, RBACResourceTraces, RBACResourceAIJobs, RBACResourceArtifacts, RBACResourceAlerts, RBACResourceIdentity,
		RBACResourcePolicies, RBACResourceAudit, RBACResourceExports, RBACResourcePlugins,
		RBACResourceSettings, RBACResourceSystem,
	)
}

func validSurface(surface string) bool {
	return oneOf(surface, SurfacePersonal, SurfaceRelayOperator, SurfaceEnterprise, SurfacePlatform, SurfacePortal, SurfaceCustomer)
}

func validRole(role string) bool {
	switch role {
	case RoleSuperAdmin, RolePlatformAdmin, RoleKeyManager, RoleReadOnlyAuditor, RoleDeveloper:
		return true
	default:
		return false
	}
}

func (s *Service) workspaceUserByID(ctx context.Context, id string) (WorkspaceUser, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return WorkspaceUser{}, errors.New("user id is required")
	}
	users, err := s.repo.ListWorkspaceUsers(ctx)
	if err != nil {
		return WorkspaceUser{}, err
	}
	for _, user := range users {
		if user.ID == id {
			return user, nil
		}
	}
	return WorkspaceUser{}, fmt.Errorf("user %s not found", id)
}

func (s *Service) workspaceUserForActor(ctx context.Context, actor string) (WorkspaceUser, error) {
	users, err := s.repo.ListWorkspaceUsers(ctx)
	if err != nil {
		return WorkspaceUser{}, err
	}
	if user, ok := workspaceUserByActor(users, actor); ok {
		return user, nil
	}
	return WorkspaceUser{}, ErrDeploymentManagedAccount
}

func (s *Service) roleBindingByID(ctx context.Context, id string) (RoleBinding, error) {
	bindings, err := s.repo.ListRoleBindings(ctx)
	if err != nil {
		return RoleBinding{}, err
	}
	for _, binding := range bindings {
		if binding.ID == id {
			return binding, nil
		}
	}
	return RoleBinding{}, fmt.Errorf("role binding %s not found", id)
}

func (s *Service) ensureUniqueUserEmail(ctx context.Context, currentID string, email string) error {
	users, err := s.repo.ListWorkspaceUsers(ctx)
	if err != nil {
		return err
	}
	for _, user := range users {
		if user.Email == email && user.ID != currentID {
			return fmt.Errorf("user email %s already exists", email)
		}
	}
	return nil
}

func (s *Service) ensureUniqueRoleBinding(ctx context.Context, next RoleBinding) error {
	bindings, err := s.repo.ListRoleBindings(ctx)
	if err != nil {
		return err
	}
	for _, binding := range bindings {
		if binding.UserID == next.UserID && binding.Role == next.Role && binding.ScopeType == next.ScopeType && binding.ScopeID == next.ScopeID {
			return errors.New("role binding already exists")
		}
	}
	return nil
}
