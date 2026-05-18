package grpcadapter

import (
	"errors"
	"time"

	"github.com/byte-v-forge/browser-automation/internal/core"
	browserautomationv1 "github.com/byte-v-forge/internal-contracts-go/byte/v/forge/internalcontracts/browserautomation/v1"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func toProtoSession(session core.Session) *browserautomationv1.BrowserSession {
	return &browserautomationv1.BrowserSession{
		SessionId: session.ID,
		RequestId: session.RequestID,
		Status:    toProtoSessionStatus(session.Status),
		Profile:   toProtoProfile(session.Profile),
		LastError: toProtoError(session.LastError),
		Artifacts: toProtoArtifacts(session.Artifacts),
		Labels:    cloneMap(session.Labels),
		CreatedAt: toProtoTime(session.CreatedAt),
		StartedAt: toProtoTime(session.StartedAt),
		UpdatedAt: toProtoTime(session.UpdatedAt),
		StoppedAt: toProtoTime(session.StoppedAt),
		ExpiresAt: toProtoTime(session.ExpiresAt),
	}
}

func toProtoTask(task core.Task) *browserautomationv1.BrowserTask {
	return &browserautomationv1.BrowserTask{
		TaskId:      task.ID,
		RequestId:   task.RequestID,
		Status:      toProtoTaskStatus(task.Status),
		Input:       toProtoTaskInput(task.Input),
		LastError:   toProtoError(task.LastError),
		Artifacts:   toProtoArtifacts(task.Artifacts),
		Labels:      cloneMap(task.Labels),
		CreatedAt:   toProtoTime(task.CreatedAt),
		StartedAt:   toProtoTime(task.StartedAt),
		UpdatedAt:   toProtoTime(task.UpdatedAt),
		CompletedAt: toProtoTime(task.CompletedAt),
	}
}

func toProtoTasks(tasks []core.Task) []*browserautomationv1.BrowserTask {
	out := make([]*browserautomationv1.BrowserTask, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, toProtoTask(task))
	}
	return out
}

func toProtoProfile(profile core.Profile) *browserautomationv1.BrowserProfile {
	return &browserautomationv1.BrowserProfile{
		BrowserKind:           toProtoBrowserKind(profile.BrowserKind),
		Locale:                profile.Locale,
		Timezone:              profile.Timezone,
		UserAgent:             profile.UserAgent,
		Viewport:              toProtoViewport(profile.Viewport),
		ProxyRef:              profile.ProxyRef,
		StorageStateSecretRef: profile.StorageStateSecretRef,
		Labels:                cloneMap(profile.Labels),
	}
}

func fromProtoProfile(profile *browserautomationv1.BrowserProfile) core.Profile {
	if profile == nil {
		return core.Profile{}
	}
	return core.Profile{
		BrowserKind:           fromProtoBrowserKind(profile.GetBrowserKind()),
		Locale:                profile.GetLocale(),
		Timezone:              profile.GetTimezone(),
		UserAgent:             profile.GetUserAgent(),
		Viewport:              fromProtoViewport(profile.GetViewport()),
		ProxyRef:              profile.GetProxyRef(),
		StorageStateSecretRef: profile.GetStorageStateSecretRef(),
		Labels:                cloneMap(profile.GetLabels()),
	}
}

func toProtoViewport(viewport core.Viewport) *browserautomationv1.BrowserViewport {
	if viewport.Width == 0 && viewport.Height == 0 && viewport.DeviceScaleFactor == 0 && !viewport.Mobile {
		return nil
	}
	return &browserautomationv1.BrowserViewport{
		Width:             viewport.Width,
		Height:            viewport.Height,
		DeviceScaleFactor: viewport.DeviceScaleFactor,
		Mobile:            viewport.Mobile,
	}
}

func fromProtoViewport(viewport *browserautomationv1.BrowserViewport) core.Viewport {
	if viewport == nil {
		return core.Viewport{}
	}
	return core.Viewport{
		Width:             viewport.GetWidth(),
		Height:            viewport.GetHeight(),
		DeviceScaleFactor: viewport.GetDeviceScaleFactor(),
		Mobile:            viewport.GetMobile(),
	}
}

