package memory

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := NewSQLiteStore(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func sampleEntry(ns, scope, agent, content string) Entry {
	return Entry{Namespace: ns, ScopeKey: scope, Agent: agent, Workflow: "wf", Step: "analyze", Content: content}
}

// FTS5 must be available in the pinned modernc.org/sqlite; this is the codebase's
// first FTS5 user (Spec 34 §2). If the probe ever fails, ftsEnabled is false and
// Recall (Task 4) falls back to LIKE — but on this pin it must be true.
func TestFTS5ProbeSucceeds(t *testing.T) {
	if got := newTestStore(t).ftsEnabled; !got {
		t.Fatal("ftsEnabled=false: FTS5 not available in pinned modernc.org/sqlite — Recall would degrade to LIKE")
	}
}

func TestLearnAndSearch(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if err := s.Learn(ctx, sampleEntry("ns1", "ns1/agent/a", "a", "pod crashloop root cause was OOM")); err != nil {
		t.Fatalf("Learn: %v", err)
	}
	got, err := s.Search(ctx, "ns1", Query{Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0].Content != "pod crashloop root cause was OOM" {
		t.Fatalf("Search returned %+v", got)
	}
	if got[0].ID == "" {
		t.Error("Learn did not assign an ID")
	}
}

func TestSearchNamespaceIsolation(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	_ = s.Learn(ctx, sampleEntry("ns1", "ns1/agent/a", "a", "secret ns1 data"))
	_ = s.Learn(ctx, sampleEntry("ns2", "ns2/agent/b", "b", "secret ns2 data"))
	got, _ := s.Search(ctx, "ns1", Query{Limit: 10})
	if len(got) != 1 || got[0].Namespace != "ns1" {
		t.Fatalf("namespace leak: %+v", got)
	}
}

func TestContentCappedAt4096(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	big := make([]byte, 5000)
	for i := range big {
		big[i] = 'x'
	}
	_ = s.Learn(ctx, sampleEntry("ns1", "ns1/agent/a", "a", string(big)))
	got, _ := s.Search(ctx, "ns1", Query{Limit: 1})
	if len(got[0].Content) != 4096 {
		t.Errorf("content len = %d, want 4096 (capped)", len(got[0].Content))
	}
}

// The cap is a byte cap but must never split a rune: 4096 is not a multiple of
// 3, so all-3-byte-rune content forces a mid-rune cut if truncation is naive,
// which would feed invalid UTF-8 to the FTS tokenizer (spec 34 T3 review).
func TestContentCapRuneSafe(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	multi := strings.Repeat("世", 2000) // 6000 bytes, 3 bytes/rune; 4096 % 3 != 0
	if err := s.Learn(ctx, sampleEntry("ns1", "ns1/agent/a", "a", multi)); err != nil {
		t.Fatalf("Learn: %v", err)
	}
	got, err := s.Search(ctx, "ns1", Query{Limit: 1})
	if err != nil || len(got) != 1 {
		t.Fatalf("Search: %v (%d rows)", err, len(got))
	}
	c := got[0].Content
	if !utf8.ValidString(c) {
		t.Error("capped content is not valid UTF-8 (mid-rune cut)")
	}
	if len(c) > 4096 {
		t.Errorf("capped content is %d bytes, want <= 4096", len(c))
	}
	if len(c) != 4095 { // nearest rune boundary below 4096 for 3-byte runes
		t.Errorf("capped content is %d bytes, want 4095 (rune boundary)", len(c))
	}
}

func TestForgetRemovesRowAndFTS(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	_ = s.Learn(ctx, sampleEntry("ns1", "ns1/agent/a", "a", "forget me quickly"))
	got, _ := s.Search(ctx, "ns1", Query{Limit: 1})
	id := got[0].ID
	if err := s.Forget(ctx, "ns1", id); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	after, _ := s.Search(ctx, "ns1", Query{Text: "forget", Limit: 10})
	if len(after) != 0 {
		t.Errorf("Forget left %d rows (FTS trigger cleanup failed?)", len(after))
	}
	// The Search assertion above joins back to memories, so a leaked FTS row
	// would be hidden. Check the FTS index directly (spec 34 T3 review).
	if s.ftsEnabled {
		var n int
		if err := s.db.QueryRowContext(ctx,
			`SELECT count(*) FROM memories_fts WHERE memories_fts MATCH 'forget'`).Scan(&n); err != nil {
			t.Fatalf("direct FTS count: %v", err)
		}
		if n != 0 {
			t.Errorf("memories_fts still matches %d rows after Forget (delete trigger leaked)", n)
		}
	}
}

func TestForgetNamespaceScoped(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	_ = s.Learn(ctx, sampleEntry("ns1", "ns1/agent/a", "a", "belongs to ns1"))
	got, _ := s.Search(ctx, "ns1", Query{Limit: 1})
	if err := s.Forget(ctx, "ns2", got[0].ID); err != nil {
		t.Fatalf("Forget cross-ns should be a no-op, got err: %v", err)
	}
	still, _ := s.Search(ctx, "ns1", Query{Limit: 1})
	if len(still) != 1 {
		t.Error("Forget with wrong namespace deleted the entry")
	}
}

func TestStats(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	_ = s.Learn(ctx, sampleEntry("ns1", "ns1/agent/a", "a", "one"))
	_ = s.Learn(ctx, sampleEntry("ns1", "ns1/agent/a", "a", "two"))
	_ = s.Learn(ctx, sampleEntry("ns1", "ns1/agent/b", "b", "three"))
	_ = s.Learn(ctx, sampleEntry("ns2", "ns2/agent/a", "a", "four"))
	st, err := s.Stats(ctx, "ns1")
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if st.TotalEntries != 3 || st.PerAgent["a"] != 2 || st.PerAgent["b"] != 1 {
		t.Fatalf("Stats(ns1) = %+v", st)
	}
	// ns "" = provider-global: counts across ALL namespaces, PerAgent aggregated
	// (the MemoryProvider status reconciler path — spec 34 T11 review).
	global, err := s.Stats(ctx, "")
	if err != nil {
		t.Fatalf("Stats(global): %v", err)
	}
	if global.TotalEntries != 4 || global.PerAgent["a"] != 3 || global.PerAgent["b"] != 1 {
		t.Fatalf("Stats(\"\") = %+v, want 4 total with a=3 (aggregated across ns1+ns2), b=1", global)
	}
}

// Search must still filter by text when FTS5 is unavailable — the LIKE branch
// is the Spec 34 §2 fallback. Forcing ftsEnabled=false exercises it directly
// (in-package test; field access is intentional — spec 34 T3 review).
func TestSearchLIKEFallback(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	_ = s.Learn(ctx, sampleEntry("ns1", "ns1/agent/a", "a", "pod crashloop root cause was OOM"))
	_ = s.Learn(ctx, sampleEntry("ns1", "ns1/agent/a", "a", "unrelated network flake"))
	s.ftsEnabled = false
	got, err := s.Search(ctx, "ns1", Query{Text: "crashloop", Limit: 10})
	if err != nil {
		t.Fatalf("Search (LIKE fallback): %v", err)
	}
	if len(got) != 1 || got[0].Content != "pod crashloop root cause was OOM" {
		t.Fatalf("LIKE fallback returned %+v", got)
	}
}

// created_at is written explicitly as RFC3339Nano (sub-second resolution).
// Three back-to-back Learns land inside the same wall-clock second; with
// CURRENT_TIMESTAMP (1s resolution) their order would be undefined. Recall
// (Task 4) depends on this recency ordering (spec 34 T3 review).
func TestSubSecondRecencyOrdering(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	for _, c := range []string{"first", "second", "third"} {
		if err := s.Learn(ctx, sampleEntry("ns1", "ns1/agent/a", "a", c)); err != nil {
			t.Fatalf("Learn %q: %v", c, err)
		}
	}
	got, err := s.Search(ctx, "ns1", Query{Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 3 || got[0].Content != "third" || got[1].Content != "second" || got[2].Content != "first" {
		t.Fatalf("newest-first ordering broken: %+v", got)
	}
	if got[0].CreatedAt.IsZero() {
		t.Error("CreatedAt not decoded")
	}
	if !got[0].CreatedAt.After(got[2].CreatedAt) {
		t.Errorf("sub-second resolution lost: newest %v !after oldest %v", got[0].CreatedAt, got[2].CreatedAt)
	}
}

func TestPersistenceAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	s1, _ := NewSQLiteStore(path)
	_ = s1.Learn(ctx, sampleEntry("ns1", "ns1/agent/a", "a", "durable"))
	s1.Close()
	s2, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	got, _ := s2.Search(ctx, "ns1", Query{Limit: 10})
	if len(got) != 1 {
		t.Errorf("reopen lost data: %d rows", len(got))
	}
}

func TestRecallRelevantBeatsRecent(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	// Older but relevant vs newer but irrelevant.
	_ = s.Learn(ctx, sampleEntry("ns1", "ns1/agent/a", "a", "kubernetes pod crashloop OOM killed memory limit"))
	_ = s.Learn(ctx, sampleEntry("ns1", "ns1/agent/a", "a", "unrelated note about billing invoices"))
	got, err := s.Recall(ctx, "ns1", "ns1/agent/a", "why did the pod crashloop", 2048)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(got) == 0 || got[0].Content[:10] != "kubernetes" {
		t.Fatalf("relevant entry did not rank first: %+v", got)
	}
}

func TestRecallScopeIsolation(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	_ = s.Learn(ctx, sampleEntry("ns1", "ns1/agent/a", "a", "agent a private memory pod"))
	_ = s.Learn(ctx, sampleEntry("ns1", "ns1/agent/b", "b", "agent b private memory pod"))
	got, _ := s.Recall(ctx, "ns1", "ns1/agent/a", "pod", 2048)
	for _, e := range got {
		if e.Agent == "b" {
			t.Fatalf("agent a recalled agent b's agent-scoped memory: %+v", e)
		}
	}
}

func TestRecallGroupScopeShared(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	_ = s.Learn(ctx, sampleEntry("ns1", "ns1/group/triage", "a", "shared group lesson learned pod"))
	_ = s.Learn(ctx, sampleEntry("ns1", "ns1/group/triage", "b", "another group lesson pod"))
	got, _ := s.Recall(ctx, "ns1", "ns1/group/triage", "pod", 2048)
	if len(got) != 2 {
		t.Fatalf("group scope should pool both agents' entries, got %d", len(got))
	}
}

func TestRecallTokenBudget(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	for i := 0; i < 20; i++ {
		body := make([]byte, 400)
		for j := range body {
			body[j] = 'z'
		}
		_ = s.Learn(ctx, sampleEntry("ns1", "ns1/agent/a", "a", "pod "+string(body)))
	}
	// budget 256 tokens ≈ 1024 chars → at ~404 chars/entry, expect ~2 entries.
	got, _ := s.Recall(ctx, "ns1", "ns1/agent/a", "pod", 256)
	if len(got) == 0 || len(got) > 3 {
		t.Fatalf("token budget not enforced: got %d entries", len(got))
	}
}

func TestFTSQuerySanitization(t *testing.T) {
	cases := map[string]string{
		`why did "pod" crash?`: "why OR did OR pod OR crash",
		`a AND b OR c`:         "a OR b OR c",
		`  spaced   out  `:     "spaced OR out",
		`NEAR(x y)`:            "x OR y",
		`café AND ☃`:           "caf", // ASCII extraction: non-ASCII runes are term boundaries
		``:                     "",
	}
	for in, want := range cases {
		if got := ftsQuery(in); got != want {
			t.Errorf("ftsQuery(%q) = %q, want %q", in, got, want)
		}
	}
}

// Both entries match the query, but the OLDER one matches more terms — bm25 must
// rank it first despite recency. Proves real relevance ranking, not just
// match-vs-nonmatch (spec 34 T4 review).
func TestRecallBM25RanksMoreRelevantOlderEntryFirst(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	_ = s.Learn(ctx, sampleEntry("ns1", "ns1/agent/a", "a", "kubernetes pod crashloop OOM killed memory limit"))
	_ = s.Learn(ctx, sampleEntry("ns1", "ns1/agent/a", "a", "the pod restarted cleanly after the deploy"))
	got, err := s.Recall(ctx, "ns1", "ns1/agent/a", "pod crashloop OOM", 2048)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected both matching entries, got %d: %+v", len(got), got)
	}
	if got[0].Content[:10] != "kubernetes" {
		t.Fatalf("older-but-more-relevant entry did not rank first: %+v", got)
	}
}

// Empty (or operator-only) query has no terms after sanitization: Recall falls
// back to pure recency, newest first (spec 34 T4 review).
func TestRecallEmptyQueryFallsToRecency(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	_ = s.Learn(ctx, sampleEntry("ns1", "ns1/agent/a", "a", "older entry"))
	_ = s.Learn(ctx, sampleEntry("ns1", "ns1/agent/a", "a", "newer entry"))
	got, err := s.Recall(ctx, "ns1", "ns1/agent/a", "", 2048)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(got) != 2 || got[0].Content != "newer entry" {
		t.Fatalf("empty-query recency fallback broken: %+v", got)
	}
}

// With ftsEnabled=false the fallback must TERM-FILTER via LIKE, not return
// recency-only: non-matching rows must be excluded (spec 34 T4 review, arbiter
// decision). In-package field access is intentional, same as TestSearchLIKEFallback.
func TestRecallLIKEFallbackTermFilters(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	_ = s.Learn(ctx, sampleEntry("ns1", "ns1/agent/a", "a", "pod crashloop root cause was OOM"))
	_ = s.Learn(ctx, sampleEntry("ns1", "ns1/agent/a", "a", "unrelated network flake"))
	s.ftsEnabled = false
	got, err := s.Recall(ctx, "ns1", "ns1/agent/a", "crashloop", 2048)
	if err != nil {
		t.Fatalf("Recall (LIKE fallback): %v", err)
	}
	if len(got) != 1 || got[0].Content != "pod crashloop root cause was OOM" {
		t.Fatalf("LIKE fallback did not term-filter: %+v", got)
	}
}

// setCreatedAtForTest backdates a memory row (mirrors history's test-only helper).
func (s *SQLiteStore) setCreatedAtForTest(id string, t time.Time) error {
	_, err := s.db.Exec(`UPDATE memories SET created_at = ? WHERE id = ?`, t.UTC().Format(time.RFC3339Nano), id)
	return err
}

func TestRetainEvictsOldestOverCap(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	for i := 0; i < 10; i++ {
		_ = s.Learn(ctx, sampleEntry("ns1", "ns1/agent/a", "a", fmt.Sprintf("entry %d", i)))
	}
	n, err := s.Retain(ctx, "ns1/agent/a", 3)
	if err != nil {
		t.Fatalf("Retain: %v", err)
	}
	if n != 7 {
		t.Errorf("evicted %d, want 7", n)
	}
	got, _ := s.Search(ctx, "ns1", Query{ScopeKey: "ns1/agent/a", Limit: 100})
	if len(got) != 3 {
		t.Fatalf("after retain %d rows, want 3", len(got))
	}
	// Survivors must be the NEWEST 3 inserted — a keep-oldest bug would pass a
	// count-only check (spec 34 T5 review).
	want := map[string]bool{"entry 7": true, "entry 8": true, "entry 9": true}
	for _, e := range got {
		if !want[e.Content] {
			t.Errorf("survivor %q is not one of the newest 3 entries (keep-oldest bug?)", e.Content)
		}
	}
}

func TestDeleteOlderThan(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	_ = s.Learn(ctx, sampleEntry("ns1", "ns1/agent/a", "a", "old"))
	e, _ := s.Search(ctx, "ns1", Query{Limit: 1})
	_ = s.setCreatedAtForTest(e[0].ID, time.Now().AddDate(0, 0, -100))
	_ = s.Learn(ctx, sampleEntry("ns1", "ns1/agent/a", "a", "fresh"))
	n, err := s.DeleteOlderThan(ctx, 30)
	if err != nil {
		t.Fatalf("DeleteOlderThan: %v", err)
	}
	if n != 1 {
		t.Errorf("deleted %d, want 1", n)
	}
}

func TestDeleteRecallLogOlderThan(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	_ = s.WriteRecallLog(ctx, "ns1", "run-old", "s", []string{"x"})
	// Backdate in recall_log's NATIVE format ('YYYY-MM-DD HH:MM:SS', space-separated,
	// what CURRENT_TIMESTAMP actually stores) so the test exercises the real
	// stored-format-vs-RFC3339Nano-cutoff lexicographic comparison (spec 34 T5 review).
	_, _ = s.db.Exec(`UPDATE recall_log SET created_at = datetime('now','-100 days') WHERE workflow_run_id = 'run-old'`)
	_ = s.WriteRecallLog(ctx, "ns1", "run-new", "s", []string{"y"})
	n, err := s.DeleteRecallLogOlderThan(ctx, 90)
	if err != nil {
		t.Fatalf("DeleteRecallLogOlderThan: %v", err)
	}
	if n != 1 {
		t.Errorf("deleted %d recall_log rows, want 1", n)
	}
}

func TestRecallLogRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	_ = s.Learn(ctx, sampleEntry("ns1", "ns1/agent/a", "a", "recalled content one"))
	e, _ := s.Search(ctx, "ns1", Query{Limit: 1})
	id := e[0].ID
	if err := s.WriteRecallLog(ctx, "ns1", "run-1", "analyze", []string{id}); err != nil {
		t.Fatalf("WriteRecallLog: %v", err)
	}
	entries, ids, err := s.ReadRecallLog(ctx, "ns1", "run-1", "analyze")
	if err != nil {
		t.Fatalf("ReadRecallLog: %v", err)
	}
	if len(entries) != 1 || entries[0].ID != id || len(ids) != 1 {
		t.Fatalf("round trip failed: entries=%+v ids=%v", entries, ids)
	}
	// dangling id survives as an id even after Forget (Spec 34 §5 "memory deleted").
	_ = s.Forget(ctx, "ns1", id)
	entries2, ids2, _ := s.ReadRecallLog(ctx, "ns1", "run-1", "analyze")
	if len(entries2) != 0 || len(ids2) != 1 {
		t.Fatalf("dangling id handling wrong: entries=%v ids=%v", entries2, ids2)
	}
}

func TestRecallLogLastWriteWins(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	_ = s.WriteRecallLog(ctx, "ns1", "run-1", "analyze", []string{"a", "b"})
	_ = s.WriteRecallLog(ctx, "ns1", "run-1", "analyze", []string{"c"}) // retry overwrites
	_, ids, _ := s.ReadRecallLog(ctx, "ns1", "run-1", "analyze")
	if len(ids) != 1 || ids[0] != "c" {
		t.Fatalf("retry did not overwrite per run/step: %v", ids)
	}
}
