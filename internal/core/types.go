package core

import (
	"fmt"
	"time"
)

type ErrorCode string

const (
	CodeValidationFailed     ErrorCode = "validation_failed"
	CodeSessionNotFound      ErrorCode = "session_not_found"
	CodeTaskNotFound         ErrorCode = "task_not_found"
	CodeArtifactNotFound     ErrorCode = "artifact_not_found"
	CodeSessionFinalized     ErrorCode = "session_already_finalized"
	CodeTaskFinalized        ErrorCode = "task_already_finalized"
	CodeCapacityUnavailable  ErrorCode = "capacity_unavailable"
	CodeBrowserUnavailable   ErrorCode = "browser_unavailable"
	CodeProxyFailed          ErrorCode = "proxy_failed"
	CodeNavigationFailed     ErrorCode = "navigation_failed"
	CodeScriptFailed         ErrorCode = "script_failed"
	CodeTimeout              ErrorCode = "timeout"
	CodeUnsupportedOperation ErrorCode = "unsupported_operation"
	CodeInternal             ErrorCode = "internal"
)

type Error struct {
	Code      ErrorCode
	Message   string
	Retryable bool
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message == "" {
		return string(e.Code)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func NewError(code ErrorCode, message string, retryable bool) *Error {
	return &Error{Code: code, Message: message, Retryable: retryable}
}

type BrowserKind string

const (
	BrowserKindChromium BrowserKind = "chromium"
	BrowserKindFirefox  BrowserKind = "firefox"
	BrowserKindWebKit   BrowserKind = "webkit"
)

type SessionStatus string

const (
	SessionStatusStarting SessionStatus = "starting"
	SessionStatusRunning  SessionStatus = "running"
	SessionStatusStopping SessionStatus = "stopping"
	SessionStatusStopped  SessionStatus = "stopped"
	SessionStatusFailed   SessionStatus = "failed"
	SessionStatusExpired  SessionStatus = "expired"
)

func (s SessionStatus) IsFinal() bool {
	switch s {
	case SessionStatusStopped, SessionStatusFailed, SessionStatusExpired:
		return true
	default:
		return false
	}
}

type TaskStatus string

const (
	TaskStatusQueued    TaskStatus = "queued"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusSucceeded TaskStatus = "succeeded"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCanceled  TaskStatus = "canceled"
	TaskStatusTimeout   TaskStatus = "timeout"
)

func (s TaskStatus) IsFinal() bool {
	switch s {
	case TaskStatusSucceeded, TaskStatusFailed, TaskStatusCanceled, TaskStatusTimeout:
		return true
	default:
		return false
	}
}

type ArtifactKind string

const (
	ArtifactKindScreenshot ArtifactKind = "screenshot"
	ArtifactKindVideo      ArtifactKind = "video"
	ArtifactKindTrace      ArtifactKind = "trace"
	ArtifactKindHAR        ArtifactKind = "har"
	ArtifactKindConsoleLog ArtifactKind = "console_log"
	ArtifactKindNetworkLog ArtifactKind = "network_log"
	ArtifactKindDownload   ArtifactKind = "download"
)

type Viewport struct {
	Width             int32
	Height            int32
	DeviceScaleFactor float64
	Mobile            bool
}

type Profile struct {
	BrowserKind           BrowserKind
	Locale                string
	Timezone              string
	UserAgent             string
	Viewport              Viewport
	ProxyRef              string
	StorageStateSecretRef string
	Labels                map[string]string
}

type Artifact struct {
	ID          string
	Kind        ArtifactKind
	URI         string
	ContentType string
	SizeBytes   int64
	Labels      map[string]string
	CreatedAt   time.Time
}

type Session struct {
	ID        string
	RequestID string
	Status    SessionStatus
	Profile   Profile
	LastError *Error
	Artifacts []Artifact
	Labels    map[string]string
	CreatedAt time.Time
	StartedAt time.Time
	UpdatedAt time.Time
	StoppedAt time.Time
	ExpiresAt time.Time
}

type TaskInput struct {
	SessionID   string
	TaskKey     string
	ScenarioKey string
	TargetURL   string
	Timeout     time.Duration
	Labels      map[string]string
}

type Task struct {
	ID          string
	RequestID   string
	Status      TaskStatus
	Input       TaskInput
	LastError   *Error
	Artifacts   []Artifact
	Labels      map[string]string
	CreatedAt   time.Time
	StartedAt   time.Time
	UpdatedAt   time.Time
	CompletedAt time.Time
}

type TaskFilter struct {
	SessionID     string
	Status        TaskStatus
	TaskKey       string
	ScenarioKey   string
	LabelKey      string
	LabelValue    string
	CreatedAfter  time.Time
	CreatedBefore time.Time
}

type TaskListResult struct {
	Tasks         []Task
	NextPageToken string
}
