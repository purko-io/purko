package dashboard

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/purko-io/purko/api/v1alpha1"
	"github.com/purko-io/purko/pkg/memory"
)

// memoryJSON is the wire shape for a memory entry (Spec 34 §8) — DB structs are
// never serialized directly (mirrors history's runToJSON).
type memoryJSON struct {
	ID        string `json:"id"`
	Namespace string `json:"namespace"`
	ScopeKey  string `json:"scopeKey"`
	Agent     string `json:"agent"`
	Workflow  string `json:"workflow"`
	Step      string `json:"step"`
	Content   string `json:"content"`
	CreatedAt string `json:"createdAt"`
}

func memToJSON(e memory.Entry) memoryJSON {
	return memoryJSON{
		ID: e.ID, Namespace: e.Namespace, ScopeKey: e.ScopeKey, Agent: e.Agent,
		Workflow: e.Workflow, Step: e.Step, Content: e.Content,
		CreatedAt: e.CreatedAt.Format(time.RFC3339),
	}
}

// memoryEnabled guards handlers against a nil store (mirrors historyEnabled).
func (s *Server) memoryEnabled(w http.ResponseWriter) bool {
	if s.Memory == nil {
		http.Error(w, "memory is not enabled", http.StatusServiceUnavailable)
		return false
	}
	return true
}

func (s *Server) nsOrDefault(v string) string {
	if v == "" {
		return s.ns()
	}
	return v
}

// handleMemory serves GET /api/memory?namespace=&q=&agent=&limit= (Spec 34 §8).
func (s *Server) handleMemory(w http.ResponseWriter, r *http.Request) {
	if !s.memoryEnabled(w) {
		return
	}
	q := r.URL.Query()
	opts := memory.Query{Text: q.Get("q"), Agent: q.Get("agent"), ScopeKey: q.Get("scope"), Limit: 100}
	if v, err := strconv.Atoi(q.Get("limit")); err == nil && v > 0 {
		opts.Limit = v
	}
	entries, err := s.Memory.Search(r.Context(), s.nsOrDefault(q.Get("namespace")), opts)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]memoryJSON, 0, len(entries))
	for _, e := range entries {
		out = append(out, memToJSON(e))
	}
	writeJSON(w, out)
}

// handleMemoryStats serves GET /api/memory/stats?namespace= (Spec 34 §8). Also
// reports the default provider's health so the Memory page can render a health
// badge (Spec §4/§8) — best-effort: nil client or no provider CR -> healthy omitted.
func (s *Server) handleMemoryStats(w http.ResponseWriter, r *http.Request) {
	if !s.memoryEnabled(w) {
		return
	}
	st, err := s.Memory.Stats(r.Context(), s.nsOrDefault(r.URL.Query().Get("namespace")))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := map[string]interface{}{
		"totalEntries": st.TotalEntries,
		"perAgent":     st.PerAgent,
		"providerType": st.ProviderType,
	}
	if healthy, lastErr, ok := s.defaultProviderHealth(r.Context()); ok {
		out["healthy"] = healthy
		out["lastError"] = lastErr
	}
	writeJSON(w, out)
}

// defaultProviderHealth reads the default MemoryProvider's status.healthy/lastError
// (written by the status reconciler, Task 11). Returns ok=false when the client is
// nil (standalone dashboard) or no default provider CR exists — the badge is then
// simply not rendered. Never fails the stats response.
func (s *Server) defaultProviderHealth(ctx context.Context) (healthy bool, lastErr string, ok bool) {
	if s.Client == nil {
		return false, "", false
	}
	var list v1alpha1.MemoryProviderList
	if err := s.Client.List(ctx, &list); err != nil {
		return false, "", false
	}
	var chosen *v1alpha1.MemoryProvider
	for i := range list.Items {
		if list.Items[i].Spec.Default {
			chosen = &list.Items[i]
			break
		}
	}
	if chosen == nil && len(list.Items) == 1 {
		chosen = &list.Items[0]
	}
	if chosen == nil {
		return false, "", false
	}
	return chosen.Status.Healthy, chosen.Status.LastError, true
}

// handleMemoryRecall serves GET /api/memory/recall?namespace=&runId=&step= — the
// step transparency panel (Spec 34 §8). Returns still-existing entries plus the
// ids that no longer resolve (rendered "memory deleted").
func (s *Server) handleMemoryRecall(w http.ResponseWriter, r *http.Request) {
	if !s.memoryEnabled(w) {
		return
	}
	q := r.URL.Query()
	entries, ids, err := s.Memory.ReadRecallLog(r.Context(), s.nsOrDefault(q.Get("namespace")), q.Get("runId"), q.Get("step"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	present := map[string]bool{}
	out := make([]memoryJSON, 0, len(entries))
	for _, e := range entries {
		out = append(out, memToJSON(e))
		present[e.ID] = true
	}
	dangling := []string{}
	for _, id := range ids {
		if !present[id] {
			dangling = append(dangling, id)
		}
	}
	writeJSON(w, map[string]interface{}{"entries": out, "danglingIds": dangling})
}

// handleMemoryDelete serves DELETE /api/memory/{id}?namespace= (Spec 34 §6).
// Gated behind PURKO_MEMORY_FORGET_ENABLED until Spec 32 authz middleware lands.
func (s *Server) handleMemoryDelete(w http.ResponseWriter, r *http.Request) {
	if !s.memoryEnabled(w) {
		return
	}
	if os.Getenv("PURKO_MEMORY_FORGET_ENABLED") != "true" {
		http.Error(w, "memory forget is disabled (set PURKO_MEMORY_FORGET_ENABLED=true; requires Spec 32 authz)", http.StatusForbidden)
		return
	}
	if r.Method != "DELETE" && r.Method != "POST" {
		http.Error(w, "DELETE or POST required", http.StatusMethodNotAllowed)
		return
	}
	s.logUser(r) // audit through the existing dashboard auth log (Spec 34 §6)
	id := r.URL.Path[len("/api/memory/"):]
	ns := s.nsOrDefault(r.URL.Query().Get("namespace"))
	if err := s.Memory.Forget(r.Context(), ns, id); err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"status": "deleted", "id": id})
}
