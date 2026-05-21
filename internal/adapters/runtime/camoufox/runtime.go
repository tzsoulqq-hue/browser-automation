package camoufox

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	browserautomationv1 "github.com/byte-v-forge/browser-automation/gen/go/byte/v/forge/contracts/browserautomation/v1"
	"github.com/byte-v-forge/browser-automation/internal/core"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/durationpb"
)

var endpointPattern = regexp.MustCompile(`ws://[^\s]+`)

type Runtime struct {
	cfg      Config
	scripts  scripts
	mu       sync.Mutex
	sessions map[string]*sessionRuntime
}

func NewRuntime(cfg Config) (*Runtime, error) {
	normalized, err := cfg.normalize()
	if err != nil {
		return nil, core.NewError(core.CodeValidationFailed, err.Error(), false)
	}
	return &Runtime{
		cfg:      normalized,
		scripts:  defaultScripts(),
		sessions: make(map[string]*sessionRuntime),
	}, nil
}

func (r *Runtime) DefaultBrowserKind() browserautomationv1.BrowserKind {
	return browserautomationv1.BrowserKind_BROWSER_KIND_FIREFOX
}

func (r *Runtime) StartSession(ctx context.Context, session *core.Session) error {
	if session == nil {
		return core.NewError(core.CodeValidationFailed, "session is required", false)
	}
	if session.GetProfile().GetBrowserKind() != browserautomationv1.BrowserKind_BROWSER_KIND_FIREFOX {
		return core.NewError(core.CodeUnsupportedOperation, "camoufox runtime supports firefox browser kind", false)
	}
	r.mu.Lock()
	if existing := r.sessions[session.GetSessionId()]; existing != nil {
		r.mu.Unlock()
		return nil
	}
	r.mu.Unlock()
	if err := os.MkdirAll(r.cfg.ArtifactsDir, 0o700); err != nil {
		return core.NewError(core.CodeInternal, err.Error(), true)
	}
	endpoint, server, err := r.startServer(ctx, session)
	if err != nil {
		return err
	}
	worker, err := r.startWorker(ctx, endpoint, session)
	if err != nil {
		_ = server.stop(r.cfg.ShutdownTimeout)
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing := r.sessions[session.GetSessionId()]; existing != nil {
		_ = worker.stop(r.cfg.ShutdownTimeout)
		_ = server.stop(r.cfg.ShutdownTimeout)
		return nil
	}
	r.sessions[session.GetSessionId()] = &sessionRuntime{
		sessionID: session.GetSessionId(),
		endpoint:  endpoint,
		server:    server,
		worker:    worker,
	}
	return nil
}

func (r *Runtime) StopSession(_ context.Context, session *core.Session, _ string) error {
	if session == nil {
		return nil
	}
	r.mu.Lock()
	runtime := r.sessions[session.GetSessionId()]
	delete(r.sessions, session.GetSessionId())
	r.mu.Unlock()
	if runtime == nil {
		return nil
	}
	return runtime.stop(r.cfg.ShutdownTimeout)
}

func (r *Runtime) EnqueueTask(context.Context, *core.Task) error {
	return nil
}

func (r *Runtime) ExecuteTask(ctx context.Context, task *core.Task) (core.TaskExecutionResult, error) {
	if task == nil || task.GetInput() == nil {
		return core.TaskExecutionResult{}, core.NewError(core.CodeValidationFailed, "task input is required", false)
	}
	r.mu.Lock()
	session := r.sessions[task.GetInput().GetSessionId()]
	r.mu.Unlock()
	if session == nil {
		return core.TaskExecutionResult{}, core.NewError(core.CodeBrowserUnavailable, "camoufox session runtime is not running", true)
	}
	return session.executeTask(ctx, task, r.cfg.TaskTimeout)
}

func (r *Runtime) startServer(ctx context.Context, session *core.Session) (string, *serverProcess, error) {
	optionsMap, err := serverOptions(r.cfg, session)
	if err != nil {
		return "", nil, core.NewError(core.CodeProxyFailed, err.Error(), false)
	}
	options, err := encodeOptions(optionsMap)
	if err != nil {
		return "", nil, core.NewError(core.CodeInternal, err.Error(), false)
	}
	cmd := exec.Command(r.cfg.PythonPath, "-u", "-c", r.scripts.server)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = append(os.Environ(), r.cfg.ExtraEnv...)
	cmd.Env = append(cmd.Env, "CAMOUFOX_SERVER_OPTIONS_JSON="+options)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", nil, core.NewError(core.CodeInternal, err.Error(), true)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", nil, core.NewError(core.CodeInternal, err.Error(), true)
	}
	if err := cmd.Start(); err != nil {
		return "", nil, core.NewError(core.CodeBrowserUnavailable, err.Error(), true)
	}
	process := &serverProcess{
		cmd:    cmd,
		done:   make(chan error, 1),
		stdout: newTailBuffer(16 * 1024),
		stderr: newTailBuffer(16 * 1024),
	}
	go func() {
		process.done <- cmd.Wait()
	}()
	go copyTail(process.stderr, stderr)

	endpointCh := make(chan string, 1)
	go scanEndpoint(process.stdout, stdout, endpointCh)

	timeout := time.NewTimer(r.cfg.StartupTimeout)
	defer timeout.Stop()
	select {
	case endpoint := <-endpointCh:
		return endpoint, process, nil
	case err := <-process.done:
		return "", nil, core.NewError(core.CodeBrowserUnavailable, process.failureMessage("camoufox server exited before endpoint", err), true)
	case <-timeout.C:
		_ = process.stop(r.cfg.ShutdownTimeout)
		return "", nil, core.NewError(core.CodeTimeout, process.failureMessage("camoufox server startup timed out", nil), true)
	case <-ctx.Done():
		_ = process.stop(r.cfg.ShutdownTimeout)
		return "", nil, core.NewError(core.CodeTimeout, ctx.Err().Error(), true)
	}
}

