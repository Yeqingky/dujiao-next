package settingssecurity

import (
	"errors"
	"testing"

	"github.com/dujiao-next/internal/config"
	"github.com/dujiao-next/internal/constants"
)

func TestPublicCapSettingOmitsSecret(t *testing.T) {
	setting := CaptchaSetting{
		Provider: constants.CaptchaProviderCap,
		Scenes:   CaptchaSceneSetting{Login: true},
		Cap: CaptchaCapSetting{
			Endpoint:  "https://cap.example.com/",
			SiteKey:   "site-key",
			SecretKey: "site-secret",
			TimeoutMS: 2000,
		},
	}

	public := PublicCaptchaSetting(setting)
	capConfig, ok := public["cap"].(map[string]interface{})
	if !ok {
		t.Fatalf("public cap config missing: %#v", public)
	}
	if capConfig["endpoint"] != "https://cap.example.com" || capConfig["site_key"] != "site-key" {
		t.Fatalf("unexpected public cap config: %#v", capConfig)
	}
	if _, exists := capConfig["secret_key"]; exists {
		t.Fatal("public cap config must not contain secret_key")
	}
}

func TestCapSettingValidation(t *testing.T) {
	valid := CaptchaSetting{
		Provider: constants.CaptchaProviderCap,
		Cap: CaptchaCapSetting{
			Endpoint:  "https://cap.example.com",
			SiteKey:   "site-key",
			SecretKey: "site-secret",
			TimeoutMS: 2000,
		},
	}
	if err := ValidateCaptchaSetting(valid); err != nil {
		t.Fatalf("valid Cap setting rejected: %v", err)
	}

	invalid := valid
	invalid.Cap.Endpoint = "ftp://cap.example.com"
	if err := ValidateCaptchaSetting(invalid); !errors.Is(err, ErrCaptchaConfigInvalid) {
		t.Fatalf("invalid Cap endpoint error got %v", err)
	}
}

func TestCapSettingPatchKeepsExistingSecretWhenBlank(t *testing.T) {
	current := DefaultCaptchaSetting(config.CaptchaConfig{
		Provider: constants.CaptchaProviderCap,
		Cap: config.CaptchaCapConfig{
			Endpoint:  "https://cap.example.com",
			SiteKey:   "site-key",
			SecretKey: "existing-secret",
			TimeoutMS: 2000,
		},
	})
	blank := "   "
	next, err := ApplyCaptchaSettingPatch(current, CaptchaSettingPatch{
		Provider: &[]string{constants.CaptchaProviderCap}[0],
		Cap:      &CaptchaCapPatch{SecretKey: &blank},
	})
	if err != nil {
		t.Fatalf("patch failed: %v", err)
	}
	if next.Cap.SecretKey != "existing-secret" {
		t.Fatalf("secret was not preserved: %q", next.Cap.SecretKey)
	}
}
