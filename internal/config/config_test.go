package config

import (
	"strings"
	"testing"
)

// setRequiredEnv 设置所有必填变量，返回用于清理的 key 列表。
func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GITLAB_URL", "https://gitlab.example.com")
	t.Setenv("GITLAB_TOKEN", "glpat-test")
	t.Setenv("WEBHOOK_SECRET", "secret")
	t.Setenv("OPENAI_API_KEY", "sk-test")
}

func TestLoad_AllVarsSet(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("OPENAI_MODEL", "gpt-4-turbo")
	t.Setenv("SERVER_PORT", "9090")
	t.Setenv("MAX_DIFF_BYTES", "50000")
	t.Setenv("OPENAI_BASE_URL", "https://sub2api.example.com/v1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.GitLabURL != "https://gitlab.example.com" {
		t.Errorf("GitLabURL = %q", cfg.GitLabURL)
	}
	if cfg.OpenAIModel != "gpt-4-turbo" {
		t.Errorf("OpenAIModel = %q", cfg.OpenAIModel)
	}
	if cfg.ServerPort != "9090" {
		t.Errorf("ServerPort = %q", cfg.ServerPort)
	}
	if cfg.MaxDiffBytes != 50000 {
		t.Errorf("MaxDiffBytes = %d", cfg.MaxDiffBytes)
	}
	if cfg.OpenAIBaseURL != "https://sub2api.example.com/v1" {
		t.Errorf("OpenAIBaseURL = %q", cfg.OpenAIBaseURL)
	}
}

func TestLoad_Defaults(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.OpenAIModel != "gpt-4o" {
		t.Errorf("default OpenAIModel = %q, want gpt-4o", cfg.OpenAIModel)
	}
	if cfg.ServerPort != "8080" {
		t.Errorf("default ServerPort = %q, want 8080", cfg.ServerPort)
	}
	if cfg.MaxDiffBytes != 60000 {
		t.Errorf("default MaxDiffBytes = %d, want 60000", cfg.MaxDiffBytes)
	}
	if cfg.OpenAIBaseURL != "" {
		t.Errorf("default OpenAIBaseURL = %q, want empty", cfg.OpenAIBaseURL)
	}
}

func TestLoad_MissingGitLabURL(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("GITLAB_URL", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "GITLAB_URL") {
		t.Errorf("error %q should mention GITLAB_URL", err)
	}
}

func TestLoad_MissingGitLabToken(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("GITLAB_TOKEN", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "GITLAB_TOKEN") {
		t.Errorf("error %q should mention GITLAB_TOKEN", err)
	}
}

func TestLoad_MissingWebhookSecret(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("WEBHOOK_SECRET", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "WEBHOOK_SECRET") {
		t.Errorf("error %q should mention WEBHOOK_SECRET", err)
	}
}

func TestLoad_MissingOpenAIAPIKey(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("OPENAI_API_KEY", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Errorf("error %q should mention OPENAI_API_KEY", err)
	}
}

func TestLoad_MultipleMissing(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("GITLAB_TOKEN", "")
	t.Setenv("OPENAI_API_KEY", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "GITLAB_TOKEN") || !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Errorf("error %q should mention both missing vars", err)
	}
}

func TestLoad_InvalidMaxDiffBytes(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("MAX_DIFF_BYTES", "notanumber")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "MAX_DIFF_BYTES") {
		t.Errorf("error %q should mention MAX_DIFF_BYTES", err)
	}
}

func TestLoad_MaxDiffBytesZero(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("MAX_DIFF_BYTES", "0")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.MaxDiffBytes != 0 {
		t.Errorf("MaxDiffBytes = %d, want 0", cfg.MaxDiffBytes)
	}
}

func TestLoad_ClaudeCLIBackend_NoOpenAIKeyRequired(t *testing.T) {
	t.Setenv("GITLAB_URL", "https://gitlab.example.com")
	t.Setenv("GITLAB_TOKEN", "glpat-test")
	t.Setenv("WEBHOOK_SECRET", "secret")
	t.Setenv("AI_BACKEND", "claude-cli")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error with claude-cli backend: %v", err)
	}
	if cfg.AIBackend != "claude-cli" {
		t.Errorf("AIBackend = %q, want claude-cli", cfg.AIBackend)
	}
	if cfg.ClaudeBinPath != "claude" {
		t.Errorf("ClaudeBinPath = %q, want 'claude'", cfg.ClaudeBinPath)
	}
	if cfg.MaxDiffBytes != 150000 {
		t.Errorf("claude-cli default MaxDiffBytes = %d, want 150000", cfg.MaxDiffBytes)
	}
}

func TestLoad_InvalidAIBackend(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("AI_BACKEND", "gpt-magic")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid AI_BACKEND")
	}
	if !strings.Contains(err.Error(), "AI_BACKEND") {
		t.Errorf("error %q should mention AI_BACKEND", err)
	}
}

func TestLoad_ClaudeBinPathDefault(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ClaudeBinPath != "claude" {
		t.Errorf("ClaudeBinPath default = %q, want 'claude'", cfg.ClaudeBinPath)
	}
}
