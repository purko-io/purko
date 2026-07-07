package dashboard

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	v1alpha1 "github.com/purko-io/purko/api/v1alpha1"
	"github.com/purko-io/purko/pkg/history"
)

// llmProviderNamespace is where LLMProvider CRs live — it must match the
// namespace controllers/workflow_controller.go resolveLLMProvider lists.
const llmProviderNamespace = "purko-system"

type Server struct {
	Client    client.Client
	Clientset *kubernetes.Clientset
	Port      int
	Scheduler *Scheduler
	Registry  *MCPServerRegistry
	LLM       LLMProvider
	IntentLLM LLMProvider   // Opus-based LLM for intent workflow design
	Namespace string        // default namespace for agents/workflows
	History   history.Store // optional execution history archive (Spec 24)
	mu        sync.Mutex
}

type AgentSummary struct {
	Name       string `json:"name"`
	Namespace  string `json:"namespace"`
	Type       string `json:"type"`
	Provider   string `json:"provider"`
	Model      string `json:"model"`
	Phase      string `json:"phase"`
	Replicas   int    `json:"replicas"`
	Autonomy   string `json:"autonomy"`
	ToolCount  int    `json:"toolCount"`
	Age        string `json:"age"`
	Generation int64  `json:"generation"`
	Group      string `json:"group"`
	Image      string `json:"image"`
}

type WorkflowSummary struct {
	Name          string            `json:"name"`
	Namespace     string            `json:"namespace"`
	Phase         string            `json:"phase"`
	Total         int               `json:"totalSteps"`
	Completed     int               `json:"completedSteps"`
	Failed        int               `json:"failedSteps"`
	Duration      string            `json:"duration"`
	Steps         []StepBrief       `json:"steps"`
	Age           string            `json:"age"`
	Repository    string            `json:"repository"`
	Parameters    map[string]string `json:"parameters,omitempty"`
	TriggerType   string            `json:"triggerType"`
	TriggerSource string            `json:"triggerSource"`
	TriggerRoute  string            `json:"triggerRoute"`
	TemplateRef   string            `json:"templateRef"`
}

type StepBrief struct {
	Name     string   `json:"name"`
	Phase    string   `json:"phase"`
	Agent    string   `json:"agent"`
	Type     string   `json:"type"`
	JobName  string   `json:"jobName"`
	Duration string   `json:"duration"`
	DepsOn   []string `json:"dependsOn"`
}

type OverviewData struct {
	AgentCount    int               `json:"agentCount"`
	AgentReady    int               `json:"agentReady"`
	WorkflowCount int               `json:"workflowCount"`
	WfSucceeded   int               `json:"wfSucceeded"`
	WfRunning     int               `json:"wfRunning"`
	WfFailed      int               `json:"wfFailed"`
	DeployCount   int               `json:"deployCount"`
	HPACount      int               `json:"hpaCount"`
	Agents        []AgentSummary    `json:"agents"`
	Workflows     []WorkflowSummary `json:"workflows"`
	Timestamp     string            `json:"timestamp"`
}

func (s *Server) ns() string {
	if s.Namespace != "" {
		return s.Namespace
	}
	return "ai-agents"
}

func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", staticHandler()))
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/overview", s.handleOverview)
	mux.HandleFunc("/api/agents", s.handleAgents)
	mux.HandleFunc("/api/workflows", s.handleWorkflows)
	mux.HandleFunc("/api/agent/", s.handleAgentDetail)
	mux.HandleFunc("/api/workflow/", s.handleWorkflowDetail)
	mux.HandleFunc("/api/events", s.handleSSE)
	mux.HandleFunc("/api/create/agent", s.handleCreateAgent)
	mux.HandleFunc("/api/create/workflow", s.handleCreateWorkflow)
	mux.HandleFunc("/api/trigger/rules", s.handleTriggerRules)
	mux.HandleFunc("/api/schedules", s.handleSchedules)
	mux.HandleFunc("/api/update/agent", s.handleUpdateAgent)
	mux.HandleFunc("/api/mcp/tools", s.handleMCPTools)
	mux.HandleFunc("/api/presets", s.handlePresets)
	mux.HandleFunc("/api/delete/agent/", s.handleDeleteAgent)
	mux.HandleFunc("/api/delete/workflow/", s.handleDeleteWorkflow)
	mux.HandleFunc("/api/rerun/workflow/", s.handleRerunWorkflow)
	mux.HandleFunc("/api/logs/", s.handleStepLogs)
	mux.HandleFunc("/api/deny/", s.handleDenyStep)
	mux.HandleFunc("/api/approve/", s.handleApproveStep)
	mux.HandleFunc("/api/mcp/servers", s.handleMCPServers)
	mux.HandleFunc("/api/mcp/server/", s.handleMCPServerCRUD)
	mux.HandleFunc("/api/mcp/server", s.handleMCPServerCreate)
	mux.HandleFunc("/api/llm/providers", s.handleLLMProviders)
	mux.HandleFunc("/api/llm/provider/", s.handleLLMProviderCRUD)
	mux.HandleFunc("/api/llm/provider", s.handleLLMProviderCreate)
	mux.HandleFunc("/api/trigger/", s.handleWebhookTrigger)
	mux.HandleFunc("/api/history/runs", s.handleHistoryRuns)
	mux.HandleFunc("/api/history/run/", s.handleHistoryRunDetail)
	mux.HandleFunc("/api/history/step/", s.handleHistoryStepTools)
	mux.HandleFunc("/api/features", s.handleFeatures)
	mux.HandleFunc("/api/whoami", s.handleWhoami)
	s.registerProHandlers(mux) // pro: intent + autonomy; community: no-op

	addr := fmt.Sprintf(":%d", s.Port)
	srv := &http.Server{Addr: addr, Handler: mux}

	logger := ctrllog.FromContext(ctx)
	logger.Info("Starting dashboard", "addr", addr)

	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()

	return srv.ListenAndServe()
}

// handleWhoami surfaces the identity the auth proxy forwards (F28). Reads
// the headers directly so the shared dashboard builds without the Pro sso
// package; community installs have no proxy → empty user → UI hides the chip.
func (s *Server) handleWhoami(w http.ResponseWriter, r *http.Request) {
	user := r.Header.Get("X-Forwarded-Email")
	if user == "" {
		user = r.Header.Get("X-Forwarded-User")
	}
	writeJSON(w, map[string]string{"user": user})
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	data, _ := staticFiles.ReadFile("static/index.html")
	w.Write(data)
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := s.buildOverview(ctx)
	writeJSON(w, data)
}

