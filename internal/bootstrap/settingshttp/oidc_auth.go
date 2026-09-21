package settingsbootstrap

import (
	"github.com/dujiao-next/internal/config"
	oidcauthapp "github.com/dujiao-next/internal/modules/identity/oidcauth/application"
	settingsapp "github.com/dujiao-next/internal/modules/settings/application"
	settingssecurity "github.com/dujiao-next/internal/modules/settings/schema/security"
)

type oidcAuthRuntime interface {
	SetConfig(config.OIDCAuthConfig)
}

type settingsOIDCAuthAdapter struct {
	settings *settingsapp.Service
	cfg      *config.Config
	oidcAuth oidcAuthRuntime
}

func (a settingsOIDCAuthAdapter) GetOIDCAuthSetting() (settingssecurity.OIDCAuthSetting, error) {
	return a.settings.GetOIDCAuthSetting(a.cfg.OIDCAuth)
}

func (a settingsOIDCAuthAdapter) PatchOIDCAuthSetting(patch settingssecurity.OIDCAuthSettingPatch) (settingssecurity.OIDCAuthSetting, error) {
	return a.settings.PatchOIDCAuthSetting(a.cfg.OIDCAuth, patch)
}

func (a settingsOIDCAuthAdapter) ApplyRuntime(setting settingssecurity.OIDCAuthSetting) {
	runtimeCfg := settingssecurity.OIDCAuthSettingToConfig(setting)
	a.cfg.OIDCAuth = runtimeCfg
	if a.oidcAuth != nil {
		a.oidcAuth.SetConfig(runtimeCfg)
	}
}

var _ oidcAuthRuntime = (*oidcauthapp.Service)(nil)
