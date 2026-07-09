package controllers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/purko-io/purko/api/v1alpha1"
	"github.com/purko-io/purko/pkg/memory"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func agentWithMemory(name, ns string, mem *v1alpha1.MemorySpec, labels map[string]string) *v1alpha1.Agent {
	return &v1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: labels},
		Spec:       v1alpha1.AgentSpec{Memory: mem},
	}
}

func TestMemoryScopeKey(t *testing.T) {
	wf := &v1alpha1.Workflow{ObjectMeta: metav1.ObjectMeta{Namespace: "ns1"}}
	r := &WorkflowReconciler{}
	cases := []struct {
		name   string
		mem    *v1alpha1.MemorySpec
		labels map[string]string
		want   string
	}{
		{"agent default", &v1alpha1.MemorySpec{Behavior: "persistent"}, nil, "ns1/agent/a"},
		{"agent explicit", &v1alpha1.MemorySpec{Behavior: "persistent", Scope: "agent"}, nil, "ns1/agent/a"},
		{"group", &v1alpha1.MemorySpec{Behavior: "persistent", Scope: "group"}, map[string]string{"app.kubernetes.io/component": "triage"}, "ns1/group/triage"},
		{"namespace", &v1alpha1.MemorySpec{Behavior: "persistent", Scope: "namespace"}, nil, "ns1/ns1"},
		// group scope without the component label must NOT collapse into a shared
		// "ns1/group/" pool — it falls back to agent scope (pre-webhook safety).
		{"group missing label falls back to agent", &v1alpha1.MemorySpec{Behavior: "persistent", Scope: "group"}, nil, "ns1/agent/a"},
		{"group empty label falls back to agent", &v1alpha1.MemorySpec{Behavior: "persistent", Scope: "group"}, map[string]string{"app.kubernetes.io/component": ""}, "ns1/agent/a"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := r.memoryScopeKey(agentWithMemory("a", "ns1", c.mem, c.labels), wf)
			if got != c.want {
				t.Errorf("scopeKey = %q, want %q", got, c.want)
			}
		})
	}
}

func TestMemoryBehavior(t *testing.T) {
	cases := []struct {
		mem  *v1alpha1.MemorySpec
		want string
	}{
		{nil, "session"},                    // unset maps to session (today's default)
		{&v1alpha1.MemorySpec{}, "session"}, // memory present but no behavior/type
		{&v1alpha1.MemorySpec{Behavior: "off"}, "off"},
		{&v1alpha1.MemorySpec{Behavior: "persistent"}, "persistent"},
		{&v1alpha1.MemorySpec{Type: "vector"}, ""},  // legacy type, no behavior -> legacy path (empty)
		{&v1alpha1.MemorySpec{Type: "summary"}, ""}, // legacy summary keeps ConfigMap path
	}
	for _, c := range cases {
		if got := memoryBehavior(agentWithMemory("a", "ns1", c.mem, nil)); got != c.want {
			t.Errorf("memoryBehavior(%+v) = %q, want %q", c.mem, got, c.want)
		}
	}
}

func TestFormatRecallBlock(t *testing.T) {
	entries := []memory.Entry{
		{Workflow: "incident", Step: "analyze", Content: "Task: pod crashloop | Result: OOM"},
		{Workflow: "incident", Step: "analyze", Content: "Task: dns fail | Result: coredns"},
	}
	got := formatRecallBlock(entries)
	if got == "" || !contains(got, "pod crashloop") || !contains(got, "---") {
		t.Fatalf("recall block missing provenance/separator: %q", got)
	}
}

// ── resolveMemoryRetention ───────────────────────────────────────────

func memProvider(name string, def bool, maxEntries *int) *v1alpha1.MemoryProvider {
	p := &v1alpha1.MemoryProvider{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "purko-system"},
		Spec:       v1alpha1.MemoryProviderSpec{Default: def},
	}
	if maxEntries != nil {
		p.Spec.Retention = &v1alpha1.MemoryRetention{MaxEntriesPerScope: maxEntries}
	}
	return p
}

