package cap

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dujiao-next/internal/modules/captcha/contract"
	settingssecurity "github.com/dujiao-next/internal/modules/settings/schema/security"
)

func TestClientPostsSiteverifyJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method got %s", r.Method)
		}
		if r.URL.Path != "/site-key/siteverify" {
			t.Errorf("path got %s", r.URL.Path)
		}
		if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
			t.Errorf("content type got %q", contentType)
		}

		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if payload["secret"] != "secret-1" || payload["response"] != "token-1" {
			t.Errorf("unexpected payload: %#v", payload)
		}
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	t.Cleanup(server.Close)

	err := New().Verify(settingssecurity.CaptchaCapSetting{
		Endpoint:  server.URL,
		SiteKey:   "site-key",
		SecretKey: "secret-1",
		TimeoutMS: 2000,
	}, "token-1", "127.0.0.1")
	if err != nil {
		t.Fatalf("verify failed: %v", err)
	}
}

func TestClientMapsRejectedToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success":false}`))
	}))
	t.Cleanup(server.Close)

	err := New().Verify(settingssecurity.CaptchaCapSetting{
		Endpoint:  server.URL,
		SiteKey:   "site-key",
		SecretKey: "secret-1",
		TimeoutMS: 2000,
	}, "bad-token", "")
	if !errors.Is(err, contract.ErrInvalid) {
		t.Fatalf("rejected token error got %v", err)
	}
}

func TestClientMapsHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)

	err := New().Verify(settingssecurity.CaptchaCapSetting{
		Endpoint:  server.URL,
		SiteKey:   "site-key",
		SecretKey: "secret-1",
		TimeoutMS: 2000,
	}, "token-1", "")
	if !errors.Is(err, contract.ErrVerifyFailed) {
		t.Fatalf("HTTP failure error got %v", err)
	}
}

func TestClientRejectsInvalidConfig(t *testing.T) {
	err := New().Verify(settingssecurity.CaptchaCapSetting{
		Endpoint:  "not-a-url",
		SiteKey:   "site-key",
		SecretKey: "secret-1",
	}, "token-1", "")
	if !errors.Is(err, contract.ErrConfigInvalid) {
		t.Fatalf("invalid config error got %v", err)
	}
}