func (r *Runtime) startWorker(ctx context.Context, endpoint string, session *core.Session) (*workerProcess, error) {
	options, err := encodeOptions(workerOptions(endpoint, r.cfg, session))
	if err != nil {
		return nil, core.NewError(core.CodeInternal, err.Error(), false)
	}
	cmd := exec.Command(r.cfg.PythonPath, "-u", "-c", r.scripts.worker)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = append(os.Environ(), r.cfg.ExtraEnv...)
	cmd.Env = append(cmd.Env, "CAMOUFOX_WORKER_OPTIONS_JSON="+options)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, core.NewError(core.CodeInternal, err.Error(), true)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, core.NewError(core.CodeInternal, err.Error(), true)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, core.NewError(core.CodeInternal, err.Error(), true)
	}
	if err := cmd.Start(); err != nil {
		return nil, core.NewError(core.CodeBrowserUnavailable, err.Error(), true)
	}
	worker := &workerProcess{
		cmd:    cmd,
		stdin:  stdin,
		lines:  make(chan string),
		done:   make(chan error, 1),
		stderr: newTailBuffer(16 * 1024),
	}
	go func() {
		worker.done <- cmd.Wait()
	}()
	go copyTail(worker.stderr, stderr)
	go scanWorkerLines(stdout, worker.lines)

	line, err := worker.readLine(ctx, r.cfg.StartupTimeout)
	if err != nil {
		_ = worker.stop(r.cfg.ShutdownTimeout)
		return nil, err
	}
	var ready workerReady
	if err := json.Unmarshal([]byte(line), &ready); err != nil || ready.Type != "ready" {
		_ = worker.stop(r.cfg.ShutdownTimeout)
		if err != nil {
			return nil, core.NewError(core.CodeBrowserUnavailable, "camoufox worker readiness response is invalid: "+err.Error(), true)
		}
		return nil, core.NewError(core.CodeBrowserUnavailable, "camoufox worker readiness response is invalid", true)
	}
	return worker, nil
}

type sessionRuntime struct {
	sessionID string
	endpoint  string
	server    *serverProcess
	worker    *workerProcess
}