func TestResolveMemoryRetention(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	n100, n250 := 100, 250
	cases := []struct {
		name        string
		providers   []*v1alpha1.MemoryProvider
		providerRef string
		want        int
	}{
		{"no CRs -> builtin 500", nil, "", 500},
		// "zz-custom" sorts after "platform" in the fake client's list, so the
		// Default-flagged provider is chosen FIRST and the ref match must override it.
		{"providerRef wins over default", []*v1alpha1.MemoryProvider{
			memProvider("zz-custom", false, &n100),
			memProvider("platform", true, &n250),
		}, "zz-custom", 100},
		{"providerRef missing -> falls to Default-flagged", []*v1alpha1.MemoryProvider{
			memProvider("platform", true, &n250),
		}, "nonexistent", 250},
		{"empty ref -> Default-flagged", []*v1alpha1.MemoryProvider{
			memProvider("platform", true, &n250),
		}, "", 250},
		{"chosen CR without retention -> builtin 500", []*v1alpha1.MemoryProvider{
			memProvider("platform", true, nil),
		}, "", 500},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := fake.NewClientBuilder().WithScheme(scheme)
			for _, p := range c.providers {
				b = b.WithObjects(p)
			}
			r := &WorkflowReconciler{Client: b.Build()}
			if got := r.resolveMemoryRetention(context.Background(), c.providerRef); got != c.want {
				t.Errorf("resolveMemoryRetention(%q) = %d, want %d", c.providerRef, got, c.want)
			}
		})
	}
}

// ── recallMemory ─────────────────────────────────────────────────────

// fakeRecallStore implements memory.Store for recallMemory tests. Only Recall and
// WriteRecallLog behave; everything else panics — recallMemory must not touch them.
type fakeRecallStore struct {
	recallEntries []memory.Entry
	recallErr     error
	loggedRunID   string
	loggedStep    string
	loggedIDs     []string
}

func (f *fakeRecallStore) Recall(ctx context.Context, ns, scopeKey, query string, maxTokens int) ([]memory.Entry, error) {
	return f.recallEntries, f.recallErr
}
func (f *fakeRecallStore) WriteRecallLog(ctx context.Context, ns, runID, step string, memoryIDs []string) error {
	f.loggedRunID, f.loggedStep, f.loggedIDs = runID, step, memoryIDs
	return nil
}
func (f *fakeRecallStore) Learn(ctx context.Context, e memory.Entry) error { panic("unused") }
func (f *fakeRecallStore) Search(ctx context.Context, ns string, q memory.Query) ([]memory.Entry, error) {
	panic("unused")
}
func (f *fakeRecallStore) Forget(ctx context.Context, ns, id string) error { panic("unused") }
func (f *fakeRecallStore) Stats(ctx context.Context, ns string) (memory.Stats, error) {
	panic("unused")
}
func (f *fakeRecallStore) Healthy(ctx context.Context) error { panic("unused") }
func (f *fakeRecallStore) ReadRecallLog(ctx context.Context, ns, runID, step string) ([]memory.Entry, []string, error) {
	panic("unused")
}
func (f *fakeRecallStore) Retain(ctx context.Context, scopeKey string, maxEntries int) (int64, error) {
	panic("unused")
}
func (f *fakeRecallStore) DeleteOlderThan(ctx context.Context, days int) (int64, error) {
	panic("unused")
}
func (f *fakeRecallStore) DeleteRecallLogOlderThan(ctx context.Context, days int) (int64, error) {
	panic("unused")
}
func (f *fakeRecallStore) Close() error { panic("unused") }

// fakeMemStore implements memory.Store for persistMemory / updateAgentMetrics /
// MemoryProviderReconciler tests. Learn records entries; Stats returns the
// configured stats and captures the ns it was called with; Healthy returns the
// configured error; all other methods are no-ops (not panics) so that Retain
// can be called without exploding.
type fakeMemStore struct {
	learned    []memory.Entry
	stats      memory.Stats
	statsNS    string
	healthyErr error
}

