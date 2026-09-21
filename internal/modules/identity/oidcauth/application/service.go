package oidcauthapp

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	cryptorand "crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dujiao-next/internal/config"
	"github.com/golang-jwt/jwt/v5"
)

const (
	ClientAuthMethodNone  = "none"
	ClientAuthMethodBasic = "client_secret_basic"
	ClientAuthMethodPost  = "client_secret_post"

	defaultProviderName   = "OIDC"
	defaultScopes         = "openid profile email"
	defaultHTTPTimeout    = 10 * time.Second
	defaultMetadataTTL    = 10 * time.Minute
	defaultJWKSCacheTTL   = time.Hour
	unknownKidRefreshWait = 5 * time.Second
	stateTTLSeconds       = 600

	maxMetadataBytes      = 1 << 20
	maxJWKSBytes          = 1 << 20
	maxTokenResponseBytes = 1 << 20
	maxSubjectBytes       = 255
	maxPictureURLBytes    = 2048
)

const (
	intentLogin = "login"
	intentBind  = "bind"
	statePrefix = "oidc:state:"
)

const (
	IntentLogin = intentLogin
	IntentBind  = intentBind
)

// IdentityVerified is the normalized identity returned after ID token
// verification. The userauth module applies account and binding policy.
type IdentityVerified struct {
	Provider       string
	ProviderUserID string
	Username       string
	Email          string
	EmailVerified  bool
	DisplayName    string
	AvatarURL      string
	AuthAt         time.Time
}

type OIDCStateSetFunc func(ctx context.Context, key, value string, ttlSeconds int) (bool, error)
type OIDCStateTakeFunc func(ctx context.Context, key string) (string, bool, error)

type Option func(*Service)

// WithOIDCStateStore injects the single-use state store used by the browser
// redirect flow.
func WithOIDCStateStore(set OIDCStateSetFunc, take OIDCStateTakeFunc) Option {
	return func(service *Service) {
		if set != nil && take != nil {
			service.stateSet = set
			service.stateTake = take
		}
	}
}

// WithHTTPClient injects an HTTP client, primarily for tests.
func WithHTTPClient(client *http.Client) Option {
	return func(service *Service) {
		if client != nil {
			service.httpClient = client
		}
	}
}

type oidcState struct {
	CodeVerifier     string `json:"code_verifier,omitempty"`
	Nonce            string `json:"nonce"`
	Intent           string `json:"intent"`
	UserID           uint   `json:"user_id"`
	Issuer           string `json:"issuer"`
	ClientID         string `json:"client_id"`
	RedirectURI      string `json:"redirect_uri"`
	TokenEndpoint    string `json:"token_endpoint"`
	JWKSURI          string `json:"jwks_uri"`
	MetadataIssuer   string `json:"metadata_issuer"`
	ClientAuthMethod string `json:"client_auth_method"`
	UsePKCE          bool   `json:"use_pkce"`
}

type providerMetadata struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	JWKSURI               string   `json:"jwks_uri"`
	TokenAuthMethods      []string `json:"token_endpoint_auth_methods_supported"`
	SigningAlgorithms     []string `json:"id_token_signing_alg_values_supported"`
}

type tokenResponse struct {
	IDToken     string `json:"id_token"`
	Error       string `json:"error"`
	Description string `json:"error_description"`
}

type oidcJWK struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

type cachedVerificationKey struct {
	Key interface{}
	Alg string
}

type Service struct {
	configMu sync.RWMutex
	cfg      config.OIDCAuthConfig

	httpClient *http.Client
	stateSet   OIDCStateSetFunc
	stateTake  OIDCStateTakeFunc

	metadataMu    sync.Mutex
	metadata      *providerMetadata
	metadataURL   string
	metadataUntil time.Time

	keysMu            sync.RWMutex
	keys              map[string]cachedVerificationKey
	keysURL           string
	keysUntil         time.Time
	lastForcedRefresh time.Time
	refreshMu         sync.Mutex
}

