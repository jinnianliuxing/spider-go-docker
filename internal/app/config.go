package app

import (
	"fmt"
	"os"

	"github.com/spf13/viper"
)

type Config struct {
	App      Appconfig          `yaml:"app" mapstructure:"app"`
	CORS     CORSConfig         `yaml:"cors" mapstructure:"cors"`
	Database DatabaseConfig     `yaml:"database" mapstructure:"database"`
	Redis    RedisClusterConfig `yaml:"redis" mapstructure:"redis"`
	Jwc      JwcConfig          `yaml:"jwc" mapstructure:"jwc"`
	JWT      JWTConfig          `yaml:"jwt" mapstructure:"jwt"`
	Email    EmailConfig        `yaml:"email" mapstructure:"email"`
	Wx       WxConfig           `yaml:"wx" mapstructure:"wx"`
	OSS      OSSConfig          `yaml:"oss" mapstructure:"oss"`
}

type Appconfig struct {
	Port    int    `yaml:"port" mapstructure:"port"`
	Env     string `yaml:"env" mapstructure:"env"`           // 环境：dev, production
	BaseURL string `yaml:"base_url" mapstructure:"base_url"` // 前端页面基础 URL，用于 Magic Link 等
}

// CORSConfig CORS 跨域配置
type CORSConfig struct {
	AllowOrigins     []string `yaml:"allow_origins" mapstructure:"allow_origins"`
	AllowMethods     []string `yaml:"allow_methods" mapstructure:"allow_methods"`
	AllowHeaders     []string `yaml:"allow_headers" mapstructure:"allow_headers"`
	ExposeHeaders    []string `yaml:"expose_headers" mapstructure:"expose_headers"`
	AllowCredentials bool     `yaml:"allow_credentials" mapstructure:"allow_credentials"`
	MaxAge           int      `yaml:"max_age" mapstructure:"max_age"` // 预检请求缓存时间（秒）
}

// JwcConfig 教务系统配置
type JwcConfig struct {
	Mode            string        `yaml:"mode" mapstructure:"mode"` // 模式：campus 或 webvpn
	Campus          JwcModeConfig `yaml:"campus" mapstructure:"campus"`
	Webvpn          JwcModeConfig `yaml:"webvpn" mapstructure:"webvpn"`
	GetRSAKeyURL    string        `yaml:"rsa_url" mapstructure:"rsa_url"`
	MFADetectURL    string        `yaml:"mfa_detect_url" mapstructure:"mfa_detect_url"`
	CaptchaURL      string        `yaml:"captcha_url" mapstructure:"captcha_url"`
	CaptchaImageURL string        `yaml:"captcha_image_url" mapstructure:"captcha_image_url"`
	WebvpnTokenURL  string        `yaml:"webvpn_token_url" mapstructure:"webvpn_token_url"`
}

// JwcModeConfig 教务系统单个模式的配置
type JwcModeConfig struct {
	LoginURL      string `yaml:"login_url" mapstructure:"login_url"`
	RedirectURL   string `yaml:"redirect_url" mapstructure:"redirect_url"`
	CourseURL     string `yaml:"course_url" mapstructure:"course_url"`
	GradeURL      string `yaml:"grade_url" mapstructure:"grade_url"`
	GradeLevelURL string `yaml:"grade_level_url" mapstructure:"grade_level_url"`
	ExamURL       string `yaml:"exam_url" mapstructure:"exam_url"`
	// 教评系统配置
	EvaluationRedirectURL string `yaml:"evaluation_redirect_url" mapstructure:"evaluation_redirect_url"`
	EvaluationInfoURL     string `yaml:"evaluation_info_url" mapstructure:"evaluation_info_url"`
	EvaluationDoLoginURL  string `yaml:"evaluation_do_login_url" mapstructure:"evaluation_do_login_url"`
}

// GetCurrentModeConfig 获取当前模式的配置
func (c *JwcConfig) GetCurrentModeConfig() JwcModeConfig {
	if c.Mode == "webvpn" {
		return c.Webvpn
	}
	return c.Campus // 默认使用校园网模式
}

type JWTConfig struct {
	Secret string `yaml:"secret" mapstructure:"secret"`
	Issuer string `yaml:"issuer" mapstructure:"issuer"`
}

// EmailConfig 邮件服务配置
type EmailConfig struct {
	SMTPHost string `yaml:"smtp_host" mapstructure:"smtp_host"` // SMTP 服务器地址
	SMTPPort int    `yaml:"smtp_port" mapstructure:"smtp_port"` // SMTP 端口
	Username string `yaml:"username" mapstructure:"username"`   // 发件人邮箱
	Password string `yaml:"password" mapstructure:"password"`   // SMTP 授权码
	FromName string `yaml:"from_name" mapstructure:"from_name"` // 发件人名称
}

type DatabaseConfig struct {
	Host string `yaml:"source" mapstructure:"source"`
	Port int    `yaml:"port" mapstructure:"port"`
	User string `yaml:"user" mapstructure:"user"`
	Pass string `yaml:"pass" mapstructure:"pass"`
	Name string `yaml:"name" mapstructure:"name"`
}

// RedisConfig 单个 Redis 数据库配置
type RedisConfig struct {
	Host string `yaml:"host" mapstructure:"host"`
	Port int    `yaml:"port" mapstructure:"port"`
	Pass string `yaml:"pass" mapstructure:"pass"`
	DB   int    `yaml:"db" mapstructure:"db"`
}

