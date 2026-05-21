package main

import (
	"log"
	"net/http"
	"time"

	"code-review/internal/api"
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
	aiClient := openai.NewClient(cfg.OpenAIAPIKey, cfg.OpenAIModel, cfg.OpenAIBaseURL)
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
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("server starting on port %s", cfg.ServerPort)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
