package grpcadapter

import (
	"context"

	"github.com/byte-v-forge/browser-automation/internal/app"
	browserautomationv1 "github.com/byte-v-forge/internal-contracts-go/byte/v/forge/internalcontracts/browserautomation/v1"
)

type AutomationServer struct {
	browserautomationv1.UnimplementedBrowserAutomationServiceServer
	service *app.AutomationService
}

func NewAutomationServer(service *app.AutomationService) *AutomationServer {
	return &AutomationServer{service: service}
}

func (s *AutomationServer) StartBrowserSession(ctx context.Context, request *browserautomationv1.StartBrowserSessionRequest) (*browserautomationv1.StartBrowserSessionResponse, error) {
	session, err := s.service.StartBrowserSession(ctx, request.GetRequestId(), fromProtoProfile(request.GetProfile()), protoDuration(request.GetTtl()))
	if err != nil {
		return &browserautomationv1.StartBrowserSessionResponse{Session: toProtoSession(session), Error: toProtoError(err)}, nil
	}
	return &browserautomationv1.StartBrowserSessionResponse{Session: toProtoSession(session)}, nil
}

func (s *AutomationServer) GetBrowserSession(ctx context.Context, request *browserautomationv1.GetBrowserSessionRequest) (*browserautomationv1.GetBrowserSessionResponse, error) {
	session, err := s.service.GetBrowserSession(ctx, request.GetSessionId())
	if err != nil {
		return &browserautomationv1.GetBrowserSessionResponse{Error: toProtoError(err)}, nil
	}
	return &browserautomationv1.GetBrowserSessionResponse{Session: toProtoSession(session)}, nil
}

func (s *AutomationServer) StopBrowserSession(ctx context.Context, request *browserautomationv1.StopBrowserSessionRequest) (*browserautomationv1.StopBrowserSessionResponse, error) {
	session, err := s.service.StopBrowserSession(ctx, request.GetSessionId(), request.GetReason())
	if err != nil {
		return &browserautomationv1.StopBrowserSessionResponse{Session: toProtoSession(session), Error: toProtoError(err)}, nil
	}
	return &browserautomationv1.StopBrowserSessionResponse{Session: toProtoSession(session)}, nil
}

func (s *AutomationServer) StartBrowserTask(ctx context.Context, request *browserautomationv1.StartBrowserTaskRequest) (*browserautomationv1.StartBrowserTaskResponse, error) {
	task, err := s.service.StartBrowserTask(ctx, request.GetRequestId(), fromProtoTaskInput(request.GetInput()))
	if err != nil {
		return &browserautomationv1.StartBrowserTaskResponse{Task: toProtoTask(task), Error: toProtoError(err)}, nil
	}
	return &browserautomationv1.StartBrowserTaskResponse{Task: toProtoTask(task)}, nil
}

func (s *AutomationServer) GetBrowserTask(ctx context.Context, request *browserautomationv1.GetBrowserTaskRequest) (*browserautomationv1.GetBrowserTaskResponse, error) {
	task, err := s.service.GetBrowserTask(ctx, request.GetTaskId())
	if err != nil {
		return &browserautomationv1.GetBrowserTaskResponse{Error: toProtoError(err)}, nil
	}
	return &browserautomationv1.GetBrowserTaskResponse{Task: toProtoTask(task)}, nil
}

func (s *AutomationServer) ListBrowserTasks(ctx context.Context, request *browserautomationv1.ListBrowserTasksRequest) (*browserautomationv1.ListBrowserTasksResponse, error) {
	result, err := s.service.ListBrowserTasks(ctx, fromProtoTaskFilter(request.GetFilter()), int(request.GetPageSize()), request.GetPageToken())
	if err != nil {
		return &browserautomationv1.ListBrowserTasksResponse{Error: toProtoError(err)}, nil
	}
	return &browserautomationv1.ListBrowserTasksResponse{
		Tasks:         toProtoTasks(result.Tasks),
		NextPageToken: result.NextPageToken,
	}, nil
}
