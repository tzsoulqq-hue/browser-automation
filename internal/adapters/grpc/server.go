package grpcadapter

import (
	"context"
	"time"

	"github.com/byte-v-forge/browser-automation/internal/app"
	"github.com/byte-v-forge/browser-automation/internal/core"
	browserautomationv1 "github.com/byte-v-forge/contracts-go/byte/v/forge/contracts/browserautomation/v1"
	"google.golang.org/protobuf/types/known/durationpb"
)

type AutomationServer struct {
	browserautomationv1.UnimplementedBrowserAutomationServiceServer
	service *app.AutomationService
}

func NewAutomationServer(service *app.AutomationService) *AutomationServer {
	return &AutomationServer{service: service}
}

func (s *AutomationServer) StartBrowserSession(ctx context.Context, request *browserautomationv1.StartBrowserSessionRequest) (*browserautomationv1.StartBrowserSessionResponse, error) {
	session, err := s.service.StartBrowserSession(ctx, request.GetRequestId(), request.GetProfile(), protoDuration(request.GetTtl()))
	if err != nil {
		return &browserautomationv1.StartBrowserSessionResponse{Session: session, Error: core.AutomationError(err)}, nil
	}
	return &browserautomationv1.StartBrowserSessionResponse{Session: session}, nil
}

func (s *AutomationServer) GetBrowserSession(ctx context.Context, request *browserautomationv1.GetBrowserSessionRequest) (*browserautomationv1.GetBrowserSessionResponse, error) {
	session, err := s.service.GetBrowserSession(ctx, request.GetSessionId())
	if err != nil {
		return &browserautomationv1.GetBrowserSessionResponse{Error: core.AutomationError(err)}, nil
	}
	return &browserautomationv1.GetBrowserSessionResponse{Session: session}, nil
}

func (s *AutomationServer) StopBrowserSession(ctx context.Context, request *browserautomationv1.StopBrowserSessionRequest) (*browserautomationv1.StopBrowserSessionResponse, error) {
	session, err := s.service.StopBrowserSession(ctx, request.GetSessionId(), request.GetReason())
	if err != nil {
		return &browserautomationv1.StopBrowserSessionResponse{Session: session, Error: core.AutomationError(err)}, nil
	}
	return &browserautomationv1.StopBrowserSessionResponse{Session: session}, nil
}

func (s *AutomationServer) StartBrowserTask(ctx context.Context, request *browserautomationv1.StartBrowserTaskRequest) (*browserautomationv1.StartBrowserTaskResponse, error) {
	task, err := s.service.StartBrowserTask(ctx, request.GetRequestId(), request.GetInput())
	if err != nil {
		return &browserautomationv1.StartBrowserTaskResponse{Task: task, Error: core.AutomationError(err)}, nil
	}
	return &browserautomationv1.StartBrowserTaskResponse{Task: task}, nil
}

func (s *AutomationServer) ExecuteBrowserCommands(ctx context.Context, request *browserautomationv1.ExecuteBrowserCommandsRequest) (*browserautomationv1.ExecuteBrowserCommandsResponse, error) {
	task, err := s.service.ExecuteBrowserCommands(ctx, request.GetRequestId(), request.GetInput())
	if err != nil {
		return &browserautomationv1.ExecuteBrowserCommandsResponse{
			Task:    task,
			Results: taskResults(task),
			Error:   core.AutomationError(err),
		}, nil
	}
	return &browserautomationv1.ExecuteBrowserCommandsResponse{
		Task:    task,
		Results: taskResults(task),
	}, nil
}

func (s *AutomationServer) GetBrowserTask(ctx context.Context, request *browserautomationv1.GetBrowserTaskRequest) (*browserautomationv1.GetBrowserTaskResponse, error) {
	task, err := s.service.GetBrowserTask(ctx, request.GetTaskId())
	if err != nil {
		return &browserautomationv1.GetBrowserTaskResponse{Error: core.AutomationError(err)}, nil
	}
	return &browserautomationv1.GetBrowserTaskResponse{Task: task}, nil
}

func (s *AutomationServer) ListBrowserTasks(ctx context.Context, request *browserautomationv1.ListBrowserTasksRequest) (*browserautomationv1.ListBrowserTasksResponse, error) {
	result, err := s.service.ListBrowserTasks(ctx, request.GetFilter(), int(request.GetPageSize()), request.GetPageToken())
	if err != nil {
		return &browserautomationv1.ListBrowserTasksResponse{Error: core.AutomationError(err)}, nil
	}
	return &browserautomationv1.ListBrowserTasksResponse{
		Tasks:         result.Tasks,
		NextPageToken: result.NextPageToken,
	}, nil
}

func protoDuration(value *durationpb.Duration) time.Duration {
	if value == nil {
		return 0
	}
	return value.AsDuration()
}

func taskResults(task *browserautomationv1.BrowserTask) []*browserautomationv1.BrowserCommandResult {
	if task == nil {
		return nil
	}
	return task.GetResults()
}
