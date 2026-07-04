// Package history persists workflow execution history, step outputs, and
// tool call audit trails in an embedded SQLite database (Spec 24). The
// database is the persistent archive; ConfigMap-based step outputs remain
// the runtime source for variable substitution.
package history

import (
	"database/sql"
	"fmt"
	"net/url"
	"time"

	_ "modernc.org/sqlite"
)

type WorkflowRun struct {
	ID             string
	Name           string
	Namespace      string
	Phase          string
	Parameters     string // JSON
	TotalSteps     int
	CompletedSteps int
	FailedSteps    int
	StartTime      *time.Time
	CompletionTime *time.Time
	Message        string
	CreatedAt      time.Time
}

type StepExecution struct {
	ID             string
	WorkflowRunID  string
	StepName       string
	AgentName      string
	Phase          string
	Input          string // JSON
	Output         string // JSON
	Error          string
	RetryCount     int
	JobName        string
	StartTime      *time.Time
	CompletionTime *time.Time
	TokensIn       int
	TokensOut      int
	CostUSD        float64
	CreatedAt      time.Time
}

type ToolCall struct {
	ID              int64
	StepExecutionID string
	ToolName        string
	ToolType        string
	ToolServer      string
	InputPreview    string
	ResultBytes     int
	ElapsedMs       int
	AutonomyLevel   string
	Timestamp       time.Time
}

// ListOptions provides filtering and pagination for list queries.
type ListOptions struct {
	Namespace string
	Name      string
	Limit     int
	Offset    int
}

// Store is the interface for execution history persistence.
// SQLite implementation is the default; PostgreSQL can be added later
// behind this same interface.
type Store interface {
	// Workflow runs
	SaveWorkflowRun(run *WorkflowRun) error
	UpdateWorkflowRun(run *WorkflowRun) error
	GetWorkflowRun(id string) (*WorkflowRun, error)
	ListWorkflowRuns(opts ListOptions) ([]WorkflowRun, error)

	// Step executions
	SaveStepExecution(step *StepExecution) error
	UpdateStepExecution(step *StepExecution) error
	GetStepExecution(id string) (*StepExecution, error)
	ListStepExecutions(workflowRunID string) ([]StepExecution, error)

	// Tool calls
	SaveToolCalls(stepExecutionID string, calls []ToolCall) error
	ListToolCalls(stepExecutionID string) ([]ToolCall, error)

	// Cleanup — deletes workflow_runs older than N days.
	// Step executions and tool calls cascade automatically.
	DeleteOlderThan(days int) (int64, error)

	// Lifecycle
	Close() error
}

const schema = `
CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL);

CREATE TABLE IF NOT EXISTS workflow_runs (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    namespace TEXT NOT NULL,
    phase TEXT NOT NULL DEFAULT 'Pending',
    parameters TEXT,
    total_steps INTEGER DEFAULT 0,
    completed_steps INTEGER DEFAULT 0,
    failed_steps INTEGER DEFAULT 0,
    start_time TIMESTAMP,
    completion_time TIMESTAMP,
    message TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_workflow_runs_name ON workflow_runs(name);
CREATE INDEX IF NOT EXISTS idx_workflow_runs_ns_name ON workflow_runs(namespace, name);
CREATE INDEX IF NOT EXISTS idx_workflow_runs_created ON workflow_runs(created_at);

CREATE TABLE IF NOT EXISTS step_executions (
    id TEXT PRIMARY KEY,
    workflow_run_id TEXT NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
    step_name TEXT NOT NULL,
    agent_name TEXT,
    phase TEXT NOT NULL DEFAULT 'Pending',
    input TEXT,
    output TEXT,
    error TEXT,
    retry_count INTEGER DEFAULT 0,
    job_name TEXT,
    start_time TIMESTAMP,
    completion_time TIMESTAMP,
    tokens_in INTEGER DEFAULT 0,
    tokens_out INTEGER DEFAULT 0,
    cost_usd REAL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_step_executions_run ON step_executions(workflow_run_id);

CREATE TABLE IF NOT EXISTS tool_calls (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    step_execution_id TEXT NOT NULL REFERENCES step_executions(id) ON DELETE CASCADE,
    tool_name TEXT NOT NULL,
    tool_type TEXT,
    tool_server TEXT,
    input_preview TEXT,
    result_bytes INTEGER DEFAULT 0,
    elapsed_ms INTEGER DEFAULT 0,
    autonomy_level TEXT,
    timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_tool_calls_step ON tool_calls(step_execution_id);
`

