package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	GitLabURL     string
	GitLabToken   string
	WebhookSecret string
	OpenAIAPIKey  string
	OpenAIModel   string
	ServerPort    string
	MaxDiffBytes  int
}

func Load() (*Config, error) {
	cfg := &Config{
		OpenAIModel:  getEnvOrDefault("OPENAI_MODEL", "gpt-4o"),
		ServerPort:   getEnvOrDefault("SERVER_PORT", "8080"),
		MaxDiffBytes: 60000,
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
	if cfg.OpenAIAPIKey == "" {
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
