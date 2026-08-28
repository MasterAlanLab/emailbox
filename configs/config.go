package configs

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"

	"emailbox/pkg/crypto"

	"github.com/joho/godotenv"
)

// AppEnvProduction 是生产模式的取值。若干安全项在该模式下从「警告」升级为「启动失败」。
const AppEnvProduction = "production"

// 注册模式取值。
const (
	RegistrationOpen   = "open"   // 任何人可注册
	RegistrationClosed = "closed" // 只能由管理员在后台创建用户
)

type Config struct {
	AppEnv   string         `json:"app_env"`
	Server   ServerConfig   `json:"server"`
	Database DatabaseConfig `json:"database"`
	Session  SessionConfig  `json:"session"`
	Crypto   CryptoConfig   `json:"crypto"`
	SaaS     SaaSConfig     `json:"saas"`
	Job      JobConfig      `json:"job"`
	OAuth    OAuthConfig    `json:"oauth"`
}
type ServerConfig struct {
	Port        string   `json:"port"`
	Host        string   `json:"host"`
	CORSOrigins []string `json:"cors_origins"`
	TrustProxy  bool     `json:"trust_proxy"`
}
type DatabaseConfig struct {
	Driver   string `json:"driver"`
	Host     string `json:"host"`
	Port     string `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	DBName   string `json:"dbname"`
	SSLMode  string `json:"sslmode"`
	Path     string `json:"path"`
}
type SessionConfig struct {
	ExpireHour   int  `json:"expire_hour"`
	CookieSecure bool `json:"cookie_secure"`
}

// CryptoConfig 是敏感字段加密的配置。密钥独立于会话密钥，
// 因为两者生命周期不同：轮换会话密钥不应导致全部邮箱账号无法解密。
type CryptoConfig struct {
	Key string `json:"-"` // ENCRYPTION_KEY，32 字节密钥的 base64 或 hex
}

// SaaSConfig 是开放注册与平台管理员相关的配置。
type SaaSConfig struct {
	RegistrationMode string `json:"registration_mode"`
	// 认用户名而不是邮箱：邮箱自 000008 起可以不填，按邮箱找的话，
	// 一个所有人都没填邮箱的部署将永远产生不出管理员。
	BootstrapAdminUsername string `json:"bootstrap_admin_username"`
	// BootstrapAdminPassword 只在该用户尚不存在时用来把他建出来；
	// 用户已存在时不会改他的密码——配置文件不该能悄悄接管一个已有账号。
	BootstrapAdminPassword string `json:"-"`
	DefaultPlanCode        string `json:"default_plan_code"`
}

// JobConfig 是批量任务的调度参数。
//
// 默认值按「5000 个账号、单账号约 1.5 秒」估：8 并发约 16 分钟。
// 调大并发要同时考虑服务商风控——把 AccountDelay 一起调大更稳妥。
type JobConfig struct {
	Workers        int `json:"workers"`
	AccountDelayMS int `json:"account_delay_ms"`
	// EventRetentionDays 控制 job_events 的保留天数。事件量与账号数成正比，
	// 不清理会一直涨；断线重连只需要最近的那些。
	EventRetentionDays int `json:"event_retention_days"`
}

// OAuthConfig 是平台托管的 Microsoft 应用配置。ClientSecret 只用于机密客户端；
// 参考项目使用公共客户端，因此默认留空并依靠 PKCE。
type OAuthConfig struct {
	Enabled      bool   `json:"enabled"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"-"`
	Tenant       string `json:"tenant"`
	RedirectURI  string `json:"redirect_uri"`
	ReturnURL    string `json:"return_url"`
}

var AppConfig *Config

// IsProduction 表示当前是否运行在生产模式。
func (c *Config) IsProduction() bool { return c.AppEnv == AppEnvProduction }

func Init() error {
	if err := godotenv.Load(); err != nil {
		slog.Info("未找到 .env，使用环境变量或默认值")
	}
	origins, err := parseOrigins(getEnv("CORS_ALLOW_ORIGINS", "http://localhost:5173,http://localhost:3000"))
	if err != nil {
		return err
	}
	cookieSecure, err := getEnvBool("COOKIE_SECURE", false)
	if err != nil {
		return err
	}
	trustProxy, err := getEnvBool("TRUST_PROXY", false)
	if err != nil {
		return err
	}
	oauthEnabled, err := getEnvBool("MICROSOFT_OAUTH_ENABLED", true)
	if err != nil {
		return err
	}
	AppConfig = &Config{AppEnv: getEnv("APP_ENV", "development"), Server: ServerConfig{Port: getEnv("SERVER_PORT", "1323"), Host: getEnv("SERVER_HOST", "0.0.0.0"), CORSOrigins: origins, TrustProxy: trustProxy}, Database: DatabaseConfig{Driver: getEnv("DB_DRIVER", "sqlite"), Host: getEnv("DB_HOST", "localhost"), Port: getEnv("DB_PORT", "5432"), Username: getEnv("DB_USERNAME", "postgres"), Password: getEnv("DB_PASSWORD", ""), DBName: getEnv("DB_NAME", "emailbox"), SSLMode: getEnv("DB_SSLMODE", "disable"), Path: getEnv("DB_PATH", "app.db")}, Session: SessionConfig{ExpireHour: getEnvInt("SESSION_EXPIRE_HOUR", 24), CookieSecure: cookieSecure}, Crypto: CryptoConfig{Key: getEnv("ENCRYPTION_KEY", "")}, SaaS: SaaSConfig{RegistrationMode: getEnv("REGISTRATION_MODE", RegistrationOpen), BootstrapAdminUsername: strings.TrimSpace(getEnv("BOOTSTRAP_ADMIN_USERNAME", "")), BootstrapAdminPassword: getEnv("BOOTSTRAP_ADMIN_PASSWORD", ""), DefaultPlanCode: getEnv("DEFAULT_PLAN_CODE", "free")}, Job: JobConfig{Workers: getEnvInt("JOB_WORKERS", 8), AccountDelayMS: getEnvInt("JOB_ACCOUNT_DELAY_MS", 0), EventRetentionDays: getEnvInt("JOB_EVENT_RETENTION_DAYS", 7)}, OAuth: OAuthConfig{Enabled: oauthEnabled, ClientID: strings.TrimSpace(getEnv("MICROSOFT_OAUTH_CLIENT_ID", "9e5f94bc-e8a4-4e73-b8be-63364c29d753")), ClientSecret: getEnv("MICROSOFT_OAUTH_CLIENT_SECRET", ""), Tenant: strings.TrimSpace(getEnv("MICROSOFT_OAUTH_TENANT", "common")), RedirectURI: strings.TrimSpace(getEnv("MICROSOFT_OAUTH_REDIRECT_URI", "http://localhost:8080")), ReturnURL: strings.TrimSpace(getEnv("MICROSOFT_OAUTH_RETURN_URL", "http://localhost:5173/mail/tokens"))}}
	if AppConfig.Database.Driver != "sqlite" && AppConfig.Database.Driver != "postgres" && AppConfig.Database.Driver != "postgresql" {
		return fmt.Errorf("DB_DRIVER 仅支持 sqlite 或 postgres")
	}
	switch AppConfig.SaaS.RegistrationMode {
	case RegistrationOpen, RegistrationClosed:
	default:
		return fmt.Errorf("REGISTRATION_MODE 仅支持 %s / %s", RegistrationOpen, RegistrationClosed)
	}
	if err := validateOAuth(AppConfig); err != nil {
		return err
	}
	return validateCrypto(AppConfig)
}

func validateOAuth(c *Config) error {
	if !c.OAuth.Enabled {
		return nil
	}
	if c.OAuth.ClientID == "" || c.OAuth.Tenant == "" {
		return fmt.Errorf("微软 OAuth 已启用，但 client_id 或 tenant 为空")
	}
	for name, raw := range map[string]string{"MICROSOFT_OAUTH_REDIRECT_URI": c.OAuth.RedirectURI, "MICROSOFT_OAUTH_RETURN_URL": c.OAuth.ReturnURL} {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("%s 含非法 URL %q", name, raw)
		}
	}
	return nil
}

// validateCrypto 校验加密密钥。生产模式缺少密钥直接启动失败——
// 邮箱凭据以明文落库的风险远高于一次启动失败。
func validateCrypto(c *Config) error {
	if c.Crypto.Key == "" {
		if c.IsProduction() {
			return fmt.Errorf("生产模式必须配置 ENCRYPTION_KEY（32 字节密钥的 base64 或 hex）")
		}
		slog.Warn("未配置 ENCRYPTION_KEY，邮箱凭据将以明文存储，仅限本地开发")
		return nil
	}
	if _, err := crypto.ParseKey(c.Crypto.Key); err != nil {
		return fmt.Errorf("ENCRYPTION_KEY 非法: %w", err)
	}
	return nil
}

// NewCipher 按配置构造敏感字段加解密器。未配置密钥时（仅开发模式可达）返回明文实现。
func (c *Config) NewCipher() (crypto.Cipher, error) {
	if c.Crypto.Key == "" {
		return crypto.NewPlaintext(), nil
	}
	return crypto.New(c.Crypto.Key)
}

// parseOrigins 解析逗号分隔的来源列表。这里必须自己校验并返回错误，
// 因为 echo 的 CORS 中间件遇到非法来源会直接 panic，而不是报配置错误。
func parseOrigins(raw string) ([]string, error) {
	items := strings.Split(raw, ",")
	origins := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if item == "*" {
			return nil, fmt.Errorf("CORS_ALLOW_ORIGINS 不能为 *，会话使用 Cookie 凭证，通配来源不被允许")
		}
		u, err := url.Parse(item)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("CORS_ALLOW_ORIGINS 含非法来源 %q，需形如 https://example.com", item)
		}
		origins = append(origins, item)
	}
	if len(origins) == 0 {
		return nil, fmt.Errorf("CORS_ALLOW_ORIGINS 不能为空")
	}
	return origins, nil
}
func getEnv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func getEnvInt(k string, d int) int {
	raw := os.Getenv(k)
	if raw == "" {
		return d
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		slog.Warn("环境变量不是合法整数，使用默认值", "key", k, "value", raw, "default", d)
		return d
	}
	return v
}

// getEnvBool 严格解析布尔值：安全开关一旦拼写错误就静默失败，比启动失败更危险。
func getEnvBool(k string, d bool) (bool, error) {
	raw := os.Getenv(k)
	if raw == "" {
		return d, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("环境变量 %s=%q 不是合法布尔值（true/false/1/0）", k, raw)
	}
	return v, nil
}

// sqliteDSN 拼接 SQLite DSN。加了 file: 前缀后整串会按 URI 解析，
// 路径里未转义的 ? 和 # 会截断文件名，并把 pragma 一起丢掉。
func sqliteDSN(path string) string {
	const pragmas = "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	if strings.HasPrefix(path, "file:") {
		if strings.Contains(path, "?") {
			return path + "&" + pragmas
		}
		return path + "?" + pragmas
	}
	escaped := strings.NewReplacer("%", "%25", "?", "%3F", "#", "%23").Replace(path)
	return "file:" + escaped + "?" + pragmas
}

func (c *Config) GetDatabaseDSN() string {
	if c.Database.Driver == "sqlite" {
		// 外键约束是连接级设置，必须通过 DSN 在每个连接上开启，
		// 否则 ON DELETE CASCADE / SET NULL 都不会生效。
		return sqliteDSN(c.Database.Path)
	}
	u := &url.URL{Scheme: "postgres", Host: c.Database.Host + ":" + c.Database.Port, Path: c.Database.DBName}
	if c.Database.Username != "" {
		u.User = url.UserPassword(c.Database.Username, c.Database.Password)
	}
	q := u.Query()
	q.Set("sslmode", c.Database.SSLMode)
	u.RawQuery = q.Encode()
	return u.String()
}
func (c *Config) GetServerAddress() string { return c.Server.Host + ":" + c.Server.Port }
