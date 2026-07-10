package dashboard

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/purko-io/purko/pkg/memory"
)

func newMemoryServer(t *testing.T) (*Server, *memory.SQLiteStore) {
	t.Helper()
	st, err := memory.NewSQLiteStore(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return &Server{Memory: st, Namespace: "ns1"}, st
}

func seedMem(t *testing.T, st *memory.SQLiteStore, ns, agent, content string) string {
	t.Helper()
	_ = st.Learn(context.Background(), memory.Entry{Namespace: ns, ScopeKey: ns + "/agent/" + agent, Agent: agent, Content: content})
	got, _ := st.Search(context.Background(), ns, memory.Query{Limit: 1})
	return got[0].ID
}

func TestHandleMemorySearch(t *testing.T) {
	s, st := newMemoryServer(t)
	seedMem(t, st, "ns1", "a", "pod crashloop OOM")
	seedMem(t, st, "ns2", "b", "other ns secret")
	rec := httptest.NewRecorder()
	s.handleMemory(rec, httptest.NewRequest("GET", "/api/memory?namespace=ns1&q=pod", nil))
	var out []memoryJSON
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out) != 1 || out[0].Agent != "a" {
		t.Fatalf("search returned %+v", out)
	}
}

func TestHandleMemoryStats(t *testing.T) {
	s, st := newMemoryServer(t)
	seedMem(t, st, "ns1", "a", "one")
	seedMem(t, st, "ns1", "a", "two")
	rec := httptest.NewRecorder()
	s.handleMemoryStats(rec, httptest.NewRequest("GET", "/api/memory/stats?namespace=ns1", nil))
	var out struct {
		TotalEntries int64            `json:"totalEntries"`
		PerAgent     map[string]int64 `json:"perAgent"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.TotalEntries != 2 || out.PerAgent["a"] != 2 {
		t.Fatalf("stats %+v", out)
	}
}

func TestHandleMemoryForgetGated(t *testing.T) {
	s, st := newMemoryServer(t)
	id := seedMem(t, st, "ns1", "a", "delete me")
	// Default (forget disabled): 403.
	t.Setenv("PURKO_MEMORY_FORGET_ENABLED", "false")
	rec := httptest.NewRecorder()
	s.handleMemoryDelete(rec, httptest.NewRequest("DELETE", "/api/memory/"+id+"?namespace=ns1", nil))
	if rec.Code != 403 {
		t.Fatalf("forget should be 403 when disabled, got %d", rec.Code)
	}
	// Enabled: deletes.
	t.Setenv("PURKO_MEMORY_FORGET_ENABLED", "true")
	rec = httptest.NewRecorder()
	s.handleMemoryDelete(rec, httptest.NewRequest("DELETE", "/api/memory/"+id+"?namespace=ns1", nil))
	if rec.Code != 200 {
		t.Fatalf("forget enabled should 200, got %d", rec.Code)
	}
	left, _ := st.Search(context.Background(), "ns1", memory.Query{Limit: 10})
	if len(left) != 0 {
		t.Errorf("entry not deleted")
	}
}

func TestHandleMemoryDisabled503(t *testing.T) {
	s := &Server{Namespace: "ns1"} // no Memory
	rec := httptest.NewRecorder()
	s.handleMemory(rec, httptest.NewRequest("GET", "/api/memory?namespace=ns1", nil))
	if rec.Code != 503 {
		t.Fatalf("nil store: handleMemory should 503, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	s.handleMemoryStats(rec, httptest.NewRequest("GET", "/api/memory/stats?namespace=ns1", nil))
	if rec.Code != 503 {
		t.Fatalf("nil store: handleMemoryStats should 503, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	s.handleMemoryDelete(rec, httptest.NewRequest("DELETE", "/api/memory/any-id?namespace=ns1", nil))
	if rec.Code != 503 {
		t.Fatalf("nil store: handleMemoryDelete should 503, got %d", rec.Code)
	}
}

func TestHandleMemoryForgetGatedRowPreserved(t *testing.T) {
	s, st := newMemoryServer(t)
	id := seedMem(t, st, "ns1", "a", "should survive 403")
	t.Setenv("PURKO_MEMORY_FORGET_ENABLED", "false")
	rec := httptest.NewRecorder()
	s.handleMemoryDelete(rec, httptest.NewRequest("DELETE", "/api/memory/"+id+"?namespace=ns1", nil))
	if rec.Code != 403 {
		t.Fatalf("forget disabled should 403, got %d", rec.Code)
	}
	left, _ := st.Search(context.Background(), "ns1", memory.Query{Limit: 10})
	if len(left) != 1 {
		t.Errorf("row must survive a 403 forget: got %d entries", len(left))
	}
}

func TestHandleMemoryRecall(t *testing.T) {
	s, st := newMemoryServer(t)
	id := seedMem(t, st, "ns1", "a", "recalled for this step")
	_ = st.WriteRecallLog(context.Background(), "ns1", "run-1", "analyze", []string{id, "gone-id"})
	rec := httptest.NewRecorder()
	s.handleMemoryRecall(rec, httptest.NewRequest("GET", "/api/memory/recall?namespace=ns1&runId=run-1&step=analyze", nil))
	var out struct {
		Entries     []memoryJSON `json:"entries"`
		DanglingIds []string     `json:"danglingIds"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Entries) != 1 || out.Entries[0].ID != id {
		t.Fatalf("entries %+v", out.Entries)
	}
	if len(out.DanglingIds) != 1 || out.DanglingIds[0] != "gone-id" {
		t.Fatalf("dangling %v", out.DanglingIds)
	}
}
