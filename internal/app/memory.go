package app

import (
	"context"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/byte-v-forge/browser-automation/internal/core"
)

const (
	defaultPageSize = 50
	maxPageSize     = 200
)

type SystemClock struct{}

func (SystemClock) Now() time.Time {
	return time.Now().UTC()
}

type MemoryStore struct {
	mu               sync.RWMutex
	sessions         map[string]core.Session
	sessionRequestID map[string]string
	tasks            map[string]core.Task
	taskRequestID    map[string]string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		sessions:         make(map[string]core.Session),
		sessionRequestID: make(map[string]string),
		tasks:            make(map[string]core.Task),
		taskRequestID:    make(map[string]string),
	}
}

func NewMemoryStoreWithData(sessions []core.Session, tasks []core.Task) *MemoryStore {
	store := NewMemoryStore()
	for _, session := range sessions {
		store.sessions[session.ID] = cloneSession(session)
		if session.RequestID != "" {
			store.sessionRequestID[session.RequestID] = session.ID
		}
	}
	for _, task := range tasks {
		store.tasks[task.ID] = cloneTask(task)
		if task.RequestID != "" {
			store.taskRequestID[task.RequestID] = task.ID
		}
	}
	return store
}

func (s *MemoryStore) CreateSession(_ context.Context, session core.Session) error {
	if session.ID == "" {
		return core.NewError(core.CodeValidationFailed, "session_id is required", false)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[session.ID]; ok {
		return core.NewError(core.CodeValidationFailed, "session already exists", false)
	}
	if session.RequestID != "" {
		if _, ok := s.sessionRequestID[session.RequestID]; ok {
			return core.NewError(core.CodeValidationFailed, "request_id already exists", false)
		}
		s.sessionRequestID[session.RequestID] = session.ID
	}
	s.sessions[session.ID] = cloneSession(session)
	return nil
}

func (s *MemoryStore) GetSession(_ context.Context, sessionID string) (core.Session, error) {
	if sessionID == "" {
		return core.Session{}, core.NewError(core.CodeValidationFailed, "session_id is required", false)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[sessionID]
	if !ok {
		return core.Session{}, core.NewError(core.CodeSessionNotFound, "browser session not found", false)
	}
	return cloneSession(session), nil
}

func (s *MemoryStore) GetSessionByRequestID(_ context.Context, requestID string) (core.Session, error) {
	if requestID == "" {
		return core.Session{}, core.NewError(core.CodeValidationFailed, "request_id is required", false)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	sessionID, ok := s.sessionRequestID[requestID]
	if !ok {
		return core.Session{}, core.NewError(core.CodeSessionNotFound, "browser session not found", false)
	}
	return cloneSession(s.sessions[sessionID]), nil
}

func (s *MemoryStore) UpdateSession(_ context.Context, session core.Session) error {
	if session.ID == "" {
		return core.NewError(core.CodeValidationFailed, "session_id is required", false)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[session.ID]; !ok {
		return core.NewError(core.CodeSessionNotFound, "browser session not found", false)
	}
	s.sessions[session.ID] = cloneSession(session)
	if session.RequestID != "" {
		s.sessionRequestID[session.RequestID] = session.ID
	}
	return nil
}

func (s *MemoryStore) CreateTask(_ context.Context, task core.Task) error {
	if task.ID == "" {
		return core.NewError(core.CodeValidationFailed, "task_id is required", false)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tasks[task.ID]; ok {
		return core.NewError(core.CodeValidationFailed, "task already exists", false)
	}
	if task.RequestID != "" {
		if _, ok := s.taskRequestID[task.RequestID]; ok {
			return core.NewError(core.CodeValidationFailed, "request_id already exists", false)
		}
		s.taskRequestID[task.RequestID] = task.ID
	}
	s.tasks[task.ID] = cloneTask(task)
	return nil
}

func (s *MemoryStore) GetTask(_ context.Context, taskID string) (core.Task, error) {
	if taskID == "" {
		return core.Task{}, core.NewError(core.CodeValidationFailed, "task_id is required", false)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	task, ok := s.tasks[taskID]
	if !ok {
		return core.Task{}, core.NewError(core.CodeTaskNotFound, "browser task not found", false)
	}
	return cloneTask(task), nil
}

func (s *MemoryStore) GetTaskByRequestID(_ context.Context, requestID string) (core.Task, error) {
	if requestID == "" {
		return core.Task{}, core.NewError(core.CodeValidationFailed, "request_id is required", false)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	taskID, ok := s.taskRequestID[requestID]
	if !ok {
		return core.Task{}, core.NewError(core.CodeTaskNotFound, "browser task not found", false)
	}
	return cloneTask(s.tasks[taskID]), nil
}

func (s *MemoryStore) ListTasks(_ context.Context, filter core.TaskFilter, pageSize int, pageToken string) (core.TaskListResult, error) {
	offset, err := parsePageToken(pageToken)
	if err != nil {
		return core.TaskListResult{}, err
	}
	pageSize = normalizePageSize(pageSize)
	s.mu.RLock()
	defer s.mu.RUnlock()
	matched := make([]core.Task, 0, len(s.tasks))
	for _, task := range s.tasks {
		if taskMatches(filter, task) {
			matched = append(matched, cloneTask(task))
		}
	}
	sort.Slice(matched, func(i, j int) bool {
		if !matched[i].CreatedAt.Equal(matched[j].CreatedAt) {
			return matched[i].CreatedAt.After(matched[j].CreatedAt)
		}
		return matched[i].ID < matched[j].ID
	})
	if offset >= len(matched) {
		return core.TaskListResult{}, nil
	}
	end := offset + pageSize
	if end > len(matched) {
		end = len(matched)
	}
	nextPageToken := ""
	if end < len(matched) {
		nextPageToken = strconv.Itoa(end)
	}
	return core.TaskListResult{Tasks: matched[offset:end], NextPageToken: nextPageToken}, nil
}

func (s *MemoryStore) UpdateTask(_ context.Context, task core.Task) error {
	if task.ID == "" {
		return core.NewError(core.CodeValidationFailed, "task_id is required", false)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tasks[task.ID]; !ok {
		return core.NewError(core.CodeTaskNotFound, "browser task not found", false)
	}
	s.tasks[task.ID] = cloneTask(task)
	if task.RequestID != "" {
		s.taskRequestID[task.RequestID] = task.ID
	}
	return nil
}

func parsePageToken(pageToken string) (int, error) {
	if pageToken == "" {
		return 0, nil
	}
	offset, err := strconv.Atoi(pageToken)
	if err != nil || offset < 0 {
		return 0, core.NewError(core.CodeValidationFailed, "page_token must be a non-negative offset", false)
	}
	return offset, nil
}

func normalizePageSize(pageSize int) int {
	if pageSize <= 0 {
		return defaultPageSize
	}
	if pageSize > maxPageSize {
		return maxPageSize
	}
	return pageSize
}

func taskMatches(filter core.TaskFilter, task core.Task) bool {
	if filter.SessionID != "" && task.Input.SessionID != filter.SessionID {
		return false
	}
	if filter.Status != "" && task.Status != filter.Status {
		return false
	}
	if filter.TaskKey != "" && task.Input.TaskKey != filter.TaskKey {
		return false
	}
	if filter.ScenarioKey != "" && task.Input.ScenarioKey != filter.ScenarioKey {
		return false
	}
	if filter.LabelKey != "" {
		value, ok := task.Labels[filter.LabelKey]
		if !ok || (filter.LabelValue != "" && value != filter.LabelValue) {
			return false
		}
	}
	if !filter.CreatedAfter.IsZero() && task.CreatedAt.Before(filter.CreatedAfter) {
		return false
	}
	if !filter.CreatedBefore.IsZero() && task.CreatedAt.After(filter.CreatedBefore) {
		return false
	}
	return true
}

func cloneSession(session core.Session) core.Session {
	session.Profile.Labels = cloneMap(session.Profile.Labels)
	if session.LastError != nil {
		errCopy := *session.LastError
		session.LastError = &errCopy
	}
	session.Artifacts = cloneArtifacts(session.Artifacts)
	session.Labels = cloneMap(session.Labels)
	return session
}

func cloneTask(task core.Task) core.Task {
	task.Input.Labels = cloneMap(task.Input.Labels)
	if task.LastError != nil {
		errCopy := *task.LastError
		task.LastError = &errCopy
	}
	task.Artifacts = cloneArtifacts(task.Artifacts)
	task.Labels = cloneMap(task.Labels)
	return task
}

func cloneArtifacts(artifacts []core.Artifact) []core.Artifact {
	out := make([]core.Artifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		artifact.Labels = cloneMap(artifact.Labels)
		out = append(out, artifact)
	}
	return out
}

func cloneMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
