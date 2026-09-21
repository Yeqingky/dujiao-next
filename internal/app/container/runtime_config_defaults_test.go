package container

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dujiao-next/internal/config"
)

// TestConfigLoadKeepsDefaultsForAdminManagedSettings 保护「后台管理的设置不再写入 config.yml」这一约定。
// 缺少这些 section 时必须回落到代码默认值，否则运行期行为会静默改变。
// 与 google_runtime_settings_test.go 合起来覆盖完整的优先级链：数据库设置 > config.yml > 代码默认值。
//
// 该测试放在 container 包而不是 internal/config 包，因为 .gitignore 的 `config/` 规则未锚定，
// 会连带忽略 internal/config/ 下的新文件。
func TestConfigLoadKeepsDefaultsForAdminManagedSettings(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// 只保留启动阶段必须的配置，模拟去掉 email/captcha/telegram_auth/google_auth/order 之后的 config.yml。
	if err := os.WriteFile(filepath.Join(dir, "config.yml"), []byte("app:\n  secret_key: test-secret-key\n"), 0o600); err != nil {
		t.Fatalf("write config.yml: %v", err)
	}

	cfg := config.Load()

	if cfg.Email.Enabled {
		t.Errorf("email.enabled = true, want false")
	}
	if cfg.Email.Port != 587 {
		t.Errorf("email.port = %d, want 587", cfg.Email.Port)
	}
	if !cfg.Email.UseTLS || cfg.Email.UseSSL {
		t.Errorf("email tls/ssl = %v/%v, want true/false", cfg.Email.UseTLS, cfg.Email.UseSSL)
	}
	if cfg.Email.VerifyCode.ExpireMinutes != 10 || cfg.Email.VerifyCode.SendIntervalSeconds != 60 ||
		cfg.Email.VerifyCode.MaxAttempts != 5 || cfg.Email.VerifyCode.Length != 6 {
		t.Errorf("email.verify_code = %+v, want defaults", cfg.Email.VerifyCode)
	}

	if cfg.Captcha.Provider != "none" {
		t.Errorf("captcha.provider = %q, want none", cfg.Captcha.Provider)
	}
	if cfg.Captcha.Image.Length != 5 || cfg.Captcha.Image.Width != 240 || cfg.Captcha.Image.Height != 80 ||
		cfg.Captcha.Image.ExpireSeconds != 300 || cfg.Captcha.Image.MaxStore != 10240 {
		t.Errorf("captcha.image = %+v, want defaults", cfg.Captcha.Image)
	}
	if cfg.Captcha.Turnstile.TimeoutMS != 2000 || cfg.Captcha.Cap.TimeoutMS != 2000 {
		t.Errorf("captcha timeouts = %+v, want 2000", cfg.Captcha)
	}

	if cfg.TelegramAuth.Enabled || cfg.TelegramAuth.BotUsername != "" {
		t.Errorf("telegram_auth = %+v, want disabled defaults", cfg.TelegramAuth)
	}
	if cfg.TelegramAuth.LoginExpireSeconds != 300 || cfg.TelegramAuth.ReplayTTLSeconds != 300 {
		t.Errorf("telegram_auth ttl = %+v, want 300", cfg.TelegramAuth)
	}

	if cfg.GoogleAuth.Enabled || cfg.GoogleAuth.ClientID != "" {
		t.Errorf("google_auth = %+v, want disabled defaults", cfg.GoogleAuth)
	}

	if cfg.Order.PaymentExpireMinutes != 15 || cfg.Order.MaxRefundDays != 30 {
		t.Errorf("order = %+v, want 15/30", cfg.Order)
	}

	if cfg.Queue.UpstreamSyncInterval != "5m" {
		t.Errorf("queue.upstream_sync_interval = %q, want 5m", cfg.Queue.UpstreamSyncInterval)
	}

	// 启动阶段必须的配置仍由 config.yml 提供。
	if cfg.App.SecretKey != "test-secret-key" {
		t.Errorf("app.secret_key = %q, want the value from config.yml", cfg.App.SecretKey)
	}
	if cfg.Server.Port != "8080" || cfg.Web.AdminPath != "/admin" {
		t.Errorf("server.port/web.admin_path = %q/%q, want 8080/admin", cfg.Server.Port, cfg.Web.AdminPath)
	}
}