func toProtoTaskInput(input core.TaskInput) *browserautomationv1.BrowserTaskInput {
	return &browserautomationv1.BrowserTaskInput{
		SessionId:   input.SessionID,
		TaskKey:     input.TaskKey,
		ScenarioKey: input.ScenarioKey,
		TargetUrl:   input.TargetURL,
		Timeout:     toProtoDuration(input.Timeout),
		Labels:      cloneMap(input.Labels),
	}
}

func fromProtoTaskInput(input *browserautomationv1.BrowserTaskInput) core.TaskInput {
	if input == nil {
		return core.TaskInput{}
	}
	return core.TaskInput{
		SessionID:   input.GetSessionId(),
		TaskKey:     input.GetTaskKey(),
		ScenarioKey: input.GetScenarioKey(),
		TargetURL:   input.GetTargetUrl(),
		Timeout:     protoDuration(input.GetTimeout()),
		Labels:      cloneMap(input.GetLabels()),
	}
}

func fromProtoTaskFilter(filter *browserautomationv1.BrowserTaskFilter) core.TaskFilter {
	if filter == nil {
		return core.TaskFilter{}
	}
	return core.TaskFilter{
		SessionID:     filter.GetSessionId(),
		Status:        fromProtoTaskStatus(filter.GetStatus()),
		TaskKey:       filter.GetTaskKey(),
		ScenarioKey:   filter.GetScenarioKey(),
		LabelKey:      filter.GetLabelKey(),
		LabelValue:    filter.GetLabelValue(),
		CreatedAfter:  protoTime(filter.GetCreatedAfter()),
		CreatedBefore: protoTime(filter.GetCreatedBefore()),
	}
}

func toProtoArtifacts(artifacts []core.Artifact) []*browserautomationv1.BrowserArtifact {
	out := make([]*browserautomationv1.BrowserArtifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		out = append(out, &browserautomationv1.BrowserArtifact{
			ArtifactId:  artifact.ID,
			Kind:        toProtoArtifactKind(artifact.Kind),
			Uri:         artifact.URI,
			ContentType: artifact.ContentType,
			SizeBytes:   artifact.SizeBytes,
			Labels:      cloneMap(artifact.Labels),
			CreatedAt:   toProtoTime(artifact.CreatedAt),
		})
	}
	return out
}

func toProtoBrowserKind(kind core.BrowserKind) browserautomationv1.BrowserKind {
	switch kind {
	case core.BrowserKindChromium:
		return browserautomationv1.BrowserKind_BROWSER_KIND_CHROMIUM
	case core.BrowserKindFirefox:
		return browserautomationv1.BrowserKind_BROWSER_KIND_FIREFOX
	case core.BrowserKindWebKit:
		return browserautomationv1.BrowserKind_BROWSER_KIND_WEBKIT
	default:
		return browserautomationv1.BrowserKind_BROWSER_KIND_UNSPECIFIED
	}
}

func fromProtoBrowserKind(kind browserautomationv1.BrowserKind) core.BrowserKind {
	switch kind {
	case browserautomationv1.BrowserKind_BROWSER_KIND_CHROMIUM:
		return core.BrowserKindChromium
	case browserautomationv1.BrowserKind_BROWSER_KIND_FIREFOX:
		return core.BrowserKindFirefox
	case browserautomationv1.BrowserKind_BROWSER_KIND_WEBKIT:
		return core.BrowserKindWebKit
	default:
		return ""
	}
}

