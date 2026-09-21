package settingssecurity

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/dujiao-next/internal/config"
	settingsvalue "github.com/dujiao-next/internal/modules/settings/schema/value"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

var ErrOIDCAuthConfigInvalid = errors.New("oidc auth config invalid")

const defaultOIDCScopes = "openid profile email"

const (
	oidcClientAuthMethodNone  = "none"
	oidcClientAuthMethodBasic = "client_secret_basic"
	oidcClientAuthMethodPost  = "client_secret_post"
)

// OIDCAuthSetting 通用 OpenID Connect 登录设置。
type OIDCAuthSetting struct {
	Enabled          bool   `json:"enabled"`
	ProviderName     string `json:"provider_name"`
	IssuerURL        string `json:"issuer_url"`
	ClientID         string `json:"client_id"`
	ClientSecret     string `json:"client_secret"`
	RedirectURI      string `json:"redirect_uri"`
	ClientAuthMethod string `json:"client_auth_method"`
	UsePKCE          bool   `json:"use_pkce"`
	Scopes           string `json:"scopes"`
}

// OIDCAuthSettingPatch 通用 OIDC 登录设置补丁。
type OIDCAuthSettingPatch struct {
	Enabled          *bool   `json:"enabled"`
	ProviderName     *string `json:"provider_name"`
	IssuerURL        *string `json:"issuer_url"`
	ClientID         *string `json:"client_id"`
	ClientSecret     *string `json:"client_secret"`
	RedirectURI      *string `json:"redirect_uri"`
	ClientAuthMethod *string `json:"client_auth_method"`
	UsePKCE          *bool   `json:"use_pkce"`
	Scopes           *string `json:"scopes"`
}

// DefaultOIDCAuthSetting 根据启动配置生成默认设置。
func DefaultOIDCAuthSetting(cfg config.OIDCAuthConfig) OIDCAuthSetting {
	return NormalizeOIDCAuthSetting(OIDCAuthSetting{
		Enabled:          cfg.Enabled,
		ProviderName:     cfg.ProviderName,
		IssuerURL:        cfg.IssuerURL,
		ClientID:         cfg.ClientID,
		ClientSecret:     cfg.ClientSecret,
		RedirectURI:      cfg.RedirectURI,
		ClientAuthMethod: cfg.ClientAuthMethod,
		UsePKCE:          cfg.UsePKCE,
		Scopes:           cfg.Scopes,
	})
}

// NormalizeOIDCAuthSetting 归一化通用 OIDC 设置。
func NormalizeOIDCAuthSetting(setting OIDCAuthSetting) OIDCAuthSetting {
	setting.ProviderName = strings.TrimSpace(setting.ProviderName)
	if setting.ProviderName == "" {
		setting.ProviderName = "OIDC"
	}
	if len([]rune(setting.ProviderName)) > 64 {
		setting.ProviderName = string([]rune(setting.ProviderName)[:64])
	}
	setting.IssuerURL = strings.TrimRight(strings.TrimSpace(setting.IssuerURL), "/")
	setting.ClientID = strings.TrimSpace(setting.ClientID)
	setting.ClientSecret = strings.TrimSpace(setting.ClientSecret)
	setting.RedirectURI = strings.TrimRight(strings.TrimSpace(setting.RedirectURI), "/")
	setting.ClientAuthMethod = strings.ToLower(strings.TrimSpace(setting.ClientAuthMethod))
	if setting.ClientAuthMethod == "" {
		setting.ClientAuthMethod = oidcClientAuthMethodNone
	}
	setting.Scopes = normalizeOIDCScopes(setting.Scopes)
	return setting
}

// ValidateOIDCAuthSetting 校验通用 OIDC 设置。
func ValidateOIDCAuthSetting(setting OIDCAuthSetting) error {
	normalized := NormalizeOIDCAuthSetting(setting)
	if !normalized.Enabled {
		return nil
	}
	if normalized.IssuerURL == "" || !validOIDCBaseURL(normalized.IssuerURL) {
		return fmt.Errorf("%w: issuer_url 必须是合法的 HTTPS URL", ErrOIDCAuthConfigInvalid)
	}
	if normalized.ClientID == "" {
		return fmt.Errorf("%w: client_id 不能为空", ErrOIDCAuthConfigInvalid)
	}
	if normalized.RedirectURI == "" || !validOIDCBaseURL(normalized.RedirectURI) {
		return fmt.Errorf("%w: redirect_uri 必须是合法的 HTTPS URL", ErrOIDCAuthConfigInvalid)
	}
	switch normalized.ClientAuthMethod {
	case oidcClientAuthMethodNone:
		if !normalized.UsePKCE {
			return fmt.Errorf("%w: client_auth_method 为 none 时必须启用 PKCE", ErrOIDCAuthConfigInvalid)
		}
	case oidcClientAuthMethodBasic, oidcClientAuthMethodPost:
		if normalized.ClientSecret == "" {
			return fmt.Errorf("%w: 当前客户端认证方式需要 client_secret", ErrOIDCAuthConfigInvalid)
		}
	default:
		return fmt.Errorf("%w: client_auth_method 不受支持", ErrOIDCAuthConfigInvalid)
	}
	if !containsOIDCScope(normalized.Scopes, "openid") {
		return fmt.Errorf("%w: scopes 必须包含 openid", ErrOIDCAuthConfigInvalid)
	}
	return nil
}