func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	agents := s.listAgents(ctx)
	writeJSON(w, agents)
}

func (s *Server) handleWorkflows(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workflows := s.listWorkflows(ctx)
	writeJSON(w, workflows)
}

func (s *Server) handleAgentDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := r.URL.Path[len("/api/agent/"):]
	if name == "" {
		http.Error(w, "agent name required", 400)
		return
	}

	agent := &v1alpha1.Agent{}
	if err := s.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: s.ns()}, agent); err != nil {
		http.Error(w, err.Error(), 404)
		return
	}

	// Get deployment and HPA
	deploy := &appsv1.Deployment{}
	s.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: s.ns()}, deploy)

	hpa := &autoscalingv2.HorizontalPodAutoscaler{}
	s.Client.Get(ctx, client.ObjectKey{Name: name + "-hpa", Namespace: s.ns()}, hpa)

	// Get pods
	podList := &corev1.PodList{}
	s.Client.List(ctx, podList, client.InNamespace(s.ns()),
		client.MatchingLabels{"purko.io/agent": name})

	pods := []map[string]string{}
	for _, p := range podList.Items {
		pods = append(pods, map[string]string{
			"name":   p.Name,
			"status": string(p.Status.Phase),
			"ip":     p.Status.PodIP,
		})
	}

	detail := map[string]interface{}{
		"agent": agent,
		"pods":  pods,
	}
	writeJSON(w, detail)
}

func (s *Server) handleWorkflowDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := r.URL.Path[len("/api/workflow/"):]
	if name == "" {
		http.Error(w, "workflow name required", 400)
		return
	}

	wf := &v1alpha1.Workflow{}
	if err := s.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: s.ns()}, wf); err != nil {
		http.Error(w, err.Error(), 404)
		return
	}

	// Get outputs ConfigMap
	cm := &corev1.ConfigMap{}
	outputs := map[string]string{}
	if err := s.Client.Get(ctx, client.ObjectKey{Name: name + "-outputs", Namespace: s.ns()}, cm); err == nil {
		outputs = cm.Data
	}

	// Get jobs
	jobList := &batchv1.JobList{}
	s.Client.List(ctx, jobList, client.InNamespace(s.ns()),
		client.MatchingLabels{"purko.io/workflow": name})

	jobs := []map[string]interface{}{}
	for _, j := range jobList.Items {
		dur := ""
		if j.Status.CompletionTime != nil && j.Status.StartTime != nil {
			dur = j.Status.CompletionTime.Sub(j.Status.StartTime.Time).Round(time.Second).String()
		}
		jobs = append(jobs, map[string]interface{}{
			"name":     j.Name,
			"step":     j.Labels["purko.io/step"],
			"status":   jobStatus(&j),
			"duration": dur,
		})
	}

	detail := map[string]interface{}{
		"workflow": wf,
		"outputs":  outputs,
		"jobs":     jobs,
	}
	writeJSON(w, detail)
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", 500)
		return
	}

	ctx := r.Context()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			data := s.buildOverview(ctx)
			b, _ := json.Marshal(data)
			fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
		}
	}
}

type CreateAgentRequest struct {
	Name              string   `json:"name"`
	Namespace         string   `json:"namespace"`
	Type              string   `json:"type"`
	Provider          string   `json:"provider"`
	Model             string   `json:"model"`
	Temperature       float64  `json:"temperature"`
	Autonomy          string   `json:"autonomy"`
	Memory            string   `json:"memory"`
	Role              string   `json:"role"`
	Image             *string  `json:"image"`
	Group             string   `json:"group"`
	CostLimit         float64  `json:"costLimit"`
	MaxIterations     int      `json:"maxIterations"`
	MaxExecutionTime  string   `json:"maxExecutionTime"`
	RollbackOnFailure *bool    `json:"rollbackOnFailure"`
	SystemPrompt      string   `json:"systemPrompt"`
	Tools             []string `json:"tools"`
	MinReplicas       int      `json:"minReplicas"`
	MaxReplicas       int      `json:"maxReplicas"`
	TargetCPU         int      `json:"targetCPU"`
}

type CreateWorkflowRequest struct {
	Name        string `json:"name"`
	Namespace   string `json:"namespace"`
	Description string `json:"description"`
	Steps       []struct {
		Name      string   `json:"name"`
		Agent     string   `json:"agent"`
		Type      string   `json:"type"`
		DependsOn []string `json:"dependsOn"`
		Input     string   `json:"input"`
	} `json:"steps"`
	Parallelism int               `json:"parallelism"`
	Strategy    string            `json:"strategy"`
	Parameters  map[string]string `json:"parameters"`
	Concurrency string            `json:"concurrency"`
	Cron        string            `json:"cron"`
}

func (s *Server) handleCreateAgent(w http.ResponseWriter, r *http.Request) {
	s.logUser(r)
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	ctx := r.Context()
	var req CreateAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	if req.Namespace == "" {
		req.Namespace = s.ns()
	}

	temp := req.Temperature
	minR := req.MinReplicas
	if minR <= 0 {
		minR = 1
	}
	maxR := req.MaxReplicas
	if maxR <= 0 {
		maxR = 3
	}
	cpu := req.TargetCPU
	if cpu <= 0 {
		cpu = 70
	}

	agent := &v1alpha1.Agent{}
	agent.APIVersion = "purko.io/v1alpha1"
	agent.Kind = "Agent"
	agent.Name = req.Name
	agent.Namespace = req.Namespace
	agent.Spec.Type = req.Type
	agent.Spec.Model.Provider = req.Provider
	agent.Spec.Model.Name = req.Model
	agent.Spec.Model.Temperature = &temp
	agent.Spec.AutonomyLevel = req.Autonomy
	if req.Memory != "" && req.Memory != "buffer" {
		agent.Spec.Memory = &v1alpha1.MemorySpec{Type: req.Memory}
	}
	if req.Group != "" && req.Group != "general" {
		agent.Labels = map[string]string{
			"app.kubernetes.io/component": req.Group,
		}
	}
	agent.Spec.Role = req.Role
	agent.Spec.SystemPrompt = req.SystemPrompt
	if req.Image != nil && *req.Image != "" {
		agent.Spec.Runtime = &v1alpha1.RuntimeSpec{Image: *req.Image}
	}
	// Set guardrails if cost limit or iterations specified
	if req.CostLimit > 0 || req.MaxIterations > 0 || req.MaxExecutionTime != "" || req.RollbackOnFailure != nil {
		guardrails := map[string]interface{}{}
		if req.CostLimit > 0 {
			guardrails["costLimitUSD"] = req.CostLimit
		}
		if req.MaxIterations > 0 {
			guardrails["maxIterations"] = req.MaxIterations
		}
		if req.MaxExecutionTime != "" {
			guardrails["maxExecutionTime"] = req.MaxExecutionTime
		}
		if req.RollbackOnFailure != nil {
			guardrails["rollbackOnFailure"] = *req.RollbackOnFailure
		}
		guardrailsJSON, _ := json.Marshal(guardrails)
		agent.Spec.Guardrails = &runtime.RawExtension{Raw: guardrailsJSON}
	}
	agent.Spec.Scaling = &v1alpha1.ScalingSpec{
		MinReplicas:       &minR,
		MaxReplicas:       &maxR,
		TargetUtilization: &cpu,
	}

	for _, toolName := range req.Tools {
		agent.Spec.Tools = append(agent.Spec.Tools, v1alpha1.ToolSpec{
			Name: toolName,
			Type: "mcp",
		})
	}

	if err := s.Client.Create(ctx, agent); err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"status": "created", "name": req.Name})
}

