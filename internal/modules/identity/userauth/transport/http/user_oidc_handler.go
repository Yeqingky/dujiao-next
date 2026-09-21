package userauthhttp

import (
	"errors"
	"time"

	"github.com/dujiao-next/internal/constants"
	externalidentitydomain "github.com/dujiao-next/internal/modules/identity/externalidentity/domain"
	oidcauthapp "github.com/dujiao-next/internal/modules/identity/oidcauth/application"
	userauthapp "github.com/dujiao-next/internal/modules/identity/userauth/application"
	userpresenter "github.com/dujiao-next/internal/modules/identity/userauth/transport/presenter"
	"github.com/dujiao-next/internal/platform/http/ginutil"
	"github.com/dujiao-next/internal/platform/http/response"

	"github.com/gin-gonic/gin"
)

// UserOIDCService 是通用 OIDC 端点所需的最小端口。
type UserOIDCService interface {
	GetOIDCBinding(userID uint) (*externalidentitydomain.Identity, error)
	StartOIDC(input userauthapp.StartOIDCInput) (string, error)
	LoginWithOIDC(input userauthapp.LoginWithOIDCInput) (*userauthapp.UserLoginResult, error)
	BindOIDC(input userauthapp.BindOIDCInput) (*externalidentitydomain.Identity, error)
}

// UserOIDCHandler 处理通用 OIDC 登录与绑定请求。
type UserOIDCHandler struct {
	service  UserOIDCService
	recorder LoginRecorder
}

func NewUserOIDCHandler(service UserOIDCService, recorder LoginRecorder) *UserOIDCHandler {
	if service == nil {
		panic("user oidc handler: service is nil")
	}
	return &UserOIDCHandler{service: service, recorder: recorder}
}

type oidcCallbackRequest struct {
	Code  string `json:"code" binding:"required"`
	State string `json:"state" binding:"required"`
}

func respondOIDCError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, oidcauthapp.ErrOIDCAuthDisabled):
		ginutil.RespondError(c, response.CodeBadRequest, "error.oidc_auth_disabled", nil)
	case errors.Is(err, oidcauthapp.ErrOIDCAuthConfigInvalid), errors.Is(err, oidcauthapp.ErrOIDCDiscoveryFailed):
		ginutil.RespondError(c, response.CodeInternal, "error.oidc_auth_config_invalid", err)
	case errors.Is(err, oidcauthapp.ErrOIDCStateInvalid):
		ginutil.RespondError(c, response.CodeBadRequest, "error.oidc_state_invalid", nil)
	case errors.Is(err, oidcauthapp.ErrOIDCTokenExchange):
		ginutil.RespondError(c, response.CodeBadRequest, "error.oidc_token_exchange_failed", err)
	case errors.Is(err, oidcauthapp.ErrOIDCIDTokenInvalid):
		ginutil.RespondError(c, response.CodeBadRequest, "error.oidc_id_token_invalid", nil)
	case errors.Is(err, oidcauthapp.ErrOIDCPayloadInvalid):
		ginutil.RespondError(c, response.CodeBadRequest, "error.oidc_payload_invalid", nil)
	case errors.Is(err, ErrUserOAuthIdentityExists):
		ginutil.RespondError(c, response.CodeBadRequest, "error.oidc_bind_conflict", nil)
	case errors.Is(err, ErrUserOAuthAlreadyBound):
		ginutil.RespondError(c, response.CodeBadRequest, "error.oidc_already_bound", nil)
	case errors.Is(err, userauthapp.ErrOIDCAutoLinkForbidden):
		ginutil.RespondError(c, response.CodeBadRequest, "error.oidc_email_auto_link_forbidden", nil)
	case errors.Is(err, ErrUserDisabled):
		ginutil.RespondError(c, response.CodeUnauthorized, "error.user_disabled", nil)
	case errors.Is(err, ErrRegistrationDisabled):
		ginutil.RespondError(c, response.CodeForbidden, "error.registration_disabled", nil)
	default:
		ginutil.RespondError(c, response.CodeInternal, "error.login_failed", err)
	}
}