func (f *fakeMemStore) Recall(ctx context.Context, ns, sk, q string, mt int) ([]memory.Entry, error) {
	return nil, nil
}
func (f *fakeMemStore) Learn(ctx context.Context, e memory.Entry) error {
	f.learned = append(f.learned, e)
	return nil
}
func (f *fakeMemStore) Search(ctx context.Context, ns string, q memory.Query) ([]memory.Entry, error) {
	return nil, nil
}
func (f *fakeMemStore) Forget(ctx context.Context, ns, id string) error { return nil }
func (f *fakeMemStore) Stats(ctx context.Context, ns string) (memory.Stats, error) {
	f.statsNS = ns
	return f.stats, nil
}
func (f *fakeMemStore) Healthy(ctx context.Context) error { return f.healthyErr }
func (f *fakeMemStore) WriteRecallLog(ctx context.Context, ns, r, s string, ids []string) error {
	return nil
}
func (f *fakeMemStore) ReadRecallLog(ctx context.Context, ns, r, s string) ([]memory.Entry, []string, error) {
	return nil, nil, nil
}
func (f *fakeMemStore) Retain(ctx context.Context, sk string, m int) (int64, error) { return 0, nil }
func (f *fakeMemStore) DeleteOlderThan(ctx context.Context, d int) (int64, error)   { return 0, nil }
func (f *fakeMemStore) DeleteRecallLogOlderThan(ctx context.Context, d int) (int64, error) {
	return 0, nil
}
func (f *fakeMemStore) Close() error { return nil }

func TestRecallMemoryNilStore(t *testing.T) {
	r := &WorkflowReconciler{} // Memory nil, Recorder nil — must not panic
	agent := agentWithMemory("a", "ns1", &v1alpha1.MemorySpec{Behavior: "persistent"}, nil)
	wf := &v1alpha1.Workflow{ObjectMeta: metav1.ObjectMeta{Name: "wf", Namespace: "ns1"}}
	if got := r.recallMemory(context.Background(), agent, wf, "run-1", "step-1", "input"); got != "" {
		t.Errorf("recallMemory with nil store = %q, want \"\"", got)
	}
}

func TestRecallMemoryErrorIsAdvisory(t *testing.T) {
	rec := record.NewFakeRecorder(4)
	r := &WorkflowReconciler{
		Memory:   &fakeRecallStore{recallErr: errors.New("db locked")},
		Recorder: rec,
	}
	agent := agentWithMemory("a", "ns1", &v1alpha1.MemorySpec{Behavior: "persistent"}, nil)
	wf := &v1alpha1.Workflow{ObjectMeta: metav1.ObjectMeta{Name: "wf", Namespace: "ns1"}}
	if got := r.recallMemory(context.Background(), agent, wf, "run-1", "step-1", "input"); got != "" {
		t.Errorf("recallMemory on store error = %q, want \"\" (advisory)", got)
	}
	select {
	case ev := <-rec.Events:
		if !contains(ev, "MemoryRecallFailed") || !contains(ev, "Warning") {
			t.Errorf("event = %q, want Warning MemoryRecallFailed", ev)
		}
	default:
		t.Error("no event emitted for recall failure")
	}
}

func TestRecallMemorySuccess(t *testing.T) {
	store := &fakeRecallStore{recallEntries: []memory.Entry{
		{ID: "id-1", Workflow: "incident", Step: "analyze", Content: "Task: pod crashloop | Result: OOM", CreatedAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)},
		{ID: "id-2", Workflow: "incident", Step: "analyze", Content: "Task: dns fail | Result: coredns", CreatedAt: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)},
	}}
	r := &WorkflowReconciler{Memory: store}
	agent := agentWithMemory("a", "ns1", &v1alpha1.MemorySpec{Behavior: "persistent"}, nil)
	wf := &v1alpha1.Workflow{ObjectMeta: metav1.ObjectMeta{Name: "wf", Namespace: "ns1"}}

	got := r.recallMemory(context.Background(), agent, wf, "run-1", "step-1", "pod crashing")
	if !contains(got, "pod crashloop") || !contains(got, "2026-07-01") || !contains(got, "---") {
		t.Errorf("recall block missing content/provenance/separator: %q", got)
	}
	if store.loggedRunID != "run-1" || store.loggedStep != "step-1" {
		t.Errorf("recall_log keyed %q/%q, want run-1/step-1", store.loggedRunID, store.loggedStep)
	}
	if len(store.loggedIDs) != 2 || store.loggedIDs[0] != "id-1" || store.loggedIDs[1] != "id-2" {
		t.Errorf("recall_log ids = %v, want [id-1 id-2]", store.loggedIDs)
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (indexOf(s, sub) >= 0) }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func envMap(vars []corev1.EnvVar) map[string]string {
	m := map[string]string{}
	for _, v := range vars {
		m[v.Name] = v.Value
	}
	return m
}