func (s *Server) handleCreateWorkflow(w http.ResponseWriter, r *http.Request) {
	s.logUser(r)
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	ctx := r.Context()
	var req CreateWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	if req.Namespace == "" {
		req.Namespace = s.ns()
	}
	if req.Parallelism <= 0 {
		req.Parallelism = 1
	}
	if req.Strategy == "" {
		req.Strategy = "failFast"
	}

	wf := &v1alpha1.Workflow{}
	wf.APIVersion = "purko.io/v1alpha1"
	wf.Kind = "Workflow"
	wf.Name = req.Name
	wf.Namespace = req.Namespace
	wf.Spec.Description = req.Description
	wf.Spec.FailureStrategy = req.Strategy
	par := req.Parallelism
	wf.Spec.Parallelism = &par
	if len(req.Parameters) > 0 {
		wf.Spec.Parameters = req.Parameters
	}
	if req.Concurrency != "" {
		wf.Spec.Concurrency = &v1alpha1.ConcurrencySpec{Policy: req.Concurrency}
	}

	for _, step := range req.Steps {
		ws := v1alpha1.WorkflowStep{
			Name:      step.Name,
			DependsOn: step.DependsOn,
		}
		if step.Agent != "" {
			ws.AgentRef = v1alpha1.AgentRef{Name: step.Agent}
		}
		if step.Type != "" {
			ws.Type = step.Type
		}
		if step.Input != "" {
			inputJSON, _ := json.Marshal(map[string]string{"task": step.Input})
			ws.Input = &runtime.RawExtension{Raw: inputJSON}
		}
		wf.Spec.Steps = append(wf.Spec.Steps, ws)
	}

	// Add schedule trigger if cron is provided
	if req.Cron != "" {
		wf.Spec.Trigger = &v1alpha1.TriggerSpec{
			Type: "schedule",
			Schedule: &v1alpha1.ScheduleTrigger{
				Cron: req.Cron,
			},
		}
	}

	if err := s.Client.Create(ctx, wf); err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"status": "created", "name": req.Name})
}

func (s *Server) handleSchedules(w http.ResponseWriter, r *http.Request) {
	if s.Scheduler != nil {
		writeJSON(w, map[string]interface{}{"schedules": s.Scheduler.GetSchedules()})
	} else {
		writeJSON(w, map[string]interface{}{"schedules": []interface{}{}})
	}
}

func (s *Server) handleTriggerRules(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ns := s.ns()

	if r.Method == "GET" {
		rules := loadRules(ctx, s.Client, ns)
		writeJSON(w, map[string]interface{}{"rules": rules})
		return
	}

	if r.Method == "POST" {
		var rules []TriggerRule
		if err := json.NewDecoder(r.Body).Decode(&rules); err != nil {
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}

		rulesJSON, _ := json.MarshalIndent(rules, "", "  ")

		cm := &corev1.ConfigMap{}
		err := s.Client.Get(ctx, client.ObjectKey{Name: "trigger-rules", Namespace: ns}, cm)
		if errors.IsNotFound(err) {
			cm = &corev1.ConfigMap{}
			cm.Name = "trigger-rules"
			cm.Namespace = ns
			cm.Data = map[string]string{"rules": string(rulesJSON)}
			if err := s.Client.Create(ctx, cm); err != nil {
				writeJSON(w, map[string]string{"error": err.Error()})
				return
			}
		} else if err == nil {
			cm.Data["rules"] = string(rulesJSON)
			if err := s.Client.Update(ctx, cm); err != nil {
				writeJSON(w, map[string]string{"error": err.Error()})
				return
			}
		} else {
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}

		writeJSON(w, map[string]string{"status": "saved", "count": fmt.Sprintf("%d", len(rules))})
		return
	}

	http.Error(w, "GET or POST required", 405)
}

