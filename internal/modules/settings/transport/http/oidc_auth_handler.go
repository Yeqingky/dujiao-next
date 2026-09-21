package settingshttp

import (
	"errors"

	"github.com/gin-gonic/gin"

	"github.com/dujiao-next/internal/cache"
	settingssecurity "github.com/dujiao-next/internal/modules/settings/schema/security"
	ginutil "github.com/dujiao-next/internal/platform/http/ginutil"
	"github.com/dujiao-next/internal/platform/http/response"
)

// OIDCAuthAdminService 是后台通用 OIDC 登录设置端口。
type OIDCAuthAdminService interface {
	GetOIDCAuthSetting() (settingssecurity.OIDCAuthSetting, error)
	PatchOIDCAuthSetting(patch settingssecurity.OIDCAuthSettingPatch) (settingssecurity.OIDCAuthSetting, error)
	ApplyRuntime(setting settingssecurity.OIDCAuthSetting)
}

// OIDCAuthHandler 处理后台通用 OIDC 登录设置请求。
type OIDCAuthHandler struct {
	oidcAuth OIDCAuthAdminService
}

func NewOIDCAuthHandler(oidcAuth OIDCAuthAdminService) *OIDCAuthHandler {
	if oidcAuth == nil {
		panic("settings oidc auth handler: oidcAuth is nil")
	}
	return &OIDCAuthHandler{oidcAuth: oidcAuth}
}

// GetOIDCAuth 获取通用 OIDC 配置。
func (h *OIDCAuthHandler) GetOIDCAuth(c *gin.Context) {
	setting, err := h.oidcAuth.GetOIDCAuthSetting()
	if err != nil {
		ginutil.RespondError(c, response.CodeInternal, "error.settings_fetch_failed", err)
		return
	}
	response.Success(c, settingssecurity.MaskOIDCAuthSettingForAdmin(setting))
}

// UpdateOIDCAuth 更新通用 OIDC 配置并热更新运行时服务。
func (h *OIDCAuthHandler) UpdateOIDCAuth(c *gin.Context) {
	var req settingssecurity.OIDCAuthSettingPatch
	if err := c.ShouldBindJSON(&req); err != nil {
		ginutil.RespondBindError(c, err)
		return
	}
	setting, err := h.oidcAuth.PatchOIDCAuthSetting(req)
	if err != nil {
		if errors.Is(err, settingssecurity.ErrOIDCAuthConfigInvalid) {
			ginutil.RespondErrorWithMsg(c, response.CodeBadRequest, err.Error(), nil)
			return
		}
		ginutil.RespondError(c, response.CodeInternal, "error.settings_save_failed", err)
		return
	}
	h.oidcAuth.ApplyRuntime(setting)
	_ = cache.DelAllPublicConfig(c.Request.Context())
	response.Success(c, settingssecurity.MaskOIDCAuthSettingForAdmin(setting))
}