func (s *sessionRuntime) executeTask(ctx context.Context, task *core.Task, defaultTimeout time.Duration) (core.TaskExecutionResult, error) {
	timeout := taskTimeout(task, defaultTimeout)
	taskJSON, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(task)
	if err != nil {
		return core.TaskExecutionResult{}, core.NewError(core.CodeInternal, err.Error(), false)
	}
	request := []byte(`{"type":"execute_task","task":` + string(taskJSON) + "}\n")
	s.worker.mu.Lock()
	defer s.worker.mu.Unlock()
	if _, err := s.worker.stdin.Write(request); err != nil {
		return core.TaskExecutionResult{}, core.NewError(core.CodeBrowserUnavailable, s.worker.failureMessage("camoufox worker write failed", err), true)
	}
	line, err := s.worker.readLine(ctx, timeout)
	if err != nil {
		return core.TaskExecutionResult{}, err
	}
	response, err := decodeWorkerResponse(line)
	if err != nil {
		return core.TaskExecutionResult{}, err
	}
	result := core.TaskExecutionResult{Results: response.Results, Artifacts: response.Artifacts}
	if response.Error != nil {
		return result, response.Error
	}
	return result, nil
}

func (s *sessionRuntime) stop(timeout time.Duration) error {
	var err error
	if s.worker != nil {
		err = errors.Join(err, s.worker.stop(timeout))
	}
	if s.server != nil {
		err = errors.Join(err, s.server.stop(timeout))
	}
	return err
}

type serverProcess struct {
	cmd    *exec.Cmd
	done   chan error
	stdout *tailBuffer
	stderr *tailBuffer
}

func (p *serverProcess) stop(timeout time.Duration) error {
	return stopCommand(p.cmd, p.done, timeout)
}

func (p *serverProcess) failureMessage(prefix string, err error) string {
	return failureMessage(prefix, err, p.stderr.String(), p.stdout.String())
}

type workerProcess struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	lines  chan string
	done   chan error
	stderr *tailBuffer
	mu     sync.Mutex
}

func (p *workerProcess) readLine(ctx context.Context, timeout time.Duration) (string, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case line, ok := <-p.lines:
		if !ok {
			return "", core.NewError(core.CodeBrowserUnavailable, p.failureMessage("camoufox worker stdout closed", nil), true)
		}
		return line, nil
	case err := <-p.done:
		return "", core.NewError(core.CodeBrowserUnavailable, p.failureMessage("camoufox worker exited", err), true)
	case <-timer.C:
		return "", core.NewError(core.CodeTimeout, p.failureMessage("camoufox worker timed out", nil), true)
	case <-ctx.Done():
		return "", core.NewError(core.CodeTimeout, ctx.Err().Error(), true)
	}
}

func (p *workerProcess) stop(timeout time.Duration) error {
	_, _ = io.WriteString(p.stdin, `{"type":"stop"}`+"\n")
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-p.done:
		return ignoreFinished(err)
	case <-timer.C:
	}
	return stopCommand(p.cmd, p.done, timeout)
}

func (p *workerProcess) failureMessage(prefix string, err error) string {
	return failureMessage(prefix, err, p.stderr.String(), "")
}

type workerReady struct {
	Type string `json:"type"`
}

type workerResponseEnvelope struct {
	Type      string            `json:"type"`
	TaskID    string            `json:"task_id"`
	Results   []json.RawMessage `json:"results"`
	Artifacts []json.RawMessage `json:"artifacts"`
	Error     *workerError      `json:"error"`
}

type workerError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

type decodedWorkerResponse struct {
	Results   []*browserautomationv1.BrowserCommandResult
	Artifacts []*browserautomationv1.BrowserArtifact
	Error     *core.Error
}