// ── persistMemory ────────────────────────────────────────────────────

func TestPersistUsesMemoryUpdate(t *testing.T) {
	fms := &fakeMemStore{}
	r := &WorkflowReconciler{Memory: fms}
	agent := agentWithMemory("a", "ns1", &v1alpha1.MemorySpec{Behavior: "persistent"}, nil)
	wf := &v1alpha1.Workflow{ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "wf"}}
	r.persistMemory(context.Background(), agent, wf, "analyze", "Task: X | Result: Y", "the input", "the response")
	if len(fms.learned) != 1 || fms.learned[0].Content != "Task: X | Result: Y" {
		t.Fatalf("expected the _memory_update content stored, got %+v", fms.learned)
	}
	if fms.learned[0].ScopeKey != "ns1/agent/a" || fms.learned[0].Agent != "a" {
		t.Errorf("provenance wrong: %+v", fms.learned[0])
	}
}

func TestPersistAutoSummaryFallback(t *testing.T) {
	fms := &fakeMemStore{}
	r := &WorkflowReconciler{Memory: fms}
	agent := agentWithMemory("a", "ns1", &v1alpha1.MemorySpec{Behavior: "persistent"}, nil)
	wf := &v1alpha1.Workflow{ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "wf"}}
	// No _memory_update -> synthesize Task/Result from input+response.
	r.persistMemory(context.Background(), agent, wf, "analyze", "", "investigate the pod", "found OOM")
	if len(fms.learned) != 1 || !contains(fms.learned[0].Content, "Task: investigate the pod") || !contains(fms.learned[0].Content, "Result: found OOM") {
		t.Fatalf("auto-summary fallback wrong: %+v", fms.learned)
	}
}

// memScheme builds a scheme with v1alpha1 + corev1 (ConfigMaps must be
// registered so a wrongly-routed legacy write would succeed and be detectable).
func memScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme v1alpha1: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme corev1: %v", err)
	}
	return scheme
}

// assertNoMemoryConfigMaps fails if any ConfigMap exists in ns (the persistent
// path must never write the legacy <agent>-memory ConfigMap).
func assertNoMemoryConfigMaps(t *testing.T, cl client.Client, ns string) {
	t.Helper()
	var cms corev1.ConfigMapList
	if err := cl.List(context.Background(), &cms, client.InNamespace(ns)); err != nil {
		t.Fatalf("list configmaps: %v", err)
	}
	if len(cms.Items) != 0 {
		t.Fatalf("expected no ConfigMaps, found %d (first: %s)", len(cms.Items), cms.Items[0].Name)
	}
}