// NewService creates the runtime OIDC verifier and authorization-code client.
func NewService(cfg config.OIDCAuthConfig, options ...Option) *Service {
	service := &Service{
		httpClient: &http.Client{Timeout: defaultHTTPTimeout},
		keys:       make(map[string]cachedVerificationKey),
	}
	service.SetConfig(cfg)
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

// SetConfig updates runtime configuration and invalidates discovery/JWKS data
// when the issuer or client changes.
func (s *Service) SetConfig(cfg config.OIDCAuthConfig) {
	if s == nil {
		return
	}
	cfg = normalizeConfig(cfg)
	s.configMu.Lock()
	changed := s.cfg.IssuerURL != cfg.IssuerURL || s.cfg.ClientID != cfg.ClientID
	s.cfg = cfg
	s.configMu.Unlock()
	if changed {
		s.clearProviderCache()
	}
}

func (s *Service) configSnapshot() config.OIDCAuthConfig {
	if s == nil {
		return config.OIDCAuthConfig{}
	}
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	return s.cfg
}

// PublicConfig returns only browser-safe configuration.
func (s *Service) PublicConfig() map[string]interface{} {
	cfg := s.configSnapshot()
	return map[string]interface{}{
		"enabled":       cfg.Enabled && strings.TrimSpace(cfg.IssuerURL) != "" && strings.TrimSpace(cfg.ClientID) != "" && strings.TrimSpace(cfg.RedirectURI) != "",
		"provider_name": strings.TrimSpace(cfg.ProviderName),
	}
}

// StartOIDCLogin creates a one-time authorization URL.
func (s *Service) StartOIDCLogin(ctx context.Context, intent string, userID uint) (string, error) {
	if s == nil {
		return "", ErrOIDCAuthConfigInvalid
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if intent != intentLogin && intent != intentBind {
		return "", ErrOIDCPayloadInvalid
	}
	if intent == intentBind && userID == 0 {
		return "", ErrOIDCPayloadInvalid
	}
	cfg := s.configSnapshot()
	if err := validateRuntimeConfig(cfg); err != nil {
		return "", err
	}
	metadata, err := s.loadMetadata(ctx, cfg.IssuerURL)
	if err != nil {
		return "", err
	}
	state, err := randomURLString(32)
	if err != nil {
		return "", err
	}
	nonce, err := randomURLString(32)
	if err != nil {
		return "", err
	}
	codeVerifier := ""
	codeChallenge := ""
	if cfg.UsePKCE {
		codeVerifier, codeChallenge, err = newPKCEPair()
		if err != nil {
			return "", err
		}
	}
	record, err := json.Marshal(oidcState{
		CodeVerifier:     codeVerifier,
		Nonce:            nonce,
		Intent:           intent,
		UserID:           userID,
		Issuer:           cfg.IssuerURL,
		ClientID:         cfg.ClientID,
		RedirectURI:      cfg.RedirectURI,
		TokenEndpoint:    metadata.TokenEndpoint,
		JWKSURI:          metadata.JWKSURI,
		MetadataIssuer:   metadata.Issuer,
		ClientAuthMethod: cfg.ClientAuthMethod,
		UsePKCE:          cfg.UsePKCE,
	})
	if err != nil || s.stateSet == nil {
		return "", ErrOIDCAuthConfigInvalid
	}
	stored, err := s.stateSet(ctx, statePrefix+state, string(record), stateTTLSeconds)
	if err != nil {
		return "", err
	}
	if !stored {
		return "", ErrOIDCStateInvalid
	}

	query := url.Values{}
	query.Set("client_id", cfg.ClientID)
	query.Set("redirect_uri", cfg.RedirectURI)
	query.Set("response_type", "code")
	query.Set("scope", normalizeScopes(cfg.Scopes))
	query.Set("state", state)
	query.Set("nonce", nonce)
	if cfg.UsePKCE {
		query.Set("code_challenge", codeChallenge)
		query.Set("code_challenge_method", "S256")
	}
	return metadata.AuthorizationEndpoint + "?" + query.Encode(), nil
}

// CompleteOIDCLogin exchanges a one-time code and verifies the returned ID
// token. State is consumed before exchange to prevent replay.
func (s *Service) CompleteOIDCLogin(ctx context.Context, code, state string) (*IdentityVerified, string, uint, error) {
	if s == nil {
		return nil, "", 0, ErrOIDCAuthConfigInvalid
	}
	if ctx == nil {
		ctx = context.Background()
	}
	code = strings.TrimSpace(code)
	state = strings.TrimSpace(state)
	if code == "" || state == "" || s.stateTake == nil {
		return nil, "", 0, ErrOIDCPayloadInvalid
	}
	cfg := s.configSnapshot()
	if err := validateRuntimeConfig(cfg); err != nil {
		return nil, "", 0, err
	}
	raw, ok, err := s.stateTake(ctx, statePrefix+state)
	if err != nil {
		return nil, "", 0, err
	}
	if !ok || raw == "" {
		return nil, "", 0, ErrOIDCStateInvalid
	}
	var record oidcState
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		return nil, "", 0, ErrOIDCStateInvalid
	}
	if (record.Intent != intentLogin && record.Intent != intentBind) || record.Nonce == "" || record.ClientID == "" || record.TokenEndpoint == "" || record.JWKSURI == "" || record.MetadataIssuer == "" {
		return nil, "", 0, ErrOIDCStateInvalid
	}
	if record.Issuer != cfg.IssuerURL || record.ClientID != cfg.ClientID || record.RedirectURI != cfg.RedirectURI || record.ClientAuthMethod != cfg.ClientAuthMethod || record.UsePKCE != cfg.UsePKCE {
		return nil, "", 0, ErrOIDCStateInvalid
	}
	if record.Intent == intentBind && record.UserID == 0 {
		return nil, "", 0, ErrOIDCStateInvalid
	}

	token, err := s.exchangeCode(ctx, cfg, record, code)
	if err != nil {
		return nil, "", 0, err
	}
	verified, err := s.verifyIDToken(ctx, token.IDToken, record)
	if err != nil {
		return nil, "", 0, err
	}
	return verified, record.Intent, record.UserID, nil
}

func (s *Service) exchangeCode(ctx context.Context, cfg config.OIDCAuthConfig, record oidcState, code string) (*tokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", record.RedirectURI)
	form.Set("client_id", record.ClientID)
	if record.UsePKCE {
		if record.CodeVerifier == "" {
			return nil, ErrOIDCStateInvalid
		}
		form.Set("code_verifier", record.CodeVerifier)
	}
	if record.ClientAuthMethod == ClientAuthMethodBasic {
		basic := base64.StdEncoding.EncodeToString([]byte(url.QueryEscape(record.ClientID) + ":" + url.QueryEscape(cfg.ClientSecret)))
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, record.TokenEndpoint, strings.NewReader(form.Encode()))
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrOIDCTokenExchange, err)
		}
		req.Header.Set("Authorization", "Basic "+basic)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return s.doTokenRequest(req)
	}
	if record.ClientAuthMethod == ClientAuthMethodPost {
		form.Set("client_secret", cfg.ClientSecret)
	}
	if record.ClientAuthMethod != ClientAuthMethodNone && record.ClientAuthMethod != ClientAuthMethodPost {
		return nil, ErrOIDCAuthConfigInvalid
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, record.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOIDCTokenExchange, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return s.doTokenRequest(req)
}

