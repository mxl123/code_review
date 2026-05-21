package store

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

const (
	StatusPending = "pending"
	StatusDone    = "done"
	StatusFailed  = "failed"

	jobTTL = time.Hour // 超过此时间的 job 自动清理
)

type Job struct {
	ID        string
	Status    string
	Result    string
	Error     string
	CreatedAt time.Time
}

type Store struct {
	mu   sync.RWMutex
	jobs map[string]*Job
}

func New() *Store {
	s := &Store{jobs: make(map[string]*Job)}
	go s.cleanupLoop()
	return s
}

func (s *Store) Create() *Job {
	job := &Job{
		ID:        newID(),
		Status:    StatusPending,
		CreatedAt: time.Now(),
	}
	s.mu.Lock()
	s.jobs[job.ID] = job
	s.mu.Unlock()
	return job
}

func (s *Store) Get(id string) (*Job, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[id]
	return j, ok
}

func (s *Store) SetDone(id, result string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j, ok := s.jobs[id]; ok {
		j.Status = StatusDone
		j.Result = result
	}
}

func (s *Store) SetFailed(id, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j, ok := s.jobs[id]; ok {
		j.Status = StatusFailed
		j.Error = errMsg
	}
}

// cleanupLoop 每隔 10 分钟清理过期的 job，防止内存持续增长。
func (s *Store) cleanupLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		for id, j := range s.jobs {
			if time.Since(j.CreatedAt) > jobTTL {
				delete(s.jobs, id)
			}
		}
		s.mu.Unlock()
	}
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
