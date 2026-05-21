package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"code-review/internal/gitlab"
	"code-review/internal/store"
)

// Runner 是 reviewer.Reviewer 的接口抽象，方便测试。
type Runner interface {
	RunReview(gl gitlab.GitLabClient, projectID, mrIID int) (string, error)
	RunReviewStream(gl gitlab.GitLabClient, projectID, mrIID int, onToken func(string)) error
}

type Handler struct {
	runner      Runner
	store       *store.Store
	apiKey      string // 为空时不校验
	corsOrigin  string // 允许的跨域来源，为空时允许所有
	gitlabURL   string // 服务端配置的 GitLab 地址（作为 fallback）
	gitlabToken string // 服务端配置的 GitLab token（作为 fallback）
}

func NewHandler(runner Runner, store *store.Store, apiKey, corsOrigin, gitlabURL, gitlabToken string) *Handler {
	return &Handler{
		runner:      runner,
		store:       store,
		apiKey:      apiKey,
		corsOrigin:  corsOrigin,
		gitlabURL:   gitlabURL,
		gitlabToken: gitlabToken,
	}
}

// HandleReview 处理 POST /api/review 和 GET /api/review/{job_id}。
func (h *Handler) HandleReview(w http.ResponseWriter, r *http.Request) {
	h.setCORS(w, r)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !h.checkAPIKey(w, r) {
		return
	}

	// 路径分发：
	//   /api/review          → 触发审查（轮询模式）
	//   /api/review/stream   → 触发审查（SSE 流式模式）
	//   /api/review/{job_id} → 查询轮询结果
	suffix := strings.TrimPrefix(r.URL.Path, "/api/review")
	suffix = strings.TrimPrefix(suffix, "/")

	switch suffix {
	case "":
		h.triggerReview(w, r)
	case "stream":
		h.triggerStream(w, r)
	default:
		h.getResult(w, r, suffix)
	}
}

type triggerRequest struct {
	ProjectID   int    `json:"project_id"`
	MRIID       int    `json:"mr_iid"`
	GitLabToken string `json:"gitlab_token"` // 可选，用用户自己的 read_api token
}

func (h *Handler) triggerReview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req triggerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "请求体 JSON 解析失败", http.StatusBadRequest)
		return
	}
	if req.ProjectID == 0 || req.MRIID == 0 {
		jsonError(w, "project_id 和 mr_iid 不能为空", http.StatusBadRequest)
		return
	}

	// 优先使用请求中的 token，否则 fallback 到服务端配置的 token
	token := req.GitLabToken
	if token == "" {
		token = h.gitlabToken
	}

	gl := gitlab.NewClient(h.gitlabURL, token)
	job := h.store.Create()

	log.Printf("api: 触发审查 job=%s project=%d mr=%d", job.ID, req.ProjectID, req.MRIID)

	go func() {
		result, err := h.runner.RunReview(gl, req.ProjectID, req.MRIID)
		if err != nil {
			log.Printf("api: 审查失败 job=%s: %v", job.ID, err)
			h.store.SetFailed(job.ID, err.Error())
			return
		}
		log.Printf("api: 审查完成 job=%s", job.ID)
		h.store.SetDone(job.ID, result)
	}()

	jsonOK(w, map[string]string{
		"job_id": job.ID,
		"status": store.StatusPending,
	})
}

func (h *Handler) getResult(w http.ResponseWriter, r *http.Request, jobID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	job, ok := h.store.Get(jobID)
	if !ok {
		jsonError(w, "job 不存在或已过期", http.StatusNotFound)
		return
	}

	resp := map[string]string{"job_id": job.ID, "status": job.Status}
	if job.Result != "" {
		resp["result"] = job.Result
	}
	if job.Error != "" {
		resp["error"] = job.Error
	}
	jsonOK(w, resp)
}

func (h *Handler) setCORS(w http.ResponseWriter, r *http.Request) {
	origin := h.corsOrigin
	if origin == "" {
		origin = r.Header.Get("Origin")
		if origin == "" {
			origin = "*"
		}
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key")
	// Chrome Private Network Access 策略：公网页面访问 localhost 时需要此头
	if r.Header.Get("Access-Control-Request-Private-Network") == "true" {
		w.Header().Set("Access-Control-Allow-Private-Network", "true")
	}
}

func (h *Handler) checkAPIKey(w http.ResponseWriter, r *http.Request) bool {
	if h.apiKey == "" {
		return true
	}
	if r.Header.Get("X-API-Key") != h.apiKey {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

func jsonOK(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// triggerStream 处理 POST /api/review/stream，以 SSE 流式返回审查结果。
func (h *Handler) triggerStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req triggerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.ProjectID == 0 || req.MRIID == 0 {
		http.Error(w, "project_id 和 mr_iid 不能为空", http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	h.setCORS(w, r)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no") // 关闭 nginx 缓冲，确保实时推送

	token := req.GitLabToken
	if token == "" {
		token = h.gitlabToken
	}
	gl := gitlab.NewClient(h.gitlabURL, token)

	log.Printf("api: 流式审查开始 project=%d mr=%d", req.ProjectID, req.MRIID)

	err := h.runner.RunReviewStream(gl, req.ProjectID, req.MRIID, func(chunk string) {
		sendSSE(w, flusher, "token", chunk)
	})

	if err != nil {
		log.Printf("api: 流式审查失败 project=%d mr=%d: %v", req.ProjectID, req.MRIID, err)
		sendSSE(w, flusher, "error", err.Error())
		return
	}

	log.Printf("api: 流式审查完成 project=%d mr=%d", req.ProjectID, req.MRIID)
	sendSSE(w, flusher, "done", "")
}

// sendSSE 向客户端发送一个 SSE 事件并立即 flush。
func sendSSE(w http.ResponseWriter, f http.Flusher, event, data string) {
	payload, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload)
	f.Flush()
}