func (s *Service) doTokenRequest(req *http.Request) (*tokenResponse, error) {
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOIDCTokenExchange, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxTokenResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOIDCTokenExchange, err)
	}
	var token tokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, fmt.Errorf("%w: invalid response", ErrOIDCTokenExchange)
	}
	if resp.StatusCode/100 != 2 || strings.TrimSpace(token.Error) != "" || strings.TrimSpace(token.IDToken) == "" {
		if token.Description != "" {
			return nil, fmt.Errorf("%w: %s", ErrOIDCTokenExchange, token.Description)
		}
		return nil, fmt.Errorf("%w: http %d", ErrOIDCTokenExchange, resp.StatusCode)
	}
	return &token, nil
}

func (s *Service) verifyIDToken(ctx context.Context, raw string, record oidcState) (*IdentityVerified, error) {
	raw = strings.TrimSpace(raw)
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return nil, ErrOIDCIDTokenInvalid
	}
	var header struct {
		Algorithm string `json:"alg"`
		KeyID     string `json:"kid"`
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || json.Unmarshal(headerBytes, &header) != nil || !supportedSigningAlgorithm(header.Algorithm) {
		return nil, ErrOIDCIDTokenInvalid
	}
	keyID := strings.TrimSpace(header.KeyID)
	if keyID == "" {
		keyID = "_default"
	}
	key, err := s.keyForID(ctx, record.JWKSURI, keyID, header.Algorithm)
	if err != nil {
		return nil, ErrOIDCIDTokenInvalid
	}
	claims := jwt.MapClaims{}
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{header.Algorithm}),
		jwt.WithIssuer(record.MetadataIssuer),
		jwt.WithAudience(record.ClientID),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(time.Minute),
	)
	token, err := parser.ParseWithClaims(raw, claims, func(*jwt.Token) (interface{}, error) {
		return key.Key, nil
	})
	if err != nil || token == nil || !token.Valid {
		return nil, ErrOIDCIDTokenInvalid
	}
	if !validateAuthorizedParty(claims, record.ClientID) || stringClaim(claims, "nonce") != record.Nonce {
		return nil, ErrOIDCIDTokenInvalid
	}
	return identityFromClaims(claims, record.MetadataIssuer)
}

