package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/dujiao-next/internal/constants"
	externalidentitydomain "github.com/dujiao-next/internal/modules/identity/externalidentity/domain"
	oidcauthapp "github.com/dujiao-next/internal/modules/identity/oidcauth/application"
	userdomain "github.com/dujiao-next/internal/modules/identity/user/domain"
	settingsapp "github.com/dujiao-next/internal/modules/settings/application"

	"golang.org/x/crypto/bcrypt"
)

// StartOIDCInput 启动通用 OIDC 流程输入。
type StartOIDCInput struct {
	Intent  string
	UserID  uint
	Context context.Context
}

// LoginWithOIDCInput 通用 OIDC 登录输入。
type LoginWithOIDCInput struct {
	Code    string
	State   string
	Context context.Context
}

// BindOIDCInput 通用 OIDC 绑定输入。
type BindOIDCInput struct {
	UserID  uint
	Code    string
	State   string
	Context context.Context
}

// StartOIDC 生成通用 OIDC 授权 URL。
func (s *Service) StartOIDC(input StartOIDCInput) (string, error) {
	if s == nil || s.oidcAuthService == nil {
		return "", oidcauthapp.ErrOIDCAuthConfigInvalid
	}
	ctx := input.Context
	if ctx == nil {
		ctx = context.Background()
	}
	if input.Intent != oidcauthapp.IntentLogin && input.Intent != oidcauthapp.IntentBind {
		return "", oidcauthapp.ErrOIDCPayloadInvalid
	}
	if input.Intent == oidcauthapp.IntentBind && input.UserID == 0 {
		return "", ErrNotFound
	}
	return s.oidcAuthService.StartOIDCLogin(ctx, input.Intent, input.UserID)
}

// LoginWithOIDC 通过通用 OIDC 回调登录。
func (s *Service) LoginWithOIDC(input LoginWithOIDCInput) (*UserLoginResult, error) {
	if s == nil || s.oidcAuthService == nil {
		return nil, oidcauthapp.ErrOIDCAuthConfigInvalid
	}
	ctx := input.Context
	if ctx == nil {
		ctx = context.Background()
	}
	verified, intent, _, err := s.oidcAuthService.CompleteOIDCLogin(ctx, input.Code, input.State)
	if err != nil {
		return nil, err
	}
	if intent != oidcauthapp.IntentLogin {
		return nil, oidcauthapp.ErrOIDCPayloadInvalid
	}
	return s.loginVerifiedOIDC(ctx, verified)
}

// BindOIDC 通过通用 OIDC 回调绑定当前用户。
func (s *Service) GetOIDCBinding(userID uint) (*externalidentitydomain.Identity, error) {
	if userID == 0 {
		return nil, ErrNotFound
	}
	if s == nil || s.cfg == nil || s.oidcAuthService == nil || s.userOAuthIdentityRepo == nil {
		return nil, oidcauthapp.ErrOIDCAuthConfigInvalid
	}
	return s.userOAuthIdentityRepo.GetByUserProvider(userID, oidcauthapp.ProviderKey(s.cfg.OIDCAuth.IssuerURL))
}

func (s *Service) BindOIDC(input BindOIDCInput) (*externalidentitydomain.Identity, error) {
	if input.UserID == 0 {
		return nil, ErrNotFound
	}
	if s == nil || s.oidcAuthService == nil || s.userOAuthIdentityRepo == nil || s.authUnitOfWork == nil {
		return nil, oidcauthapp.ErrOIDCAuthConfigInvalid
	}
	ctx := input.Context
	if ctx == nil {
		ctx = context.Background()
	}
	verified, intent, userID, err := s.oidcAuthService.CompleteOIDCLogin(ctx, input.Code, input.State)
	if err != nil {
		return nil, err
	}
	if intent != oidcauthapp.IntentBind || userID != input.UserID {
		return nil, oidcauthapp.ErrOIDCPayloadInvalid
	}
	return s.bindVerifiedOIDC(ctx, input.UserID, verified)
}