const schemaVersion = 1

// SQLiteStore implements Store backed by a SQLite database file.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens (creating if necessary) the SQLite database at path,
// enables WAL mode and foreign keys, and runs migrations.
func NewSQLiteStore(path string) (*SQLiteStore, error) {
	// _pragma DSN options are applied to every pooled connection.
	dsn := fmt.Sprintf("file:%s?%s", url.PathEscape(path), url.Values{
		"_pragma": []string{
			"journal_mode(WAL)",
			"synchronous(NORMAL)",
			"foreign_keys(1)",
			"busy_timeout(5000)",
		},
	}.Encode())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate schema: %w", err)
	}
	if err := ensureSchemaVersion(db); err != nil {
		db.Close()
		return nil, err
	}
	return &SQLiteStore{db: db}, nil
}

func ensureSchemaVersion(db *sql.DB) error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_version`).Scan(&count); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if count == 0 {
		if _, err := db.Exec(`INSERT INTO schema_version (version) VALUES (?)`, schemaVersion); err != nil {
			return fmt.Errorf("init schema version: %w", err)
		}
	}
	return nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// --- time helpers -----------------------------------------------------------
// All timestamps are stored as RFC3339 UTC strings. Reads also tolerate
// SQLite's CURRENT_TIMESTAMP format ("2006-01-02 15:04:05").

func encodeTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func decodeTime(s sql.NullString) *time.Time {
	if !s.Valid || s.String == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s.String); err == nil {
			t = t.UTC()
			return &t
		}
	}
	return nil
}

func nowUTC() time.Time {
	return time.Now().UTC()
}

// --- workflow runs ----------------------------------------------------------

func (s *SQLiteStore) SaveWorkflowRun(run *WorkflowRun) error {
	createdAt := run.CreatedAt
	if createdAt.IsZero() {
		createdAt = nowUTC()
	}
	_, err := s.db.Exec(`
		INSERT INTO workflow_runs
			(id, name, namespace, phase, parameters, total_steps, completed_steps,
			 failed_steps, start_time, completion_time, message, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			phase = excluded.phase,
			parameters = excluded.parameters,
			total_steps = excluded.total_steps,
			completed_steps = excluded.completed_steps,
			failed_steps = excluded.failed_steps,
			start_time = excluded.start_time,
			completion_time = excluded.completion_time,
			message = excluded.message`,
		run.ID, run.Name, run.Namespace, run.Phase, run.Parameters,
		run.TotalSteps, run.CompletedSteps, run.FailedSteps,
		encodeTime(run.StartTime), encodeTime(run.CompletionTime),
		run.Message, encodeTime(&createdAt))
	if err != nil {
		return fmt.Errorf("save workflow run %s: %w", run.ID, err)
	}
	return nil
}

func (s *SQLiteStore) UpdateWorkflowRun(run *WorkflowRun) error {
	res, err := s.db.Exec(`
		UPDATE workflow_runs SET
			phase = ?, parameters = ?, total_steps = ?, completed_steps = ?,
			failed_steps = ?, start_time = ?, completion_time = ?, message = ?
		WHERE id = ?`,
		run.Phase, run.Parameters, run.TotalSteps, run.CompletedSteps,
		run.FailedSteps, encodeTime(run.StartTime), encodeTime(run.CompletionTime),
		run.Message, run.ID)
	if err != nil {
		return fmt.Errorf("update workflow run %s: %w", run.ID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("update workflow run %s: not found", run.ID)
	}
	return nil
}

const workflowRunColumns = `id, name, namespace, phase, parameters, total_steps,
	completed_steps, failed_steps, start_time, completion_time, message, created_at`

func scanWorkflowRun(scan func(dest ...any) error) (*WorkflowRun, error) {
	var run WorkflowRun
	var params, message sql.NullString
	var start, completion, created sql.NullString
	if err := scan(&run.ID, &run.Name, &run.Namespace, &run.Phase, &params,
		&run.TotalSteps, &run.CompletedSteps, &run.FailedSteps,
		&start, &completion, &message, &created); err != nil {
		return nil, err
	}
	run.Parameters = params.String
	run.Message = message.String
	run.StartTime = decodeTime(start)
	run.CompletionTime = decodeTime(completion)
	if t := decodeTime(created); t != nil {
		run.CreatedAt = *t
	}
	return &run, nil
}

func (s *SQLiteStore) GetWorkflowRun(id string) (*WorkflowRun, error) {
	row := s.db.QueryRow(`SELECT `+workflowRunColumns+` FROM workflow_runs WHERE id = ?`, id)
	run, err := scanWorkflowRun(row.Scan)
	if err != nil {
		return nil, fmt.Errorf("get workflow run %s: %w", id, err)
	}
	return run, nil
}

func (s *SQLiteStore) ListWorkflowRuns(opts ListOptions) ([]WorkflowRun, error) {
	query := `SELECT ` + workflowRunColumns + ` FROM workflow_runs WHERE 1=1`
	var args []any
	if opts.Namespace != "" {
		query += ` AND namespace = ?`
		args = append(args, opts.Namespace)
	}
	if opts.Name != "" {
		query += ` AND name = ?`
		args = append(args, opts.Name)
	}
	query += ` ORDER BY created_at DESC, id`
	limit := opts.Limit
	if limit <= 0 {
		limit = -1 // SQLite: no limit
	}
	query += ` LIMIT ? OFFSET ?`
	args = append(args, limit, opts.Offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list workflow runs: %w", err)
	}
	defer rows.Close()

	var runs []WorkflowRun
	for rows.Next() {
		run, err := scanWorkflowRun(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan workflow run: %w", err)
		}
		runs = append(runs, *run)
	}
	return runs, rows.Err()
}

// --- step executions --------------------------------------------------------

func (s *SQLiteStore) SaveStepExecution(step *StepExecution) error {
	createdAt := step.CreatedAt
	if createdAt.IsZero() {
		createdAt = nowUTC()
	}
	_, err := s.db.Exec(`
		INSERT INTO step_executions
			(id, workflow_run_id, step_name, agent_name, phase, input, output,
			 error, retry_count, job_name, start_time, completion_time,
			 tokens_in, tokens_out, cost_usd, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			phase = excluded.phase,
			input = excluded.input,
			output = excluded.output,
			error = excluded.error,
			job_name = excluded.job_name,
			start_time = excluded.start_time,
			completion_time = excluded.completion_time,
			tokens_in = excluded.tokens_in,
			tokens_out = excluded.tokens_out,
			cost_usd = excluded.cost_usd`,
		step.ID, step.WorkflowRunID, step.StepName, step.AgentName, step.Phase,
		step.Input, step.Output, step.Error, step.RetryCount, step.JobName,
		encodeTime(step.StartTime), encodeTime(step.CompletionTime),
		step.TokensIn, step.TokensOut, step.CostUSD, encodeTime(&createdAt))
	if err != nil {
		return fmt.Errorf("save step execution %s: %w", step.ID, err)
	}
	return nil
}

func (s *SQLiteStore) UpdateStepExecution(step *StepExecution) error {
	res, err := s.db.Exec(`
		UPDATE step_executions SET
			phase = ?, input = ?, output = ?, error = ?, job_name = ?,
			start_time = ?, completion_time = ?, tokens_in = ?, tokens_out = ?,
			cost_usd = ?
		WHERE id = ?`,
		step.Phase, step.Input, step.Output, step.Error, step.JobName,
		encodeTime(step.StartTime), encodeTime(step.CompletionTime),
		step.TokensIn, step.TokensOut, step.CostUSD, step.ID)
	if err != nil {
		return fmt.Errorf("update step execution %s: %w", step.ID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("update step execution %s: not found", step.ID)
	}
	return nil
}

const stepExecutionColumns = `id, workflow_run_id, step_name, agent_name, phase,
	input, output, error, retry_count, job_name, start_time, completion_time,
	tokens_in, tokens_out, cost_usd, created_at`

func scanStepExecution(scan func(dest ...any) error) (*StepExecution, error) {
	var step StepExecution
	var agent, input, output, errMsg, job sql.NullString
	var start, completion, created sql.NullString
	if err := scan(&step.ID, &step.WorkflowRunID, &step.StepName, &agent,
		&step.Phase, &input, &output, &errMsg, &step.RetryCount, &job,
		&start, &completion, &step.TokensIn, &step.TokensOut, &step.CostUSD,
		&created); err != nil {
		return nil, err
	}
	step.AgentName = agent.String
	step.Input = input.String
	step.Output = output.String
	step.Error = errMsg.String
	step.JobName = job.String
	step.StartTime = decodeTime(start)
	step.CompletionTime = decodeTime(completion)
	if t := decodeTime(created); t != nil {
		step.CreatedAt = *t
	}
	return &step, nil
}

func (s *SQLiteStore) GetStepExecution(id string) (*StepExecution, error) {
	row := s.db.QueryRow(`SELECT `+stepExecutionColumns+` FROM step_executions WHERE id = ?`, id)
	step, err := scanStepExecution(row.Scan)
	if err != nil {
		return nil, fmt.Errorf("get step execution %s: %w", id, err)
	}
	return step, nil
}

func (s *SQLiteStore) ListStepExecutions(workflowRunID string) ([]StepExecution, error) {
	rows, err := s.db.Query(`SELECT `+stepExecutionColumns+
		` FROM step_executions WHERE workflow_run_id = ? ORDER BY created_at, id`, workflowRunID)
	if err != nil {
		return nil, fmt.Errorf("list step executions for %s: %w", workflowRunID, err)
	}
	defer rows.Close()

	var steps []StepExecution
	for rows.Next() {
		step, err := scanStepExecution(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan step execution: %w", err)
		}
		steps = append(steps, *step)
	}
	return steps, rows.Err()
}

// --- tool calls -------------------------------------------------------------

func (s *SQLiteStore) SaveToolCalls(stepExecutionID string, calls []ToolCall) error {
	if len(calls) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tool call tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO tool_calls
			(step_execution_id, tool_name, tool_type, tool_server, input_preview,
			 result_bytes, elapsed_ms, autonomy_level, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare tool call insert: %w", err)
	}
	defer stmt.Close()

	for _, call := range calls {
		ts := call.Timestamp
		if ts.IsZero() {
			ts = nowUTC()
		}
		if _, err := stmt.Exec(stepExecutionID, call.ToolName, call.ToolType,
			call.ToolServer, call.InputPreview, call.ResultBytes, call.ElapsedMs,
			call.AutonomyLevel, ts.UTC().Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("insert tool call %s: %w", call.ToolName, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tool calls: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListToolCalls(stepExecutionID string) ([]ToolCall, error) {
	rows, err := s.db.Query(`
		SELECT id, step_execution_id, tool_name, tool_type, tool_server,
		       input_preview, result_bytes, elapsed_ms, autonomy_level, timestamp
		FROM tool_calls WHERE step_execution_id = ? ORDER BY id`, stepExecutionID)
	if err != nil {
		return nil, fmt.Errorf("list tool calls for %s: %w", stepExecutionID, err)
	}
	defer rows.Close()

	var calls []ToolCall
	for rows.Next() {
		var call ToolCall
		var toolType, server, preview, autonomy sql.NullString
		var ts sql.NullString
		if err := rows.Scan(&call.ID, &call.StepExecutionID, &call.ToolName,
			&toolType, &server, &preview, &call.ResultBytes, &call.ElapsedMs,
			&autonomy, &ts); err != nil {
			return nil, fmt.Errorf("scan tool call: %w", err)
		}
		call.ToolType = toolType.String
		call.ToolServer = server.String
		call.InputPreview = preview.String
		call.AutonomyLevel = autonomy.String
		if t := decodeTime(ts); t != nil {
			call.Timestamp = *t
		}
		calls = append(calls, call)
	}
	return calls, rows.Err()
}

// --- cleanup ----------------------------------------------------------------

func (s *SQLiteStore) DeleteOlderThan(days int) (int64, error) {
	cutoff := nowUTC().AddDate(0, 0, -days).Format(time.RFC3339Nano)
	// datetime() normalizes both RFC3339 and SQLite CURRENT_TIMESTAMP formats.
	res, err := s.db.Exec(
		`DELETE FROM workflow_runs WHERE datetime(created_at) < datetime(?)`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete history older than %d days: %w", days, err)
	}
	return res.RowsAffected()
}

// setCreatedAtForTest backdates a workflow run's created_at. Test helper only.
func (s *SQLiteStore) setCreatedAtForTest(id string, t time.Time) error {
	_, err := s.db.Exec(`UPDATE workflow_runs SET created_at = ? WHERE id = ?`,
		t.UTC().Format(time.RFC3339Nano), id)
	return err
}
