package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	SchemeGroupVersion = schema.GroupVersion{Group: "purko.io", Version: "v1alpha1"}
	SchemeBuilder      = runtime.NewSchemeBuilder(addKnownTypes)
	AddToScheme        = SchemeBuilder.AddToScheme
)

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(SchemeGroupVersion,
		&Agent{},
		&AgentList{},
		&Workflow{},
		&WorkflowList{},
		&AgentAutonomyPolicy{},
		&AgentAutonomyPolicyList{},
		&MCPServer{},
		&MCPServerList{},
		&LLMProvider{},
		&LLMProviderList{},
		&MemoryProvider{},
		&MemoryProviderList{},
	)
	metav1.AddToGroupVersion(scheme, SchemeGroupVersion)
	return nil
}

func Resource(resource string) schema.GroupResource {
	return SchemeGroupVersion.WithResource(resource).GroupResource()
}

// ── Shared Types ─────────────────────────────────────────────────────

type SecretRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Key       string `json:"key,omitempty"`
}

type EndpointSpec struct {
	URL            string            `json:"url"`
	Method         string            `json:"method,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	TimeoutSeconds int               `json:"timeoutSeconds,omitempty"`
	AuthScheme     string            `json:"authScheme,omitempty"` // Bearer, Token, Basic, ApiKey
	AuthHeader     string            `json:"authHeader,omitempty"` // header name, default: Authorization
}

type RetryPolicySpec struct {
	MaxRetries     int      `json:"maxRetries,omitempty"`
	BackoffSeconds int      `json:"backoffSeconds,omitempty"`
	Backoff        string   `json:"backoff,omitempty"`
	RetryOn        []string `json:"retryOn,omitempty"`
}

type StepTimeoutSpec struct {
	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`
}

type EnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// ── Agent ────────────────────────────────────────────────────────────

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type Agent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              AgentSpec   `json:"spec,omitempty"`
	Status            AgentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type AgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Agent `json:"items"`
}

type AgentSpec struct {
	Type                string          `json:"type,omitempty"` // planner, executor, reviewer, router, monitor, retriever
	Model               ModelSpec       `json:"model"`
	Role                string          `json:"role,omitempty"`
	SystemPrompt        string          `json:"systemPrompt,omitempty"`
	Instructions        string          `json:"instructions,omitempty"`
	AutonomyLevel       string          `json:"autonomyLevel,omitempty"`
	ShuHaRi             *ShuHaRiSpec    `json:"shuHaRi,omitempty"`
	ApprovalPolicy      *ApprovalPolicy `json:"approvalPolicy,omitempty"`
	ConfidenceThreshold *float64        `json:"confidenceThreshold,omitempty"`
	Replicas            *int            `json:"replicas,omitempty"`
	MaxConcurrency      *int            `json:"maxConcurrency,omitempty"`
	Timeout             string          `json:"timeout,omitempty"`
	Tools               []ToolSpec      `json:"tools,omitempty"`
	Memory              *MemorySpec     `json:"memory,omitempty"`
	Runtime             *RuntimeSpec    `json:"runtime,omitempty"`
	Scaling             *ScalingSpec    `json:"scaling,omitempty"`
	// Free-form fields (complex, schema-flexible structures)
	Guardrails    *runtime.RawExtension  `json:"guardrails,omitempty"`
	Observability *runtime.RawExtension  `json:"observability,omitempty"`
	Lifecycle     *runtime.RawExtension  `json:"lifecycle,omitempty"`
	Schedule      *runtime.RawExtension  `json:"schedule,omitempty"`
	BlastRadius   *runtime.RawExtension  `json:"blastRadiusLimit,omitempty"`
	Escalation    *runtime.RawExtension  `json:"escalationPolicy,omitempty"`
	Capabilities  []runtime.RawExtension `json:"capabilities,omitempty"`
}

type ModelSpec struct {
	Provider             string                `json:"provider"`
	Name                 string                `json:"name"`
	Version              string                `json:"version,omitempty"`
	Temperature          *float64              `json:"temperature,omitempty"`
	MaxTokens            *int                  `json:"maxTokens,omitempty"`
	TopP                 *float64              `json:"topP,omitempty"`
	FrequencyPenalty     *float64              `json:"frequencyPenalty,omitempty"`
	PresencePenalty      *float64              `json:"presencePenalty,omitempty"`
	CredentialsSecretRef *SecretRef            `json:"credentialsSecretRef,omitempty"`
	Fallback             *runtime.RawExtension `json:"fallback,omitempty"`
}

