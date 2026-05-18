package core

import (
	"context"
	"time"

	browserautomationv1 "github.com/byte-v-forge/browser-automation/gen/go/byte/v/forge/contracts/browserautomation/v1"
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

type RuntimeProfileDefaults interface {
	DefaultBrowserKind() browserautomationv1.BrowserKind
}

type TaskExecutionResult struct {
	Results   []*CommandResult
	Artifacts []*Artifact
}
