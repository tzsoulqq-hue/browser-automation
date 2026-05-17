package app

import (
	"context"
	"errors"
	"time"

	"github.com/byte-v-forge/browser-automation/internal/core"
)

const defaultSessionTTL = 30 * time.Minute

type AutomationService struct {
	store   core.Store
	runtime core.Runtime
	clock   core.Clock
	ids     core.IDGenerator
}

func NewAutomationService(store core.Store, runtime core.Runtime, clock core.Clock, ids core.IDGenerator) *AutomationService {
	if runtime == nil {
		runtime = NoopRuntime{}
	}
	if clock == nil {
		clock = SystemClock{}
	}
	if ids == nil {
		ids = RandomIDGenerator{}
	}
	return &AutomationService{store: store, runtime: runtime, clock: clock, ids: ids}
}

func (s *AutomationService) StartBrowserSession(ctx context.Context, requestID string, profile core.Profile, ttl time.Duration) (core.Session, error) {
	if requestID != "" {
		existing, err := s.store.GetSessionByRequestID(ctx, requestID)
		if err == nil {
			return existing, nil
		}
		if !isSessionNotFound(err) {
			return core.Session{}, err
		}
	}
	if profile.BrowserKind == "" {
		profile.BrowserKind = core.BrowserKindChromium
	}
	if ttl < 0 {
		return core.Session{}, core.NewError(core.CodeValidationFailed, "ttl cannot be negative", false)
	}
	if ttl == 0 {
		ttl = defaultSessionTTL
	}
	now := s.clock.Now()
	if requestID == "" {
		requestID = s.ids.NewID("req_")
	}
	session := core.Session{
		ID:        s.ids.NewID("brsess_"),
		RequestID: requestID,
		Status:    core.SessionStatusStarting,
		Profile:   profile,
		Labels:    cloneMap(profile.Labels),
		CreatedAt: now,
		UpdatedAt: now,
		ExpiresAt: now.Add(ttl),
	}
	if err := s.store.CreateSession(ctx, session); err != nil {
		return core.Session{}, err
	}
	if err := s.runtime.StartSession(ctx, session); err != nil {
		session.Status = core.SessionStatusFailed
		session.LastError = asCoreError(err, core.CodeBrowserUnavailable)
		session.UpdatedAt = s.clock.Now()
		_ = s.store.UpdateSession(ctx, session)
		return session, err
	}
	session.Status = core.SessionStatusRunning
	session.StartedAt = s.clock.Now()
	session.UpdatedAt = session.StartedAt
	if err := s.store.UpdateSession(ctx, session); err != nil {
		return core.Session{}, err
	}
	return session, nil
}

func (s *AutomationService) GetBrowserSession(ctx context.Context, sessionID string) (core.Session, error) {
	session, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		return core.Session{}, err
	}
	return s.expireSessionIfNeeded(ctx, session)
}

func (s *AutomationService) StopBrowserSession(ctx context.Context, sessionID, reason string) (core.Session, error) {
	session, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		return core.Session{}, err
	}
	if session.Status.IsFinal() {
		return session, core.NewError(core.CodeSessionFinalized, "browser session already finalized", false)
	}
	session.Status = core.SessionStatusStopping
	session.UpdatedAt = s.clock.Now()
	if err := s.store.UpdateSession(ctx, session); err != nil {
		return core.Session{}, err
	}
	if err := s.runtime.StopSession(ctx, session, reason); err != nil {
		session.Status = core.SessionStatusFailed
		session.LastError = asCoreError(err, core.CodeBrowserUnavailable)
		session.UpdatedAt = s.clock.Now()
		_ = s.store.UpdateSession(ctx, session)
		return session, err
	}
	session.Status = core.SessionStatusStopped
	session.StoppedAt = s.clock.Now()
	session.UpdatedAt = session.StoppedAt
	if reason != "" {
		if session.Labels == nil {
			session.Labels = make(map[string]string)
		}
		session.Labels["stop_reason"] = reason
	}
	if err := s.store.UpdateSession(ctx, session); err != nil {
		return core.Session{}, err
	}
	return session, nil
}