func (s *Server) handleUpdateAgent(w http.ResponseWriter, r *http.Request) {
	s.logUser(r)
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	ctx := r.Context()
	var req CreateAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	if req.Namespace == "" {
		req.Namespace = s.ns()
	}

	// Get existing agent
	agent := &v1alpha1.Agent{}
	if err := s.Client.Get(ctx, client.ObjectKey{Name: req.Name, Namespace: req.Namespace}, agent); err != nil {
		writeJSON(w, map[string]string{"error": "agent not found: " + err.Error()})
		return
	}

	// Update fields
	agent.Spec.Model.Provider = req.Provider
	agent.Spec.Model.Name = req.Model
	temp := req.Temperature
	agent.Spec.Model.Temperature = &temp
	agent.Spec.AutonomyLevel = req.Autonomy
	agent.Spec.Role = req.Role
	agent.Spec.SystemPrompt = req.SystemPrompt
	// Image semantics (F41): omitted = untouched; empty = clear the pin so
	// the operator's executor image applies; value = explicit pin.
	if req.Image != nil {
		if *req.Image == "" {
			if agent.Spec.Runtime != nil {
				agent.Spec.Runtime.Image = ""
			}
		} else {
			if agent.Spec.Runtime == nil {
				agent.Spec.Runtime = &v1alpha1.RuntimeSpec{}
			}
			agent.Spec.Runtime.Image = *req.Image
		}
	}
	if req.Group != "" {
		if agent.Labels == nil {
			agent.Labels = map[string]string{}
		}
		agent.Labels["app.kubernetes.io/component"] = req.Group
	}

	// Merge guardrails: overlay provided values, preserve everything else
	// (guardrails are schemaless — the form doesn't know all keys).
	guardrails := map[string]interface{}{}
	if agent.Spec.Guardrails != nil && agent.Spec.Guardrails.Raw != nil {
		_ = json.Unmarshal(agent.Spec.Guardrails.Raw, &guardrails)
	}
	if req.CostLimit > 0 {
		guardrails["costLimitUSD"] = req.CostLimit
	}
	if req.MaxIterations > 0 {
		guardrails["maxIterations"] = req.MaxIterations
	}
	if req.MaxExecutionTime != "" {
		guardrails["maxExecutionTime"] = req.MaxExecutionTime
	}
	if req.RollbackOnFailure != nil {
		guardrails["rollbackOnFailure"] = *req.RollbackOnFailure
	}
	if len(guardrails) > 0 {
		raw, err := json.Marshal(guardrails)
		if err == nil {
			agent.Spec.Guardrails = &runtime.RawExtension{Raw: raw}
		}
	}

	// Update tools
	agent.Spec.Tools = nil
	for _, toolName := range req.Tools {
		agent.Spec.Tools = append(agent.Spec.Tools, v1alpha1.ToolSpec{Name: toolName, Type: "mcp"})
	}

	if err := s.Client.Update(ctx, agent); err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"status": "updated", "name": req.Name})
}

func (s *Server) handleMCPTools(w http.ResponseWriter, r *http.Request) {
	if s.Registry == nil {
		writeJSON(w, map[string]interface{}{
			"servers":    []interface{}{},
			"totalTools": 0,
		})
		return
	}

	// Re-sync if stale
	s.Registry.SyncIfStale(r.Context())

	servers := s.Registry.GetServers()
	totalTools := 0
	for _, srv := range servers {
		totalTools += srv.ToolCount
	}

	writeJSON(w, map[string]interface{}{
		"servers":    servers,
		"totalTools": totalTools,
	})
}

func (s *Server) handlePresets(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cm := &corev1.ConfigMap{}
	if err := s.Client.Get(ctx, client.ObjectKey{Name: "purko-presets", Namespace: s.ns()}, cm); err != nil {
		// No presets ConfigMap — return empty list
		writeJSON(w, map[string]interface{}{"presets": []interface{}{}})
		return
	}

	presetsYAML, ok := cm.Data["presets"]
	if !ok {
		writeJSON(w, map[string]interface{}{"presets": []interface{}{}})
		return
	}

	// Parse YAML presets into JSON-compatible structure
	var presets []interface{}
	if err := json.Unmarshal([]byte(presetsYAML), &presets); err != nil {
		// Try YAML format — convert to JSON
		writeJSON(w, map[string]interface{}{"presets": []interface{}{}, "error": "invalid presets format"})
		return
	}

	writeJSON(w, map[string]interface{}{"presets": presets})
}

func (s *Server) handleDeleteAgent(w http.ResponseWriter, r *http.Request) {
	s.logUser(r)
	if r.Method != "DELETE" && r.Method != "POST" {
		http.Error(w, "DELETE or POST required", 405)
		return
	}
	name := r.URL.Path[len("/api/delete/agent/"):]
	agent := &v1alpha1.Agent{}
	agent.Name = name
	agent.Namespace = s.ns()
	if err := s.Client.Delete(r.Context(), agent); err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"status": "deleted", "name": name})
}

func (s *Server) handleDeleteWorkflow(w http.ResponseWriter, r *http.Request) {
	s.logUser(r)
	if r.Method != "DELETE" && r.Method != "POST" {
		http.Error(w, "DELETE or POST required", 405)
		return
	}
	name := r.URL.Path[len("/api/delete/workflow/"):]
	wf := &v1alpha1.Workflow{}
	wf.Name = name
	wf.Namespace = s.ns()
	if err := s.Client.Delete(r.Context(), wf); err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"status": "deleted", "name": name})
}

// handleFeatures reports the compiled-in capability set (Spec 28).
// proFeatures() is a per-edition compile constant: all-true in the Pro
// build (handlers_pro.go), all-false in community (handlers_community.go).
func (s *Server) handleFeatures(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, proFeatures())
}

