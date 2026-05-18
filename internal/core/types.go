package core

import (
	"errors"
	"fmt"

	browserautomationv1 "github.com/byte-v-forge/browser-automation/gen/go/byte/v/forge/contracts/browserautomation/v1"
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

func AutomationError(err error) *browserautomationv1.BrowserAutomationError {
	if err == nil {
		return nil
	}
	var automationErr *Error
	if !errors.As(err, &automationErr) {
		automationErr = NewError(CodeInternal, err.Error(), false)
	}
	return &browserautomationv1.BrowserAutomationError{
		Code:      ErrorCodeToProto(automationErr.Code),
		Message:   automationErr.Message,
		Retryable: automationErr.Retryable,
	}
}

func ErrorCodeToProto(code ErrorCode) browserautomationv1.BrowserAutomationErrorCode {
	switch code {
	case CodeValidationFailed:
		return browserautomationv1.BrowserAutomationErrorCode_BROWSER_AUTOMATION_ERROR_CODE_VALIDATION_FAILED
	case CodeSessionNotFound:
		return browserautomationv1.BrowserAutomationErrorCode_BROWSER_AUTOMATION_ERROR_CODE_SESSION_NOT_FOUND
	case CodeTaskNotFound:
		return browserautomationv1.BrowserAutomationErrorCode_BROWSER_AUTOMATION_ERROR_CODE_TASK_NOT_FOUND
	case CodeArtifactNotFound:
		return browserautomationv1.BrowserAutomationErrorCode_BROWSER_AUTOMATION_ERROR_CODE_ARTIFACT_NOT_FOUND
	case CodeSessionFinalized:
		return browserautomationv1.BrowserAutomationErrorCode_BROWSER_AUTOMATION_ERROR_CODE_SESSION_ALREADY_FINALIZED
	case CodeTaskFinalized:
		return browserautomationv1.BrowserAutomationErrorCode_BROWSER_AUTOMATION_ERROR_CODE_TASK_ALREADY_FINALIZED
	case CodeCapacityUnavailable:
		return browserautomationv1.BrowserAutomationErrorCode_BROWSER_AUTOMATION_ERROR_CODE_CAPACITY_UNAVAILABLE
	case CodeBrowserUnavailable:
		return browserautomationv1.BrowserAutomationErrorCode_BROWSER_AUTOMATION_ERROR_CODE_BROWSER_UNAVAILABLE
	case CodeProxyFailed:
		return browserautomationv1.BrowserAutomationErrorCode_BROWSER_AUTOMATION_ERROR_CODE_PROXY_FAILED
	case CodeNavigationFailed:
		return browserautomationv1.BrowserAutomationErrorCode_BROWSER_AUTOMATION_ERROR_CODE_NAVIGATION_FAILED
	case CodeScriptFailed:
		return browserautomationv1.BrowserAutomationErrorCode_BROWSER_AUTOMATION_ERROR_CODE_SCRIPT_FAILED
	case CodeTimeout:
		return browserautomationv1.BrowserAutomationErrorCode_BROWSER_AUTOMATION_ERROR_CODE_TIMEOUT
	case CodeUnsupportedOperation:
		return browserautomationv1.BrowserAutomationErrorCode_BROWSER_AUTOMATION_ERROR_CODE_UNSUPPORTED_OPERATION
	case CodeInternal:
		return browserautomationv1.BrowserAutomationErrorCode_BROWSER_AUTOMATION_ERROR_CODE_INTERNAL
	default:
		return browserautomationv1.BrowserAutomationErrorCode_BROWSER_AUTOMATION_ERROR_CODE_UNSPECIFIED
	}
}

type Session = browserautomationv1.BrowserSession
type Task = browserautomationv1.BrowserTask
type Profile = browserautomationv1.BrowserProfile
type TaskInput = browserautomationv1.BrowserTaskInput
type TaskFilter = browserautomationv1.BrowserTaskFilter
type Artifact = browserautomationv1.BrowserArtifact
type CommandResult = browserautomationv1.BrowserCommandResult

type TaskListResult struct {
	Tasks         []*Task
	NextPageToken string
}

func SessionStatusIsFinal(status browserautomationv1.BrowserSessionStatus) bool {
	switch status {
	case browserautomationv1.BrowserSessionStatus_BROWSER_SESSION_STATUS_STOPPED,
		browserautomationv1.BrowserSessionStatus_BROWSER_SESSION_STATUS_FAILED,
		browserautomationv1.BrowserSessionStatus_BROWSER_SESSION_STATUS_EXPIRED:
		return true
	default:
		return false
	}
}

func TaskStatusIsFinal(status browserautomationv1.BrowserTaskStatus) bool {
	switch status {
	case browserautomationv1.BrowserTaskStatus_BROWSER_TASK_STATUS_SUCCEEDED,
		browserautomationv1.BrowserTaskStatus_BROWSER_TASK_STATUS_FAILED,
		browserautomationv1.BrowserTaskStatus_BROWSER_TASK_STATUS_CANCELED,
		browserautomationv1.BrowserTaskStatus_BROWSER_TASK_STATUS_TIMEOUT:
		return true
	default:
		return false
	}
}
