package core

import (
	"context"
	"time"
)

type Clock interface {
	Now() time.Time
}

type IDGenerator interface {
	NewID(prefix string) string
}

type Store interface {
	CreateSession(ctx context.Context, session *Session) error
	GetSession(ctx context.Context, sessionID string) (*Session, error)
	GetSessionByRequestID(ctx context.Context, requestID string) (*Session, error)
	UpdateSession(ctx context.Context, session *Session) error
	AcquireSessionLease(ctx context.Context, sessionID, owner, leaseToken string, now time.Time, ttl time.Duration) (*Session, error)
	RenewSessionLease(ctx context.Context, sessionID, leaseToken string, now time.Time, ttl time.Duration) (*Session, error)
	ReleaseSessionLease(ctx context.Context, sessionID, leaseToken, reason string, now time.Time) (*Session, error)
	CreateTask(ctx context.Context, task *Task) error
	GetTask(ctx context.Context, taskID string) (*Task, error)
	GetTaskByRequestID(ctx context.Context, requestID string) (*Task, error)
	ListTasks(ctx context.Context, filter *TaskFilter, pageSize int, pageToken string) (TaskListResult, error)
	UpdateTask(ctx context.Context, task *Task) error
}

type Runtime interface {
	StartSession(ctx context.Context, session *Session) error
	StopSession(ctx context.Context, session *Session, reason string) error
	EnqueueTask(ctx context.Context, task *Task) error
	ExecuteTask(ctx context.Context, task *Task) (TaskExecutionResult, error)
}

type TaskExecutionResult struct {
	Results   []*CommandResult
	Artifacts []*Artifact
}
