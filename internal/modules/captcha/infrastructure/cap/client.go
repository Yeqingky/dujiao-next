package cap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dujiao-next/internal/modules/captcha/contract"
	settingssecurity "github.com/dujiao-next/internal/modules/settings/schema/security"
)

// Client 通过 Cap Standalone siteverify API 校验令牌。
type Client struct{}

var _ contract.CapVerifier = Client{}

// New 创建 Cap Standalone 校验客户端。
func New() Client { return Client{} }

type verifyRequest struct {
	Secret   string `json:"secret"`
	Response string `json:"response"`
}

type verifyResponse struct {
	Success bool `json:"success"`
}

// Verify 校验 Cap 令牌。clientIP 预留给未来的 Cap 服务端扩展，当前 API 不发送该字段。
func (Client) Verify(cfg settingssecurity.CaptchaCapSetting, token, _ string) error {
	secret := strings.TrimSpace(cfg.SecretKey)
	if secret == "" {
		return contract.ErrConfigInvalid
	}

	verifyURL, err := buildVerifyURL(cfg.Endpoint, cfg.SiteKey)
	if err != nil {
		return contract.ErrConfigInvalid
	}

	payload, err := json.Marshal(verifyRequest{Secret: secret, Response: token})
	if err != nil {
		return fmt.Errorf("%w: %v", contract.ErrVerifyFailed, err)
	}

	timeout := cfg.TimeoutMS
	if timeout < 500 || timeout > 10000 {
		timeout = 2000
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, verifyURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("%w: %v", contract.ErrVerifyFailed, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: time.Duration(timeout) * time.Millisecond}).Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", contract.ErrVerifyFailed, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("%w: Cap siteverify returned HTTP %d", contract.ErrVerifyFailed, resp.StatusCode)
	}

	var result verifyResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("%w: %v", contract.ErrVerifyFailed, err)
	}
	if !result.Success {
		return contract.ErrInvalid
	}
	return nil
}

func buildVerifyURL(endpoint, siteKey string) (string, error) {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	siteKey = strings.TrimSpace(siteKey)
	if endpoint == "" || siteKey == "" {
		return "", contract.ErrConfigInvalid
	}

	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", contract.ErrConfigInvalid
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", contract.ErrConfigInvalid
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/" + url.PathEscape(siteKey) + "/siteverify"
	parsed.RawPath = ""
	return parsed.String(), nil
}