func (s *Service) loginVerifiedOIDC(ctx context.Context, verified *oidcauthapp.IdentityVerified) (*UserLoginResult, error) {
	verified, err := normalizeVerifiedOIDC(verified)
	if err != nil {
		return nil, err
	}
	if s.userOAuthIdentityRepo == nil || s.authUnitOfWork == nil {
		return nil, oidcauthapp.ErrOIDCAuthConfigInvalid
	}
	if ctx == nil {
		ctx = context.Background()
	}

	provider := verified.Provider
	identity, err := s.userOAuthIdentityRepo.GetByProviderUserID(provider, verified.ProviderUserID)
	if err != nil {
		return nil, err
	}
	if identity != nil {
		user, err := s.getActiveUserByID(identity.UserID)
		if err != nil {
			return nil, err
		}
		if applyOIDCIdentity(verified, identity) {
			identity.UpdatedAt = time.Now()
			if err := s.userOAuthIdentityRepo.Update(identity); err != nil {
				return nil, err
			}
		}
		return s.completeExternalLogin(user, constants.LoginLogSourceOIDC)
	}

	var resolved *userdomain.User
	var resolvedIdentity *externalidentitydomain.Identity
	createdUser := false
	registrationEnabled, domainPolicy, err := s.loadOIDCRegistrationSnapshot(verified.Email != "")
	if err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		createdUser = false
		err = s.authUnitOfWork.WithinTransaction(ctx, func(tx AuthTransaction) error {
			mapped, txErr := tx.GetIdentityByProviderUserID(provider, verified.ProviderUserID)
			if txErr != nil {
				return txErr
			}
			if mapped != nil {
				user, userErr := activeTransactionUser(tx, mapped.UserID)
				if userErr != nil {
					return userErr
				}
				resolved = user
				resolvedIdentity = mapped
				if applyOIDCIdentity(verified, mapped) {
					mapped.UpdatedAt = time.Now()
					return tx.UpdateIdentity(mapped)
				}
				return nil
			}

			email := verified.Email
			var user *userdomain.User
			if email != "" {
				user, txErr = tx.GetUserByEmail(email)
				if txErr != nil {
					return txErr
				}
			}
			if user != nil {
				if !verified.EmailVerified {
					return ErrOIDCAutoLinkForbidden
				}
				if strings.ToLower(strings.TrimSpace(user.Status)) != constants.UserStatusActive {
					return ErrUserDisabled
				}
				now := time.Now()
				if user.EmailVerifiedAt == nil {
					user.EmailVerifiedAt = &now
					user.UpdatedAt = now
					if txErr = tx.UpdateUser(user); txErr != nil {
						return txErr
					}
				}
			} else {
				if !registrationEnabled {
					return ErrRegistrationDisabled
				}
				if email != "" {
					if txErr = settingsapp.CheckRegistrationEmailDomainAllowed(email, domainPolicy); txErr != nil {
						return txErr
					}
				}
				user, txErr = newOIDCUser(verified)
				if txErr != nil {
					return txErr
				}
				if txErr = tx.CreateUser(user); txErr != nil {
					return txErr
				}
				createdUser = true
			}

			current, txErr := tx.GetIdentityByUserProvider(user.ID, provider)
			if txErr != nil {
				return txErr
			}
			if current != nil && current.ProviderUserID != verified.ProviderUserID {
				return ErrUserOAuthAlreadyBound
			}
			now := time.Now()
			if current == nil {
				current = &externalidentitydomain.Identity{
					UserID:         user.ID,
					Provider:       provider,
					ProviderUserID: verified.ProviderUserID,
					Username:       resolvedOIDCUsername(verified),
					AvatarURL:      verified.AvatarURL,
					AuthAt:         &verified.AuthAt,
					CreatedAt:      now,
					UpdatedAt:      now,
				}
				if txErr = tx.CreateIdentity(current); txErr != nil {
					return txErr
				}
			} else if applyOIDCIdentity(verified, current) {
				current.UpdatedAt = now
				if txErr = tx.UpdateIdentity(current); txErr != nil {
					return txErr
				}
			}
			resolved = user
			resolvedIdentity = current
			return nil
		})
		if err == nil {
			break
		}
		if attempt == 0 {
			occupied, lookupErr := s.userOAuthIdentityRepo.GetByProviderUserID(provider, verified.ProviderUserID)
			if lookupErr == nil && occupied != nil {
				continue
			}
		}
		return nil, err
	}
	if resolved == nil || resolvedIdentity == nil {
		return nil, oidcauthapp.ErrOIDCAuthConfigInvalid
	}
	if resolvedIdentity.AuthAt == nil {
		now := time.Now()
		resolvedIdentity.AuthAt = &now
	}
	if createdUser && s.memberLevelSvc != nil {
		_ = s.memberLevelSvc.AssignDefaultLevel(resolved.ID)
		if refreshed, refreshErr := s.userRepo.GetByID(resolved.ID); refreshErr == nil && refreshed != nil {
			resolved.MemberLevelID = refreshed.MemberLevelID
		}
	}
	return s.completeExternalLogin(resolved, constants.LoginLogSourceOIDC)
}