// ApplyOIDCAuthSettingPatch 应用设置补丁，不执行最终校验。
func ApplyOIDCAuthSettingPatch(current OIDCAuthSetting, patch OIDCAuthSettingPatch) OIDCAuthSetting {
	next := current
	if patch.Enabled != nil {
		next.Enabled = *patch.Enabled
	}
	if patch.ProviderName != nil {
		next.ProviderName = strings.TrimSpace(*patch.ProviderName)
	}
	if patch.IssuerURL != nil {
		next.IssuerURL = strings.TrimSpace(*patch.IssuerURL)
	}
	if patch.ClientID != nil {
		next.ClientID = strings.TrimSpace(*patch.ClientID)
	}
	if patch.ClientSecret != nil && strings.TrimSpace(*patch.ClientSecret) != "" {
		next.ClientSecret = strings.TrimSpace(*patch.ClientSecret)
	}
	if patch.RedirectURI != nil {
		next.RedirectURI = strings.TrimSpace(*patch.RedirectURI)
	}
	if patch.ClientAuthMethod != nil {
		next.ClientAuthMethod = strings.TrimSpace(*patch.ClientAuthMethod)
	}
	if patch.UsePKCE != nil {
		next.UsePKCE = *patch.UsePKCE
	}
	if patch.Scopes != nil {
		next.Scopes = strings.TrimSpace(*patch.Scopes)
	}
	return next
}

// OIDCAuthSettingToConfig 转换为运行时配置。
func OIDCAuthSettingToConfig(setting OIDCAuthSetting) config.OIDCAuthConfig {
	normalized := NormalizeOIDCAuthSetting(setting)
	return config.OIDCAuthConfig{
		Enabled:          normalized.Enabled,
		ProviderName:     normalized.ProviderName,
		IssuerURL:        normalized.IssuerURL,
		ClientID:         normalized.ClientID,
		ClientSecret:     normalized.ClientSecret,
		RedirectURI:      normalized.RedirectURI,
		ClientAuthMethod: normalized.ClientAuthMethod,
		UsePKCE:          normalized.UsePKCE,
		Scopes:           normalized.Scopes,
	}
}

// EncodeOIDCAuthSetting 转换为设置存储结构。
func EncodeOIDCAuthSetting(setting OIDCAuthSetting) jsonmap.JSON {
	normalized := NormalizeOIDCAuthSetting(setting)
	return jsonmap.JSON{
		"enabled":            normalized.Enabled,
		"provider_name":      normalized.ProviderName,
		"issuer_url":         normalized.IssuerURL,
		"client_id":          normalized.ClientID,
		"client_secret":      normalized.ClientSecret,
		"redirect_uri":       normalized.RedirectURI,
		"client_auth_method": normalized.ClientAuthMethod,
		"use_pkce":           normalized.UsePKCE,
		"scopes":             normalized.Scopes,
	}
}

// MaskOIDCAuthSettingForAdmin 返回脱敏后的后台设置。
func MaskOIDCAuthSettingForAdmin(setting OIDCAuthSetting) jsonmap.JSON {
	normalized := NormalizeOIDCAuthSetting(setting)
	return jsonmap.JSON{
		"enabled":            normalized.Enabled,
		"provider_name":      normalized.ProviderName,
		"issuer_url":         normalized.IssuerURL,
		"client_id":          normalized.ClientID,
		"has_client_secret":  normalized.ClientSecret != "",
		"redirect_uri":       normalized.RedirectURI,
		"client_auth_method": normalized.ClientAuthMethod,
		"use_pkce":           normalized.UsePKCE,
		"scopes":             normalized.Scopes,
	}
}

// DecodeOIDCAuthSetting 从持久化 JSON 解码，并使用 fallback 填充缺失值。
func DecodeOIDCAuthSetting(raw jsonmap.JSON, fallback OIDCAuthSetting) OIDCAuthSetting {
	next := fallback
	if raw == nil {
		return NormalizeOIDCAuthSetting(next)
	}
	if value, ok := raw["enabled"]; ok {
		next.Enabled = settingsvalue.ParseBool(value)
	}
	if value, ok := raw["provider_name"].(string); ok {
		next.ProviderName = value
	}
	if value, ok := raw["issuer_url"].(string); ok {
		next.IssuerURL = value
	}
	if value, ok := raw["client_id"].(string); ok {
		next.ClientID = value
	}
	if value, ok := raw["client_secret"].(string); ok {
		next.ClientSecret = value
	}
	if value, ok := raw["redirect_uri"].(string); ok {
		next.RedirectURI = value
	}
	if value, ok := raw["client_auth_method"].(string); ok {
		next.ClientAuthMethod = value
	}
	if value, ok := raw["use_pkce"]; ok {
		next.UsePKCE = settingsvalue.ParseBool(value)
	}
	if value, ok := raw["scopes"].(string); ok {
		next.Scopes = value
	}
	return NormalizeOIDCAuthSetting(next)
}

// NormalizeOIDCAuthSettingJSON 是 Registry 使用的原始 JSON 写入策略。
func NormalizeOIDCAuthSettingJSON(raw jsonmap.JSON) jsonmap.JSON {
	return EncodeOIDCAuthSetting(DecodeOIDCAuthSetting(raw, DefaultOIDCAuthSetting(config.OIDCAuthConfig{})))
}

func normalizeOIDCScopes(value string) string {
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
		return defaultOIDCScopes
	}
	if _, exists := seen["openid"]; !exists {
		result = append([]string{"openid"}, result...)
	}
	return strings.Join(result, " ")
}

func containsOIDCScope(scopes, target string) bool {
	for _, scope := range strings.Fields(scopes) {
		if scope == target {
			return true
		}
	}
	return false
}

func validOIDCBaseURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	if parsed.Scheme == "https" {
		return true
	}
	return parsed.Scheme == "http" && isLocalOIDCHost(parsed.Hostname())
}

func isLocalOIDCHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "[::1]"
}
