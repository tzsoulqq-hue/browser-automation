package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/byte-v-forge/browser-automation/internal/core"
	browserautomationv1 "github.com/byte-v-forge/contracts-go/byte/v/forge/contracts/browserautomation/v1"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
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
	startedSessionIDs []string
	stoppedSessionIDs []string
	taskIDs           []string
	executedTaskIDs   []string
	executedInputs    []*core.TaskInput
	err               error
}

func (r *recordingRuntime) StartSession(_ context.Context, session *core.Session) error {
	r.startedSessionIDs = append(r.startedSessionIDs, session.GetSessionId())
	return r.err
}

func (r *recordingRuntime) StopSession(_ context.Context, session *core.Session, _ string) error {
	r.stoppedSessionIDs = append(r.stoppedSessionIDs, session.GetSessionId())
	return r.err
}

func (r *recordingRuntime) EnqueueTask(_ context.Context, task *core.Task) error {
	r.taskIDs = append(r.taskIDs, task.GetTaskId())
	return r.err
}

func (r *recordingRuntime) ExecuteTask(_ context.Context, task *core.Task) (core.TaskExecutionResult, error) {
	r.executedTaskIDs = append(r.executedTaskIDs, task.GetTaskId())
	r.executedInputs = append(r.executedInputs, task.GetInput())
	return core.TaskExecutionResult{
		Results: []*core.CommandResult{{
			CommandId:   task.GetInput().GetCommands()[0].GetCommandId(),
			CommandKey:  task.GetInput().GetCommands()[0].GetCommandKey(),
			Status:      browserautomationv1.BrowserCommandStatus_BROWSER_COMMAND_STATUS_SUCCEEDED,
			CompletedAt: timestamppb.New(baseTime()),
		}},
	}, r.err
}

func TestStartBrowserSessionCreatesRunningSession(t *testing.T) {
	ctx := context.Background()
	runtime := &recordingRuntime{}
	service := newTestService(NewMemoryStore(), runtime, []string{"session-1"})

	session, err := service.StartBrowserSession(ctx, "req-1", &browserautomationv1.BrowserProfile{
		Locale: "en-US",
		Labels: map[string]string{"pool": "default"},
	}, 0)
	if err != nil {
		t.Fatalf("StartBrowserSession() error = %v", err)
	}
	if session.GetSessionId() != "session-1" {
		t.Fatalf("session_id = %q, want session-1", session.GetSessionId())
	}
	if session.GetStatus() != browserautomationv1.BrowserSessionStatus_BROWSER_SESSION_STATUS_RUNNING {
		t.Fatalf("status = %q, want running", session.GetStatus())
	}
	if session.GetProfile().GetBrowserKind() != browserautomationv1.BrowserKind_BROWSER_KIND_CHROMIUM {
		t.Fatalf("browser_kind = %q, want chromium", session.GetProfile().GetBrowserKind())
	}
	if session.GetExpiresAt() == nil || session.GetStartedAt() == nil {
		t.Fatal("timestamps should be filled")
	}
	if len(runtime.startedSessionIDs) != 1 || runtime.startedSessionIDs[0] != session.GetSessionId() {
		t.Fatalf("runtime started = %#v, want one session", runtime.startedSessionIDs)
	}
}

func TestStartBrowserSessionIsIdempotentByRequestID(t *testing.T) {
	ctx := context.Background()
	runtime := &recordingRuntime{}
	service := newTestService(NewMemoryStore(), runtime, []string{"session-1"})

	first, err := service.StartBrowserSession(ctx, "req-1", nil, 0)
	if err != nil {
		t.Fatalf("StartBrowserSession() first error = %v", err)
	}
	second, err := service.StartBrowserSession(ctx, "req-1", nil, 0)
	if err != nil {
		t.Fatalf("StartBrowserSession() second error = %v", err)
	}
	if first.GetSessionId() != second.GetSessionId() {
		t.Fatalf("second session id = %q, want %q", second.GetSessionId(), first.GetSessionId())
	}
	if len(runtime.startedSessionIDs) != 1 {
		t.Fatalf("runtime starts = %d, want 1", len(runtime.startedSessionIDs))
	}
}