func toProtoSessionStatus(status core.SessionStatus) browserautomationv1.BrowserSessionStatus {
	switch status {
	case core.SessionStatusStarting:
		return browserautomationv1.BrowserSessionStatus_BROWSER_SESSION_STATUS_STARTING
	case core.SessionStatusRunning:
		return browserautomationv1.BrowserSessionStatus_BROWSER_SESSION_STATUS_RUNNING
	case core.SessionStatusStopping:
		return browserautomationv1.BrowserSessionStatus_BROWSER_SESSION_STATUS_STOPPING
	case core.SessionStatusStopped:
		return browserautomationv1.BrowserSessionStatus_BROWSER_SESSION_STATUS_STOPPED
	case core.SessionStatusFailed:
		return browserautomationv1.BrowserSessionStatus_BROWSER_SESSION_STATUS_FAILED
	case core.SessionStatusExpired:
		return browserautomationv1.BrowserSessionStatus_BROWSER_SESSION_STATUS_EXPIRED
	default:
		return browserautomationv1.BrowserSessionStatus_BROWSER_SESSION_STATUS_UNSPECIFIED
	}
}

func toProtoTaskStatus(status core.TaskStatus) browserautomationv1.BrowserTaskStatus {
	switch status {
	case core.TaskStatusQueued:
		return browserautomationv1.BrowserTaskStatus_BROWSER_TASK_STATUS_QUEUED
	case core.TaskStatusRunning:
		return browserautomationv1.BrowserTaskStatus_BROWSER_TASK_STATUS_RUNNING
	case core.TaskStatusSucceeded:
		return browserautomationv1.BrowserTaskStatus_BROWSER_TASK_STATUS_SUCCEEDED
	case core.TaskStatusFailed:
		return browserautomationv1.BrowserTaskStatus_BROWSER_TASK_STATUS_FAILED
	case core.TaskStatusCanceled:
		return browserautomationv1.BrowserTaskStatus_BROWSER_TASK_STATUS_CANCELED
	case core.TaskStatusTimeout:
		return browserautomationv1.BrowserTaskStatus_BROWSER_TASK_STATUS_TIMEOUT
	default:
		return browserautomationv1.BrowserTaskStatus_BROWSER_TASK_STATUS_UNSPECIFIED
	}
}

func fromProtoTaskStatus(status browserautomationv1.BrowserTaskStatus) core.TaskStatus {
	switch status {
	case browserautomationv1.BrowserTaskStatus_BROWSER_TASK_STATUS_QUEUED:
		return core.TaskStatusQueued
	case browserautomationv1.BrowserTaskStatus_BROWSER_TASK_STATUS_RUNNING:
		return core.TaskStatusRunning
	case browserautomationv1.BrowserTaskStatus_BROWSER_TASK_STATUS_SUCCEEDED:
		return core.TaskStatusSucceeded
	case browserautomationv1.BrowserTaskStatus_BROWSER_TASK_STATUS_FAILED:
		return core.TaskStatusFailed
	case browserautomationv1.BrowserTaskStatus_BROWSER_TASK_STATUS_CANCELED:
		return core.TaskStatusCanceled
	case browserautomationv1.BrowserTaskStatus_BROWSER_TASK_STATUS_TIMEOUT:
		return core.TaskStatusTimeout
	default:
		return ""
	}
}

func toProtoArtifactKind(kind core.ArtifactKind) browserautomationv1.BrowserArtifactKind {
	switch kind {
	case core.ArtifactKindScreenshot:
		return browserautomationv1.BrowserArtifactKind_BROWSER_ARTIFACT_KIND_SCREENSHOT
	case core.ArtifactKindVideo:
		return browserautomationv1.BrowserArtifactKind_BROWSER_ARTIFACT_KIND_VIDEO
	case core.ArtifactKindTrace:
		return browserautomationv1.BrowserArtifactKind_BROWSER_ARTIFACT_KIND_TRACE
	case core.ArtifactKindHAR:
		return browserautomationv1.BrowserArtifactKind_BROWSER_ARTIFACT_KIND_HAR
	case core.ArtifactKindConsoleLog:
		return browserautomationv1.BrowserArtifactKind_BROWSER_ARTIFACT_KIND_CONSOLE_LOG
	case core.ArtifactKindNetworkLog:
		return browserautomationv1.BrowserArtifactKind_BROWSER_ARTIFACT_KIND_NETWORK_LOG
	case core.ArtifactKindDownload:
		return browserautomationv1.BrowserArtifactKind_BROWSER_ARTIFACT_KIND_DOWNLOAD
	default:
		return browserautomationv1.BrowserArtifactKind_BROWSER_ARTIFACT_KIND_UNSPECIFIED
	}
}

