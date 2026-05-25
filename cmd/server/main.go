package main

import (
	"log"
	"net/http"
	"time"

	"code-review/internal/api"
	"code-review/internal/claudecli"
	"code-review/internal/config"
	"code-review/internal/gitlab"
	"code-review/internal/openai"
	"code-review/internal/reviewer"
	"code-review/internal/store"
	"code-review/internal/webhook"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	glClient := gitlab.NewClient(cfg.GitLabURL, cfg.GitLabToken)

	var aiClient reviewer.AIClient
	switch cfg.AIBackend {
	case "claude-cli":
		aiClient = claudecli.NewClient(cfg.ClaudeBinPath, cfg.ClaudeModel, cfg.ClaudeSystemPromptFile, cfg.ClaudeSkill)
		if cfg.ClaudeSkill != "" {
			log.Printf("AI 后端: claude-cli skill=/%s (bin=%s model=%q)", cfg.ClaudeSkill, cfg.ClaudeBinPath, cfg.ClaudeModel)
		} else {
			log.Printf("AI 后端: claude-cli prompt-file=%s (bin=%s model=%q)", cfg.ClaudeSystemPromptFile, cfg.ClaudeBinPath, cfg.ClaudeModel)
		}
	default:
		aiClient = openai.NewClient(cfg.OpenAIAPIKey, cfg.OpenAIModel, cfg.OpenAIBaseURL)
		log.Printf("AI 后端: openai (model=%s)", cfg.OpenAIModel)
	}

	rev := reviewer.New(glClient, aiClient, cfg.MaxDiffBytes)
	jobStore := store.New()

	webhookHandler := webhook.NewHandler(cfg.WebhookSecret, rev)
	apiHandler := api.NewHandler(rev, jobStore, cfg.APIKey, cfg.CORSOrigin, cfg.GitLabURL, cfg.GitLabToken)

	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", webhookHandler.ServeHTTP)
	mux.HandleFunc("/api/review/", apiHandler.HandleReview) // GET /api/review/{job_id}
	mux.HandleFunc("/api/review", apiHandler.HandleReview)  // POST /api/review
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:         ":" + cfg.ServerPort,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 180 * time.Second, // SSE 流式连接可能持续数分钟
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("server starting on port %s", cfg.ServerPort)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