func TestStartBrowserTaskRequiresRunningSession(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStoreWithData([]*core.Session{{
		SessionId: "session-1",
		Status:    browserautomationv1.BrowserSessionStatus_BROWSER_SESSION_STATUS_STOPPED,
		CreatedAt: timestamppb.New(baseTime()),
		UpdatedAt: timestamppb.New(baseTime()),
	}}, nil)
	service := newTestService(store, &recordingRuntime{}, []string{"task-1"})

	_, err := service.StartBrowserTask(ctx, "task-req-1", &browserautomationv1.BrowserTaskInput{SessionId: "session-1", TaskKey: "login"})
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

	session, err := service.StartBrowserSession(ctx, "session-req-1", nil, 0)
	if err != nil {
		t.Fatalf("StartBrowserSession() error = %v", err)
	}
	task, err := service.StartBrowserTask(ctx, "task-req-1", &core.TaskInput{
		SessionId:   session.GetSessionId(),
		TaskKey:     "register",
		ScenarioKey: "outlook",
		Labels:      map[string]string{"batch": "a"},
	})
	if err != nil {
		t.Fatalf("StartBrowserTask() error = %v", err)
	}
	if task.GetTaskId() != "task-1" || task.GetStatus() != browserautomationv1.BrowserTaskStatus_BROWSER_TASK_STATUS_QUEUED {
		t.Fatalf("task = %#v, want queued task-1", task)
	}
	if len(runtime.taskIDs) != 1 || runtime.taskIDs[0] != task.GetTaskId() {
		t.Fatalf("runtime tasks = %#v, want one task", runtime.taskIDs)
	}

	result, err := service.ListBrowserTasks(ctx, &core.TaskFilter{
		SessionId:   session.GetSessionId(),
		TaskKey:     "register",
		ScenarioKey: "outlook",
		LabelKey:    "batch",
		LabelValue:  "a",
	}, 10, "")
	if err != nil {
		t.Fatalf("ListBrowserTasks() error = %v", err)
	}
	if len(result.Tasks) != 1 || result.Tasks[0].GetTaskId() != task.GetTaskId() {
		t.Fatalf("tasks = %#v, want task", result.Tasks)
	}

	stopped, err := service.StopBrowserSession(ctx, session.GetSessionId(), "done")
	if err != nil {
		t.Fatalf("StopBrowserSession() error = %v", err)
	}
	if stopped.GetStatus() != browserautomationv1.BrowserSessionStatus_BROWSER_SESSION_STATUS_STOPPED || stopped.GetLabels()["stop_reason"] != "done" {
		t.Fatalf("stopped session = %#v", stopped)
	}
}

func TestExecuteBrowserCommandsRunsRuntimeAndStoresResults(t *testing.T) {
	ctx := context.Background()
	runtime := &recordingRuntime{}
	service := newTestService(NewMemoryStore(), runtime, []string{"session-1", "task-1"})

	session, err := service.StartBrowserSession(ctx, "session-req-1", nil, 0)
	if err != nil {
		t.Fatalf("StartBrowserSession() error = %v", err)
	}
	task, err := service.ExecuteBrowserCommands(ctx, "command-req-1", &core.TaskInput{
		SessionId: session.GetSessionId(),
		Commands: []*browserautomationv1.BrowserCommand{{
			CommandId:  "cmd-1",
			CommandKey: "navigate",
			Operation: &browserautomationv1.BrowserCommand_Navigate{
				Navigate: &browserautomationv1.NavigateCommand{
					Url:     "https://example.test",
					Timeout: durationpb.New(time.Second),
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("ExecuteBrowserCommands() error = %v", err)
	}
	if task.GetStatus() != browserautomationv1.BrowserTaskStatus_BROWSER_TASK_STATUS_SUCCEEDED {
		t.Fatalf("task status = %q, want succeeded", task.GetStatus())
	}
	if task.GetInput().GetTaskKey() != "browser.commands" {
		t.Fatalf("task key = %q, want browser.commands", task.GetInput().GetTaskKey())
	}
	if len(runtime.executedTaskIDs) != 1 || runtime.executedTaskIDs[0] != task.GetTaskId() {
		t.Fatalf("runtime executed = %#v, want one task", runtime.executedTaskIDs)
	}
	stored, err := service.GetBrowserTask(ctx, task.GetTaskId())
	if err != nil {
		t.Fatalf("GetBrowserTask() error = %v", err)
	}
	if len(stored.GetResults()) != 1 || stored.GetResults()[0].GetCommandId() != "cmd-1" {
		t.Fatalf("stored results = %#v, want command result", stored.GetResults())
	}
}

func TestExecuteBrowserCommandsAcceptsSelectorGroupAndSelectOption(t *testing.T) {
	ctx := context.Background()
	runtime := &recordingRuntime{}
	service := newTestService(NewMemoryStore(), runtime, []string{"session-1", "task-1"})

	session, err := service.StartBrowserSession(ctx, "session-req-1", nil, 0)
	if err != nil {
		t.Fatalf("StartBrowserSession() error = %v", err)
	}
	_, err = service.ExecuteBrowserCommands(ctx, "command-req-1", &core.TaskInput{
		SessionId: session.GetSessionId(),
		Commands: []*browserautomationv1.BrowserCommand{{
			CommandId:       "cmd-1",
			CommandKey:      "select_month",
			ContinueOnError: true,
			Operation: &browserautomationv1.BrowserCommand_SelectOption{
				SelectOption: &browserautomationv1.SelectOptionCommand{
					SelectorGroup: &browserautomationv1.BrowserSelectorGroup{
						Selectors: []*browserautomationv1.BrowserSelector{
							{Kind: browserautomationv1.BrowserSelectorKind_BROWSER_SELECTOR_KIND_LABEL, Value: "Month", Exact: true},
							{Kind: browserautomationv1.BrowserSelectorKind_BROWSER_SELECTOR_KIND_CSS, Value: "select[name='BirthMonth']"},
						},
					},
					Values: []string{"1"},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("ExecuteBrowserCommands() error = %v", err)
	}
	if len(runtime.executedInputs) != 1 {
		t.Fatalf("executed inputs = %d, want 1", len(runtime.executedInputs))
	}
	command := runtime.executedInputs[0].GetCommands()[0]
	selectOption := command.GetSelectOption()
	if !command.GetContinueOnError() || len(selectOption.GetSelectorGroup().GetSelectors()) != 2 {
		t.Fatalf("command = %#v, want structured selector group and continue_on_error", command)
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
