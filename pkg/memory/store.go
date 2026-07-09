// Package memory implements the Spec 34 built-in memory provider: a SQLite+FTS5
// store on the same PVC as pkg/history. It follows pkg/history/store.go patterns
// (modernc driver, inline schema, schema_version stamp). Recall/recall_log land in
// the sibling methods; this file is the store skeleton + write/search/forget/stats.
package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// compile-time proof that *SQLiteStore satisfies the Store interface.
var _ Store = (*SQLiteStore)(nil)

const maxContentChars = 4096

// Entry is a single memory record (Spec 34 §2 schema).
type Entry struct {
	ID        string
	Namespace string
	ScopeKey  string
	Agent     string
	Workflow  string
	Step      string
	Content   string
	CreatedAt time.Time
}

// Query filters dashboard browse/search (Spec 34 §8). Text empty = list newest.
type Query struct {
	Text     string
	Agent    string
	ScopeKey string
	Since    *time.Time
	Limit    int
}

// Stats powers the Memory page header + agent detail counts (Spec 34 §8).
type Stats struct {
	TotalEntries int64
	PerAgent     map[string]int64
	ProviderType string
}

// Provider is the seam every memory backend implements (Spec 34 §2); redis/pgvector/
// MCP providers drop in behind it later.
type Provider interface {
	Recall(ctx context.Context, ns, scopeKey, query string, maxTokens int) ([]Entry, error)
	Learn(ctx context.Context, e Entry) error
	Search(ctx context.Context, ns string, q Query) ([]Entry, error)
	Forget(ctx context.Context, ns, id string) error
	// Stats: ns empty = all namespaces — provider-global stats (the MemoryProvider
	// status reconciler uses this; dashboard callers always pass a real namespace).
	Stats(ctx context.Context, ns string) (Stats, error)
	Healthy(ctx context.Context) error
}

// Store is the operator-side concrete surface: Provider plus recall-log audit and
// retention. The reconciler and dashboard both hold a Store.
type Store interface {
	Provider
	WriteRecallLog(ctx context.Context, ns, runID, step string, memoryIDs []string) error
	ReadRecallLog(ctx context.Context, ns, runID, step string) ([]Entry, []string, error)
	Retain(ctx context.Context, scopeKey string, maxEntries int) (int64, error)
	DeleteOlderThan(ctx context.Context, days int) (int64, error)
	DeleteRecallLogOlderThan(ctx context.Context, days int) (int64, error)
	Close() error
}

const schema = `
CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL);

CREATE TABLE IF NOT EXISTS memories (
    id          TEXT PRIMARY KEY,
    namespace   TEXT NOT NULL,
    scope_key   TEXT NOT NULL,
    agent       TEXT NOT NULL,
    workflow    TEXT,
    step        TEXT,
    content     TEXT NOT NULL,
    created_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_memories_scope ON memories(scope_key, created_at);
CREATE INDEX IF NOT EXISTS idx_memories_ns_agent ON memories(namespace, agent);

CREATE TABLE IF NOT EXISTS recall_log (
    namespace       TEXT NOT NULL,
    workflow_run_id TEXT NOT NULL,
    step            TEXT NOT NULL,
    memory_ids      TEXT NOT NULL,
    created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (namespace, workflow_run_id, step)
);
`

// ftsSchema is applied separately so a probe failure (older sqlite) leaves the base
// tables usable and flips ftsEnabled=false (Spec 34 §2 LIKE fallback).
const ftsSchema = `
CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
    content, content='memories', content_rowid='rowid'
);
CREATE TRIGGER IF NOT EXISTS memories_ai AFTER INSERT ON memories BEGIN
    INSERT INTO memories_fts(rowid, content) VALUES (new.rowid, new.content);
END;
CREATE TRIGGER IF NOT EXISTS memories_ad AFTER DELETE ON memories BEGIN
    INSERT INTO memories_fts(memories_fts, rowid, content) VALUES ('delete', old.rowid, old.content);
END;
`

const schemaVersion = 1

// SQLiteStore is the built-in provider.
type SQLiteStore struct {
	db         *sql.DB
	ftsEnabled bool
}

