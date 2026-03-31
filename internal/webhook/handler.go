package webhook

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"log"
	"net/http"

	"code-review/internal/reviewer"
)

type mrEvent struct {
	ObjectKind       string       `json:"object_kind"`
	ObjectAttributes mrAttributes `json:"object_attributes"`
	Project          projectInfo  `json:"project"`
}

type mrAttributes struct {
	IID    int    `json:"iid"`
	Action string `json:"action"`
}

type projectInfo struct {
	ID int `json:"id"`
}

type Handler struct {
	secret   []byte
	reviewer *reviewer.Reviewer
}

func NewHandler(secret string, r *reviewer.Reviewer) *Handler {
	return &Handler{
		secret:   []byte(secret),
		reviewer: r,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := []byte(r.Header.Get("X-Gitlab-Token"))
	if subtle.ConstantTimeCompare(token, h.secret) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if r.Header.Get("X-Gitlab-Event") != "Merge Request Hook" {
		w.WriteHeader(http.StatusOK)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 限制请求体最大 1MB，防止内存耗尽
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}

	var event mrEvent
	if err := json.Unmarshal(body, &event); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	action := event.ObjectAttributes.Action
	if action != "open" && action != "update" && action != "reopen" {
		w.WriteHeader(http.StatusOK)
		return
	}

	projectID := event.Project.ID
	mrIID := event.ObjectAttributes.IID
	log.Printf("received MR event: action=%s project=%d mr=%d", action, projectID, mrIID)

	// 立即返回 200，审查在后台异步执行，避免 GitLab webhook 超时重试
	w.WriteHeader(http.StatusOK)
	go h.reviewer.Process(projectID, mrIID)
}
