package app

import (
	"context"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/byte-v-forge/browser-automation/internal/core"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
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
	sessions         map[string]*core.Session
	sessionRequestID map[string]string
	tasks            map[string]*core.Task
	taskRequestID    map[string]string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		sessions:         make(map[string]*core.Session),
		sessionRequestID: make(map[string]string),
		tasks:            make(map[string]*core.Task),
		taskRequestID:    make(map[string]string),
	}
}

func NewMemoryStoreWithData(sessions []*core.Session, tasks []*core.Task) *MemoryStore {
	store := NewMemoryStore()
	for _, session := range sessions {
		if session == nil {
			continue
		}
		store.sessions[session.GetSessionId()] = cloneSession(session)
		if session.GetRequestId() != "" {
			store.sessionRequestID[session.GetRequestId()] = session.GetSessionId()
		}
	}
	for _, task := range tasks {
		if task == nil {
			continue
		}
		store.tasks[task.GetTaskId()] = cloneTask(task)
		if task.GetRequestId() != "" {
			store.taskRequestID[task.GetRequestId()] = task.GetTaskId()
		}
	}
	return store
}

func (s *MemoryStore) CreateSession(_ context.Context, session *core.Session) error {
	if session == nil || session.GetSessionId() == "" {
		return core.NewError(core.CodeValidationFailed, "session_id is required", false)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[session.GetSessionId()]; ok {
		return core.NewError(core.CodeValidationFailed, "session already exists", false)
	}
	if session.GetRequestId() != "" {
		if _, ok := s.sessionRequestID[session.GetRequestId()]; ok {
			return core.NewError(core.CodeValidationFailed, "request_id already exists", false)
		}
		s.sessionRequestID[session.GetRequestId()] = session.GetSessionId()
	}
	s.sessions[session.GetSessionId()] = cloneSession(session)
	return nil
}

func (s *MemoryStore) GetSession(_ context.Context, sessionID string) (*core.Session, error) {
	if sessionID == "" {
		return nil, core.NewError(core.CodeValidationFailed, "session_id is required", false)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[sessionID]
	if !ok {
		return nil, core.NewError(core.CodeSessionNotFound, "browser session not found", false)
	}
	return cloneSession(session), nil
}

func (s *MemoryStore) GetSessionByRequestID(_ context.Context, requestID string) (*core.Session, error) {
	if requestID == "" {
		return nil, core.NewError(core.CodeValidationFailed, "request_id is required", false)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	sessionID, ok := s.sessionRequestID[requestID]
	if !ok {
		return nil, core.NewError(core.CodeSessionNotFound, "browser session not found", false)
	}
	return cloneSession(s.sessions[sessionID]), nil
}

func (s *MemoryStore) UpdateSession(_ context.Context, session *core.Session) error {
	if session == nil || session.GetSessionId() == "" {
		return core.NewError(core.CodeValidationFailed, "session_id is required", false)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[session.GetSessionId()]; !ok {
		return core.NewError(core.CodeSessionNotFound, "browser session not found", false)
	}
	s.sessions[session.GetSessionId()] = cloneSession(session)
	if session.GetRequestId() != "" {
		s.sessionRequestID[session.GetRequestId()] = session.GetSessionId()
	}
	return nil
}

func (s *MemoryStore) CreateTask(_ context.Context, task *core.Task) error {
	if task == nil || task.GetTaskId() == "" {
		return core.NewError(core.CodeValidationFailed, "task_id is required", false)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tasks[task.GetTaskId()]; ok {
		return core.NewError(core.CodeValidationFailed, "task already exists", false)
	}
	if task.GetRequestId() != "" {
		if _, ok := s.taskRequestID[task.GetRequestId()]; ok {
			return core.NewError(core.CodeValidationFailed, "request_id already exists", false)
		}
		s.taskRequestID[task.GetRequestId()] = task.GetTaskId()
	}
	s.tasks[task.GetTaskId()] = cloneTask(task)
	return nil
}

func (s *MemoryStore) GetTask(_ context.Context, taskID string) (*core.Task, error) {
	if taskID == "" {
		return nil, core.NewError(core.CodeValidationFailed, "task_id is required", false)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	task, ok := s.tasks[taskID]
	if !ok {
		return nil, core.NewError(core.CodeTaskNotFound, "browser task not found", false)
	}
	return cloneTask(task), nil
}

func (s *MemoryStore) GetTaskByRequestID(_ context.Context, requestID string) (*core.Task, error) {
	if requestID == "" {
		return nil, core.NewError(core.CodeValidationFailed, "request_id is required", false)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	taskID, ok := s.taskRequestID[requestID]
	if !ok {
		return nil, core.NewError(core.CodeTaskNotFound, "browser task not found", false)
	}
	return cloneTask(s.tasks[taskID]), nil
}

func (s *MemoryStore) ListTasks(_ context.Context, filter *core.TaskFilter, pageSize int, pageToken string) (core.TaskListResult, error) {
	offset, err := parsePageToken(pageToken)
	if err != nil {
		return core.TaskListResult{}, err
	}
	pageSize = normalizePageSize(pageSize)
	s.mu.RLock()
	defer s.mu.RUnlock()
	matched := make([]*core.Task, 0, len(s.tasks))
	for _, task := range s.tasks {
		if taskMatches(filter, task) {
			matched = append(matched, cloneTask(task))
		}
	}
	sort.Slice(matched, func(i, j int) bool {
		left := protoTime(matched[i].GetCreatedAt())
		right := protoTime(matched[j].GetCreatedAt())
		if !left.Equal(right) {
			return left.After(right)
		}
		return matched[i].GetTaskId() < matched[j].GetTaskId()
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

func (s *MemoryStore) UpdateTask(_ context.Context, task *core.Task) error {
	if task == nil || task.GetTaskId() == "" {
		return core.NewError(core.CodeValidationFailed, "task_id is required", false)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tasks[task.GetTaskId()]; !ok {
		return core.NewError(core.CodeTaskNotFound, "browser task not found", false)
	}
	s.tasks[task.GetTaskId()] = cloneTask(task)
	if task.GetRequestId() != "" {
		s.taskRequestID[task.GetRequestId()] = task.GetTaskId()
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

func taskMatches(filter *core.TaskFilter, task *core.Task) bool {
	if filter == nil {
		return true
	}
	input := task.GetInput()
	if filter.GetSessionId() != "" && input.GetSessionId() != filter.GetSessionId() {
		return false
	}
	if filter.GetStatus() != 0 && task.GetStatus() != filter.GetStatus() {
		return false
	}
	if filter.GetTaskKey() != "" && input.GetTaskKey() != filter.GetTaskKey() {
		return false
	}
	if filter.GetScenarioKey() != "" && input.GetScenarioKey() != filter.GetScenarioKey() {
		return false
	}
	if filter.GetLabelKey() != "" {
		value, ok := task.GetLabels()[filter.GetLabelKey()]
		if !ok || (filter.GetLabelValue() != "" && value != filter.GetLabelValue()) {
			return false
		}
	}
	if filter.GetCreatedAfter() != nil && protoTime(task.GetCreatedAt()).Before(protoTime(filter.GetCreatedAfter())) {
		return false
	}
	if filter.GetCreatedBefore() != nil && protoTime(task.GetCreatedAt()).After(protoTime(filter.GetCreatedBefore())) {
		return false
	}
	return true
}

func cloneSession(session *core.Session) *core.Session {
	if session == nil {
		return nil
	}
	return proto.Clone(session).(*core.Session)
}

func cloneTask(task *core.Task) *core.Task {
	if task == nil {
		return nil
	}
	return proto.Clone(task).(*core.Task)
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

func protoTime(timestamp *timestamppb.Timestamp) time.Time {
	if timestamp == nil {
		return time.Time{}
	}
	return timestamp.AsTime()
}