func (s *Service) loadMetadata(ctx context.Context, issuer string) (*providerMetadata, error) {
	normalizedIssuer := normalizeIssuer(issuer)
	s.metadataMu.Lock()
	if s.metadata != nil && s.metadataURL == normalizedIssuer && time.Now().Before(s.metadataUntil) {
		metadata := *s.metadata
		s.metadataMu.Unlock()
		return &metadata, nil
	}
	s.metadataMu.Unlock()

	discoveryURL := strings.TrimRight(normalizedIssuer, "/") + "/.well-known/openid-configuration"
	var metadata providerMetadata
	if err := s.fetchJSON(ctx, discoveryURL, maxMetadataBytes, &metadata); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOIDCDiscoveryFailed, err)
	}
	metadata.Issuer = normalizeIssuer(metadata.Issuer)
	if metadata.Issuer == "" || metadata.Issuer != normalizedIssuer || !validHTTPURL(metadata.AuthorizationEndpoint) || !validHTTPURL(metadata.TokenEndpoint) || !validHTTPURL(metadata.JWKSURI) {
		return nil, ErrOIDCDiscoveryFailed
	}
	s.metadataMu.Lock()
	s.metadata = &metadata
	s.metadataURL = normalizedIssuer
	s.metadataUntil = time.Now().Add(defaultMetadataTTL)
	s.metadataMu.Unlock()
	return &metadata, nil
}

func (s *Service) fetchJSON(ctx context.Context, endpoint string, limit int, target interface{}) error {
	requestCtx, cancel := context.WithTimeout(ctx, defaultHTTPTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, int64(limit))).Decode(target)
}

func (s *Service) keyForID(ctx context.Context, endpoint, kid, alg string) (cachedVerificationKey, error) {
	s.keysMu.RLock()
	cachedURL := s.keysURL
	cachedUntil := s.keysUntil
	key := s.keys[kid]
	s.keysMu.RUnlock()
	if cachedURL == endpoint && time.Now().Before(cachedUntil) && key.Key != nil && (key.Alg == "" || key.Alg == alg) {
		return key, nil
	}
	if err := s.refreshKeys(ctx, endpoint, false); err != nil {
		return cachedVerificationKey{}, err
	}
	s.keysMu.RLock()
	key = s.keys[kid]
	fresh := s.keysURL == endpoint && time.Now().Before(s.keysUntil)
	lastForced := s.lastForcedRefresh
	if key.Key == nil && kid != "_default" && len(s.keys) == 1 {
		key = onlyKey(s.keys)
	}
	s.keysMu.RUnlock()
	if key.Key != nil && (key.Alg == "" || key.Alg == alg) {
		return key, nil
	}
	if fresh && !lastForced.IsZero() && time.Since(lastForced) < unknownKidRefreshWait {
		return cachedVerificationKey{}, ErrOIDCIDTokenInvalid
	}
	if err := s.refreshKeys(ctx, endpoint, true); err != nil {
		return cachedVerificationKey{}, err
	}
	s.keysMu.RLock()
	key = s.keys[kid]
	if key.Key == nil && kid != "_default" && len(s.keys) == 1 {
		key = onlyKey(s.keys)
	}
	s.keysMu.RUnlock()
	if key.Key == nil || (key.Alg != "" && key.Alg != alg) {
		return cachedVerificationKey{}, ErrOIDCIDTokenInvalid
	}
	return key, nil
}

