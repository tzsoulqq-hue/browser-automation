package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	browserautomationv1 "github.com/byte-v-forge/browser-automation/gen/go/byte/v/forge/contracts/browserautomation/v1"
	"github.com/byte-v-forge/browser-automation/internal/core"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Repository struct {
	pool             *pgxpool.Pool
	statementTimeout time.Duration
	marshal          protojson.MarshalOptions
	unmarshal        protojson.UnmarshalOptions
}

var _ core.Store = (*Repository)(nil)

func NewRepository(pool *pgxpool.Pool, statementTimeout time.Duration) *Repository {
	return &Repository{
		pool:             pool,
		statementTimeout: statementTimeout,
		marshal: protojson.MarshalOptions{
			UseProtoNames:   false,
			EmitUnpopulated: false,
		},
		unmarshal: protojson.UnmarshalOptions{
			DiscardUnknown: true,
		},
	}
}

func (r *Repository) CreateSession(ctx context.Context, session *core.Session) error {
	if session == nil || session.GetSessionId() == "" {
		return core.NewError(core.CodeValidationFailed, "session_id is required", false)
	}
	data, err := r.encode(session)
	if err != nil {
		return err
	}
	labels, err := jsonMap(session.GetLabels())
	if err != nil {
		return err
	}
	ctx, cancel := r.context(ctx)
	defer cancel()
	_, err = r.pool.Exec(ctx, `
		insert into browser_automation_sessions (
			session_id, request_id, status, labels, data, created_at, updated_at, expires_at
		)
		values ($1, $2, $3, $4, $5, $6, $7, $8)
	`,
		session.GetSessionId(),
		nullableString(session.GetRequestId()),
		int32(session.GetStatus()),
		labels,
		data,
		timestampTime(session.GetCreatedAt()),
		timestampTime(session.GetUpdatedAt()),
		nullableTimestampTime(session.GetExpiresAt()),
	)
	return mapUniqueViolation(err, "session already exists")
}

func (r *Repository) GetSession(ctx context.Context, sessionID string) (*core.Session, error) {
	return r.getSession(ctx, "session_id = $1", sessionID)
}

func (r *Repository) GetSessionByRequestID(ctx context.Context, requestID string) (*core.Session, error) {
	return r.getSession(ctx, "request_id = $1", requestID)
}

func (r *Repository) UpdateSession(ctx context.Context, session *core.Session) error {
	if session == nil || session.GetSessionId() == "" {
		return core.NewError(core.CodeValidationFailed, "session_id is required", false)
	}
	data, err := r.encode(session)
	if err != nil {
		return err
	}
	labels, err := jsonMap(session.GetLabels())
	if err != nil {
		return err
	}
	ctx, cancel := r.context(ctx)
	defer cancel()
	tag, err := r.pool.Exec(ctx, `
		update browser_automation_sessions
		set request_id = $2,
		    status = $3,
		    labels = $4,
		    data = $5,
		    updated_at = $6,
		    expires_at = $7
		where session_id = $1
	`,
		session.GetSessionId(),
		nullableString(session.GetRequestId()),
		int32(session.GetStatus()),
		labels,
		data,
		timestampTime(session.GetUpdatedAt()),
		nullableTimestampTime(session.GetExpiresAt()),
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return core.NewError(core.CodeSessionNotFound, "browser session not found", false)
	}
	return nil
}

func (r *Repository) CreateTask(ctx context.Context, task *core.Task) error {
	if task == nil || task.GetTaskId() == "" {
		return core.NewError(core.CodeValidationFailed, "task_id is required", false)
	}
	data, err := r.encode(task)
	if err != nil {
		return err
	}
	labels, err := jsonMap(task.GetLabels())
	if err != nil {
		return err
	}
	ctx, cancel := r.context(ctx)
	defer cancel()
	_, err = r.pool.Exec(ctx, `
		insert into browser_automation_tasks (
			task_id, request_id, session_id, status, task_key, scenario_key, labels,
			data, created_at, updated_at, completed_at
		)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`,
		task.GetTaskId(),
		nullableString(task.GetRequestId()),
		task.GetInput().GetSessionId(),
		int32(task.GetStatus()),
		task.GetInput().GetTaskKey(),
		task.GetInput().GetScenarioKey(),
		labels,
		data,
		timestampTime(task.GetCreatedAt()),
		timestampTime(task.GetUpdatedAt()),
		nullableTimestampTime(task.GetCompletedAt()),
	)
	return mapUniqueViolation(err, "task already exists")
}

func (r *Repository) GetTask(ctx context.Context, taskID string) (*core.Task, error) {
	return r.getTask(ctx, "task_id = $1", taskID)
}

func (r *Repository) GetTaskByRequestID(ctx context.Context, requestID string) (*core.Task, error) {
	return r.getTask(ctx, "request_id = $1", requestID)
}