func decodeWorkerResponse(line string) (decodedWorkerResponse, error) {
	var envelope workerResponseEnvelope
	if err := json.Unmarshal([]byte(line), &envelope); err != nil {
		return decodedWorkerResponse{}, core.NewError(core.CodeBrowserUnavailable, "camoufox worker response is invalid: "+err.Error(), true)
	}
	if envelope.Type != "task_result" {
		return decodedWorkerResponse{}, core.NewError(core.CodeBrowserUnavailable, "camoufox worker response type is invalid", true)
	}
	unmarshal := protojson.UnmarshalOptions{DiscardUnknown: true}
	response := decodedWorkerResponse{
		Results:   make([]*browserautomationv1.BrowserCommandResult, 0, len(envelope.Results)),
		Artifacts: make([]*browserautomationv1.BrowserArtifact, 0, len(envelope.Artifacts)),
	}
	for _, raw := range envelope.Results {
		result := &browserautomationv1.BrowserCommandResult{}
		if err := unmarshal.Unmarshal(raw, result); err != nil {
			return decodedWorkerResponse{}, core.NewError(core.CodeBrowserUnavailable, "camoufox command result is invalid: "+err.Error(), true)
		}
		response.Results = append(response.Results, result)
	}
	for _, raw := range envelope.Artifacts {
		artifact := &browserautomationv1.BrowserArtifact{}
		if err := unmarshal.Unmarshal(raw, artifact); err != nil {
			return decodedWorkerResponse{}, core.NewError(core.CodeBrowserUnavailable, "camoufox artifact is invalid: "+err.Error(), true)
		}
		response.Artifacts = append(response.Artifacts, artifact)
	}
	if envelope.Error != nil {
		response.Error = core.NewError(core.ErrorCode(envelope.Error.Code), envelope.Error.Message, envelope.Error.Retryable)
	}
	return response, nil
}

func taskTimeout(task *core.Task, fallback time.Duration) time.Duration {
	if task == nil || task.GetInput() == nil {
		return fallback
	}
	timeout := duration(task.GetInput().GetTimeout())
	if timeout <= 0 {
		return fallback
	}
	return timeout
}

func duration(value *durationpb.Duration) time.Duration {
	if value == nil {
		return 0
	}
	return value.AsDuration()
}

func scanEndpoint(log io.Writer, stdout io.Reader, endpointCh chan<- string) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	sent := false
	for scanner.Scan() {
		line := scanner.Text()
		_, _ = io.WriteString(log, line+"\n")
		if sent {
			continue
		}
		if endpoint := endpointPattern.FindString(line); endpoint != "" {
			endpointCh <- endpoint
			sent = true
		}
	}
}

func scanWorkerLines(stdout io.Reader, lines chan<- string) {
	defer close(lines)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		lines <- scanner.Text()
	}
}

func copyTail(dst io.Writer, src io.Reader) {
	_, _ = io.Copy(dst, src)
}

func stopCommand(cmd *exec.Cmd, done <-chan error, timeout time.Duration) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	select {
	case err := <-done:
		return ignoreFinished(err)
	default:
	}
	signalProcessGroup(cmd, syscall.SIGINT)
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-done:
		return ignoreFinished(err)
	case <-timer.C:
		signalProcessGroup(cmd, syscall.SIGKILL)
		select {
		case err := <-done:
			return ignoreFinished(err)
		case <-time.After(time.Second):
			return core.NewError(core.CodeBrowserUnavailable, "process did not exit after kill", true)
		}
	}
}

func signalProcessGroup(cmd *exec.Cmd, signal syscall.Signal) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	if pid <= 0 {
		return
	}
	if err := syscall.Kill(-pid, signal); err == nil {
		return
	}
	if signal == syscall.SIGINT {
		_ = cmd.Process.Signal(os.Interrupt)
		return
	}
	_ = cmd.Process.Kill()
}

func ignoreFinished(err error) error {
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return nil
	}
	return err
}

func failureMessage(prefix string, err error, stderr string, stdout string) string {
	parts := []string{prefix}
	if err != nil {
		parts = append(parts, err.Error())
	}
	if strings.TrimSpace(stderr) != "" {
		parts = append(parts, "stderr: "+strings.TrimSpace(stderr))
	}
	if strings.TrimSpace(stdout) != "" {
		parts = append(parts, "stdout: "+strings.TrimSpace(stdout))
	}
	return strings.Join(parts, "; ")
}