func onlyKey(keys map[string]cachedVerificationKey) cachedVerificationKey {
	for _, key := range keys {
		return key
	}
	return cachedVerificationKey{}
}

func (s *Service) refreshKeys(ctx context.Context, endpoint string, force bool) error {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	s.keysMu.RLock()
	if !force && s.keysURL == endpoint && len(s.keys) > 0 && time.Now().Before(s.keysUntil) {
		s.keysMu.RUnlock()
		return nil
	}
	if force && !s.lastForcedRefresh.IsZero() && time.Since(s.lastForcedRefresh) < unknownKidRefreshWait {
		s.keysMu.RUnlock()
		return nil
	}
	s.keysMu.RUnlock()

	var payload struct {
		Keys []oidcJWK `json:"keys"`
	}
	if err := s.fetchJSON(ctx, endpoint, maxJWKSBytes, &payload); err != nil {
		return fmt.Errorf("%w: %v", ErrOIDCJWKSUnavailable, err)
	}
	keys := make(map[string]cachedVerificationKey, len(payload.Keys))
	for _, item := range payload.Keys {
		key, err := parseJWK(item)
		if err != nil {
			continue
		}
		kid := strings.TrimSpace(item.Kid)
		if kid == "" {
			kid = "_default"
		}
		keys[kid] = key
	}
	if len(keys) == 0 {
		return fmt.Errorf("%w: no usable signing keys", ErrOIDCJWKSUnavailable)
	}
	s.keysMu.Lock()
	s.keys = keys
	s.keysURL = endpoint
	s.keysUntil = time.Now().Add(defaultJWKSCacheTTL)
	if force {
		s.lastForcedRefresh = time.Now()
	}
	s.keysMu.Unlock()
	return nil
}

func (s *Service) clearProviderCache() {
	s.metadataMu.Lock()
	s.metadata = nil
	s.metadataURL = ""
	s.metadataUntil = time.Time{}
	s.metadataMu.Unlock()
	s.keysMu.Lock()
	s.keys = make(map[string]cachedVerificationKey)
	s.keysURL = ""
	s.keysUntil = time.Time{}
	s.lastForcedRefresh = time.Time{}
	s.keysMu.Unlock()
}

func identityFromClaims(claims jwt.MapClaims, issuer string) (*IdentityVerified, error) {
	subject := strings.TrimSpace(stringClaim(claims, "sub"))
	if subject == "" || len(subject) > maxSubjectBytes {
		return nil, ErrOIDCIDTokenInvalid
	}
	emailRaw, _ := firstClaimString(claims, "email", "preferred_username", "upn")
	email := normalizeEmail(emailRaw)
	username, _ := firstClaimString(claims, "preferred_username", "upn", "email", "name", "sub")
	if username == "" {
		username = subject
	}
	displayName, _ := firstClaimString(claims, "name")
	if displayName == "" {
		given, _ := firstClaimString(claims, "given_name")
		family, _ := firstClaimString(claims, "family_name")
		displayName = strings.TrimSpace(strings.TrimSpace(given) + " " + strings.TrimSpace(family))
	}
	issuedAt := time.Now()
	if seconds, ok := numericClaim(claims["iat"]); ok {
		issuedAt = time.Unix(seconds, 0)
	}
	return &IdentityVerified{
		Provider:       ProviderKey(issuer),
		ProviderUserID: subject,
		Username:       strings.TrimSpace(username),
		Email:          email,
		EmailVerified:  boolClaim(claims, "email_verified"),
		DisplayName:    strings.TrimSpace(displayName),
		AvatarURL:      normalizePictureURL(stringClaim(claims, "picture")),
		AuthAt:         issuedAt,
	}, nil
}