func (r *Repository) ListTasks(ctx context.Context, filter *core.TaskFilter, pageSize int, pageToken string) (core.TaskListResult, error) {
	offset, err := parsePageToken(pageToken)
	if err != nil {
		return core.TaskListResult{}, err
	}
	pageSize = normalizePageSize(pageSize)
	if filter == nil {
		filter = &browserautomationv1.BrowserTaskFilter{}
	}
	ctx, cancel := r.context(ctx)
	defer cancel()
	rows, err := r.pool.Query(ctx, `
		select data
		from browser_automation_tasks
		where ($1 = '' or session_id = $1)
		  and ($2::int = 0 or status = $2)
		  and ($3 = '' or task_key = $3)
		  and ($4 = '' or scenario_key = $4)
		  and ($5 = '' or (labels ? $5 and ($6 = '' or labels ->> $5 = $6)))
		  and ($7::timestamptz is null or created_at >= $7)
		  and ($8::timestamptz is null or created_at <= $8)
		order by created_at desc, task_id asc
		limit $9 offset $10
	`,
		filter.GetSessionId(),
		int32(filter.GetStatus()),
		filter.GetTaskKey(),
		filter.GetScenarioKey(),
		filter.GetLabelKey(),
		filter.GetLabelValue(),
		nullableTimestampTime(filter.GetCreatedAfter()),
		nullableTimestampTime(filter.GetCreatedBefore()),
		pageSize+1,
		offset,
	)
	if err != nil {
		return core.TaskListResult{}, err
	}
	defer rows.Close()

	tasks := make([]*core.Task, 0, pageSize)
	hasMore := false
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return core.TaskListResult{}, err
		}
		if len(tasks) == pageSize {
			hasMore = true
			continue
		}
		task, err := r.decodeTask(data)
		if err != nil {
			return core.TaskListResult{}, err
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return core.TaskListResult{}, err
	}
	nextPageToken := ""
	if hasMore {
		nextPageToken = strconv.Itoa(offset + pageSize)
	}
	return core.TaskListResult{Tasks: tasks, NextPageToken: nextPageToken}, nil
}

func (r *Repository) UpdateTask(ctx context.Context, task *core.Task) error {
	if task == nil || task.GetTaskId() == "" {
		return core.NewError(core.CodeValidationFailed, "task_id is required", false)
	}
	data, err := r.encode(task)
	if err != nil {
		return err
	}
	labels, err := jsonMap(task.GetLabels())
	if err != nil {
		return err
	}
	ctx, cancel := r.context(ctx)
	defer cancel()
	tag, err := r.pool.Exec(ctx, `
		update browser_automation_tasks
		set request_id = $2,
		    session_id = $3,
		    status = $4,
		    task_key = $5,
		    scenario_key = $6,
		    labels = $7,
		    data = $8,
		    updated_at = $9,
		    completed_at = $10
		where task_id = $1
	`,
		task.GetTaskId(),
		nullableString(task.GetRequestId()),
		task.GetInput().GetSessionId(),
		int32(task.GetStatus()),
		task.GetInput().GetTaskKey(),
		task.GetInput().GetScenarioKey(),
		labels,
		data,
		timestampTime(task.GetUpdatedAt()),
		nullableTimestampTime(task.GetCompletedAt()),
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return core.NewError(core.CodeTaskNotFound, "browser task not found", false)
	}
	return nil
}

func (r *Repository) getSession(ctx context.Context, where string, value string) (*core.Session, error) {
	if value == "" {
		return nil, core.NewError(core.CodeValidationFailed, "session lookup value is required", false)
	}
	ctx, cancel := r.context(ctx)
	defer cancel()
	var data []byte
	err := r.pool.QueryRow(ctx, "select data from browser_automation_sessions where "+where, value).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, core.NewError(core.CodeSessionNotFound, "browser session not found", false)
	}
	if err != nil {
		return nil, err
	}
	return r.decodeSession(data)
}

func (r *Repository) getTask(ctx context.Context, where string, value string) (*core.Task, error) {
	if value == "" {
		return nil, core.NewError(core.CodeValidationFailed, "task lookup value is required", false)
	}
	ctx, cancel := r.context(ctx)
	defer cancel()
	var data []byte
	err := r.pool.QueryRow(ctx, "select data from browser_automation_tasks where "+where, value).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, core.NewError(core.CodeTaskNotFound, "browser task not found", false)
	}
	if err != nil {
		return nil, err
	}
	return r.decodeTask(data)
}

func (r *Repository) encode(message proto.Message) ([]byte, error) {
	data, err := r.marshal.Marshal(message)
	if err != nil {
		return nil, core.NewError(core.CodeInternal, "encode browser automation projection failed", false)
	}
	return data, nil
}

func (r *Repository) decodeSession(data []byte) (*core.Session, error) {
	session := &browserautomationv1.BrowserSession{}
	if err := r.unmarshal.Unmarshal(data, session); err != nil {
		return nil, core.NewError(core.CodeInternal, "decode browser session failed", false)
	}
	return proto.Clone(session).(*core.Session), nil
}

func (r *Repository) decodeTask(data []byte) (*core.Task, error) {
	task := &browserautomationv1.BrowserTask{}
	if err := r.unmarshal.Unmarshal(data, task); err != nil {
		return nil, core.NewError(core.CodeInternal, "decode browser task failed", false)
	}
	return proto.Clone(task).(*core.Task), nil
}

func (r *Repository) context(ctx context.Context) (context.Context, context.CancelFunc) {
	if r.statementTimeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, r.statementTimeout)
}

func timestampTime(timestamp *timestamppb.Timestamp) time.Time {
	if timestamp == nil {
		return time.Time{}
	}
	return timestamp.AsTime()
}

func nullableTimestampTime(timestamp *timestamppb.Timestamp) any {
	if timestamp == nil {
		return nil
	}
	return timestamp.AsTime()
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func jsonMap(values map[string]string) ([]byte, error) {
	if len(values) == 0 {
		return []byte(`{}`), nil
	}
	return json.Marshal(values)
}

func parsePageToken(pageToken string) (int, error) {
	if pageToken == "" {
		return 0, nil
	}
	offset, err := strconv.Atoi(pageToken)
	if err != nil || offset < 0 {
		return 0, core.NewError(core.CodeValidationFailed, "page_token must be a non-negative offset", false)
	}
	return offset, nil
}

func normalizePageSize(pageSize int) int {
	if pageSize <= 0 {
		return 50
	}
	if pageSize > 200 {
		return 200
	}
	return pageSize
}

func mapUniqueViolation(err error, message string) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return core.NewError(core.CodeValidationFailed, message, false)
	}
	return err
}