func (s *Service) bindVerifiedOIDC(ctx context.Context, userID uint, verified *oidcauthapp.IdentityVerified) (*externalidentitydomain.Identity, error) {
	verified, err := normalizeVerifiedOIDC(verified)
	if err != nil {
		return nil, err
	}
	var current *externalidentitydomain.Identity
	err = s.authUnitOfWork.WithinTransaction(ctx, func(tx AuthTransaction) error {
		user, txErr := activeTransactionUser(tx, userID)
		if txErr != nil {
			return txErr
		}
		occupied, txErr := tx.GetIdentityByProviderUserID(verified.Provider, verified.ProviderUserID)
		if txErr != nil {
			return txErr
		}
		if occupied != nil && occupied.UserID != user.ID {
			return ErrUserOAuthIdentityExists
		}
		current, txErr = tx.GetIdentityByUserProvider(user.ID, verified.Provider)
		if txErr != nil {
			return txErr
		}
		if current != nil && current.ProviderUserID != verified.ProviderUserID {
			return ErrUserOAuthAlreadyBound
		}
		now := time.Now()
		if current == nil {
			current = &externalidentitydomain.Identity{
				UserID:         user.ID,
				Provider:       verified.Provider,
				ProviderUserID: verified.ProviderUserID,
				Username:       resolvedOIDCUsername(verified),
				AvatarURL:      verified.AvatarURL,
				AuthAt:         &verified.AuthAt,
				CreatedAt:      now,
				UpdatedAt:      now,
			}
			return tx.CreateIdentity(current)
		}
		if applyOIDCIdentity(verified, current) {
			current.UpdatedAt = now
			return tx.UpdateIdentity(current)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return current, nil
}

func normalizeVerifiedOIDC(verified *oidcauthapp.IdentityVerified) (*oidcauthapp.IdentityVerified, error) {
	if verified == nil || strings.TrimSpace(verified.Provider) == "" || strings.TrimSpace(verified.ProviderUserID) == "" {
		return nil, oidcauthapp.ErrOIDCIDTokenInvalid
	}
	copy := *verified
	copy.Provider = strings.TrimSpace(copy.Provider)
	copy.ProviderUserID = strings.TrimSpace(copy.ProviderUserID)
	if len(copy.ProviderUserID) > 255 {
		return nil, oidcauthapp.ErrOIDCIDTokenInvalid
	}
	copy.Email = strings.ToLower(strings.TrimSpace(copy.Email))
	if copy.AuthAt.IsZero() {
		copy.AuthAt = time.Now()
	}
	return &copy, nil
}

func newOIDCUser(verified *oidcauthapp.IdentityVerified) (*userdomain.User, error) {
	email := strings.TrimSpace(verified.Email)
	if email == "" {
		email = buildOIDCPlaceholderEmail(verified.Provider, verified.ProviderUserID)
	}
	suffix, err := randomNumericCode(16)
	if err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("oidc_"+verified.ProviderUserID+"_"+suffix), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	var verifiedAt *time.Time
	if verified.Email != "" && verified.EmailVerified {
		verifiedAt = &now
	}
	displayName := strings.TrimSpace(verified.DisplayName)
	if displayName == "" {
		displayName = resolveNicknameFromEmail(email)
	}
	return &userdomain.User{
		Email:                 email,
		PasswordHash:          string(hash),
		PasswordSetupRequired: true,
		DisplayName:           displayName,
		Status:                constants.UserStatusActive,
		EmailVerifiedAt:       verifiedAt,
		LastLoginAt:           &now,
		CreatedAt:             now,
		UpdatedAt:             now,
	}, nil
}

func buildOIDCPlaceholderEmail(provider, subject string) string {
	sum := sha256.Sum256([]byte(provider + ":" + subject))
	return "oidc-" + hex.EncodeToString(sum[:])[:32] + "@invalid.local"
}

func resolvedOIDCUsername(verified *oidcauthapp.IdentityVerified) string {
	if value := strings.TrimSpace(verified.Username); value != "" {
		return value
	}
	if value := strings.TrimSpace(verified.Email); value != "" {
		return value
	}
	return verified.ProviderUserID
}

func applyOIDCIdentity(verified *oidcauthapp.IdentityVerified, identity *externalidentitydomain.Identity) bool {
	if verified == nil || identity == nil {
		return false
	}
	changed := false
	username := resolvedOIDCUsername(verified)
	if identity.Username != username {
		identity.Username = username
		changed = true
	}
	if identity.AvatarURL != verified.AvatarURL {
		identity.AvatarURL = verified.AvatarURL
		changed = true
	}
	if identity.AuthAt == nil || verified.AuthAt.After(*identity.AuthAt) {
		authAt := verified.AuthAt
		identity.AuthAt = &authAt
		changed = true
	}
	return changed
}

func (s *Service) loadOIDCRegistrationSnapshot(withEmail bool) (bool, settingsapp.RegistrationEmailDomainPolicy, error) {
	if s.settingService == nil {
		return true, settingsapp.RegistrationEmailDomainPolicy{}, nil
	}
	enabled, err := s.settingService.GetRegistrationEnabled(true)
	if err != nil {
		return false, settingsapp.RegistrationEmailDomainPolicy{}, err
	}
	if !withEmail {
		return enabled, settingsapp.RegistrationEmailDomainPolicy{}, nil
	}
	policy, err := s.settingService.GetRegistrationEmailDomainPolicy()
	if err != nil {
		return false, settingsapp.RegistrationEmailDomainPolicy{}, err
	}
	return enabled, policy, nil
}