// ProviderKey returns a short stable provider identifier derived from issuer.
// It prevents subject collisions when the configured issuer changes while
// fitting the existing varchar(32) provider column.
func ProviderKey(issuer string) string {
	sum := sha256.Sum256([]byte(normalizeIssuer(issuer)))
	return "oidc-" + hex.EncodeToString(sum[:])[:24]
}

func normalizeConfig(cfg config.OIDCAuthConfig) config.OIDCAuthConfig {
	cfg.ProviderName = strings.TrimSpace(cfg.ProviderName)
	if cfg.ProviderName == "" {
		cfg.ProviderName = defaultProviderName
	}
	cfg.IssuerURL = normalizeIssuer(cfg.IssuerURL)
	cfg.ClientID = strings.TrimSpace(cfg.ClientID)
	cfg.ClientSecret = strings.TrimSpace(cfg.ClientSecret)
	cfg.RedirectURI = strings.TrimRight(strings.TrimSpace(cfg.RedirectURI), "/")
	cfg.ClientAuthMethod = strings.ToLower(strings.TrimSpace(cfg.ClientAuthMethod))
	if cfg.ClientAuthMethod == "" {
		cfg.ClientAuthMethod = ClientAuthMethodNone
	}
	cfg.Scopes = normalizeScopes(cfg.Scopes)
	return cfg
}

func validateRuntimeConfig(cfg config.OIDCAuthConfig) error {
	if !cfg.Enabled {
		return ErrOIDCAuthDisabled
	}
	if !validIssuerURL(cfg.IssuerURL) || strings.TrimSpace(cfg.ClientID) == "" || !validHTTPURL(cfg.RedirectURI) {
		return ErrOIDCAuthConfigInvalid
	}
	switch cfg.ClientAuthMethod {
	case ClientAuthMethodNone:
		if !cfg.UsePKCE {
			return ErrOIDCAuthConfigInvalid
		}
	case ClientAuthMethodBasic, ClientAuthMethodPost:
		if cfg.ClientSecret == "" {
			return ErrOIDCAuthConfigInvalid
		}
	default:
		return ErrOIDCAuthConfigInvalid
	}
	return nil
}

func normalizeIssuer(value string) string {
	return strings.TrimRight(strings.TrimSpace(value), "/")
}

func normalizeScopes(value string) string {
	parts := strings.Fields(strings.TrimSpace(value))
	seen := make(map[string]struct{}, len(parts)+1)
	result := make([]string, 0, len(parts)+1)
	for _, part := range parts {
		if _, exists := seen[part]; exists {
			continue
		}
		seen[part] = struct{}{}
		result = append(result, part)
	}
	if len(result) == 0 {
		return defaultScopes
	}
	if _, exists := seen["openid"]; !exists {
		result = append([]string{"openid"}, result...)
	}
	return strings.Join(result, " ")
}

func validIssuerURL(value string) bool {
	parsed, err := url.Parse(normalizeIssuer(value))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	return parsed.Scheme == "https" || (parsed.Scheme == "http" && isLocalHost(parsed.Hostname()))
}

func validHTTPURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	return parsed.Scheme == "https" || (parsed.Scheme == "http" && isLocalHost(parsed.Hostname()))
}

func isLocalHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "[::1]"
}