type ToolSpec struct {
	Name                 string                `json:"name"`
	Type                 string                `json:"type"`
	Endpoint             *EndpointSpec         `json:"endpoint,omitempty"`
	CredentialsSecretRef *SecretRef            `json:"credentialsSecretRef,omitempty"`
	Config               *runtime.RawExtension `json:"config,omitempty"`
}

type MemorySpec struct {
	// Deprecated: use Behavior. Equivalence for webhook validation and dashboard
	// display ONLY (none≈off, buffer≈session, summary≈persistent, vector≈persistent).
	// At runtime, legacy Type keeps its legacy code path unchanged; Behavior, when
	// set, wins and drives the new provider-mediated path (Spec 34 §1).
	Type              string             `json:"type,omitempty"`        // buffer, summary, vector, none
	Behavior          string             `json:"behavior,omitempty"`    // off, session, persistent
	ProviderRef       string             `json:"providerRef,omitempty"` // MemoryProvider name; empty = platform default
	Scope             string             `json:"scope,omitempty"`       // agent (default), group, namespace
	Backend           string             `json:"backend,omitempty"`     // Deprecated, unused (kept for back-compat)
	TTL               string             `json:"ttl,omitempty"`
	MaxEntries        *int               `json:"maxEntries,omitempty"`        // per scope key (default 500)
	MaxContextTokens  *int               `json:"maxContextTokens,omitempty"`  // cap on injected recall (default 2048)
	RetentionPolicy   string             `json:"retentionPolicy,omitempty"`   // Deprecated, unused (kept for back-compat)
	PersistentStorage *PersistentStorage `json:"persistentStorage,omitempty"` // legacy vector path only
}

type PersistentStorage struct {
	Enabled        bool   `json:"enabled"`
	VolumeClaimRef string `json:"volumeClaimRef,omitempty"`
}

type RuntimeSpec struct {
	Image              string                `json:"image,omitempty"`
	ServiceAccountName string                `json:"serviceAccountName,omitempty"`
	Env                []EnvVar              `json:"env,omitempty"`
	Config             map[string]string     `json:"config,omitempty"` // passed as EXECUTOR_* env vars
	Resources          *runtime.RawExtension `json:"resources,omitempty"`
	CodeExecution      *CodeExecutionSpec    `json:"codeExecution,omitempty"`
}

type CodeExecutionSpec struct {
	Enabled      bool         `json:"enabled"`
	Languages    []string     `json:"languages,omitempty"` // python, bash
	Sandbox      *SandboxSpec `json:"sandbox,omitempty"`
	Preinstalled []string     `json:"preinstalled,omitempty"` // documentation only
}

type SandboxSpec struct {
	MaxExecutionSeconds int      `json:"maxExecutionSeconds,omitempty"` // default 30
	MaxOutputBytes      int      `json:"maxOutputBytes,omitempty"`      // default 100000
	NetworkAccess       bool     `json:"networkAccess,omitempty"`       // default false
	WritablePaths       []string `json:"writablePaths,omitempty"`       // default ["/tmp"]
}

type ScalingSpec struct {
	MinReplicas       *int                   `json:"minReplicas,omitempty"`
	MaxReplicas       *int                   `json:"maxReplicas,omitempty"`
	TargetUtilization *int                   `json:"targetUtilization,omitempty"`
	Metrics           []runtime.RawExtension `json:"metrics,omitempty"`
	Behavior          *runtime.RawExtension  `json:"behavior,omitempty"`
}

type AgentStatus struct {
	Phase               string             `json:"phase,omitempty"`
	Message             string             `json:"message,omitempty"`
	ObservedGeneration  int64              `json:"observedGeneration,omitempty"`
	AvailableReplicas   int                `json:"availableReplicas,omitempty"`
	TotalTasksProcessed int64              `json:"totalTasksProcessed,omitempty"`
	ErrorCount          int64              `json:"errorCount,omitempty"`
	StartTime           *metav1.Time       `json:"startTime,omitempty"`
	CompletionTime      *metav1.Time       `json:"completionTime,omitempty"`
	LastActiveTime      *metav1.Time       `json:"lastActiveTime,omitempty"`
	Conditions          []metav1.Condition `json:"conditions,omitempty"`
	Metrics             *AgentMetrics      `json:"metrics,omitempty"`
	ShuHaRi             *ShuHaRiStatus     `json:"shuHaRi,omitempty"`
}