func (h *UserOIDCHandler) recordLogin(c *gin.Context, email string, userID uint, status, failReason string) {
	if h == nil || h.recorder == nil || c == nil {
		return
	}
	requestID := ""
	if value, ok := c.Get("request_id"); ok {
		requestID, _ = value.(string)
	}
	h.recorder.Record(email, userID, status, failReason, constants.LoginLogSourceOIDC, c.ClientIP(), c.GetHeader("User-Agent"), requestID)
}

// GetMyOIDCBinding 返回当前用户的通用 OIDC 绑定状态。
func (h *UserOIDCHandler) GetMyOIDCBinding(c *gin.Context) {
	uid, ok := ginutil.GetUserID(c)
	if !ok {
		return
	}
	identity, err := h.service.GetOIDCBinding(uid)
	if err != nil {
		respondOIDCError(c, err)
		return
	}
	response.Success(c, userpresenter.NewOIDCBindingResp(identity))
}

// StartOIDCLogin 返回通用 OIDC 登录授权 URL。
func (h *UserOIDCHandler) StartOIDCLogin(c *gin.Context) {
	authURL, err := h.service.StartOIDC(userauthapp.StartOIDCInput{Intent: oidcauthapp.IntentLogin, Context: c.Request.Context()})
	if err != nil {
		respondOIDCError(c, err)
		return
	}
	response.Success(c, gin.H{"auth_url": authURL})
}

// OIDCLoginCallback 处理通用 OIDC 登录回调。
func (h *UserOIDCHandler) OIDCLoginCallback(c *gin.Context) {
	var req oidcCallbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.recordLogin(c, "", 0, constants.LoginLogStatusFailed, constants.LoginLogFailReasonBadRequest)
		ginutil.RespondError(c, response.CodeBadRequest, "error.bad_request", err)
		return
	}
	result, err := h.service.LoginWithOIDC(userauthapp.LoginWithOIDCInput{Code: req.Code, State: req.State, Context: c.Request.Context()})
	if err != nil {
		h.recordLogin(c, "", 0, constants.LoginLogStatusFailed, constants.LoginLogFailReasonOIDCInvalid)
		respondOIDCError(c, err)
		return
	}
	if result.RequiresTOTP {
		h.recordLogin(c, result.User.Email, result.User.ID, constants.LoginLogStatusSuccess, constants.LoginLogPasswordOK2FAPending)
		response.Success(c, gin.H{
			"requires_totp":        true,
			"challenge_token":      result.ChallengeToken,
			"challenge_expires_at": result.ChallengeExpiresAt.Format(time.RFC3339),
		})
		return
	}
	h.recordLogin(c, result.User.Email, result.User.ID, constants.LoginLogStatusSuccess, "")
	response.Success(c, gin.H{
		"requires_totp": false,
		"user":          userpresenter.NewUserAuthBriefResp(result.User),
		"token":         result.Token,
		"expires_at":    result.ExpiresAt.Format(time.RFC3339),
	})
}

// StartOIDCBind 返回登录态下的通用 OIDC 绑定授权 URL。
func (h *UserOIDCHandler) StartOIDCBind(c *gin.Context) {
	uid, ok := ginutil.GetUserID(c)
	if !ok {
		return
	}
	authURL, err := h.service.StartOIDC(userauthapp.StartOIDCInput{Intent: oidcauthapp.IntentBind, UserID: uid, Context: c.Request.Context()})
	if err != nil {
		respondOIDCError(c, err)
		return
	}
	response.Success(c, gin.H{"auth_url": authURL})
}

// OIDCBindCallback 处理登录态下的通用 OIDC 绑定回调。
func (h *UserOIDCHandler) OIDCBindCallback(c *gin.Context) {
	uid, ok := ginutil.GetUserID(c)
	if !ok {
		return
	}
	var req oidcCallbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ginutil.RespondBindError(c, err)
		return
	}
	identity, err := h.service.BindOIDC(userauthapp.BindOIDCInput{UserID: uid, Code: req.Code, State: req.State, Context: c.Request.Context()})
	if err != nil {
		respondOIDCError(c, err)
		return
	}
	response.Success(c, userpresenter.NewOIDCBindingResp(identity))
}