func TestUpdateAgentMetricsPersistsWithProvenance(t *testing.T) {
	scheme := memScheme(t)
	agent := agentWithMemory("a", "ns1", &v1alpha1.MemorySpec{Behavior: "persistent"}, nil)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(agent).WithStatusSubresource(agent).Build()
	fms := &fakeMemStore{}
	r := &WorkflowReconciler{Client: cl, Memory: fms}
	wf := &v1alpha1.Workflow{ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "wf"}}
	// Output carries _metrics (required) + _memory_update (the persistent path).
	output := []byte(`{"_metrics":{"tokens_in":1,"tokens_out":2,"cost_usd":0.01},"_memory_update":"Task: X | Result: Y"}`)
	r.updateAgentMetrics(context.Background(), "ns1", "a", output, nil, nil, wf, "analyze")
	if len(fms.learned) != 1 {
		t.Fatalf("expected one Learn from the persistent branch, got %d", len(fms.learned))
	}
	e := fms.learned[0]
	if e.Workflow != "wf" || e.Step != "analyze" || e.Agent != "a" || e.ScopeKey != "ns1/agent/a" {
		t.Fatalf("provenance wrong: %+v", e)
	}
	if e.Content != "Task: X | Result: Y" {
		t.Errorf("content = %q, want the _memory_update", e.Content)
	}
	// Persistent path must SKIP the legacy ConfigMap write entirely.
	assertNoMemoryConfigMaps(t, cl, "ns1")
}

func TestUpdateAgentMetricsAutoSummaryFallback(t *testing.T) {
	scheme := memScheme(t)
	agent := agentWithMemory("a", "ns1", &v1alpha1.MemorySpec{Behavior: "persistent"}, nil)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(agent).WithStatusSubresource(agent).Build()
	fms := &fakeMemStore{}
	r := &WorkflowReconciler{Client: cl, Memory: fms}
	wf := &v1alpha1.Workflow{ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "wf"}}
	// Empty _memory_update -> auto-summary from step name + extractResponseText.
	output := []byte(`{"_metrics":{"tokens_in":1,"tokens_out":2,"cost_usd":0.01},"_memory_update":"","response":"found OOM in pod"}`)
	r.updateAgentMetrics(context.Background(), "ns1", "a", output, nil, nil, wf, "analyze")
	if len(fms.learned) != 1 {
		t.Fatalf("expected one auto-summary Learn, got %d", len(fms.learned))
	}
	c := fms.learned[0].Content
	if !contains(c, "Task: analyze") || !contains(c, "Result: found OOM in pod") {
		t.Fatalf("auto-summary content wrong: %q", c)
	}
	assertNoMemoryConfigMaps(t, cl, "ns1")
}

func TestUpdateAgentMetricsGetFailureSkipsMemory(t *testing.T) {
	// Agent absent from the client: the Get fails. The routing must NOT fall to
	// the legacy ConfigMap branch — no Learn, no ConfigMap.
	scheme := memScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	fms := &fakeMemStore{}
	r := &WorkflowReconciler{Client: cl, Memory: fms}
	wf := &v1alpha1.Workflow{ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "wf"}}
	output := []byte(`{"_metrics":{"tokens_in":1,"tokens_out":2,"cost_usd":0.01},"_memory_update":"Task: X | Result: Y"}`)
	r.updateAgentMetrics(context.Background(), "ns1", "a", output, nil, nil, wf, "analyze")
	if len(fms.learned) != 0 {
		t.Fatalf("Get failure must skip persistence, got %d Learn calls", len(fms.learned))
	}
	assertNoMemoryConfigMaps(t, cl, "ns1")
}

func TestImportLegacyMemoryOnce(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = v1alpha1.AddToScheme(scheme)
	agent := agentWithMemory("a", "ns1", &v1alpha1.MemorySpec{Behavior: "persistent"}, nil)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "a-memory", Namespace: "ns1"},
		Data:       map[string]string{"summary": "legacy lesson learned"},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(agent, cm).Build()
	fakeStore := &fakeMemStore{}
	r := &WorkflowReconciler{Client: cl, Memory: fakeStore}
	wf := &v1alpha1.Workflow{ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "wf"}}

	r.importLegacyMemory(context.Background(), agent, wf)
	if len(fakeStore.learned) != 1 || fakeStore.learned[0].Content != "legacy lesson learned" {
		t.Fatalf("first import should store the ConfigMap summary, got %+v", fakeStore.learned)
	}
	// Re-fetch the annotated agent and import again -> no-op.
	got := &v1alpha1.Agent{}
	_ = cl.Get(context.Background(), client.ObjectKey{Name: "a", Namespace: "ns1"}, got)
	if got.Annotations["purko.io/memory-imported"] != "true" {
		t.Fatalf("idempotency annotation not set: %v", got.Annotations)
	}
	r.importLegacyMemory(context.Background(), got, wf)
	if len(fakeStore.learned) != 1 {
		t.Fatalf("second import should be a no-op (annotation present), got %d", len(fakeStore.learned))
	}
}