type AgentMetrics struct {
	TotalInvocations    int64        `json:"totalInvocations"`
	TotalTokensUsed     int64        `json:"totalTokensUsed"`
	AverageLatencyMs    int64        `json:"averageLatencyMs"`
	TotalCostUSD        float64      `json:"totalCostUSD"`
	LastInvocationTime  *metav1.Time `json:"lastInvocationTime,omitempty"`
	SuccessCount        int64        `json:"successCount,omitempty"`
	FailureCount        int64        `json:"failureCount,omitempty"`
	ConsecutiveFailures int64        `json:"consecutiveFailures,omitempty"`
}

// ── Shu-Ha-Ri ────────────────────────────────────────────────────────

// ShuHaRiSpec is the agent-side Shu-Ha-Ri configuration in the Agent spec.
type ShuHaRiSpec struct {
	Level      string       `json:"level,omitempty"`      // shu, ha, ri
	PromotedAt *metav1.Time `json:"promotedAt,omitempty"` // when last promoted
}

// ShuHaRiStatus tracks progression in the agent status.
type ShuHaRiStatus struct {
	CurrentLevel      string             `json:"currentLevel"`
	ReadyForPromotion bool               `json:"readyForPromotion"`
	PromotionProgress *PromotionProgress `json:"promotionProgress,omitempty"`
}

type PromotionProgress struct {
	ActionsCompleted int64   `json:"actionsCompleted"`
	ActionsRequired  int64   `json:"actionsRequired"`
	SuccessRate      float64 `json:"successRate"`
	DaysInLevel      int     `json:"daysInLevel"`
	DaysRequired     int     `json:"daysRequired"`
}

// ApprovalPolicy derived from Shu-Ha-Ri level.
type ApprovalPolicy struct {
	Mode                 string `json:"mode,omitempty"`                 // manual, semi-autonomous, autonomous
	AutoApproveRiskBelow string `json:"autoApproveRiskBelow,omitempty"` // low, medium, high
	RequireApprovalAbove string `json:"requireApprovalAbove,omitempty"` // low, medium, high
}

// ── AgentAutonomyPolicy CRD ──────────────────────────────────────────

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type AgentAutonomyPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              AgentAutonomyPolicySpec   `json:"spec,omitempty"`
	Status            AgentAutonomyPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type AgentAutonomyPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AgentAutonomyPolicy `json:"items"`
}

type AgentAutonomyPolicySpec struct {
	ShuHaRi  ShuHaRiPolicySpec `json:"shuHaRi"`
	Rollback RollbackSpec      `json:"rollback,omitempty"`
}

type ShuHaRiPolicySpec struct {
	ProgressionCriteria ProgressionCriteria `json:"progressionCriteria"`
}

type ProgressionCriteria struct {
	ShuToHa TransitionCriteria `json:"shuToHa"`
	HaToRi  TransitionCriteria `json:"haToRi"`
}

type TransitionCriteria struct {
	MinimumActionsCompleted int     `json:"minimumActionsCompleted"`
	MinimumSuccessRate      float64 `json:"minimumSuccessRate"`
	MinimumDaysInLevel      int     `json:"minimumDaysInLevel"`
	RequiredApprovals       int     `json:"requiredApprovals,omitempty"`
	IncidentFreeStreak      int     `json:"incidentFreeStreak,omitempty"`
}

type RollbackSpec struct {
	Enabled           bool             `json:"enabled"`
	TriggerConditions RollbackTriggers `json:"triggerConditions,omitempty"`
	RollbackLevel     string           `json:"rollbackLevel,omitempty"` // shu, ha
}

type RollbackTriggers struct {
	SuccessRateBelow    float64 `json:"successRateBelow,omitempty"`
	ConsecutiveFailures int     `json:"consecutiveFailures,omitempty"`
}

type AgentAutonomyPolicyStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

func (p *AgentAutonomyPolicy) DeepCopyObject() runtime.Object {
	cp := *p
	cp.ObjectMeta = *p.ObjectMeta.DeepCopy()
	return &cp
}

func (l *AgentAutonomyPolicyList) DeepCopyObject() runtime.Object {
	cp := *l
	cp.ListMeta = *l.ListMeta.DeepCopy()
	if l.Items != nil {
		cp.Items = make([]AgentAutonomyPolicy, len(l.Items))
		for i := range l.Items {
			cp.Items[i] = *l.Items[i].DeepCopyObject().(*AgentAutonomyPolicy)
		}
	}
	return &cp
}