// RedisClusterConfig Redis 集群配置（同一个 Redis 服务器的不同数据库）
type RedisClusterConfig struct {
	Session RedisConfig `yaml:"session" mapstructure:"session"` // DB 0: 用户会话缓存
	Captcha RedisConfig `yaml:"captcha" mapstructure:"captcha"` // DB 1: 验证码存储
}
type WxConfig struct {
	AppId     string `yaml:"app_id" mapstructure:"app_id"`
	AppSecret string `yaml:"app_secret" mapstructure:"app_secret"`
}

// OSSConfig 对象存储配置
type OSSConfig struct {
	Provider string           `yaml:"provider" mapstructure:"provider"` // 存储服务提供商: aliyun, tencent
	Aliyun   AliyunOSSConfig  `yaml:"aliyun" mapstructure:"aliyun"`     // 阿里云 OSS 配置
	Tencent  TencentCOSConfig `yaml:"tencent" mapstructure:"tencent"`   // 腾讯云 COS 配置

	// 通用配置
	UploadPath        string   `yaml:"upload_path" mapstructure:"upload_path"`               // 上传文件的根路径
	MaxFileSize       int64    `yaml:"max_file_size" mapstructure:"max_file_size"`           // 最大文件大小（字节）
	AllowedExtensions []string `yaml:"allowed_extensions" mapstructure:"allowed_extensions"` // 允许上传的文件扩展名
}

// AliyunOSSConfig 阿里云 OSS 配置
type AliyunOSSConfig struct {
	Endpoint        string `yaml:"endpoint" mapstructure:"endpoint"`                   // OSS 访问域名
	AccessKeyID     string `yaml:"access_key_id" mapstructure:"access_key_id"`         // AccessKey ID
	AccessKeySecret string `yaml:"access_key_secret" mapstructure:"access_key_secret"` // AccessKey Secret
	BucketName      string `yaml:"bucket_name" mapstructure:"bucket_name"`             // 存储桶名称
	BaseURL         string `yaml:"base_url" mapstructure:"base_url"`                   // 文件访问的基础 URL
	UseCDN          bool   `yaml:"use_cdn" mapstructure:"use_cdn"`                     // 是否使用 CDN 加速域名
	CDNDomain       string `yaml:"cdn_domain" mapstructure:"cdn_domain"`               // CDN 加速域名
}

// TencentCOSConfig 腾讯云 COS 配置
type TencentCOSConfig struct {
	Region     string `yaml:"region" mapstructure:"region"`           // COS 区域
	SecretID   string `yaml:"secret_id" mapstructure:"secret_id"`     // SecretId
	SecretKey  string `yaml:"secret_key" mapstructure:"secret_key"`   // SecretKey
	BucketName string `yaml:"bucket_name" mapstructure:"bucket_name"` // 存储桶名称（格式: BucketName-APPID）
	BaseURL    string `yaml:"base_url" mapstructure:"base_url"`       // 文件访问的基础 URL
	UseCDN     bool   `yaml:"use_cdn" mapstructure:"use_cdn"`         // 是否使用 CDN 加速域名
	CDNDomain  string `yaml:"cdn_domain" mapstructure:"cdn_domain"`   // CDN 加速域名
}

// GetCurrentProvider  根据 provider 返回当前使用的 OSS 配置信息
// 返回 endpoint/region, bucketName, baseURL, useCDN, cdnDomain
func (c *OSSConfig) GetCurrentProvider() string {
	return c.Provider
}

// IsAliyun 是否使用阿里云 OSS
func (c *OSSConfig) IsAliyun() bool {
	return c.Provider == "aliyun"
}

// IsTencent 是否使用腾讯云 COS
func (c *OSSConfig) IsTencent() bool {
	return c.Provider == "tencent"
}

// GetFileURL 根据配置返回文件的完整访问 URL
func (c *OSSConfig) GetFileURL(objectKey string) string {
	var baseURL string
	var useCDN bool
	var cdnDomain string

	if c.IsAliyun() {
		baseURL = c.Aliyun.BaseURL
		useCDN = c.Aliyun.UseCDN
		cdnDomain = c.Aliyun.CDNDomain
	} else if c.IsTencent() {
		baseURL = c.Tencent.BaseURL
		useCDN = c.Tencent.UseCDN
		cdnDomain = c.Tencent.CDNDomain
	}

	if useCDN && cdnDomain != "" {
		return fmt.Sprintf("%s/%s", cdnDomain, objectKey)
	}

	return fmt.Sprintf("%s/%s", baseURL, objectKey)
}

var Conf *Config

// LoadConfigFromPath 从指定路径加载配置
// 支持通过 GO_ENV 环境变量指定环境（dev/production），默认为 dev
func LoadConfigFromPath(configPath string) (*Config, error) {
	viper.AddConfigPath(configPath)
	viper.SetConfigType("yaml")

	// 从环境变量获取环境配置，默认为 dev
	env := GetEnv()
	configName := fmt.Sprintf("config.%s", env)
	viper.SetConfigName(configName)

	// 尝试读取环境特定的配置文件
	if err := viper.ReadInConfig(); err != nil {
		// 如果环境特定配置不存在，尝试读取默认 config.yaml
		viper.SetConfigName("config")
		if err := viper.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("Load config failed: %s", err)
		}
	}

	config := &Config{}
	if err := viper.Unmarshal(config); err != nil {
		return nil, fmt.Errorf("Unmarshal config failed: %s", err)
	}

	return config, nil
}

// GetEnv 获取当前环境（dev/production）
// 优先级：GO_ENV 环境变量 > 默认值(dev)
func GetEnv() string {
	env := os.Getenv("GO_ENV")
	if env == "" {
		env = "dev"
	}

	// 只允许 dev 和 production
	if env != "dev" && env != "production" {
		return "dev"
	}

	return env
}
