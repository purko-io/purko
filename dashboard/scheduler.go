package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	v1alpha1 "github.com/purko-io/purko/api/v1alpha1"
)

// Scheduler manages cron-based workflow triggers.
type Scheduler struct {
	Client    client.Client
	Namespace string
	cron      *cron.Cron
	entries   map[string]cron.EntryID // workflow name -> cron entry ID
	mu        sync.Mutex
}

// ScheduleInfo tracks scheduled workflows for the API.
type ScheduleInfo struct {
	WorkflowName string `json:"workflowName"`
	Cron         string `json:"cron"`
	NextRun      string `json:"nextRun"`
	LastRun      string `json:"lastRun"`
	Suspended    bool   `json:"suspended"`
	RunCount     int    `json:"runCount"`
}

// Start begins the scheduler and scans for workflows with triggers.
func (s *Scheduler) Start(ctx context.Context) {
	logger := ctrllog.FromContext(ctx)
	s.cron = cron.New()
	s.entries = make(map[string]cron.EntryID)
	s.cron.Start()
	logger.Info("Scheduler started")

	// Scan every 30 seconds for new/changed workflow triggers
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				s.cron.Stop()
				return
			case <-ticker.C:
				s.sync(ctx)
			}
		}
	}()

	// Initial sync
	s.sync(ctx)
}

// sync checks all workflows for schedule triggers and updates cron entries.
func (s *Scheduler) sync(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()

	wfList := &v1alpha1.WorkflowList{}
	if err := s.Client.List(ctx, wfList, client.InNamespace(s.Namespace)); err != nil {
		return
	}

	// Track which workflows still have schedules
	active := map[string]bool{}

	for _, wf := range wfList.Items {
		// Skip runs (only process templates)
		if wf.Annotations != nil && wf.Annotations["purko.io/workflow-template"] != "" {
			continue
		}

		if wf.Spec.Trigger == nil || wf.Spec.Trigger.Schedule == nil {
			continue
		}

		sched := wf.Spec.Trigger.Schedule
		if sched.Suspend {
			// Remove if suspended
			if id, ok := s.entries[wf.Name]; ok {
				s.cron.Remove(id)
				delete(s.entries, wf.Name)
			}
			continue
		}

		active[wf.Name] = true

		// Skip if already scheduled with same cron
		if _, ok := s.entries[wf.Name]; ok {
			continue
		}

		// Add new cron entry
		wfName := wf.Name
		id, err := s.cron.AddFunc(sched.Cron, func() {
			s.triggerRun(context.Background(), wfName)
		})
		if err != nil {
			continue
		}
		s.entries[wfName] = id
	}

	// Remove entries for workflows that no longer exist or lost their schedule
	for name, id := range s.entries {
		if !active[name] {
			s.cron.Remove(id)
			delete(s.entries, name)
		}
	}
}

// triggerRun creates a new workflow run from a scheduled template.
func (s *Scheduler) triggerRun(ctx context.Context, wfName string) {
	logger := ctrllog.FromContext(ctx)

	wf := &v1alpha1.Workflow{}
	if err := s.Client.Get(ctx, client.ObjectKey{Name: wfName, Namespace: s.Namespace}, wf); err != nil {
		logger.Error(err, "Failed to get scheduled workflow", "workflow", wfName)
		return
	}

	runName := fmt.Sprintf("%s-sched-%08x", wfName, time.Now().UnixNano()&0xFFFFFFFF)

	run := &v1alpha1.Workflow{}
	run.APIVersion = "purko.io/v1alpha1"
	run.Kind = "Workflow"
	run.Name = runName
	run.Namespace = s.Namespace
	run.Annotations = map[string]string{
		"purko.io/workflow-template": wfName,
		"purko.io/trigger-type":      "schedule",
		"purko.io/trigger-source":    wf.Spec.Trigger.Schedule.Cron,
	}
	run.Spec = wf.Spec
	run.Spec.Trigger = nil // Don't copy trigger to run

	// Inject schedule context as input
	inputJSON, _ := json.Marshal(map[string]string{
		"task":      wf.Spec.Description,
		"trigger":   "schedule",
		"schedule":  wf.Spec.Trigger.Schedule.Cron,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
	for i := range run.Spec.Steps {
		run.Spec.Steps[i].Input = &runtime.RawExtension{Raw: inputJSON}
	}

	if err := s.Client.Create(ctx, run); err != nil {
		logger.Error(err, "Failed to create scheduled run", "workflow", wfName)
		return
	}

	// Update template status
	now := metav1.Now()
	wf.Status.LastTriggerTime = &now
	s.Client.Status().Update(ctx, wf)

	logger.Info("Scheduled workflow triggered", "workflow", wfName, "run", runName)
}

// GetSchedules returns info about all scheduled workflows.
func (s *Scheduler) GetSchedules() []ScheduleInfo {
	s.mu.Lock()
	defer s.mu.Unlock()

	var schedules []ScheduleInfo
	for name, id := range s.entries {
		entry := s.cron.Entry(id)
		schedules = append(schedules, ScheduleInfo{
			WorkflowName: name,
			NextRun:      entry.Next.Format(time.RFC3339),
			LastRun:      entry.Prev.Format(time.RFC3339),
		})
	}
	return schedules
}