// ── MCPServer CRD ────────────────────────────────────────────────────

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type MCPServer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              MCPServerSpec   `json:"spec,omitempty"`
	Status            MCPServerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type MCPServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MCPServer `json:"items"`
}

type MCPServerSpec struct {
	Image       string                `json:"image"`
	Port        int                   `json:"port,omitempty"`
	Args        []string              `json:"args,omitempty"` // container command args
	Replicas    *int                  `json:"replicas,omitempty"`
	Auth        string                `json:"auth,omitempty"`      // none, bearer
	SecretRef   string                `json:"secretRef,omitempty"` // Secret name for auth token
	Icon        string                `json:"icon,omitempty"`
	Category    string                `json:"category,omitempty"`
	HostNetwork bool                  `json:"hostNetwork,omitempty"` // use host networking (minikube/podman)
	Env         []EnvVar              `json:"env,omitempty"`
	Resources   *ResourceRequirements `json:"resources,omitempty"`
}

type ResourceRequirements struct {
	Requests map[string]string `json:"requests,omitempty"`
	Limits   map[string]string `json:"limits,omitempty"`
}

type MCPServerStatus struct {
	Phase         string             `json:"phase,omitempty"` // Pending, Ready, Error
	ToolCount     int                `json:"toolCount,omitempty"`
	LastDiscovery *metav1.Time       `json:"lastDiscovery,omitempty"`
	Message       string             `json:"message,omitempty"`
	Conditions    []metav1.Condition `json:"conditions,omitempty"`
}

func (m *MCPServer) DeepCopyObject() runtime.Object {
	cp := *m
	cp.ObjectMeta = *m.ObjectMeta.DeepCopy()
	return &cp
}

func (l *MCPServerList) DeepCopyObject() runtime.Object {
	cp := *l
	cp.ListMeta = *l.ListMeta.DeepCopy()
	if l.Items != nil {
		cp.Items = make([]MCPServer, len(l.Items))
		for i := range l.Items {
			cp.Items[i] = *l.Items[i].DeepCopyObject().(*MCPServer)
		}
	}
	return &cp
}

// ── LLMProvider CRD ──────────────────────────────────────────────────

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type LLMProvider struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              LLMProviderSpec   `json:"spec,omitempty"`
	Status            LLMProviderStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type LLMProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LLMProvider `json:"items"`
}

type LLMProviderSpec struct {
	Type        string            `json:"type"`                // vertex-ai, anthropic, openai, ollama, custom
	Endpoint    string            `json:"endpoint,omitempty"`  // custom API endpoint URL
	APIFormat   string            `json:"apiFormat,omitempty"` // "anthropic" or "openai" (inferred from type if empty)
	Model       string            `json:"model"`               // default model
	Models      []ModelDefinition `json:"models,omitempty"`    // available models
	Config      map[string]string `json:"config,omitempty"`    // provider-specific config
	Credentials *CredentialSpec   `json:"credentials,omitempty"`
	HealthCheck *HealthCheckSpec  `json:"healthCheck,omitempty"`
	Fallback    *FallbackSpec     `json:"fallback,omitempty"`
	Default     bool              `json:"default,omitempty"`
}

type ModelDefinition struct {
	Name        string        `json:"name"`
	DisplayName string        `json:"displayName,omitempty"`
	MaxTokens   int           `json:"maxTokens,omitempty"`
	Pricing     *ModelPricing `json:"pricing,omitempty"`
}

type ModelPricing struct {
	InputPerMToken  float64 `json:"inputPerMToken"`
	OutputPerMToken float64 `json:"outputPerMToken"`
}

type CredentialSpec struct {
	SecretRef string `json:"secretRef"`
	SecretKey string `json:"secretKey,omitempty"`
}

type HealthCheckSpec struct {
	Enabled         bool `json:"enabled"`
	IntervalSeconds int  `json:"intervalSeconds,omitempty"`
	TimeoutSeconds  int  `json:"timeoutSeconds,omitempty"`
}

type FallbackSpec struct {
	ProviderRef string `json:"providerRef"`
}

type LLMProviderStatus struct {
	Phase           string             `json:"phase,omitempty"`
	Message         string             `json:"message,omitempty"`
	LastHealthCheck *metav1.Time       `json:"lastHealthCheck,omitempty"`
	AvailableModels int                `json:"availableModels,omitempty"`
	Conditions      []metav1.Condition `json:"conditions,omitempty"`
}

