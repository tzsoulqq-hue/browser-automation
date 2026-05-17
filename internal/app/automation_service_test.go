package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/byte-v-forge/browser-automation/internal/core"
)

type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	return c.now
}

type sequenceID struct {
	values []string
}

func (s *sequenceID) NewID(_ string) string {
	value := s.values[0]
	s.values = s.values[1:]
	return value
}

type recordingRuntime struct {
	started []core.Session
	stopped []core.Session
	tasks   []core.Task
	err     error
}

func (r *recordingRuntime) StartSession(_ context.Context, session core.Session) error {
	r.started = append(r.started, session)
	return r.err
}

func (r *recordingRuntime) StopSession(_ context.Context, session core.Session, _ string) error {
	r.stopped = append(r.stopped, session)
	return r.err
}

func (r *recordingRuntime) EnqueueTask(_ context.Context, task core.Task) error {
	r.tasks = append(r.tasks, task)
	return r.err
}

func TestStartBrowserSessionCreatesRunningSession(t *testing.T) {
	ctx := context.Background()
	runtime := &recordingRuntime{}
	service := newTestService(NewMemoryStore(), runtime, []string{"session-1"})

	session, err := service.StartBrowserSession(ctx, "req-1", core.Profile{
		Locale: "en-US",
		Labels: map[string]string{"pool": "default"},
	}, 0)
	if err != nil {
		t.Fatalf("StartBrowserSession() error = %v", err)
	}
	if session.ID != "session-1" {
		t.Fatalf("session_id = %q, want session-1", session.ID)
	}
	if session.Status != core.SessionStatusRunning {
		t.Fatalf("status = %q, want running", session.Status)
	}
	if session.Profile.BrowserKind != core.BrowserKindChromium {
		t.Fatalf("browser_kind = %q, want chromium", session.Profile.BrowserKind)
	}
	if session.ExpiresAt.IsZero() || session.StartedAt.IsZero() {
		t.Fatal("timestamps should be filled")
	}
	if len(runtime.started) != 1 || runtime.started[0].ID != session.ID {
		t.Fatalf("runtime started = %#v, want one session", runtime.started)
	}
}

func TestStartBrowserSessionIsIdempotentByRequestID(t *testing.T) {
	ctx := context.Background()
	runtime := &recordingRuntime{}
	service := newTestService(NewMemoryStore(), runtime, []string{"session-1"})

	first, err := service.StartBrowserSession(ctx, "req-1", core.Profile{}, 0)
	if err != nil {
		t.Fatalf("StartBrowserSession() first error = %v", err)
	}
	second, err := service.StartBrowserSession(ctx, "req-1", core.Profile{}, 0)
	if err != nil {
		t.Fatalf("StartBrowserSession() second error = %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("second session id = %q, want %q", second.ID, first.ID)
	}
	if len(runtime.started) != 1 {
		t.Fatalf("runtime starts = %d, want 1", len(runtime.started))
	}
}

func TestStartBrowserTaskRequiresRunningSession(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStoreWithData([]core.Session{{
		ID:        "session-1",
		Status:    core.SessionStatusStopped,
		CreatedAt: baseTime(),
		UpdatedAt: baseTime(),
	}}, nil)
	service := newTestService(store, &recordingRuntime{}, []string{"task-1"})

	_, err := service.StartBrowserTask(ctx, "task-req-1", core.TaskInput{SessionID: "session-1", TaskKey: "login"})
	if err == nil {
		t.Fatal("StartBrowserTask() expected error")
	}
	var coreErr *core.Error
	if !errors.As(err, &coreErr) || coreErr.Code != core.CodeSessionFinalized {
		t.Fatalf("StartBrowserTask() error = %#v, want session finalized", err)
	}
}

func TestStartTaskStopSessionAndListTasks(t *testing.T) {
	ctx := context.Background()
	runtime := &recordingRuntime{}
	service := newTestService(NewMemoryStore(), runtime, []string{"session-1", "task-1"})

	session, err := service.StartBrowserSession(ctx, "session-req-1", core.Profile{}, 0)
	if err != nil {
		t.Fatalf("StartBrowserSession() error = %v", err)
	}
	task, err := service.StartBrowserTask(ctx, "task-req-1", core.TaskInput{
		SessionID:   session.ID,
		TaskKey:     "register",
		ScenarioKey: "outlook",
		Labels:      map[string]string{"batch": "a"},
	})
	if err != nil {
		t.Fatalf("StartBrowserTask() error = %v", err)
	}
	if task.ID != "task-1" || task.Status != core.TaskStatusQueued {
		t.Fatalf("task = %#v, want queued task-1", task)
	}
	if len(runtime.tasks) != 1 || runtime.tasks[0].ID != task.ID {
		t.Fatalf("runtime tasks = %#v, want one task", runtime.tasks)
	}

	result, err := service.ListBrowserTasks(ctx, core.TaskFilter{
		SessionID:   session.ID,
		TaskKey:     "register",
		ScenarioKey: "outlook",
		LabelKey:    "batch",
		LabelValue:  "a",
	}, 10, "")
	if err != nil {
		t.Fatalf("ListBrowserTasks() error = %v", err)
	}
	if len(result.Tasks) != 1 || result.Tasks[0].ID != task.ID {
		t.Fatalf("tasks = %#v, want task", result.Tasks)
	}

	stopped, err := service.StopBrowserSession(ctx, session.ID, "done")
	if err != nil {
		t.Fatalf("StopBrowserSession() error = %v", err)
	}
	if stopped.Status != core.SessionStatusStopped || stopped.Labels["stop_reason"] != "done" {
		t.Fatalf("stopped session = %#v", stopped)
	}
}

func newTestService(store *MemoryStore, runtime core.Runtime, ids []string) *AutomationService {
	return NewAutomationService(
		store,
		runtime,
		&fakeClock{now: baseTime()},
		&sequenceID{values: ids},
	)
}

func baseTime() time.Time {
	return time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
}