func (s *Server) handleWebhookTrigger(w http.ResponseWriter, r *http.Request) {
	s.logUser(r)
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}

	// Parse path: /api/trigger/{namespace}/{workflow-name}
	// Or: /api/trigger/{namespace} (auto-route based on payload)
	path := strings.TrimPrefix(r.URL.Path, "/api/trigger/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		writeJSON(w, map[string]string{"error": "path must be /api/trigger/{namespace} or /api/trigger/{namespace}/{workflow-name}"})
		return
	}
	ns := parts[0]
	explicitWorkflow := ""
	if len(parts) == 2 && parts[1] != "" {
		explicitWorkflow = parts[1]
	}

	ctx := r.Context()

	// Read payload
	var payload map[string]interface{}
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&payload)
	}
	if payload == nil {
		payload = map[string]interface{}{}
	}

	// Detect source from header or payload
	source := r.Header.Get("X-Trigger-Source")
	if source == "" {
		// Auto-detect from payload structure
		if _, ok := payload["event"]; ok {
			source = "pagerduty"
		} else if _, ok := payload["repository"]; ok {
			source = "github"
		} else if _, ok := payload["command"]; ok {
			source = "slack"
		} else {
			source = "unknown"
		}
	}

	// Normalize the payload
	normalized := normalizePayload(source, payload)

	// Determine which workflow to use
	var wfName string
	var routeMethod string

	if explicitWorkflow != "" {
		// Explicit workflow specified in URL
		wfName = explicitWorkflow
		routeMethod = "explicit"
	} else {
		// Route based on rules
		rules := loadRules(ctx, s.Client, ns)
		rule, matched := routeWebhook(rules, normalized)
		if matched {
			if rule.Workflow == "_intent" {
				// LLM fallback — use intent to design a workflow
				routeMethod = "llm-intent"
				wfName = s.createIntentWorkflow(ctx, ns, normalized)
				if wfName == "" {
					// Distinguish "not compiled in" (community stub) from a
					// real LLM failure (Spec 28): user-authored _intent rules
					// in the community edition should fail with a clear cause.
					if !proFeatures()["intent"] {
						writeJSON(w, map[string]string{"error": "the _intent fallback requires Purko Pro"})
						return
					}
					writeJSON(w, map[string]string{"error": "LLM intent failed to design workflow"})
					return
				}
			} else {
				wfName = rule.Workflow
				routeMethod = "rule:" + rule.Name
			}
		} else {
			writeJSON(w, map[string]string{"error": "no matching rule found"})
			return
		}
	}

	// Get the template workflow
	wf := &v1alpha1.Workflow{}
	if err := s.Client.Get(ctx, client.ObjectKey{Name: wfName, Namespace: ns}, wf); err != nil {
		// If LLM just created it, it might not exist yet as a template
		if routeMethod != "llm-intent" {
			w.WriteHeader(404)
			writeJSON(w, map[string]string{"error": fmt.Sprintf("workflow %q not found", wfName), "route": routeMethod})
			return
		}
		// For LLM-created workflows, the workflow was already created by createIntentWorkflow
		w.WriteHeader(201)
		writeJSON(w, map[string]interface{}{
			"status":     "triggered",
			"run":        wfName,
			"namespace":  ns,
			"route":      routeMethod,
			"source":     source,
			"normalized": normalized,
		})
		return
	}

	// Generate run name
	runName := fmt.Sprintf("%s-run-%s", wfName, fmt.Sprintf("%08x", time.Now().UnixNano()&0xFFFFFFFF))

	// Create a new workflow run from the template
	run := &v1alpha1.Workflow{}
	run.APIVersion = "purko.io/v1alpha1"
	run.Kind = "Workflow"
	run.Name = runName
	run.Namespace = ns
	run.Annotations = map[string]string{
		"purko.io/workflow-template": wfName,
		"purko.io/trigger-type":      "webhook",
		"purko.io/trigger-source":    source,
		"purko.io/trigger-route":     routeMethod,
	}
	run.Spec = wf.Spec

	// Merge webhook payload into workflow parameters
	// This enables ${parameters.repository}, ${parameters.issueNumber}, etc.
	if run.Spec.Parameters == nil {
		run.Spec.Parameters = map[string]string{}
	}
	for k, v := range payload {
		if strVal, ok := v.(string); ok {
			run.Spec.Parameters[k] = strVal
		} else {
			run.Spec.Parameters[k] = fmt.Sprintf("%v", v)
		}
	}

	// Do NOT overwrite step inputs — keep the template's step inputs
	// which use ${parameters.*} substitution. Only inject payload into
	// steps that have no input defined.
	for i := range run.Spec.Steps {
		if run.Spec.Steps[i].Input == nil || len(run.Spec.Steps[i].Input.Raw) <= 2 {
			inputData := map[string]interface{}{
				"webhook_source":  source,
				"webhook_payload": payload,
				"description":     normalized.Description,
				"task":            normalized.Description,
			}
			inputJSON, _ := json.Marshal(inputData)
			run.Spec.Steps[i].Input = &runtime.RawExtension{Raw: inputJSON}
		}
	}

	run.Spec.Trigger = nil

	if err := s.Client.Create(ctx, run); err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(201)
	writeJSON(w, map[string]interface{}{
		"status":     "triggered",
		"run":        runName,
		"namespace":  ns,
		"template":   wfName,
		"route":      routeMethod,
		"source":     source,
		"normalized": normalized,
	})
}

// ── MCPServer CRUD ──────────────────────────────────────────────────

func (s *Server) handleMCPServers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list := &v1alpha1.MCPServerList{}
	if err := s.Client.List(ctx, list); err != nil {
		writeJSON(w, map[string]interface{}{"servers": []interface{}{}, "error": err.Error()})
		return
	}

	servers := make([]map[string]interface{}, 0, len(list.Items))
	for _, m := range list.Items {
		servers = append(servers, map[string]interface{}{
			"metadata": m.ObjectMeta,
			"spec":     m.Spec,
			"status":   m.Status,
		})
	}
	writeJSON(w, map[string]interface{}{"servers": servers})
}