func randomURLString(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := cryptorand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func newPKCEPair() (string, string, error) {
	verifier, err := randomURLString(48)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

func supportedSigningAlgorithm(alg string) bool {
	switch alg {
	case "RS256", "RS384", "RS512", "ES256", "ES384", "ES512":
		return true
	default:
		return false
	}
}

func parseJWK(item oidcJWK) (cachedVerificationKey, error) {
	alg := strings.TrimSpace(item.Alg)
	if item.Use != "" && item.Use != "sig" {
		return cachedVerificationKey{}, ErrOIDCIDTokenInvalid
	}
	if alg != "" && !supportedSigningAlgorithm(alg) {
		return cachedVerificationKey{}, ErrOIDCIDTokenInvalid
	}
	switch item.Kty {
	case "RSA":
		n, err := decodeBase64URL(item.N)
		if err != nil || len(n) == 0 {
			return cachedVerificationKey{}, ErrOIDCIDTokenInvalid
		}
		e, err := decodeBase64URL(item.E)
		if err != nil || len(e) == 0 || len(e) > 4 {
			return cachedVerificationKey{}, ErrOIDCIDTokenInvalid
		}
		exponent := 0
		for _, value := range e {
			exponent = exponent<<8 | int(value)
		}
		if exponent < 3 {
			return cachedVerificationKey{}, ErrOIDCIDTokenInvalid
		}
		return cachedVerificationKey{Key: &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: exponent}, Alg: alg}, nil
	case "EC":
		curve, expectedSize, ok := ellipticCurve(item.Crv)
		if !ok {
			return cachedVerificationKey{}, ErrOIDCIDTokenInvalid
		}
		x, err := decodeBase64URL(item.X)
		if err != nil || len(x) != expectedSize {
			return cachedVerificationKey{}, ErrOIDCIDTokenInvalid
		}
		y, err := decodeBase64URL(item.Y)
		if err != nil || len(y) != expectedSize {
			return cachedVerificationKey{}, ErrOIDCIDTokenInvalid
		}
		return cachedVerificationKey{Key: &ecdsa.PublicKey{Curve: curve, X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}, Alg: alg}, nil
	default:
		return cachedVerificationKey{}, ErrOIDCIDTokenInvalid
	}
}

func decodeBase64URL(value string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(strings.TrimSpace(value), "="))
}

func ellipticCurve(name string) (elliptic.Curve, int, bool) {
	switch name {
	case "P-256":
		return elliptic.P256(), 32, true
	case "P-384":
		return elliptic.P384(), 48, true
	case "P-521":
		return elliptic.P521(), 66, true
	default:
		return nil, 0, false
	}
}

func stringClaim(claims jwt.MapClaims, key string) string {
	value, _ := claims[key].(string)
	return strings.TrimSpace(value)
}

func firstClaimString(claims jwt.MapClaims, keys ...string) (string, string) {
	for _, key := range keys {
		if value := stringClaim(claims, key); value != "" {
			return value, key
		}
	}
	return "", ""
}

func boolClaim(claims jwt.MapClaims, key string) bool {
	switch value := claims[key].(type) {
	case bool:
		return value
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		return err == nil && parsed
	default:
		return false
	}
}

func numericClaim(value interface{}) (int64, bool) {
	switch value := value.(type) {
	case float64:
		return int64(value), value > 0
	case json.Number:
		parsed, err := value.Int64()
		return parsed, err == nil && parsed > 0
	case int64:
		return value, value > 0
	default:
		return 0, false
	}
}

func validateAuthorizedParty(claims jwt.MapClaims, clientID string) bool {
	azp := stringClaim(claims, "azp")
	audienceCount := 0
	switch audience := claims["aud"].(type) {
	case string:
		if strings.TrimSpace(audience) != "" {
			audienceCount = 1
		}
	case []interface{}:
		for _, item := range audience {
			if value, ok := item.(string); ok && strings.TrimSpace(value) != "" {
				audienceCount++
			}
		}
	case []string:
		for _, value := range audience {
			if strings.TrimSpace(value) != "" {
				audienceCount++
			}
		}
	}
	if audienceCount > 1 {
		return azp == clientID
	}
	return azp == "" || azp == clientID
}

func normalizeEmail(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return ""
	}
	address, err := mail.ParseAddress(normalized)
	if err != nil || address.Address != normalized {
		return ""
	}
	return normalized
}

func normalizePictureURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxPictureURLBytes {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ""
	}
	return parsed.String()
}
