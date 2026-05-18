package app

import (
	"context"
	"errors"
	"time"

	browserautomationv1 "github.com/byte-v-forge/browser-automation/gen/go/byte/v/forge/contracts/browserautomation/v1"
	"github.com/byte-v-forge/browser-automation/internal/core"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
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

func (s *AutomationService) StartBrowserSession(ctx context.Context, requestID string, profile *core.Profile, ttl time.Duration) (*core.Session, error) {
	if requestID != "" {
		existing, err := s.store.GetSessionByRequestID(ctx, requestID)
		if err == nil {
			return existing, nil
		}
		if !isSessionNotFound(err) {
			return nil, err
		}
	}
	profile = cloneProfile(profile)
	if profile.GetBrowserKind() == browserautomationv1.BrowserKind_BROWSER_KIND_UNSPECIFIED {
		profile.BrowserKind = defaultBrowserKind(s.runtime)
	}
	if ttl < 0 {
		return nil, core.NewError(core.CodeValidationFailed, "ttl cannot be negative", false)
	}
	if ttl == 0 {
		ttl = defaultSessionTTL
	}
	now := s.clock.Now()
	if requestID == "" {
		requestID = s.ids.NewID("req_")
	}
	session := &browserautomationv1.BrowserSession{
		SessionId: requestIDOrNew(s.ids, "brsess_"),
		RequestId: requestID,
		Status:    browserautomationv1.BrowserSessionStatus_BROWSER_SESSION_STATUS_STARTING,
		Profile:   profile,
		Labels:    cloneMap(profile.GetLabels()),
		CreatedAt: timestamp(now),
		UpdatedAt: timestamp(now),
		ExpiresAt: timestamp(now.Add(ttl)),
	}
	if err := s.store.CreateSession(ctx, session); err != nil {
		return nil, err
	}
	if err := s.runtime.StartSession(ctx, session); err != nil {
		session.Status = browserautomationv1.BrowserSessionStatus_BROWSER_SESSION_STATUS_FAILED
		session.LastError = core.AutomationError(asCoreError(err, core.CodeBrowserUnavailable))
		session.UpdatedAt = timestamp(s.clock.Now())
		_ = s.store.UpdateSession(ctx, session)
		return session, err
	}
	startedAt := s.clock.Now()
	session.Status = browserautomationv1.BrowserSessionStatus_BROWSER_SESSION_STATUS_RUNNING
	session.StartedAt = timestamp(startedAt)
	session.UpdatedAt = timestamp(startedAt)
	if err := s.store.UpdateSession(ctx, session); err != nil {
		return nil, err
	}
	return session, nil
}

func (s *AutomationService) GetBrowserSession(ctx context.Context, sessionID string) (*core.Session, error) {
	session, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return s.expireSessionIfNeeded(ctx, session)
}

func (s *AutomationService) StopBrowserSession(ctx context.Context, sessionID, reason string) (*core.Session, error) {
	session, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if core.SessionStatusIsFinal(session.GetStatus()) {
		return session, core.NewError(core.CodeSessionFinalized, "browser session already finalized", false)
	}
	session.Status = browserautomationv1.BrowserSessionStatus_BROWSER_SESSION_STATUS_STOPPING
	session.UpdatedAt = timestamp(s.clock.Now())
	if err := s.store.UpdateSession(ctx, session); err != nil {
		return nil, err
	}
	if err := s.runtime.StopSession(ctx, session, reason); err != nil {
		session.Status = browserautomationv1.BrowserSessionStatus_BROWSER_SESSION_STATUS_FAILED
		session.LastError = core.AutomationError(asCoreError(err, core.CodeBrowserUnavailable))
		session.UpdatedAt = timestamp(s.clock.Now())
		_ = s.store.UpdateSession(ctx, session)
		return session, err
	}
	stoppedAt := s.clock.Now()
	session.Status = browserautomationv1.BrowserSessionStatus_BROWSER_SESSION_STATUS_STOPPED
	session.StoppedAt = timestamp(stoppedAt)
	session.UpdatedAt = timestamp(stoppedAt)
	if reason != "" {
		if session.Labels == nil {
			session.Labels = make(map[string]string)
		}
		session.Labels["stop_reason"] = reason
	}
	if err := s.store.UpdateSession(ctx, session); err != nil {
		return nil, err
	}
	return session, nil
}

func (s *AutomationService) StartBrowserTask(ctx context.Context, requestID string, input *core.TaskInput) (*core.Task, error) {
	input = cloneTaskInput(input)
	if requestID != "" {
		existing, err := s.store.GetTaskByRequestID(ctx, requestID)
		if err == nil {
			return existing, nil
		}
		if !isTaskNotFound(err) {
			return nil, err
		}
	}
	if err := validateTaskInput(input); err != nil {
		return nil, err
	}
	session, err := s.GetBrowserSession(ctx, input.GetSessionId())
	if err != nil {
		return nil, err
	}
	if session.GetStatus() != browserautomationv1.BrowserSessionStatus_BROWSER_SESSION_STATUS_RUNNING {
		return nil, core.NewError(core.CodeSessionFinalized, "browser session is not running", false)
	}
	now := s.clock.Now()
	if requestID == "" {
		requestID = s.ids.NewID("req_")
	}
	task := &browserautomationv1.BrowserTask{
		TaskId:    s.ids.NewID("brtask_"),
		RequestId: requestID,
		Status:    browserautomationv1.BrowserTaskStatus_BROWSER_TASK_STATUS_QUEUED,
		Input:     input,
		Labels:    cloneMap(input.GetLabels()),
		CreatedAt: timestamp(now),
		UpdatedAt: timestamp(now),
	}
	if err := s.store.CreateTask(ctx, task); err != nil {
		return nil, err
	}
	if err := s.runtime.EnqueueTask(ctx, task); err != nil {
		task.Status = browserautomationv1.BrowserTaskStatus_BROWSER_TASK_STATUS_FAILED
		task.LastError = core.AutomationError(asCoreError(err, core.CodeBrowserUnavailable))
		task.UpdatedAt = timestamp(s.clock.Now())
		task.CompletedAt = task.UpdatedAt
		_ = s.store.UpdateTask(ctx, task)
		return task, err
	}
	return task, nil
}

func (s *AutomationService) ExecuteBrowserCommands(ctx context.Context, requestID string, input *core.TaskInput) (*core.Task, error) {
	input = cloneTaskInput(input)
	if requestID != "" {
		existing, err := s.store.GetTaskByRequestID(ctx, requestID)
		if err == nil {
			return existing, nil
		}
		if !isTaskNotFound(err) {
			return nil, err
		}
	}
	if input != nil && input.TaskKey == "" {
		input.TaskKey = "browser.commands"
	}
	if err := validateTaskInput(input); err != nil {
		return nil, err
	}
	if len(input.GetCommands()) == 0 {
		return nil, core.NewError(core.CodeValidationFailed, "commands are required", false)
	}
	if err := validateCommands(input.GetCommands()); err != nil {
		return nil, err
	}
	session, err := s.GetBrowserSession(ctx, input.GetSessionId())
	if err != nil {
		return nil, err
	}
	if session.GetStatus() != browserautomationv1.BrowserSessionStatus_BROWSER_SESSION_STATUS_RUNNING {
		return nil, core.NewError(core.CodeSessionFinalized, "browser session is not running", false)
	}

	now := s.clock.Now()
	if requestID == "" {
		requestID = s.ids.NewID("req_")
	}
	task := &browserautomationv1.BrowserTask{
		TaskId:    s.ids.NewID("brtask_"),
		RequestId: requestID,
		Status:    browserautomationv1.BrowserTaskStatus_BROWSER_TASK_STATUS_RUNNING,
		Input:     input,
		Labels:    cloneMap(input.GetLabels()),
		CreatedAt: timestamp(now),
		StartedAt: timestamp(now),
		UpdatedAt: timestamp(now),
	}
	if err := s.store.CreateTask(ctx, task); err != nil {
		return nil, err
	}
	result, err := s.runtime.ExecuteTask(ctx, task)
	completedAt := timestamp(s.clock.Now())
	task.Results = result.Results
	task.Artifacts = result.Artifacts
	task.UpdatedAt = completedAt
	task.CompletedAt = completedAt
	if err != nil {
		task.Status = browserautomationv1.BrowserTaskStatus_BROWSER_TASK_STATUS_FAILED
		task.LastError = core.AutomationError(asCoreError(err, core.CodeBrowserUnavailable))
		_ = s.store.UpdateTask(ctx, task)
		return task, err
	}
	task.Status = browserautomationv1.BrowserTaskStatus_BROWSER_TASK_STATUS_SUCCEEDED
	if err := s.store.UpdateTask(ctx, task); err != nil {
		return nil, err
	}
	return task, nil
}

func (s *AutomationService) GetBrowserTask(ctx context.Context, taskID string) (*core.Task, error) {
	return s.store.GetTask(ctx, taskID)
}

func (s *AutomationService) ListBrowserTasks(ctx context.Context, filter *core.TaskFilter, pageSize int, pageToken string) (core.TaskListResult, error) {
	return s.store.ListTasks(ctx, filter, pageSize, pageToken)
}

type NoopRuntime struct{}

func (NoopRuntime) StartSession(context.Context, *core.Session) error {
	return nil
}

func (NoopRuntime) StopSession(context.Context, *core.Session, string) error {
	return nil
}

func (NoopRuntime) EnqueueTask(context.Context, *core.Task) error {
	return nil
}

func (NoopRuntime) ExecuteTask(_ context.Context, task *core.Task) (core.TaskExecutionResult, error) {
	now := timestamp(time.Now().UTC())
	commands := task.GetInput().GetCommands()
	results := make([]*browserautomationv1.BrowserCommandResult, 0, len(commands))
	for _, command := range commands {
		results = append(results, &browserautomationv1.BrowserCommandResult{
			CommandId:   command.GetCommandId(),
			CommandKey:  command.GetCommandKey(),
			Status:      browserautomationv1.BrowserCommandStatus_BROWSER_COMMAND_STATUS_SUCCEEDED,
			CompletedAt: now,
		})
	}
	return core.TaskExecutionResult{Results: results}, nil
}

func (s *AutomationService) expireSessionIfNeeded(ctx context.Context, session *core.Session) (*core.Session, error) {
	if session == nil {
		return nil, core.NewError(core.CodeSessionNotFound, "browser session not found", false)
	}
	now := s.clock.Now()
	expiresAt := session.GetExpiresAt()
	if expiresAt == nil || !now.After(expiresAt.AsTime()) || core.SessionStatusIsFinal(session.GetStatus()) {
		return session, nil
	}
	session.Status = browserautomationv1.BrowserSessionStatus_BROWSER_SESSION_STATUS_EXPIRED
	session.UpdatedAt = timestamp(now)
	session.StoppedAt = timestamp(now)
	if err := s.store.UpdateSession(ctx, session); err != nil {
		return nil, err
	}
	return session, nil
}

func validateTaskInput(input *core.TaskInput) error {
	if input == nil {
		return core.NewError(core.CodeValidationFailed, "input is required", false)
	}
	if input.GetSessionId() == "" {
		return core.NewError(core.CodeValidationFailed, "session_id is required", false)
	}
	if input.GetTaskKey() == "" {
		return core.NewError(core.CodeValidationFailed, "task_key is required", false)
	}
	if duration(input.GetTimeout()) < 0 {
		return core.NewError(core.CodeValidationFailed, "timeout cannot be negative", false)
	}
	return nil
}

func validateCommands(commands []*browserautomationv1.BrowserCommand) error {
	for _, command := range commands {
		if command == nil || command.GetOperation() == nil {
			return core.NewError(core.CodeValidationFailed, "command operation is required", false)
		}
		switch operation := command.GetOperation().(type) {
		case *browserautomationv1.BrowserCommand_Navigate:
			if operation.Navigate.GetUrl() == "" {
				return core.NewError(core.CodeValidationFailed, "navigate url is required", false)
			}
		case *browserautomationv1.BrowserCommand_Reload:
		case *browserautomationv1.BrowserCommand_GoBack:
		case *browserautomationv1.BrowserCommand_GoForward:
		case *browserautomationv1.BrowserCommand_Click:
			if !hasSelector(operation.Click.GetSelector(), operation.Click.GetSelectorGroup()) {
				return core.NewError(core.CodeValidationFailed, "click selector is required", false)
			}
		case *browserautomationv1.BrowserCommand_Fill:
			if !hasSelector(operation.Fill.GetSelector(), operation.Fill.GetSelectorGroup()) {
				return core.NewError(core.CodeValidationFailed, "fill selector is required", false)
			}
		case *browserautomationv1.BrowserCommand_SetChecked:
			if !hasSelector(operation.SetChecked.GetSelector(), operation.SetChecked.GetSelectorGroup()) {
				return core.NewError(core.CodeValidationFailed, "set checked selector is required", false)
			}
		case *browserautomationv1.BrowserCommand_TypeText:
			if operation.TypeText.GetText() == "" {
				return core.NewError(core.CodeValidationFailed, "type text is required", false)
			}
		case *browserautomationv1.BrowserCommand_Clear:
			if !hasSelector(operation.Clear.GetSelector(), operation.Clear.GetSelectorGroup()) {
				return core.NewError(core.CodeValidationFailed, "clear selector is required", false)
			}
		case *browserautomationv1.BrowserCommand_Press:
			if operation.Press.GetKey() == "" {
				return core.NewError(core.CodeValidationFailed, "press key is required", false)
			}
		case *browserautomationv1.BrowserCommand_Focus:
			if !hasSelector(operation.Focus.GetSelector(), operation.Focus.GetSelectorGroup()) {
				return core.NewError(core.CodeValidationFailed, "focus selector is required", false)
			}
		case *browserautomationv1.BrowserCommand_Blur:
			if !hasSelector(operation.Blur.GetSelector(), operation.Blur.GetSelectorGroup()) {
				return core.NewError(core.CodeValidationFailed, "blur selector is required", false)
			}
		case *browserautomationv1.BrowserCommand_Hover:
			if !hasSelector(operation.Hover.GetSelector(), operation.Hover.GetSelectorGroup()) {
				return core.NewError(core.CodeValidationFailed, "hover selector is required", false)
			}
		case *browserautomationv1.BrowserCommand_MouseMove:
			if operation.MouseMove.GetPoint() == nil && len(operation.MouseMove.GetPath()) == 0 {
				return core.NewError(core.CodeValidationFailed, "mouse move point or path is required", false)
			}
		case *browserautomationv1.BrowserCommand_MouseClick:
			if operation.MouseClick.GetPoint() == nil {
				return core.NewError(core.CodeValidationFailed, "mouse click point is required", false)
			}
		case *browserautomationv1.BrowserCommand_MouseDown:
		case *browserautomationv1.BrowserCommand_MouseUp:
		case *browserautomationv1.BrowserCommand_Drag:
			source := hasSelector(operation.Drag.GetSourceSelector(), operation.Drag.GetSourceSelectorGroup()) || operation.Drag.GetSourcePoint() != nil
			target := hasSelector(operation.Drag.GetTargetSelector(), operation.Drag.GetTargetSelectorGroup()) || operation.Drag.GetTargetPoint() != nil
			if !source || !target {
				return core.NewError(core.CodeValidationFailed, "drag source and target are required", false)
			}
		case *browserautomationv1.BrowserCommand_Scroll:
			if !hasSelector(operation.Scroll.GetSelector(), operation.Scroll.GetSelectorGroup()) && operation.Scroll.GetDeltaX() == 0 && operation.Scroll.GetDeltaY() == 0 {
				return core.NewError(core.CodeValidationFailed, "scroll selector or delta is required", false)
			}
		case *browserautomationv1.BrowserCommand_WaitForSelector:
			if !hasSelector(operation.WaitForSelector.GetSelector(), operation.WaitForSelector.GetSelectorGroup()) {
				return core.NewError(core.CodeValidationFailed, "wait selector is required", false)
			}
		case *browserautomationv1.BrowserCommand_WaitForText:
			if operation.WaitForText.GetText() == "" {
				return core.NewError(core.CodeValidationFailed, "wait text is required", false)
			}
		case *browserautomationv1.BrowserCommand_WaitForUrl:
			if operation.WaitForUrl.GetUrlPattern() == "" {
				return core.NewError(core.CodeValidationFailed, "wait url pattern is required", false)
			}
		case *browserautomationv1.BrowserCommand_WaitForLoadState:
		case *browserautomationv1.BrowserCommand_WaitForTimeout:
			if duration(operation.WaitForTimeout.GetDuration()) <= 0 {
				return core.NewError(core.CodeValidationFailed, "wait timeout duration is required", false)
			}
		case *browserautomationv1.BrowserCommand_GetPageState:
		case *browserautomationv1.BrowserCommand_ExtractText:
			if !hasSelector(operation.ExtractText.GetSelector(), operation.ExtractText.GetSelectorGroup()) {
				return core.NewError(core.CodeValidationFailed, "extract selector is required", false)
			}
		case *browserautomationv1.BrowserCommand_CountElements:
			if !hasSelector(operation.CountElements.GetSelector(), operation.CountElements.GetSelectorGroup()) {
				return core.NewError(core.CodeValidationFailed, "count elements selector is required", false)
			}
		case *browserautomationv1.BrowserCommand_GetAttribute:
			if !hasSelector(operation.GetAttribute.GetSelector(), operation.GetAttribute.GetSelectorGroup()) {
				return core.NewError(core.CodeValidationFailed, "get attribute selector is required", false)
			}
			if operation.GetAttribute.GetName() == "" {
				return core.NewError(core.CodeValidationFailed, "get attribute name is required", false)
			}
		case *browserautomationv1.BrowserCommand_ExtractElement:
			if !hasSelector(operation.ExtractElement.GetSelector(), operation.ExtractElement.GetSelectorGroup()) {
				return core.NewError(core.CodeValidationFailed, "extract element selector is required", false)
			}
		case *browserautomationv1.BrowserCommand_Screenshot:
		case *browserautomationv1.BrowserCommand_UploadFile:
			if !hasSelector(operation.UploadFile.GetSelector(), operation.UploadFile.GetSelectorGroup()) {
				return core.NewError(core.CodeValidationFailed, "upload selector is required", false)
			}
			if len(operation.UploadFile.GetFileSecretRefs()) == 0 {
				return core.NewError(core.CodeValidationFailed, "file_secret_refs are required", false)
			}
		case *browserautomationv1.BrowserCommand_SelectOption:
			if !hasSelector(operation.SelectOption.GetSelector(), operation.SelectOption.GetSelectorGroup()) {
				return core.NewError(core.CodeValidationFailed, "select option selector is required", false)
			}
			if len(operation.SelectOption.GetValues()) == 0 && len(operation.SelectOption.GetLabels()) == 0 && len(operation.SelectOption.GetIndexes()) == 0 {
				return core.NewError(core.CodeValidationFailed, "select option value, label or index is required", false)
			}
		case *browserautomationv1.BrowserCommand_SubmitForm:
			if !hasSelector(operation.SubmitForm.GetSelector(), operation.SubmitForm.GetSelectorGroup()) {
				return core.NewError(core.CodeValidationFailed, "submit form selector is required", false)
			}
		case *browserautomationv1.BrowserCommand_Evaluate:
			if operation.Evaluate.GetExpression() == "" {
				return core.NewError(core.CodeValidationFailed, "evaluate expression is required", false)
			}
		default:
			return core.NewError(core.CodeUnsupportedOperation, "unsupported command operation", false)
		}
	}
	return nil
}

func hasSelector(selector *browserautomationv1.BrowserSelector, group *browserautomationv1.BrowserSelectorGroup) bool {
	if selector.GetValue() != "" {
		return true
	}
	for _, candidate := range group.GetSelectors() {
		if candidate.GetValue() != "" {
			return true
		}
	}
	return false
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

func cloneProfile(profile *core.Profile) *core.Profile {
	if profile == nil {
		return &browserautomationv1.BrowserProfile{}
	}
	return proto.Clone(profile).(*browserautomationv1.BrowserProfile)
}

func cloneTaskInput(input *core.TaskInput) *core.TaskInput {
	if input == nil {
		return nil
	}
	return proto.Clone(input).(*browserautomationv1.BrowserTaskInput)
}

func defaultBrowserKind(runtime core.Runtime) browserautomationv1.BrowserKind {
	if defaults, ok := runtime.(core.RuntimeProfileDefaults); ok {
		kind := defaults.DefaultBrowserKind()
		if kind != browserautomationv1.BrowserKind_BROWSER_KIND_UNSPECIFIED {
			return kind
		}
	}
	return browserautomationv1.BrowserKind_BROWSER_KIND_CHROMIUM
}

func timestamp(value time.Time) *timestamppb.Timestamp {
	if value.IsZero() {
		return nil
	}
	return timestamppb.New(value)
}

func duration(value *durationpb.Duration) time.Duration {
	if value == nil {
		return 0
	}
	return value.AsDuration()
}

func requestIDOrNew(ids core.IDGenerator, prefix string) string {
	return ids.NewID(prefix)
}