func (s *Server) handleMCPServerCreate(w http.ResponseWriter, r *http.Request) {
	s.logUser(r)
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	ctx := r.Context()
	var req struct {
		Name        string   `json:"name"`
		URL         string   `json:"url"`
		Image       string   `json:"image"`
		Port        int      `json:"port"`
		Category    string   `json:"category"`
		Icon        string   `json:"icon"`
		Auth        string   `json:"auth"`
		SecretRef   string   `json:"secretRef"`
		HostNetwork bool     `json:"hostNetwork"`
		Args        []string `json:"args"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	// Connect mode: register an already-running server by URL. These live in
	// the mcp-servers ConfigMap (what the registry reads) — no CR, no pod.
	if req.URL != "" {
		entry := map[string]interface{}{
			"name":     req.Name,
			"url":      req.URL,
			"auth":     req.Auth,
			"icon":     req.Icon,
			"category": req.Category,
		}
		if req.SecretRef != "" {
			entry["secretRef"] = req.SecretRef
		}
		if err := s.upsertMCPConfigEntry(ctx, entry); err != nil {
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		if s.Registry != nil {
			go s.Registry.Sync(context.Background())
		}
		writeJSON(w, map[string]string{"status": "connected", "name": req.Name})
		return
	}

	mcpServer := &v1alpha1.MCPServer{}
	mcpServer.APIVersion = "purko.io/v1alpha1"
	mcpServer.Kind = "MCPServer"
	mcpServer.Name = req.Name
	mcpServer.Namespace = s.ns()
	mcpServer.Spec.Image = req.Image
	mcpServer.Spec.Port = req.Port
	mcpServer.Spec.Category = req.Category
	mcpServer.Spec.Icon = req.Icon
	mcpServer.Spec.Auth = req.Auth
	mcpServer.Spec.SecretRef = req.SecretRef
	mcpServer.Spec.HostNetwork = req.HostNetwork
	mcpServer.Spec.Args = req.Args

	if err := s.Client.Create(ctx, mcpServer); err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"status": "created", "name": req.Name})
}

// upsertMCPConfigEntry adds or replaces (by name) a server entry in the
// mcp-servers ConfigMap — the registry's source of truth, same format the
// MCPServer controller writes.
func (s *Server) upsertMCPConfigEntry(ctx context.Context, entry map[string]interface{}) error {
	cm := &corev1.ConfigMap{}
	key := client.ObjectKey{Name: "mcp-servers", Namespace: s.ns()}
	create := false
	if err := s.Client.Get(ctx, key, cm); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		create = true
		cm.Name = key.Name
		cm.Namespace = key.Namespace
	}
	var servers []map[string]interface{}
	if raw, ok := cm.Data["servers"]; ok && raw != "" {
		_ = json.Unmarshal([]byte(raw), &servers)
	}
	replaced := false
	for i := range servers {
		if servers[i]["name"] == entry["name"] {
			servers[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		servers = append(servers, entry)
	}
	raw, err := json.MarshalIndent(servers, "", "  ")
	if err != nil {
		return err
	}
	if cm.Data == nil {
		cm.Data = map[string]string{}
	}
	cm.Data["servers"] = string(raw)
	if create {
		return s.Client.Create(ctx, cm)
	}
	return s.Client.Update(ctx, cm)
}

// removeMCPConfigEntry deletes a server entry by name; returns whether an
// entry was removed.
func (s *Server) removeMCPConfigEntry(ctx context.Context, name string) (bool, error) {
	cm := &corev1.ConfigMap{}
	key := client.ObjectKey{Name: "mcp-servers", Namespace: s.ns()}
	if err := s.Client.Get(ctx, key, cm); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	var servers []map[string]interface{}
	if raw, ok := cm.Data["servers"]; ok && raw != "" {
		_ = json.Unmarshal([]byte(raw), &servers)
	}
	kept := servers[:0]
	removed := false
	for _, srv := range servers {
		if srv["name"] == name {
			removed = true
			continue
		}
		kept = append(kept, srv)
	}
	if !removed {
		return false, nil
	}
	raw, err := json.MarshalIndent(kept, "", "  ")
	if err != nil {
		return false, err
	}
	cm.Data["servers"] = string(raw)
	return true, s.Client.Update(ctx, cm)
}

func (s *Server) handleMCPServerCRUD(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.logUser(r)
	}
	name := r.URL.Path[len("/api/mcp/server/"):]
	if name == "" {
		http.Error(w, "name required", 400)
		return
	}
	ctx := r.Context()
	// Find the MCPServer across namespaces
	list := &v1alpha1.MCPServerList{}
	s.Client.List(ctx, list)
	var found *v1alpha1.MCPServer
	for i := range list.Items {
		if list.Items[i].Name == name {
			found = &list.Items[i]
			break
		}
	}
	if found == nil {
		// URL-connected servers have no CR — they live in the ConfigMap.
		if r.Method == "DELETE" || (r.Method == "POST" && r.URL.Query().Get("action") == "delete") {
			removed, err := s.removeMCPConfigEntry(ctx, name)
			if err != nil {
				writeJSON(w, map[string]string{"error": err.Error()})
				return
			}
			if removed {
				if s.Registry != nil {
					go s.Registry.Sync(context.Background())
				}
				writeJSON(w, map[string]string{"status": "deleted", "name": name})
				return
			}
		}
		http.Error(w, "not found", 404)
		return
	}

	if r.Method == "DELETE" || (r.Method == "POST" && r.URL.Query().Get("action") == "delete") {
		if err := s.Client.Delete(ctx, found); err != nil {
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, map[string]string{"status": "deleted", "name": name})
		return
	}
	// GET
	writeJSON(w, found)
}

// ── LLMProvider CRUD ────────────────────────────────────────────────

func (s *Server) handleLLMProviders(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list := &v1alpha1.LLMProviderList{}
	if err := s.Client.List(ctx, list); err != nil {
		writeJSON(w, map[string]interface{}{"providers": []interface{}{}, "error": err.Error()})
		return
	}

	providers := make([]map[string]interface{}, 0, len(list.Items))
	for _, p := range list.Items {
		providers = append(providers, map[string]interface{}{
			"metadata": p.ObjectMeta,
			"spec":     p.Spec,
			"status":   p.Status,
		})
	}
	writeJSON(w, map[string]interface{}{"providers": providers})
}

func (s *Server) handleLLMProviderCreate(w http.ResponseWriter, r *http.Request) {
	s.logUser(r)
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	ctx := r.Context()
	var req struct {
		Name      string            `json:"name"`
		Type      string            `json:"type"`
		Model     string            `json:"model"`
		Endpoint  string            `json:"endpoint"`
		Default   bool              `json:"default"`
		SecretRef string            `json:"secretRef"`
		SecretKey string            `json:"secretKey"`
		Config    map[string]string `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	provider := &v1alpha1.LLMProvider{}
	provider.APIVersion = "purko.io/v1alpha1"
	provider.Kind = "LLMProvider"
	provider.Name = req.Name
	// Must match the namespace resolveLLMProvider lists (the controller
	// only looks in purko-system); s.ns() is the agent namespace and
	// providers created there are invisible to workflows.
	provider.Namespace = llmProviderNamespace
	provider.Spec.Type = req.Type
	provider.Spec.Model = req.Model
	provider.Spec.Default = req.Default
	// The controller only reads spec.endpoint (never config[endpoint]),
	// so promote the config key older UI guidance told users to set.
	provider.Spec.Endpoint = req.Endpoint
	if provider.Spec.Endpoint == "" {
		provider.Spec.Endpoint = req.Config["endpoint"]
	}
	delete(req.Config, "endpoint")
	if len(req.Config) > 0 {
		provider.Spec.Config = req.Config
	}
	if req.SecretRef != "" {
		key := req.SecretKey
		if key == "" {
			key = "api-key"
		}
		provider.Spec.Credentials = &v1alpha1.CredentialSpec{
			SecretRef: req.SecretRef,
			SecretKey: key,
		}
	}

	if err := s.Client.Create(ctx, provider); err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"status": "created", "name": req.Name})
}

func (s *Server) handleLLMProviderCRUD(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.logUser(r)
	}
	name := r.URL.Path[len("/api/llm/provider/"):]
	if name == "" {
		http.Error(w, "name required", 400)
		return
	}
	ctx := r.Context()
	list := &v1alpha1.LLMProviderList{}
	s.Client.List(ctx, list)
	var found *v1alpha1.LLMProvider
	for i := range list.Items {
		if list.Items[i].Name == name {
			found = &list.Items[i]
			break
		}
	}
	if found == nil {
		http.Error(w, "not found", 404)
		return
	}

	if r.Method == "DELETE" || (r.Method == "POST" && r.URL.Query().Get("action") == "delete") {
		if err := s.Client.Delete(ctx, found); err != nil {
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, map[string]string{"status": "deleted", "name": name})
		return
	}
	// GET
	writeJSON(w, found)
}