func (s *AutomationService) StartBrowserTask(ctx context.Context, requestID string, input core.TaskInput) (core.Task, error) {
	if requestID != "" {
		existing, err := s.store.GetTaskByRequestID(ctx, requestID)
		if err == nil {
			return existing, nil
		}
		if !isTaskNotFound(err) {
			return core.Task{}, err
		}
	}
	if err := validateTaskInput(input); err != nil {
		return core.Task{}, err
	}
	session, err := s.GetBrowserSession(ctx, input.SessionID)
	if err != nil {
		return core.Task{}, err
	}
	if session.Status != core.SessionStatusRunning {
		return core.Task{}, core.NewError(core.CodeSessionFinalized, "browser session is not running", false)
	}
	now := s.clock.Now()
	if requestID == "" {
		requestID = s.ids.NewID("req_")
	}
	task := core.Task{
		ID:        s.ids.NewID("brtask_"),
		RequestID: requestID,
		Status:    core.TaskStatusQueued,
		Input:     input,
		Labels:    cloneMap(input.Labels),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.store.CreateTask(ctx, task); err != nil {
		return core.Task{}, err
	}
	if err := s.runtime.EnqueueTask(ctx, task); err != nil {
		task.Status = core.TaskStatusFailed
		task.LastError = asCoreError(err, core.CodeBrowserUnavailable)
		task.UpdatedAt = s.clock.Now()
		task.CompletedAt = task.UpdatedAt
		_ = s.store.UpdateTask(ctx, task)
		return task, err
	}
	return task, nil
}

func (s *AutomationService) GetBrowserTask(ctx context.Context, taskID string) (core.Task, error) {
	return s.store.GetTask(ctx, taskID)
}

func (s *AutomationService) ListBrowserTasks(ctx context.Context, filter core.TaskFilter, pageSize int, pageToken string) (core.TaskListResult, error) {
	return s.store.ListTasks(ctx, filter, pageSize, pageToken)
}

type NoopRuntime struct{}

func (NoopRuntime) StartSession(context.Context, core.Session) error {
	return nil
}

func (NoopRuntime) StopSession(context.Context, core.Session, string) error {
	return nil
}

func (NoopRuntime) EnqueueTask(context.Context, core.Task) error {
	return nil
}

func (s *AutomationService) expireSessionIfNeeded(ctx context.Context, session core.Session) (core.Session, error) {
	now := s.clock.Now()
	if session.ExpiresAt.IsZero() || !now.After(session.ExpiresAt) || session.Status.IsFinal() {
		return session, nil
	}
	session.Status = core.SessionStatusExpired
	session.UpdatedAt = now
	session.StoppedAt = now
	if err := s.store.UpdateSession(ctx, session); err != nil {
		return core.Session{}, err
	}
	return session, nil
}

func validateTaskInput(input core.TaskInput) error {
	if input.SessionID == "" {
		return core.NewError(core.CodeValidationFailed, "session_id is required", false)
	}
	if input.TaskKey == "" {
		return core.NewError(core.CodeValidationFailed, "task_key is required", false)
	}
	if input.Timeout < 0 {
		return core.NewError(core.CodeValidationFailed, "timeout cannot be negative", false)
	}
	return nil
}

func isSessionNotFound(err error) bool {
	var coreErr *core.Error
	return errors.As(err, &coreErr) && coreErr.Code == core.CodeSessionNotFound
}

func isTaskNotFound(err error) bool {
	var coreErr *core.Error
	return errors.As(err, &coreErr) && coreErr.Code == core.CodeTaskNotFound
}

func asCoreError(err error, fallback core.ErrorCode) *core.Error {
	if err == nil {
		return nil
	}
	var coreErr *core.Error
	if errors.As(err, &coreErr) {
		return coreErr
	}
	return core.NewError(fallback, err.Error(), true)
}