func (p *LLMProvider) DeepCopyObject() runtime.Object {
	cp := *p
	cp.ObjectMeta = *p.ObjectMeta.DeepCopy()
	return &cp
}

func (l *LLMProviderList) DeepCopyObject() runtime.Object {
	cp := *l
	cp.ListMeta = *l.ListMeta.DeepCopy()
	if l.Items != nil {
		cp.Items = make([]LLMProvider, len(l.Items))
		for i := range l.Items {
			cp.Items[i] = *l.Items[i].DeepCopyObject().(*LLMProvider)
		}
	}
	return &cp
}

// ── MemoryProvider CRD (Spec 34) ──────────────────────────────────

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type MemoryProvider struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              MemoryProviderSpec   `json:"spec,omitempty"`
	Status            MemoryProviderStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type MemoryProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MemoryProvider `json:"items"`
}

type MemoryProviderSpec struct {
	Type        string            `json:"type"`             // builtin (implemented); redis, postgres, mcp (reserved)
	Config      map[string]string `json:"config,omitempty"` // provider-specific
	Credentials *CredentialSpec   `json:"credentials,omitempty"`
	Default     bool              `json:"default,omitempty"` // platform default when Agent.providerRef empty
	Retention   *MemoryRetention  `json:"retention,omitempty"`
}

type MemoryRetention struct {
	MaxEntriesPerScope  *int `json:"maxEntriesPerScope,omitempty"`  // default 500
	MaxAgeDays          *int `json:"maxAgeDays,omitempty"`          // unset = no age cap
	RecallLogMaxAgeDays *int `json:"recallLogMaxAgeDays,omitempty"` // default 90
}

type MemoryProviderStatus struct {
	Healthy     bool        `json:"healthy"`
	EntryCount  int64       `json:"entryCount,omitempty"`
	LastError   string      `json:"lastError,omitempty"`
	LastChecked metav1.Time `json:"lastChecked,omitempty"`
}

func (p *MemoryProvider) DeepCopyObject() runtime.Object {
	if p == nil {
		return nil
	}
	cp := *p
	cp.ObjectMeta = *p.ObjectMeta.DeepCopy()
	// deep-copy spec map
	if p.Spec.Config != nil {
		cp.Spec.Config = make(map[string]string, len(p.Spec.Config))
		for k, v := range p.Spec.Config {
			cp.Spec.Config[k] = v
		}
	}
	// deep-copy spec pointer fields
	if p.Spec.Credentials != nil {
		cred := *p.Spec.Credentials
		cp.Spec.Credentials = &cred
	}
	if p.Spec.Retention != nil {
		ret := *p.Spec.Retention
		if p.Spec.Retention.MaxEntriesPerScope != nil {
			v := *p.Spec.Retention.MaxEntriesPerScope
			ret.MaxEntriesPerScope = &v
		}
		if p.Spec.Retention.MaxAgeDays != nil {
			v := *p.Spec.Retention.MaxAgeDays
			ret.MaxAgeDays = &v
		}
		if p.Spec.Retention.RecallLogMaxAgeDays != nil {
			v := *p.Spec.Retention.RecallLogMaxAgeDays
			ret.RecallLogMaxAgeDays = &v
		}
		cp.Spec.Retention = &ret
	}
	return &cp
}

func (l *MemoryProviderList) DeepCopyObject() runtime.Object {
	cp := *l
	cp.ListMeta = *l.ListMeta.DeepCopy()
	if l.Items != nil {
		cp.Items = make([]MemoryProvider, len(l.Items))
		for i := range l.Items {
			cp.Items[i] = *l.Items[i].DeepCopyObject().(*MemoryProvider)
		}
	}
	return &cp
}

// ── Workflow ─────────────────────────────────────────────────────────

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type Workflow struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              WorkflowSpec   `json:"spec,omitempty"`
	Status            WorkflowStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type WorkflowList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Workflow `json:"items"`
}