// ── Autonomy Policy ─────────────────────────────────────────────────

func (s *Server) handleApproveStep(w http.ResponseWriter, r *http.Request) {
	s.logUser(r)
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	// Path: /api/approve/{workflow}/{step}
	path := strings.TrimPrefix(r.URL.Path, "/api/approve/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 {
		writeJSON(w, map[string]string{"error": "path must be /api/approve/{workflow}/{step}"})
		return
	}
	wfName, stepName := parts[0], parts[1]
	ctx := r.Context()

	wf := &v1alpha1.Workflow{}
	if err := s.Client.Get(ctx, client.ObjectKey{Name: wfName, Namespace: s.ns()}, wf); err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	if wf.Annotations == nil {
		wf.Annotations = map[string]string{}
	}
	approvalKey := fmt.Sprintf("purko.io/approve-%s", stepName)
	wf.Annotations[approvalKey] = "true"

	if err := s.Client.Update(ctx, wf); err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, map[string]string{"status": "approved", "workflow": wfName, "step": stepName})
}

// writeStepStatusLogs serves the error/output persisted in workflow status
// when the step's pod is gone (TTL or failFast cleanup) — the panel must
// still explain the failure (F26).
func (s *Server) writeStepStatusLogs(ctx context.Context, w http.ResponseWriter, wfName, stepName, fallback string) {
	wf := &v1alpha1.Workflow{}
	if err := s.Client.Get(ctx, client.ObjectKey{Name: wfName, Namespace: s.ns()}, wf); err == nil {
		for i := range wf.Status.StepStatuses {
			ss := &wf.Status.StepStatuses[i]
			if ss.Name != stepName {
				continue
			}
			var lines []string
			if ss.Error != "" {
				lines = append(lines, strings.Split(ss.Error, "\n")...)
			}
			if ss.Output != nil && len(ss.Output.Raw) > 2 {
				lines = append(lines, "OUTPUT:"+string(ss.Output.Raw))
			}
			if len(lines) > 0 {
				status := "complete"
				if ss.Phase == "Failed" {
					status = "failed"
				}
				writeJSON(w, map[string]interface{}{"lines": lines, "status": status})
				return
			}
		}
	}
	writeJSON(w, map[string]interface{}{"lines": []string{}, "status": fallback})
}

func (s *Server) handleStepLogs(w http.ResponseWriter, r *http.Request) {
	// Path: /api/logs/{workflow}/{step}
	path := strings.TrimPrefix(r.URL.Path, "/api/logs/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 {
		writeJSON(w, map[string]string{"error": "path must be /api/logs/{workflow}/{step}"})
		return
	}
	wfName, stepName := parts[0], parts[1]
	ctx := r.Context()

	// Find the Job for this step
	jobList := &batchv1.JobList{}
	if err := s.Client.List(ctx, jobList,
		client.InNamespace(s.ns()),
		client.MatchingLabels{"purko.io/workflow": wfName, "purko.io/step": stepName}); err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	if len(jobList.Items) == 0 {
		s.writeStepStatusLogs(ctx, w, wfName, stepName, "no job")
		return
	}

	job := &jobList.Items[0]

	// Find pods for this job
	if s.Clientset == nil {
		writeJSON(w, map[string]interface{}{"lines": []string{"no clientset configured"}, "status": "error"})
		return
	}

	pods, err := s.Clientset.CoreV1().Pods(s.ns()).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", job.Name),
	})
	if err != nil || len(pods.Items) == 0 {
		s.writeStepStatusLogs(ctx, w, wfName, stepName, "no pods")
		return
	}

	// Read logs
	tailLines := int64(100)
	logOpts := &corev1.PodLogOptions{TailLines: &tailLines}
	stream, err := s.Clientset.CoreV1().Pods(s.ns()).
		GetLogs(pods.Items[0].Name, logOpts).Stream(ctx)
	if err != nil {
		writeJSON(w, map[string]interface{}{"lines": []string{fmt.Sprintf("log error: %v", err)}, "status": "error"})
		return
	}
	defer stream.Close()

	var lines []string
	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 0), 256*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	// Determine status
	status := "running"
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobComplete && c.Status == corev1.ConditionTrue {
			status = "complete"
		}
		if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
			status = "failed"
		}
	}

	writeJSON(w, map[string]interface{}{"lines": lines, "status": status, "pod": pods.Items[0].Name})
}

func (s *Server) handleDenyStep(w http.ResponseWriter, r *http.Request) {
	s.logUser(r)
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/deny/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 {
		writeJSON(w, map[string]string{"error": "path must be /api/deny/{workflow}/{step}"})
		return
	}
	wfName, stepName := parts[0], parts[1]
	ctx := r.Context()

	wf := &v1alpha1.Workflow{}
	if err := s.Client.Get(ctx, client.ObjectKey{Name: wfName, Namespace: s.ns()}, wf); err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	// Mark step as Failed in status
	for i := range wf.Status.StepStatuses {
		if wf.Status.StepStatuses[i].Name == stepName {
			wf.Status.StepStatuses[i].Phase = "Failed"
			wf.Status.StepStatuses[i].Error = "Denied by human"
			now := metav1.Now()
			wf.Status.StepStatuses[i].CompletionTime = &now
			wf.Status.FailedSteps++
			break
		}
	}

	if err := s.Client.Status().Update(ctx, wf); err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, map[string]string{"status": "denied", "workflow": wfName, "step": stepName})
}