// NewSQLiteStore opens (creating if absent) the memory DB at path. Mirrors
// history.NewSQLiteStore: WAL + synchronous=NORMAL (memories are advisory,
// reconstructable losses acceptable — Spec 34 §2).
func NewSQLiteStore(path string) (*SQLiteStore, error) {
	dsn := fmt.Sprintf("file:%s?%s", url.PathEscape(path), url.Values{
		"_pragma": []string{
			"journal_mode(WAL)",
			"synchronous(NORMAL)",
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
	s := &SQLiteStore{db: db}
	s.ftsEnabled = probeFTS(db)
	return s, nil
}

// probeFTS creates the FTS5 virtual table + triggers and confirms bm25() parses.
// Returns false (LIKE fallback) if any step errors — never fatal.
func probeFTS(db *sql.DB) bool {
	if _, err := db.Exec(ftsSchema); err != nil {
		return false
	}
	// bm25() only resolves inside an FTS MATCH query; a zero-row probe is enough.
	if _, err := db.Exec(`SELECT bm25(memories_fts) FROM memories_fts WHERE memories_fts MATCH 'probe' LIMIT 0`); err != nil {
		return false
	}
	return true
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

func (s *SQLiteStore) Close() error { return s.db.Close() }

// Healthy pings the DB (Spec 34 §4 status writer).
func (s *SQLiteStore) Healthy(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// Learn inserts one memory. Content is capped at 4096 bytes at write time,
// truncated on a rune boundary so the FTS tokenizer never sees invalid UTF-8
// (Spec 34 §2). ID is a uuid; created_at is written explicitly as RFC3339Nano
// (pkg/history idiom — sub-second resolution for Recall recency ordering).
// The FTS insert trigger keeps memories_fts in sync.
func (s *SQLiteStore) Learn(ctx context.Context, e Entry) error {
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	e.Content = capContent(e.Content)
	createdAt := e.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO memories (id, namespace, scope_key, agent, workflow, step, content, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.Namespace, e.ScopeKey, e.Agent, e.Workflow, e.Step, e.Content,
		createdAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("learn: %w", err)
	}
	return nil
}

// capContent truncates to at most maxContentChars bytes without splitting a
// rune: it backs up from the byte cap to the nearest rune start (zero-alloc).
func capContent(c string) string {
	if len(c) <= maxContentChars {
		return c
	}
	cut := maxContentChars
	for cut > 0 && !utf8.RuneStart(c[cut]) {
		cut--
	}
	return c[:cut]
}

// Forget hard-deletes by uuid, namespace-scoped (Spec 34 §5, §6). The AFTER DELETE
// trigger removes the FTS row via old.rowid. Wrong-namespace delete is a no-op.
func (s *SQLiteStore) Forget(ctx context.Context, ns, id string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM memories WHERE id = ? AND namespace = ?`, id, ns)
	if err != nil {
		return fmt.Errorf("forget: %w", err)
	}
	return nil
}

// Search is the dashboard browse path (Spec 34 §8). Always namespace-filtered in
// SQL. Text uses FTS when available, else LIKE; empty Text lists newest-first.
func (s *SQLiteStore) Search(ctx context.Context, ns string, q Query) ([]Entry, error) {
	limit := q.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	args := []any{ns}
	where := "m.namespace = ?"
	if q.Agent != "" {
		where += " AND m.agent = ?"
		args = append(args, q.Agent)
	}
	if q.ScopeKey != "" {
		where += " AND m.scope_key = ?"
		args = append(args, q.ScopeKey)
	}
	if q.Since != nil {
		where += " AND datetime(m.created_at) >= datetime(?)"
		args = append(args, q.Since.UTC().Format(time.RFC3339Nano))
	}
	var query string
	if q.Text != "" && s.ftsEnabled {
		where += " AND m.rowid IN (SELECT rowid FROM memories_fts WHERE memories_fts MATCH ?)"
		args = append(args, sanitizeFTS(q.Text))
	} else if q.Text != "" {
		where += " AND m.content LIKE ?"
		args = append(args, "%"+q.Text+"%")
	}
	query = `SELECT m.id, m.namespace, m.scope_key, m.agent, m.workflow, m.step, m.content, m.created_at
	         FROM memories m WHERE ` + where + ` ORDER BY m.created_at DESC LIMIT ?`
	args = append(args, limit)
	return s.queryEntries(ctx, query, args...)
}

// Stats aggregates counts per agent for a namespace (Spec 34 §8). ns empty =
// all namespaces — provider-global stats: memories are Learned under AGENT
// namespaces, so the MemoryProvider status reconciler (whose CR lives in
// purko-system) passes "" for the provider-wide entryCount (spec 34 T11 review).
// PerAgent is then aggregated across namespaces.
func (s *SQLiteStore) Stats(ctx context.Context, ns string) (Stats, error) {
	st := Stats{PerAgent: map[string]int64{}, ProviderType: "builtin"}
	query := `SELECT agent, COUNT(*) FROM memories WHERE namespace = ? GROUP BY agent`
	args := []any{ns}
	if ns == "" {
		query = `SELECT agent, COUNT(*) FROM memories GROUP BY agent`
		args = nil
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return st, fmt.Errorf("stats: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var agent string
		var n int64
		if err := rows.Scan(&agent, &n); err != nil {
			return st, err
		}
		st.PerAgent[agent] = n
		st.TotalEntries += n
	}
	return st, rows.Err()
}

func (s *SQLiteStore) queryEntries(ctx context.Context, query string, args ...any) ([]Entry, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query memories: %w", err)
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var e Entry
		var created sql.NullString
		if err := rows.Scan(&e.ID, &e.Namespace, &e.ScopeKey, &e.Agent, &e.Workflow, &e.Step, &e.Content, &created); err != nil {
			return nil, err
		}
		if t := decodeTime(created); t != nil {
			e.CreatedAt = *t
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// decodeTime parses stored timestamps. Writes are RFC3339Nano UTC strings
// (pkg/history idiom); reads also tolerate SQLite's CURRENT_TIMESTAMP format
// for rows created before the explicit-write change.
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

var ftsWordRe = regexp.MustCompile(`[A-Za-z0-9]+`)

// ftsTerms extracts sanitized bare terms from arbitrary text: alphanumeric runs
// only, FTS operators (AND/OR/NOT/NEAR) and punctuation dropped. Shared by the
// FTS MATCH builder and the LIKE-fallback term filter (Spec 34 §2).
func ftsTerms(q string) []string {
	ops := map[string]bool{"AND": true, "OR": true, "NOT": true, "NEAR": true}
	var terms []string
	for _, w := range ftsWordRe.FindAllString(q, -1) {
		if ops[w] {
			continue
		}
		terms = append(terms, w)
	}
	return terms
}

// ftsQuery reduces arbitrary text to a safe FTS5 MATCH string: the sanitized
// bare terms OR-joined (Spec 34 §2 "bare terms OR-joined, operators stripped").
func ftsQuery(q string) string {
	return strings.Join(ftsTerms(q), " OR ")
}

// sanitizeFTS delegates to ftsQuery (Spec 34 §2).
func sanitizeFTS(q string) string { return ftsQuery(q) }

// Recall returns the best memories for a step's input text, packed best-first until
// the maxTokens budget (chars/4) is exhausted (Spec 34 §2). Always namespace- and
// scope-filtered in SQL. bm25() is more-negative-is-better, so ASC ranks best first.
// Adaptation: query leads with memories_fts so bm25() is correctly bound to the
// FTS5 query driver; the brief's aliased JOIN form (FROM memories m JOIN memories_fts f
// ... AND memories_fts MATCH ?) is ambiguous in SQLite's MATCH resolution.
func (s *SQLiteStore) Recall(ctx context.Context, ns, scopeKey, query string, maxTokens int) ([]Entry, error) {
	if maxTokens <= 0 {
		maxTokens = 2048
	}
	terms := ftsTerms(query)
	var ranked []Entry
	var err error
	switch {
	case s.ftsEnabled && len(terms) > 0:
		ranked, err = s.queryEntries(ctx,
			`SELECT m.id, m.namespace, m.scope_key, m.agent, m.workflow, m.step, m.content, m.created_at
			 FROM memories_fts
			 JOIN memories m ON m.rowid = memories_fts.rowid
			 WHERE memories_fts MATCH ? AND m.namespace = ? AND m.scope_key = ?
			 ORDER BY bm25(memories_fts) ASC, m.created_at DESC
			 LIMIT 50`, strings.Join(terms, " OR "), ns, scopeKey)
	case len(terms) > 0:
		// LIKE fallback (spec 34 T4 review, arbiter decision): term-filter with
		// OR-joined bound LIKE patterns — never recency-only when terms exist.
		// Terms are alphanumeric-only (ftsWordRe), so no LIKE wildcard injection.
		conds := make([]string, len(terms))
		args := []any{ns, scopeKey}
		for i, t := range terms {
			conds[i] = "content LIKE ?"
			args = append(args, "%"+t+"%")
		}
		ranked, err = s.queryEntries(ctx,
			`SELECT id, namespace, scope_key, agent, workflow, step, content, created_at
			 FROM memories WHERE namespace = ? AND scope_key = ?
			 AND (`+strings.Join(conds, " OR ")+`)
			 ORDER BY created_at DESC LIMIT 50`, args...)
	default:
		// Empty after sanitization: pure recency.
		ranked, err = s.queryEntries(ctx,
			`SELECT id, namespace, scope_key, agent, workflow, step, content, created_at
			 FROM memories WHERE namespace = ? AND scope_key = ?
			 ORDER BY created_at DESC LIMIT 50`, ns, scopeKey)
	}
	if err != nil {
		return nil, err
	}
	// Pack under budget: chars/4 heuristic (same as executor MAX_CONTEXT_TOKENS).
	budget := maxTokens * 4
	out := []Entry{}
	used := 0
	for _, e := range ranked {
		if used+len(e.Content) > budget && len(out) > 0 {
			break
		}
		out = append(out, e)
		used += len(e.Content)
	}
	return out, nil
}

// WriteRecallLog records which memory IDs were injected into a step, keyed by
// namespace/run-ID/step. Last-write-wins so a step retry overwrites (Spec 34 §2).
func (s *SQLiteStore) WriteRecallLog(ctx context.Context, ns, runID, step string, memoryIDs []string) error {
	b, err := json.Marshal(memoryIDs)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO recall_log (namespace, workflow_run_id, step, memory_ids)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(namespace, workflow_run_id, step)
		 DO UPDATE SET memory_ids = excluded.memory_ids, created_at = CURRENT_TIMESTAMP`,
		ns, runID, step, string(b))
	if err != nil {
		return fmt.Errorf("write recall_log: %w", err)
	}
	return nil
}

// Retain enforces the per-scope cap (Spec 34 §5): keeps the newest maxEntries rows
// for scopeKey, deletes the rest (oldest evicted). FTS cleanup rides the delete
// trigger. Returns the number evicted.
func (s *SQLiteStore) Retain(ctx context.Context, scopeKey string, maxEntries int) (int64, error) {
	if maxEntries <= 0 {
		maxEntries = 500
	}
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM memories WHERE scope_key = ? AND id NOT IN (
			SELECT id FROM memories WHERE scope_key = ? ORDER BY created_at DESC LIMIT ?
		)`, scopeKey, scopeKey, maxEntries)
	if err != nil {
		return 0, fmt.Errorf("retain: %w", err)
	}
	return res.RowsAffected()
}

// DeleteOlderThan removes memories older than days (Spec 34 §5 optional age cap).
// Adaptation: created_at is stored as RFC3339Nano strings; lexicographic comparison
// is exact at day granularity (RFC3339Nano trims trailing fractional zeros, so
// same-second ties have <1s boundary fuzz) and preferred over datetime() which
// truncates to 1-second resolution.
func (s *SQLiteStore) DeleteOlderThan(ctx context.Context, days int) (int64, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM memories WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete memories older than %dd: %w", days, err)
	}
	return res.RowsAffected()
}

// DeleteRecallLogOlderThan prunes recall_log (Spec 34 §5: default 90d, grows
// unboundedly at one row per step execution otherwise).
// Adaptation: cutoff boundary compared lexicographically (RFC3339Nano string);
// recall_log.created_at is CURRENT_TIMESTAMP format which sorts consistently
// at day-level granularity against RFC3339Nano cutoff strings.
// Boundary semantics are day-granular: the whole cutoff day is pruned (over-deletes
// at most one boundary day; monotonically safe, never under-deletes).
func (s *SQLiteStore) DeleteRecallLogOlderThan(ctx context.Context, days int) (int64, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM recall_log WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete recall_log older than %dd: %w", days, err)
	}
	return res.RowsAffected()
}

// ReadRecallLog resolves a step's recalled memories for the transparency panel
// (Spec 34 §8). Returns the still-existing entries AND the full recorded id list;
// ids without a matching entry are dangling ("memory deleted" in the UI).
func (s *SQLiteStore) ReadRecallLog(ctx context.Context, ns, runID, step string) ([]Entry, []string, error) {
	var raw string
	err := s.db.QueryRowContext(ctx,
		`SELECT memory_ids FROM recall_log WHERE namespace = ? AND workflow_run_id = ? AND step = ?`,
		ns, runID, step).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read recall_log: %w", err)
	}
	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return nil, nil, err
	}
	entries := []Entry{}
	for _, id := range ids {
		rows, err := s.queryEntries(ctx,
			`SELECT id, namespace, scope_key, agent, workflow, step, content, created_at
			 FROM memories WHERE id = ? AND namespace = ?`, id, ns)
		if err != nil {
			return nil, ids, err
		}
		entries = append(entries, rows...)
	}
	return entries, ids, nil
}