type WorkflowSpec struct {
	Description     string            `json:"description,omitempty"`
	Steps           []WorkflowStep    `json:"steps"`
	Parallelism     *int              `json:"parallelism,omitempty"`
	FailureStrategy string            `json:"failureStrategy,omitempty"`
	Parameters      map[string]string `json:"parameters,omitempty"`
	Edges           []Edge            `json:"edges,omitempty"`
	// Typed trigger and concurrency
	Trigger     *TriggerSpec     `json:"trigger,omitempty"`
	Concurrency *ConcurrencySpec `json:"concurrency,omitempty"`
	// Free-form fields
	ErrorHandling *runtime.RawExtension  `json:"errorHandling,omitempty"`
	Observability *runtime.RawExtension  `json:"observability,omitempty"`
	Timeout       *runtime.RawExtension  `json:"timeout,omitempty"`
	Variables     []runtime.RawExtension `json:"variables,omitempty"`
	Hooks         *runtime.RawExtension  `json:"hooks,omitempty"`
}

type WorkflowStep struct {
	Name          string           `json:"name"`
	AgentRef      AgentRef         `json:"agentRef,omitempty"`
	Type          string           `json:"type,omitempty"`
	Description   string           `json:"description,omitempty"`
	DependsOn     []string         `json:"dependsOn,omitempty"`
	InputFrom     []InputRef       `json:"inputFrom,omitempty"`
	RetryPolicy   *RetryPolicySpec `json:"retryPolicy,omitempty"`
	StepTimeout   *StepTimeoutSpec `json:"timeout,omitempty"`
	ConditionExpr string           `json:"condition,omitempty"` // CEL expression for conditional execution
	// Free-form fields
	Input  *runtime.RawExtension `json:"input,omitempty"`
	Output *runtime.RawExtension `json:"output,omitempty"`
	Config *runtime.RawExtension `json:"config,omitempty"`
}

type AgentRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

type InputRef struct {
	Step      string `json:"step"`
	OutputKey string `json:"outputKey"`
}

type Edge struct {
	From      string                `json:"from"`
	To        string                `json:"to"`
	Condition *runtime.RawExtension `json:"condition,omitempty"`
}

type TriggerSpec struct {
	Type          string                 `json:"type"`
	Schedule      *ScheduleTrigger       `json:"schedule,omitempty"`
	Webhook       *WebhookTrigger        `json:"webhook,omitempty"`
	EventTriggers []runtime.RawExtension `json:"eventTriggers,omitempty"`
}

type ScheduleTrigger struct {
	Cron     string `json:"cron"`
	Timezone string `json:"timezone,omitempty"`
	Suspend  bool   `json:"suspend,omitempty"`
}

type WebhookTrigger struct {
	Path      string    `json:"path,omitempty"`
	SecretRef SecretRef `json:"secret,omitempty"`
}

type ConcurrencySpec struct {
	Policy      string `json:"policy,omitempty"`
	MaxParallel int    `json:"maxParallel,omitempty"`
}

type WorkflowStatus struct {
	Phase              string             `json:"phase,omitempty"`
	Message            string             `json:"message,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	TotalSteps         int                `json:"totalSteps,omitempty"`
	CompletedSteps     int                `json:"completedSteps,omitempty"`
	FailedSteps        int                `json:"failedSteps,omitempty"`
	StartTime          *metav1.Time       `json:"startTime,omitempty"`
	CompletionTime     *metav1.Time       `json:"completionTime,omitempty"`
	StepStatuses       []StepStatus       `json:"stepStatuses,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
	// Template trigger status
	ActiveRuns      int          `json:"activeRuns,omitempty"`
	LastTriggerTime *metav1.Time `json:"lastTriggerTime,omitempty"`
	NextRunTime     *metav1.Time `json:"nextRunTime,omitempty"`
}

type StepStatus struct {
	Name           string                `json:"name,omitempty"`
	Phase          string                `json:"phase,omitempty"`
	JobName        string                `json:"jobName,omitempty"`
	StartTime      *metav1.Time          `json:"startTime,omitempty"`
	CompletionTime *metav1.Time          `json:"completionTime,omitempty"`
	Output         *runtime.RawExtension `json:"output,omitempty"`
	Error          string                `json:"error,omitempty"`
	RetryCount     int                   `json:"retryCount,omitempty"`
}

// ── DeepCopy: Agent ──────────────────────────────────────────────────

func (in *Agent) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	out := new(Agent)
	in.DeepCopyInto(out)
	return out
}

func (in *Agent) DeepCopyInto(out *Agent) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

