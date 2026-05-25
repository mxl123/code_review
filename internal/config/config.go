package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	GitLabURL     string
	GitLabToken   string
	WebhookSecret string
	OpenAIAPIKey  string
	OpenAIBaseURL string // 可选，用于对接 sub2api 等兼容平台；留空则使用 OpenAI 官方接口
	OpenAIModel   string
	ServerPort    string
	MaxDiffBytes  int
	APIKey        string // 可选，Chrome 插件调用 /api/* 时携带的鉴权 Key；为空则不校验
	CORSOrigin    string // 可选，允许的跨域来源，如 https://gitlab.example.com；为空则允许所有

	AIBackend     string // "openai"（默认）或 "claude-cli"
	ClaudeBinPath string // claude 二进制路径，默认 "claude"
	ClaudeModel   string // 可选，AI_BACKEND=claude-cli 时的模型覆盖
}

func Load() (*Config, error) {
	// 尝试加载 .env 文件，文件不存在时静默跳过，生产环境无影响
	_ = godotenv.Load()

	cfg := &Config{
		OpenAIModel:   getEnvOrDefault("OPENAI_MODEL", "gpt-4o"),
		ServerPort:    getEnvOrDefault("SERVER_PORT", "8080"),
		MaxDiffBytes:  60000,
		AIBackend:     getEnvOrDefault("AI_BACKEND", "openai"),
		ClaudeBinPath: getEnvOrDefault("CLAUDE_BIN_PATH", "claude"),
	}

	if v := os.Getenv("MAX_DIFF_BYTES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid MAX_DIFF_BYTES: %w", err)
		}
		cfg.MaxDiffBytes = n
	}

	cfg.GitLabURL = os.Getenv("GITLAB_URL")
	cfg.GitLabToken = os.Getenv("GITLAB_TOKEN")
	cfg.WebhookSecret = os.Getenv("WEBHOOK_SECRET")
	cfg.OpenAIAPIKey = os.Getenv("OPENAI_API_KEY")
	cfg.OpenAIBaseURL = os.Getenv("OPENAI_BASE_URL") // 可选
	cfg.APIKey = os.Getenv("API_KEY")                // 可选
	cfg.CORSOrigin = os.Getenv("CORS_ORIGIN")        // 可选
	cfg.ClaudeModel = os.Getenv("CLAUDE_MODEL")      // 可选

	if cfg.AIBackend != "openai" && cfg.AIBackend != "claude-cli" {
		return nil, fmt.Errorf("AI_BACKEND 必须为 'openai' 或 'claude-cli'，当前值: %q", cfg.AIBackend)
	}

	var missing []string
	if cfg.GitLabURL == "" {
		missing = append(missing, "GITLAB_URL")
	}
	if cfg.GitLabToken == "" {
		missing = append(missing, "GITLAB_TOKEN")
	}
	if cfg.WebhookSecret == "" {
		missing = append(missing, "WEBHOOK_SECRET")
	}
	// OPENAI_API_KEY 仅在使用 openai 后端时必填
	if cfg.AIBackend != "claude-cli" && cfg.OpenAIAPIKey == "" {
		missing = append(missing, "OPENAI_API_KEY")
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required environment variables: %v", missing)
	}

	return cfg, nil
}

func getEnvOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