func toProtoError(err error) *browserautomationv1.BrowserAutomationError {
	if err == nil {
		return nil
	}
	var automationErr *core.Error
	if !errors.As(err, &automationErr) {
		automationErr = core.NewError(core.CodeInternal, err.Error(), false)
	}
	return &browserautomationv1.BrowserAutomationError{
		Code:      toProtoErrorCode(automationErr.Code),
		Message:   automationErr.Message,
		Retryable: automationErr.Retryable,
	}
}

func toProtoErrorCode(code core.ErrorCode) browserautomationv1.BrowserAutomationErrorCode {
	switch code {
	case core.CodeValidationFailed:
		return browserautomationv1.BrowserAutomationErrorCode_BROWSER_AUTOMATION_ERROR_CODE_VALIDATION_FAILED
	case core.CodeSessionNotFound:
		return browserautomationv1.BrowserAutomationErrorCode_BROWSER_AUTOMATION_ERROR_CODE_SESSION_NOT_FOUND
	case core.CodeTaskNotFound:
		return browserautomationv1.BrowserAutomationErrorCode_BROWSER_AUTOMATION_ERROR_CODE_TASK_NOT_FOUND
	case core.CodeArtifactNotFound:
		return browserautomationv1.BrowserAutomationErrorCode_BROWSER_AUTOMATION_ERROR_CODE_ARTIFACT_NOT_FOUND
	case core.CodeSessionFinalized:
		return browserautomationv1.BrowserAutomationErrorCode_BROWSER_AUTOMATION_ERROR_CODE_SESSION_ALREADY_FINALIZED
	case core.CodeTaskFinalized:
		return browserautomationv1.BrowserAutomationErrorCode_BROWSER_AUTOMATION_ERROR_CODE_TASK_ALREADY_FINALIZED
	case core.CodeCapacityUnavailable:
		return browserautomationv1.BrowserAutomationErrorCode_BROWSER_AUTOMATION_ERROR_CODE_CAPACITY_UNAVAILABLE
	case core.CodeBrowserUnavailable:
		return browserautomationv1.BrowserAutomationErrorCode_BROWSER_AUTOMATION_ERROR_CODE_BROWSER_UNAVAILABLE
	case core.CodeProxyFailed:
		return browserautomationv1.BrowserAutomationErrorCode_BROWSER_AUTOMATION_ERROR_CODE_PROXY_FAILED
	case core.CodeNavigationFailed:
		return browserautomationv1.BrowserAutomationErrorCode_BROWSER_AUTOMATION_ERROR_CODE_NAVIGATION_FAILED
	case core.CodeScriptFailed:
		return browserautomationv1.BrowserAutomationErrorCode_BROWSER_AUTOMATION_ERROR_CODE_SCRIPT_FAILED
	case core.CodeTimeout:
		return browserautomationv1.BrowserAutomationErrorCode_BROWSER_AUTOMATION_ERROR_CODE_TIMEOUT
	case core.CodeUnsupportedOperation:
		return browserautomationv1.BrowserAutomationErrorCode_BROWSER_AUTOMATION_ERROR_CODE_UNSUPPORTED_OPERATION
	case core.CodeInternal:
		return browserautomationv1.BrowserAutomationErrorCode_BROWSER_AUTOMATION_ERROR_CODE_INTERNAL
	default:
		return browserautomationv1.BrowserAutomationErrorCode_BROWSER_AUTOMATION_ERROR_CODE_UNSPECIFIED
	}
}

func protoDuration(value *durationpb.Duration) time.Duration {
	if value == nil {
		return 0
	}
	return value.AsDuration()
}

func toProtoDuration(value time.Duration) *durationpb.Duration {
	if value == 0 {
		return nil
	}
	return durationpb.New(value)
}

func protoTime(value *timestamppb.Timestamp) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.AsTime()
}

func toProtoTime(value time.Time) *timestamppb.Timestamp {
	if value.IsZero() {
		return nil
	}
	return timestamppb.New(value)
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