func (in *AgentSpec) DeepCopyInto(out *AgentSpec) {
	*out = *in
	in.Model.DeepCopyInto(&out.Model)
	if in.ConfidenceThreshold != nil {
		v := *in.ConfidenceThreshold
		out.ConfidenceThreshold = &v
	}
	if in.Replicas != nil {
		v := *in.Replicas
		out.Replicas = &v
	}
	if in.MaxConcurrency != nil {
		v := *in.MaxConcurrency
		out.MaxConcurrency = &v
	}
	if in.Tools != nil {
		out.Tools = make([]ToolSpec, len(in.Tools))
		for i := range in.Tools {
			in.Tools[i].DeepCopyInto(&out.Tools[i])
		}
	}
	if in.Memory != nil {
		out.Memory = new(MemorySpec)
		*out.Memory = *in.Memory
		// Deep-copy the pointer fields so copies don't share pointees
		// (MaxContextTokens/MaxEntries became real *int with Spec 34).
		if in.Memory.MaxContextTokens != nil {
			v := *in.Memory.MaxContextTokens
			out.Memory.MaxContextTokens = &v
		}
		if in.Memory.MaxEntries != nil {
			v := *in.Memory.MaxEntries
			out.Memory.MaxEntries = &v
		}
		if in.Memory.PersistentStorage != nil {
			ps := *in.Memory.PersistentStorage
			out.Memory.PersistentStorage = &ps
		}
	}
	if in.Runtime != nil {
		out.Runtime = new(RuntimeSpec)
		in.Runtime.DeepCopyInto(out.Runtime)
	}
	if in.Scaling != nil {
		out.Scaling = new(ScalingSpec)
		in.Scaling.DeepCopyInto(out.Scaling)
	}
}

func (in *ModelSpec) DeepCopyInto(out *ModelSpec) {
	*out = *in
	if in.Temperature != nil {
		v := *in.Temperature
		out.Temperature = &v
	}
	if in.MaxTokens != nil {
		v := *in.MaxTokens
		out.MaxTokens = &v
	}
	if in.TopP != nil {
		v := *in.TopP
		out.TopP = &v
	}
	if in.FrequencyPenalty != nil {
		v := *in.FrequencyPenalty
		out.FrequencyPenalty = &v
	}
	if in.PresencePenalty != nil {
		v := *in.PresencePenalty
		out.PresencePenalty = &v
	}
	if in.CredentialsSecretRef != nil {
		out.CredentialsSecretRef = new(SecretRef)
		*out.CredentialsSecretRef = *in.CredentialsSecretRef
	}
}

func (in *ToolSpec) DeepCopyInto(out *ToolSpec) {
	*out = *in
	if in.Endpoint != nil {
		out.Endpoint = new(EndpointSpec)
		in.Endpoint.DeepCopyInto(out.Endpoint)
	}
	if in.CredentialsSecretRef != nil {
		out.CredentialsSecretRef = new(SecretRef)
		*out.CredentialsSecretRef = *in.CredentialsSecretRef
	}
}

func (in *EndpointSpec) DeepCopyInto(out *EndpointSpec) {
	*out = *in
	if in.Headers != nil {
		out.Headers = make(map[string]string, len(in.Headers))
		for k, v := range in.Headers {
			out.Headers[k] = v
		}
	}
}

func (in *RuntimeSpec) DeepCopyInto(out *RuntimeSpec) {
	*out = *in
	if in.Env != nil {
		out.Env = make([]EnvVar, len(in.Env))
		copy(out.Env, in.Env)
	}
}

func (in *ScalingSpec) DeepCopyInto(out *ScalingSpec) {
	*out = *in
	if in.MinReplicas != nil {
		v := *in.MinReplicas
		out.MinReplicas = &v
	}
	if in.MaxReplicas != nil {
		v := *in.MaxReplicas
		out.MaxReplicas = &v
	}
	if in.TargetUtilization != nil {
		v := *in.TargetUtilization
		out.TargetUtilization = &v
	}
}

func (in *RetryPolicySpec) DeepCopyInto(out *RetryPolicySpec) {
	*out = *in
	if in.RetryOn != nil {
		out.RetryOn = make([]string, len(in.RetryOn))
		copy(out.RetryOn, in.RetryOn)
	}
}

