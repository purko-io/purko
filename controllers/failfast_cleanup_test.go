package controllers

import (
	"testing"

	v1alpha1 "github.com/purko-io/purko/api/v1alpha1"
)

// failFast must stop steps that are still pending/running but PRESERVE the
// failed step's job — deleting it destroys the logs the user needs for
// diagnosis (Stage 1 finding F26: logs panel went "No logs available"
// seconds after a failure).
func TestJobsToStopOnFailure(t *testing.T) {
	wf := &v1alpha1.Workflow{}
	wf.Status.StepStatuses = []v1alpha1.StepStatus{
		{Name: "done", Phase: "Succeeded", JobName: "job-done"},
		{Name: "boom", Phase: "Failed", JobName: "job-boom"},
		{Name: "inflight", Phase: "Running", JobName: "job-inflight"},
		{Name: "queued", Phase: "Pending", JobName: "job-queued"},
		{Name: "unstarted", Phase: "", JobName: ""},
	}

	got := jobsToStopOnFailure(wf)
	want := map[string]bool{"job-inflight": true, "job-queued": true}
	if len(got) != len(want) {
		t.Fatalf("jobs = %v, want exactly %v", got, want)
	}
	for _, name := range got {
		if !want[name] {
			t.Errorf("job %q must not be stopped (evidence preservation)", name)
		}
	}
}