// assertImportMarker re-fetches the agent and asserts the idempotency annotation.
func assertImportMarker(t *testing.T, cl client.Client, name, ns string, want bool) {
	t.Helper()
	got := &v1alpha1.Agent{}
	if err := cl.Get(context.Background(), client.ObjectKey{Name: name, Namespace: ns}, got); err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if has := got.Annotations["purko.io/memory-imported"] == "true"; has != want {
		t.Fatalf("memory-imported marker = %v, want %v (annotations: %v)", has, want, got.Annotations)
	}
}

func TestImportLegacyMemoryMissingCMMarks(t *testing.T) {
	// No legacy ConfigMap at all: nothing to Learn, but the marker must still be
	// set so the controller doesn't re-check the CM on every future run.
	scheme := memScheme(t)
	agent := agentWithMemory("a", "ns1", &v1alpha1.MemorySpec{Behavior: "persistent"}, nil)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(agent).Build()
	fms := &fakeMemStore{}
	r := &WorkflowReconciler{Client: cl, Memory: fms}
	wf := &v1alpha1.Workflow{ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "wf"}}

	r.importLegacyMemory(context.Background(), agent, wf)
	if len(fms.learned) != 0 {
		t.Fatalf("missing CM must not Learn anything, got %+v", fms.learned)
	}
	assertImportMarker(t, cl, "a", "ns1", true)
}

func TestImportLegacyMemoryNoSummaryKeyMarks(t *testing.T) {
	// CM exists but has no "summary" key: nothing to Learn, marker still set.
	scheme := memScheme(t)
	agent := agentWithMemory("a", "ns1", &v1alpha1.MemorySpec{Behavior: "persistent"}, nil)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "a-memory", Namespace: "ns1"},
		Data:       map[string]string{"other": "not a summary"},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(agent, cm).Build()
	fms := &fakeMemStore{}
	r := &WorkflowReconciler{Client: cl, Memory: fms}
	wf := &v1alpha1.Workflow{ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "wf"}}

	r.importLegacyMemory(context.Background(), agent, wf)
	if len(fms.learned) != 0 {
		t.Fatalf("CM without summary key must not Learn anything, got %+v", fms.learned)
	}
	assertImportMarker(t, cl, "a", "ns1", true)
}

func TestImportLegacyMemoryMarkerPatchFailureAdvisory(t *testing.T) {
	// Marker patch fails (interceptor rejects Patch on Agents): the entry is
	// still Learned (Learn-first, no data loss), a Warning event is emitted, no
	// panic. Then the documented retry residual: the next reconcile re-fetches
	// the still-unannotated agent and re-imports — learned goes to 2. That
	// duplicate is accepted ONLY for the marker-write failure window; routine
	// 409s can no longer cause it because the marker is a merge patch with no
	// resourceVersion precondition.
	scheme := memScheme(t)
	agent := agentWithMemory("a", "ns1", &v1alpha1.MemorySpec{Behavior: "persistent"}, nil)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "a-memory", Namespace: "ns1"},
		Data:       map[string]string{"summary": "legacy lesson learned"},
	}
	patchErr := errors.New("agent patch denied")
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(agent, cm).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				if _, ok := obj.(*v1alpha1.Agent); ok {
					return patchErr
				}
				return c.Patch(ctx, obj, patch, opts...)
			},
		}).Build()
	fms := &fakeMemStore{}
	rec := record.NewFakeRecorder(4)
	r := &WorkflowReconciler{Client: cl, Memory: fms, Recorder: rec}
	wf := &v1alpha1.Workflow{ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "wf"}}

	r.importLegacyMemory(context.Background(), agent, wf)
	if len(fms.learned) != 1 || fms.learned[0].Content != "legacy lesson learned" {
		t.Fatalf("Learn must succeed despite marker failure, got %+v", fms.learned)
	}
	select {
	case ev := <-rec.Events:
		if !contains(ev, "MemoryImportFailed") || !contains(ev, "Warning") {
			t.Errorf("event = %q, want Warning MemoryImportFailed", ev)
		}
	default:
		t.Error("no event emitted for marker patch failure")
	}
	// Marker never landed on the server.
	assertImportMarker(t, cl, "a", "ns1", false)
	// Next reconcile: fresh Get returns the un-annotated agent -> re-import.
	fresh := &v1alpha1.Agent{}
	if err := cl.Get(context.Background(), client.ObjectKey{Name: "a", Namespace: "ns1"}, fresh); err != nil {
		t.Fatalf("get agent: %v", err)
	}
	r.importLegacyMemory(context.Background(), fresh, wf)
	if len(fms.learned) != 2 {
		t.Fatalf("failed marker write must retry (and re-import) next run, got %d Learn calls", len(fms.learned))
	}
}