func (in *AgentStatus) DeepCopyInto(out *AgentStatus) {
	*out = *in
	if in.StartTime != nil {
		out.StartTime = in.StartTime.DeepCopy()
	}
	if in.CompletionTime != nil {
		out.CompletionTime = in.CompletionTime.DeepCopy()
	}
	if in.LastActiveTime != nil {
		out.LastActiveTime = in.LastActiveTime.DeepCopy()
	}
	if in.Conditions != nil {
		out.Conditions = make([]metav1.Condition, len(in.Conditions))
		for i := range in.Conditions {
			in.Conditions[i].DeepCopyInto(&out.Conditions[i])
		}
	}
}

func (in *AgentList) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	out := new(AgentList)
	in.DeepCopyInto(out)
	return out
}

func (in *AgentList) DeepCopyInto(out *AgentList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]Agent, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

// ── DeepCopy: Workflow ───────────────────────────────────────────────

func (in *Workflow) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	out := new(Workflow)
	in.DeepCopyInto(out)
	return out
}

func (in *Workflow) DeepCopyInto(out *Workflow) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

func (in *WorkflowSpec) DeepCopyInto(out *WorkflowSpec) {
	*out = *in
	if in.Steps != nil {
		out.Steps = make([]WorkflowStep, len(in.Steps))
		for i := range in.Steps {
			in.Steps[i].DeepCopyInto(&out.Steps[i])
		}
	}
	if in.Parallelism != nil {
		v := *in.Parallelism
		out.Parallelism = &v
	}
	if in.Edges != nil {
		out.Edges = make([]Edge, len(in.Edges))
		copy(out.Edges, in.Edges)
	}
	if in.Trigger != nil {
		out.Trigger = new(TriggerSpec)
		in.Trigger.DeepCopyInto(out.Trigger)
	}
	if in.Concurrency != nil {
		out.Concurrency = new(ConcurrencySpec)
		*out.Concurrency = *in.Concurrency
	}
}

func (in *TriggerSpec) DeepCopyInto(out *TriggerSpec) {
	*out = *in
	if in.Schedule != nil {
		out.Schedule = new(ScheduleTrigger)
		*out.Schedule = *in.Schedule
	}
	if in.Webhook != nil {
		out.Webhook = new(WebhookTrigger)
		*out.Webhook = *in.Webhook
	}
}

func (in *WorkflowStep) DeepCopyInto(out *WorkflowStep) {
	*out = *in
	out.AgentRef = in.AgentRef
	if in.DependsOn != nil {
		out.DependsOn = make([]string, len(in.DependsOn))
		copy(out.DependsOn, in.DependsOn)
	}
	if in.InputFrom != nil {
		out.InputFrom = make([]InputRef, len(in.InputFrom))
		copy(out.InputFrom, in.InputFrom)
	}
	if in.RetryPolicy != nil {
		out.RetryPolicy = new(RetryPolicySpec)
		in.RetryPolicy.DeepCopyInto(out.RetryPolicy)
	}
	if in.StepTimeout != nil {
		out.StepTimeout = new(StepTimeoutSpec)
		*out.StepTimeout = *in.StepTimeout
	}
}

func (in *WorkflowStatus) DeepCopyInto(out *WorkflowStatus) {
	*out = *in
	if in.StartTime != nil {
		out.StartTime = in.StartTime.DeepCopy()
	}
	if in.CompletionTime != nil {
		out.CompletionTime = in.CompletionTime.DeepCopy()
	}
	if in.LastTriggerTime != nil {
		out.LastTriggerTime = in.LastTriggerTime.DeepCopy()
	}
	if in.NextRunTime != nil {
		out.NextRunTime = in.NextRunTime.DeepCopy()
	}
	if in.StepStatuses != nil {
		out.StepStatuses = make([]StepStatus, len(in.StepStatuses))
		for i := range in.StepStatuses {
			out.StepStatuses[i] = in.StepStatuses[i]
			if in.StepStatuses[i].StartTime != nil {
				out.StepStatuses[i].StartTime = in.StepStatuses[i].StartTime.DeepCopy()
			}
			if in.StepStatuses[i].CompletionTime != nil {
				out.StepStatuses[i].CompletionTime = in.StepStatuses[i].CompletionTime.DeepCopy()
			}
		}
	}
	if in.Conditions != nil {
		out.Conditions = make([]metav1.Condition, len(in.Conditions))
		for i := range in.Conditions {
			in.Conditions[i].DeepCopyInto(&out.Conditions[i])
		}
	}
}

func (in *WorkflowList) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	out := new(WorkflowList)
	in.DeepCopyInto(out)
	return out
}

func (in *WorkflowList) DeepCopyInto(out *WorkflowList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]Workflow, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}