func (s *Server) handleRerunWorkflow(w http.ResponseWriter, r *http.Request) {
	s.logUser(r)
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	ctx := r.Context()
	name := r.URL.Path[len("/api/rerun/workflow/"):]
	if name == "" {
		writeJSON(w, map[string]string{"error": "workflow name required"})
		return
	}

	// Read the existing workflow
	wf := &v1alpha1.Workflow{}
	if err := s.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: s.ns()}, wf); err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	// Save the spec; apply optional parameter overrides from the body while
	// keeping everything else (step input templates, timeouts, retries).
	spec := wf.Spec
	var req struct {
		Parameters map[string]string `json:"parameters"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req) // empty body is fine
	}
	if len(req.Parameters) > 0 {
		merged := map[string]string{}
		for k, v := range spec.Parameters {
			merged[k] = v
		}
		for k, v := range req.Parameters {
			merged[k] = v
		}
		spec.Parameters = merged
	}

	// Delete the old workflow (cascading cleans up Jobs + ConfigMap)
	if err := s.Client.Delete(ctx, wf); err != nil {
		writeJSON(w, map[string]string{"error": "delete failed: " + err.Error()})
		return
	}

	// Wait briefly for deletion to propagate
	time.Sleep(2 * time.Second)

	// Create a fresh workflow with the same spec
	newWf := &v1alpha1.Workflow{}
	newWf.APIVersion = "purko.io/v1alpha1"
	newWf.Kind = "Workflow"
	newWf.Name = name
	newWf.Namespace = s.ns()
	newWf.Spec = spec

	if err := s.Client.Create(ctx, newWf); err != nil {
		writeJSON(w, map[string]string{"error": "re-create failed: " + err.Error()})
		return
	}

	writeJSON(w, map[string]string{"status": "restarted", "name": name})
}

func (s *Server) buildOverview(ctx context.Context) OverviewData {
	agents := s.listAgents(ctx)
	workflows := s.listWorkflows(ctx)

	ready := 0
	for _, a := range agents {
		if a.Phase == "Ready" {
			ready++
		}
	}

	succeeded, running, failed := 0, 0, 0
	for _, w := range workflows {
		switch w.Phase {
		case "Succeeded":
			succeeded++
		case "Running":
			running++
		case "Failed", "CompletedWithErrors":
			failed++
		}
	}

	deployList := &appsv1.DeploymentList{}
	s.Client.List(ctx, deployList, client.InNamespace(s.ns()),
		client.MatchingLabels{"app.kubernetes.io/part-of": "agentic-crds"})

	hpaList := &autoscalingv2.HorizontalPodAutoscalerList{}
	s.Client.List(ctx, hpaList, client.InNamespace(s.ns()))

	return OverviewData{
		AgentCount:    len(agents),
		AgentReady:    ready,
		WorkflowCount: len(workflows),
		WfSucceeded:   succeeded,
		WfRunning:     running,
		WfFailed:      failed,
		DeployCount:   len(deployList.Items),
		HPACount:      len(hpaList.Items),
		Agents:        agents,
		Workflows:     workflows,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
	}
}

func (s *Server) listAgents(ctx context.Context) []AgentSummary {
	agentList := &v1alpha1.AgentList{}
	s.Client.List(ctx, agentList, client.InNamespace(s.ns()))

	result := make([]AgentSummary, 0, len(agentList.Items))
	for _, a := range agentList.Items {
		group := a.Labels["app.kubernetes.io/component"]
		if group == "" {
			group = "general"
		}
		image := "purko-executor:latest"
		if a.Spec.Runtime != nil && a.Spec.Runtime.Image != "" {
			image = a.Spec.Runtime.Image
		}
		result = append(result, AgentSummary{
			Name:       a.Name,
			Namespace:  a.Namespace,
			Type:       a.Spec.Type,
			Provider:   a.Spec.Model.Provider,
			Model:      a.Spec.Model.Name,
			Phase:      a.Status.Phase,
			Replicas:   a.Status.AvailableReplicas,
			Autonomy:   a.Spec.AutonomyLevel,
			ToolCount:  len(a.Spec.Tools),
			Age:        time.Since(a.CreationTimestamp.Time).Round(time.Second).String(),
			Generation: a.Generation,
			Group:      group,
			Image:      image,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func (s *Server) listWorkflows(ctx context.Context) []WorkflowSummary {
	wfList := &v1alpha1.WorkflowList{}
	s.Client.List(ctx, wfList, client.InNamespace(s.ns()))

	result := make([]WorkflowSummary, 0, len(wfList.Items))
	for _, w := range wfList.Items {
		dur := ""
		if w.Status.CompletionTime != nil && w.Status.StartTime != nil {
			dur = w.Status.CompletionTime.Sub(w.Status.StartTime.Time).Round(time.Second).String()
		}

		steps := make([]StepBrief, 0, len(w.Spec.Steps))
		statusMap := map[string]v1alpha1.StepStatus{}
		for _, ss := range w.Status.StepStatuses {
			statusMap[ss.Name] = ss
		}

		for _, step := range w.Spec.Steps {
			ss := statusMap[step.Name]
			stepDur := ""
			if ss.CompletionTime != nil && ss.StartTime != nil {
				stepDur = ss.CompletionTime.Sub(ss.StartTime.Time).Round(time.Second).String()
			}
			steps = append(steps, StepBrief{
				Name:     step.Name,
				Phase:    ss.Phase,
				Agent:    step.AgentRef.Name,
				Type:     step.Type,
				JobName:  ss.JobName,
				Duration: stepDur,
				DepsOn:   step.DependsOn,
			})
		}

		triggerType := ""
		triggerSource := ""
		triggerRoute := ""
		templateRef := ""
		if w.Annotations != nil {
			triggerType = w.Annotations["purko.io/trigger-type"]
			triggerSource = w.Annotations["purko.io/trigger-source"]
			triggerRoute = w.Annotations["purko.io/trigger-route"]
			templateRef = w.Annotations["purko.io/workflow-template"]
		}

		// Extract repository from parameters
		repo := ""
		if w.Spec.Parameters != nil {
			repo = w.Spec.Parameters["repository"]
		}

		result = append(result, WorkflowSummary{
			Name:          w.Name,
			Namespace:     w.Namespace,
			Phase:         w.Status.Phase,
			Total:         w.Status.TotalSteps,
			Completed:     w.Status.CompletedSteps,
			Failed:        w.Status.FailedSteps,
			Duration:      dur,
			Steps:         steps,
			Age:           time.Since(w.CreationTimestamp.Time).Round(time.Second).String(),
			TriggerType:   triggerType,
			TriggerSource: triggerSource,
			TriggerRoute:  triggerRoute,
			TemplateRef:   templateRef,
			Repository:    repo,
			Parameters:    w.Spec.Parameters,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func jobStatus(j *batchv1.Job) string {
	for _, c := range j.Status.Conditions {
		if c.Type == batchv1.JobComplete && c.Status == corev1.ConditionTrue {
			return "Complete"
		}
		if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
			return "Failed"
		}
	}
	if j.Status.Active > 0 {
		return "Running"
	}
	return "Pending"
}

func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(data)
}
