package application

import (
	"testing"

	"github.com/dujiao-next/internal/config"
	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/modules/captcha/contract"
	settingssecurity "github.com/dujiao-next/internal/modules/settings/schema/security"
)

type settingReaderStub struct {
	setting settingssecurity.CaptchaSetting
}

func (s settingReaderStub) GetCaptchaSetting(config.CaptchaConfig) (settingssecurity.CaptchaSetting, error) {
	return s.setting, nil
}

type turnstileStub struct {
	cfg      settingssecurity.CaptchaTurnstileSetting
	token    string
	clientIP string
}

type capStub struct {
	cfg      settingssecurity.CaptchaCapSetting
	token    string
	clientIP string
}

func (s *turnstileStub) Verify(cfg settingssecurity.CaptchaTurnstileSetting, token, clientIP string) error {
	s.cfg = cfg
	s.token = token
	s.clientIP = clientIP
	return nil
}

func (s *capStub) Verify(cfg settingssecurity.CaptchaCapSetting, token, clientIP string) error {
	s.cfg = cfg
	s.token = token
	s.clientIP = clientIP
	return nil
}

func TestVerifyTurnstileDelegatesToVerifier(t *testing.T) {
	setting := settingssecurity.CaptchaSetting{
		Provider: constants.CaptchaProviderTurnstile,
		Scenes: settingssecurity.CaptchaSceneSetting{
			Login: true,
		},
		Turnstile: settingssecurity.CaptchaTurnstileSetting{
			SecretKey: "secret",
			VerifyURL: "https://captcha.example/verify",
			TimeoutMS: 2000,
		},
	}
	verifier := &turnstileStub{}
	service := NewService(settingReaderStub{setting: setting}, config.CaptchaConfig{}, verifier, nil)

	err := service.Verify(constants.CaptchaSceneLogin, contract.VerifyPayload{TurnstileToken: " token-1 "}, " 127.0.0.1 ")
	if err != nil {
		t.Fatalf("verify failed: %v", err)
	}
	if verifier.token != "token-1" || verifier.clientIP != "127.0.0.1" || verifier.cfg.SecretKey != "secret" {
		t.Fatalf("turnstile input mismatch: token=%q ip=%q cfg=%#v", verifier.token, verifier.clientIP, verifier.cfg)
	}
}

func TestVerifyCapDelegatesToVerifier(t *testing.T) {
	setting := settingssecurity.CaptchaSetting{
		Provider: constants.CaptchaProviderCap,
		Scenes:   settingssecurity.CaptchaSceneSetting{Login: true},
		Cap: settingssecurity.CaptchaCapSetting{
			Endpoint:  "https://cap.example",
			SiteKey:   "site-key",
			SecretKey: "secret",
			TimeoutMS: 2000,
		},
	}
	verifier := &capStub{}
	service := NewService(settingReaderStub{setting: setting}, config.CaptchaConfig{}, nil, verifier)

	if err := service.Verify(constants.CaptchaSceneLogin, contract.VerifyPayload{CapToken: " token-1 "}, " 127.0.0.1 "); err != nil {
		t.Fatalf("verify failed: %v", err)
	}
	if verifier.token != "token-1" || verifier.clientIP != "127.0.0.1" || verifier.cfg.SecretKey != "secret" {
		t.Fatalf("cap input mismatch: token=%q ip=%q cfg=%#v", verifier.token, verifier.clientIP, verifier.cfg)
	}
}

func TestVerifyImageRequiresChallengePayload(t *testing.T) {
	setting := settingssecurity.CaptchaSetting{
		Provider: constants.CaptchaProviderImage,
		Scenes: settingssecurity.CaptchaSceneSetting{
			Login: true,
		},
	}
	service := NewService(settingReaderStub{setting: setting}, config.CaptchaConfig{}, &turnstileStub{}, nil)
	if err := service.Verify(constants.CaptchaSceneLogin, contract.VerifyPayload{}, ""); err != contract.ErrRequired {
		t.Fatalf("empty image captcha error got %v want ErrRequired", err)
	}
}

func TestVerifySkipsDisabledScene(t *testing.T) {
	setting := settingssecurity.CaptchaSetting{Provider: constants.CaptchaProviderTurnstile}
	service := NewService(settingReaderStub{setting: setting}, config.CaptchaConfig{}, nil, nil)
	if err := service.Verify(constants.CaptchaSceneLogin, contract.VerifyPayload{}, ""); err != nil {
		t.Fatalf("disabled scene must skip captcha, got %v", err)
	}
}
