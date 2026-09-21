package settingssecurity

import (
	"errors"
	"testing"

	"github.com/dujiao-next/internal/config"
)

func TestOIDCAuthSettingNormalizesAndValidatesPKCE(t *testing.T) {
	setting := NormalizeOIDCAuthSetting(OIDCAuthSetting{
		Enabled:          true,
		ProviderName:     " Microsoft Entra ID ",
		IssuerURL:        "https://login.microsoftonline.com/tenant/v2.0/",
		ClientID:         " client-id ",
		RedirectURI:      "https://shop.example.com/auth/oidc/callback/",
		ClientAuthMethod: "none",
		UsePKCE:          true,
		Scopes:           "profile email",
	})
	if setting.ProviderName != "Microsoft Entra ID" || setting.IssuerURL != "https://login.microsoftonline.com/tenant/v2.0" {
		t.Fatalf("normalized setting = %#v", setting)
	}
	if setting.Scopes != "openid profile email" {
		t.Fatalf("scopes = %q", setting.Scopes)
	}
	if err := ValidateOIDCAuthSetting(setting); err != nil {
		t.Fatalf("valid PKCE setting rejected: %v", err)
	}
	if err := ValidateOIDCAuthSetting(OIDCAuthSetting{
		Enabled:          true,
		IssuerURL:        "https://login.microsoftonline.com/tenant/v2.0",
		ClientID:         "client-id",
		RedirectURI:      "https://shop.example.com/auth/oidc/callback",
		ClientAuthMethod: "none",
	}); !errors.Is(err, ErrOIDCAuthConfigInvalid) {
		t.Fatalf("none without PKCE error = %v", err)
	}
}

func TestOIDCAuthSettingSupportsBasicCompatibilityAndMasksSecret(t *testing.T) {
	setting := OIDCAuthSetting{
		Enabled:          true,
		IssuerURL:        "https://login.microsoftonline.com/tenant/v2.0",
		ClientID:         "client-id",
		ClientSecret:     "secret",
		RedirectURI:      "https://shop.example.com/auth/oidc/callback",
		ClientAuthMethod: "client_secret_basic",
		UsePKCE:          false,
	}
	if err := ValidateOIDCAuthSetting(setting); err != nil {
		t.Fatalf("basic compatibility setting rejected: %v", err)
	}
	masked := MaskOIDCAuthSettingForAdmin(setting)
	if masked["client_secret"] != nil || masked["has_client_secret"] != true {
		t.Fatalf("secret was not masked: %#v", masked)
	}
	cfg := OIDCAuthSettingToConfig(setting)
	if cfg.ClientSecret != "secret" || cfg.ClientAuthMethod != "client_secret_basic" {
		t.Fatalf("runtime config = %#v", cfg)
	}
}

func TestDefaultOIDCAuthSettingUsesConfiguredPKCEFallback(t *testing.T) {
	setting := DefaultOIDCAuthSetting(config.OIDCAuthConfig{
		ProviderName:     "Entra",
		ClientAuthMethod: "client_secret_basic",
		UsePKCE:          true,
		Scopes:           "openid profile email",
	})
	if setting.ProviderName != "Entra" || !setting.UsePKCE || setting.ClientAuthMethod != "client_secret_basic" {
		t.Fatalf("default setting = %#v", setting)
	}
}
