package oidcauthapp

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dujiao-next/internal/config"
	"github.com/golang-jwt/jwt/v5"
)

type oidcTestFixture struct {
	server       *httptest.Server
	key          *rsa.PrivateKey
	clientID     string
	clientSecret string
	mu           sync.Mutex
	nonce        string
	lastForm     url.Values
	lastAuth     string
}

func newOIDCTestFixture(t *testing.T) *oidcTestFixture {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate test key: %v", err)
	}
	fixture := &oidcTestFixture{key: key, clientID: "oidc-client", clientSecret: "oidc-secret"}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		base := fixture.server.URL
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"issuer":                                base,
			"authorization_endpoint":                base + "/authorize",
			"token_endpoint":                        base + "/token",
			"jwks_uri":                              base + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		fixture.mu.Lock()
		fixture.nonce = r.URL.Query().Get("nonce")
		fixture.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		fixture.mu.Lock()
		fixture.lastForm = r.Form
		fixture.lastAuth = r.Header.Get("Authorization")
		nonce := fixture.nonce
		fixture.mu.Unlock()
		claims := jwt.MapClaims{
			"iss":                fixture.server.URL,
			"sub":                "subject-1",
			"aud":                fixture.clientID,
			"nonce":              nonce,
			"email":              "alice@example.com",
			"email_verified":     true,
			"preferred_username": "alice@example.com",
			"name":               "Alice Example",
			"iat":                time.Now().Unix(),
			"exp":                time.Now().Add(5 * time.Minute).Unix(),
		}
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		token.Header["kid"] = "test-key"
		raw, err := token.SignedString(fixture.key)
		if err != nil {
			t.Fatalf("sign test token: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id_token": raw})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		modulus := base64.RawURLEncoding.EncodeToString(fixture.key.PublicKey.N.Bytes())
		exponent := base64.RawURLEncoding.EncodeToString([]byte{1, 0, 1})
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"keys": []map[string]string{{
			"kty": "RSA", "kid": "test-key", "use": "sig", "alg": "RS256", "n": modulus, "e": exponent,
		}}})
	})
	fixture.server = httptest.NewServer(mux)
	t.Cleanup(fixture.server.Close)
	return fixture
}

func newTestOIDCService(fixture *oidcTestFixture, cfg config.OIDCAuthConfig) (*Service, map[string]string) {
	states := make(map[string]string)
	service := NewService(cfg,
		WithOIDCStateStore(
			func(_ context.Context, key, value string, _ int) (bool, error) {
				states[key] = value
				return true, nil
			},
			func(_ context.Context, key string) (string, bool, error) {
				value, ok := states[key]
				if ok {
					delete(states, key)
				}
				return value, ok, nil
			},
		),
	)
	return service, states
}

func TestOIDCLoginUsesPKCEAndVerifiesIDToken(t *testing.T) {
	fixture := newOIDCTestFixture(t)
	service, _ := newTestOIDCService(fixture, config.OIDCAuthConfig{
		Enabled:          true,
		IssuerURL:        fixture.server.URL,
		ClientID:         fixture.clientID,
		RedirectURI:      fixture.server.URL + "/callback",
		ClientAuthMethod: ClientAuthMethodNone,
		UsePKCE:          true,
		Scopes:           "openid profile email",
	})

	authURL, err := service.StartOIDCLogin(context.Background(), IntentLogin, 0)
	if err != nil {
		t.Fatalf("start OIDC: %v", err)
	}
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse auth URL: %v", err)
	}
	query := parsed.Query()
	if query.Get("code_challenge_method") != "S256" || query.Get("code_challenge") == "" {
		t.Fatalf("missing PKCE parameters: %v", query)
	}
	if query.Get("nonce") == "" || query.Get("state") == "" {
		t.Fatalf("missing state/nonce: %v", query)
	}
	fixture.mu.Lock()
	fixture.nonce = query.Get("nonce")
	fixture.mu.Unlock()

	verified, intent, userID, err := service.CompleteOIDCLogin(context.Background(), "authorization-code", query.Get("state"))
	if err != nil {
		t.Fatalf("complete OIDC: %v", err)
	}
	if intent != IntentLogin || userID != 0 {
		t.Fatalf("intent/user id = %q/%d", intent, userID)
	}
	if verified.Provider != ProviderKey(fixture.server.URL) || verified.ProviderUserID != "subject-1" {
		t.Fatalf("identity = %+v", verified)
	}
	if verified.Email != "alice@example.com" || !verified.EmailVerified {
		t.Fatalf("email identity = %+v", verified)
	}
	fixture.mu.Lock()
	form := fixture.lastForm
	fixture.mu.Unlock()
	if form.Get("code_verifier") == "" {
		t.Fatal("token request did not include code_verifier")
	}
	if _, _, _, err := service.CompleteOIDCLogin(context.Background(), "authorization-code", query.Get("state")); err != ErrOIDCStateInvalid {
		t.Fatalf("replayed state error = %v", err)
	}
}

func TestOIDCLoginSupportsClientSecretBasicWithoutPKCE(t *testing.T) {
	fixture := newOIDCTestFixture(t)
	service, _ := newTestOIDCService(fixture, config.OIDCAuthConfig{
		Enabled:          true,
		IssuerURL:        fixture.server.URL,
		ClientID:         fixture.clientID,
		ClientSecret:     fixture.clientSecret,
		RedirectURI:      fixture.server.URL + "/callback",
		ClientAuthMethod: ClientAuthMethodBasic,
		UsePKCE:          false,
		Scopes:           "openid profile email",
	})
	authURL, err := service.StartOIDCLogin(context.Background(), IntentLogin, 0)
	if err != nil {
		t.Fatalf("start OIDC: %v", err)
	}
	query, _ := url.Parse(authURL)
	fixture.mu.Lock()
	fixture.nonce = query.Query().Get("nonce")
	fixture.mu.Unlock()
	if _, _, _, err := service.CompleteOIDCLogin(context.Background(), "authorization-code", query.Query().Get("state")); err != nil {
		t.Fatalf("complete OIDC: %v", err)
	}
	fixture.mu.Lock()
	authHeader := fixture.lastAuth
	form := fixture.lastForm
	fixture.mu.Unlock()
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte(fixture.clientID+":"+fixture.clientSecret))
	if authHeader != want {
		t.Fatalf("authorization header = %q, want %q", authHeader, want)
	}
	if form.Get("code_verifier") != "" {
		t.Fatal("unexpected code_verifier when PKCE is disabled")
	}
}

func TestProviderKeyIsStableAndBounded(t *testing.T) {
	key := ProviderKey("https://login.microsoftonline.com/tenant/v2.0")
	if key != ProviderKey("https://login.microsoftonline.com/tenant/v2.0") {
		t.Fatal("provider key is not stable")
	}
	if !strings.HasPrefix(key, "oidc-") || len(key) > 32 {
		t.Fatalf("provider key = %q", key)
	}
	if key == ProviderKey("https://login.microsoftonline.com/other/v2.0") {
		t.Fatal("different issuers share provider key")
	}
}
