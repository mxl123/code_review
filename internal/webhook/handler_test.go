package webhook

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// ---- mock Processor ----

type mockProcessor struct {
	mu    sync.Mutex
	calls []processCall
	done  chan struct{} // 每次 Process 调用后向此 channel 发送信号
}

type processCall struct{ projectID, mrIID int }

func newMockProcessor() *mockProcessor {
	return &mockProcessor{done: make(chan struct{}, 10)}
}

func (m *mockProcessor) Process(projectID, mrIID int) {
	m.mu.Lock()
	m.calls = append(m.calls, processCall{projectID, mrIID})
	m.mu.Unlock()
	m.done <- struct{}{}
}

func (m *mockProcessor) waitCall(t *testing.T) {
	t.Helper()
	select {
	case <-m.done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Process to be called")
	}
}

func (m *mockProcessor) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// ---- 辅助函数 ----

func buildRequest(method, token, event string, body interface{}) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, "/webhook", &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Gitlab-Token", token)
	}
	if event != "" {
		req.Header.Set("X-Gitlab-Event", event)
	}
	return req
}

func mrPayload(action string, projectID, mrIID int) map[string]interface{} {
	return map[string]interface{}{
		"object_kind": "merge_request",
		"project":     map[string]interface{}{"id": projectID},
		"object_attributes": map[string]interface{}{
			"iid":    mrIID,
			"action": action,
		},
	}
}

const testSecret = "my-secret"

// ---- HTTP 层测试 ----

func TestHandler_WrongMethod_GET(t *testing.T) {
	proc := newMockProcessor()
	h := NewHandler(testSecret, proc)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, buildRequest(http.MethodGet, testSecret, "Merge Request Hook", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
	if proc.callCount() != 0 {
		t.Error("Process should not be called")
	}
}

func TestHandler_WrongMethod_PUT(t *testing.T) {
	proc := newMockProcessor()
	h := NewHandler(testSecret, proc)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, buildRequest(http.MethodPut, testSecret, "Merge Request Hook", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestHandler_WrongSecret(t *testing.T) {
	proc := newMockProcessor()
	h := NewHandler(testSecret, proc)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, buildRequest(http.MethodPost, "wrong-secret", "Merge Request Hook", mrPayload("open", 1, 1)))

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if proc.callCount() != 0 {
		t.Error("Process should not be called with wrong secret")
	}
}

func TestHandler_EmptySecret(t *testing.T) {
	proc := newMockProcessor()
	h := NewHandler(testSecret, proc)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, buildRequest(http.MethodPost, "", "Merge Request Hook", mrPayload("open", 1, 1)))

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestHandler_NonMREvent(t *testing.T) {
	proc := newMockProcessor()
	h := NewHandler(testSecret, proc)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, buildRequest(http.MethodPost, testSecret, "Push Hook", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if proc.callCount() != 0 {
		t.Error("Process should not be called for non-MR events")
	}
}

func TestHandler_ActionOpen(t *testing.T) {
	proc := newMockProcessor()
	h := NewHandler(testSecret, proc)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, buildRequest(http.MethodPost, testSecret, "Merge Request Hook", mrPayload("open", 42, 7)))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	proc.waitCall(t)
	if proc.callCount() != 1 {
		t.Errorf("Process called %d times, want 1", proc.callCount())
	}
}

func TestHandler_ActionUpdate(t *testing.T) {
	proc := newMockProcessor()
	h := NewHandler(testSecret, proc)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, buildRequest(http.MethodPost, testSecret, "Merge Request Hook", mrPayload("update", 1, 1)))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	proc.waitCall(t)
}

func TestHandler_ActionReopen(t *testing.T) {
	proc := newMockProcessor()
	h := NewHandler(testSecret, proc)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, buildRequest(http.MethodPost, testSecret, "Merge Request Hook", mrPayload("reopen", 1, 1)))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	proc.waitCall(t)
}

func TestHandler_ActionClose(t *testing.T) {
	proc := newMockProcessor()
	h := NewHandler(testSecret, proc)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, buildRequest(http.MethodPost, testSecret, "Merge Request Hook", mrPayload("close", 1, 1)))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	time.Sleep(50 * time.Millisecond) // close 不应触发 Process
	if proc.callCount() != 0 {
		t.Error("Process should not be called for close action")
	}
}

func TestHandler_ActionMerge(t *testing.T) {
	proc := newMockProcessor()
	h := NewHandler(testSecret, proc)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, buildRequest(http.MethodPost, testSecret, "Merge Request Hook", mrPayload("merge", 1, 1)))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	time.Sleep(50 * time.Millisecond)
	if proc.callCount() != 0 {
		t.Error("Process should not be called for merge action")
	}
}

func TestHandler_InvalidJSON(t *testing.T) {
	proc := newMockProcessor()
	h := NewHandler(testSecret, proc)

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewBufferString("not-json"))
	req.Header.Set("X-Gitlab-Token", testSecret)
	req.Header.Set("X-Gitlab-Event", "Merge Request Hook")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if proc.callCount() != 0 {
		t.Error("Process should not be called for invalid JSON")
	}
}

func TestHandler_CorrectProjectAndMRIDs(t *testing.T) {
	proc := newMockProcessor()
	h := NewHandler(testSecret, proc)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, buildRequest(http.MethodPost, testSecret, "Merge Request Hook", mrPayload("open", 99, 12)))

	proc.waitCall(t)

	proc.mu.Lock()
	call := proc.calls[0]
	proc.mu.Unlock()

	if call.projectID != 99 {
		t.Errorf("projectID = %d, want 99", call.projectID)
	}
	if call.mrIID != 12 {
		t.Errorf("mrIID = %d, want 12", call.mrIID)
	}
}

func TestHandler_Returns200BeforeProcessFinishes(t *testing.T) {
	// Process 阻塞 200ms，但 HTTP 响应应该立即返回
	slow := &slowProcessor{delay: 200 * time.Millisecond}
	h := NewHandler(testSecret, slow)

	start := time.Now()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, buildRequest(http.MethodPost, testSecret, "Merge Request Hook", mrPayload("open", 1, 1)))
	elapsed := time.Since(start)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	// HTTP handler 应远快于 Process 的延迟
	if elapsed >= 100*time.Millisecond {
		t.Errorf("handler took %v, should return before Process completes", elapsed)
	}
}

type slowProcessor struct{ delay time.Duration }

func (s *slowProcessor) Process(_, _ int) { time.Sleep(s.delay) }