func TestTruncateRuneSafe(t *testing.T) {
	cases := []struct {
		s    string
		n    int
		want string
	}{
		{"hello", 10, "hello"}, // under budget, untouched
		{"hello", 5, "hello"},  // exactly at budget
		{"hello", 3, "hel"},    // ASCII cut
		{"aé", 2, "a"},         // é is 2 bytes; cutting at 2 would split it
		{"héllo", 2, "h"},      // cut lands mid-é -> back up
		{"日本語", 4, "日"},        // 3-byte runes; 4 lands mid-本
		{"日本語", 6, "日本"},       // exact rune boundary kept
	}
	for _, c := range cases {
		got := truncate(c.s, c.n)
		if got != c.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", c.s, c.n, got, c.want)
		}
		if len(got) > c.n {
			t.Errorf("truncate(%q, %d) = %q exceeds byte budget", c.s, c.n, got)
		}
	}
}

func TestBuildMemoryEnv(t *testing.T) {
	cases := []struct {
		name     string
		mem      *v1alpha1.MemorySpec
		recalled string
		wantType string
		wantBhv  string // MEMORY_BEHAVIOR ("" = absent)
		wantMem  bool   // AGENT_MEMORY present
		wantCM   bool   // MEMORY_CM_NAME present
	}{
		{"unset", nil, "", "buffer", "", false, false},
		{"session", &v1alpha1.MemorySpec{Behavior: "session"}, "", "buffer", "session", false, false},
		{"off", &v1alpha1.MemorySpec{Behavior: "off"}, "", "none", "off", false, false},
		{"persistent", &v1alpha1.MemorySpec{Behavior: "persistent"}, "recalled block", "summary", "persistent", true, false},
		{"persistent-empty-recall", &v1alpha1.MemorySpec{Behavior: "persistent"}, "", "summary", "persistent", true, false},
		{"legacy vector", &v1alpha1.MemorySpec{Type: "vector"}, "", "vector", "", false, false},
		{"legacy summary", &v1alpha1.MemorySpec{Type: "summary"}, "", "summary", "", false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := envMap(buildMemoryEnv(agentWithMemory("a", "ns1", c.mem, nil), c.recalled))
			if env["MEMORY_TYPE"] != c.wantType {
				t.Errorf("MEMORY_TYPE=%q want %q", env["MEMORY_TYPE"], c.wantType)
			}
			if env["MEMORY_BEHAVIOR"] != c.wantBhv {
				t.Errorf("MEMORY_BEHAVIOR=%q want %q", env["MEMORY_BEHAVIOR"], c.wantBhv)
			}
			if _, ok := env["AGENT_MEMORY"]; ok != c.wantMem {
				t.Errorf("AGENT_MEMORY present=%v want %v", ok, c.wantMem)
			}
			if _, ok := env["MEMORY_CM_NAME"]; ok != c.wantCM {
				t.Errorf("MEMORY_CM_NAME present=%v want %v", ok, c.wantCM)
			}
		})
	}
}
