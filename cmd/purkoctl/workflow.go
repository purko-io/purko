package main

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	v1alpha1 "github.com/purko-io/purko/api/v1alpha1"
)

var workflowCmd = &cobra.Command{
	Use:   "workflow",
	Short: "Manage workflows",
}

var workflowListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all workflows in the namespace",
	RunE:  runWorkflowList,
}

var workflowGetCmd = &cobra.Command{
	Use:   "get <name>",
	Short: "Show detailed workflow info including step DAG",
	Args:  cobra.ExactArgs(1),
	RunE:  runWorkflowGet,
}

var workflowTriggerCmd = &cobra.Command{
	Use:   "trigger <name>",
	Short: "Trigger a workflow execution",
	Args:  cobra.ExactArgs(1),
	RunE:  runWorkflowTrigger,
}

var triggerParams []string

var workflowLogsCmd = &cobra.Command{
	Use:   "logs <name> [step]",
	Short: "Show logs from a workflow step's Job pod",
	Args:  cobra.RangeArgs(1, 2),
	RunE:  runWorkflowLogs,
}

var followLogs bool

var workflowApproveCmd = &cobra.Command{
	Use:   "approve <name> <step>",
	Short: "Approve a step that's pending human approval",
	Args:  cobra.ExactArgs(2),
	RunE:  runWorkflowApprove,
}

var workflowDenyCmd = &cobra.Command{
	Use:   "deny <name> <step>",
	Short: "Deny a step that's pending human approval",
	Args:  cobra.ExactArgs(2),
	RunE:  runWorkflowDeny,
}

var workflowCancelCmd = &cobra.Command{
	Use:   "cancel <name>",
	Short: "Cancel a running workflow",
	Args:  cobra.ExactArgs(1),
	RunE:  runWorkflowCancel,
}

var workflowRerunCmd = &cobra.Command{
	Use:   "rerun <name>",
	Short: "Re-run a completed or failed workflow",
	Args:  cobra.ExactArgs(1),
	RunE:  runWorkflowRerun,
}

var workflowCreateFile string

var workflowCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a workflow from a YAML file",
	RunE:  runWorkflowCreate,
}

var workflowDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a workflow",
	Args:  cobra.ExactArgs(1),
	RunE:  runWorkflowDelete,
}

func init() {
	workflowCmd.AddCommand(workflowListCmd)
	workflowCmd.AddCommand(workflowGetCmd)
	workflowTriggerCmd.Flags().StringArrayVarP(&triggerParams, "param", "p", nil, "Parameter key=value (repeatable)")
	workflowCmd.AddCommand(workflowTriggerCmd)
	workflowLogsCmd.Flags().BoolVarP(&followLogs, "follow", "f", false, "Stream logs live")
	workflowCmd.AddCommand(workflowLogsCmd)
	workflowCmd.AddCommand(workflowApproveCmd)
	workflowCmd.AddCommand(workflowDenyCmd)
	workflowCmd.AddCommand(workflowCancelCmd)
	workflowCmd.AddCommand(workflowRerunCmd)
	workflowCreateCmd.Flags().StringVarP(&workflowCreateFile, "file", "f", "", "Path to workflow YAML file (required)")
	workflowCreateCmd.MarkFlagRequired("file")
	workflowCmd.AddCommand(workflowCreateCmd)
	workflowCmd.AddCommand(workflowDeleteCmd)
}

func runWorkflowCreate(cmd *cobra.Command, args []string) error {
	data, err := os.ReadFile(workflowCreateFile)
	if err != nil {
		return fmt.Errorf("unable to read file %q: %w", workflowCreateFile, err)
	}

	var wf v1alpha1.Workflow
	if err := yaml.Unmarshal(data, &wf); err != nil {
		return fmt.Errorf("unable to parse workflow YAML: %w", err)
	}

	if wf.Namespace == "" {
		wf.Namespace = namespace
	}

	if err := k8sClient.Create(context.TODO(), &wf); err != nil {
		return fmt.Errorf("unable to create workflow %q: %w", wf.Name, err)
	}

	fmt.Printf("Workflow %s created in namespace %s\n", wf.Name, wf.Namespace)
	return nil
}

func runWorkflowDelete(cmd *cobra.Command, args []string) error {
	name := args[0]
	var wf v1alpha1.Workflow
	if err := k8sClient.Get(context.TODO(), client.ObjectKey{Namespace: namespace, Name: name}, &wf); err != nil {
		return fmt.Errorf("workflow %q not found in namespace %q", name, namespace)
	}

	if err := k8sClient.Delete(context.TODO(), &wf); err != nil {
		return fmt.Errorf("unable to delete workflow %q: %w", name, err)
	}

	fmt.Printf("Workflow %s deleted from namespace %s\n", name, namespace)
	return nil
}

func runWorkflowList(cmd *cobra.Command, args []string) error {
	var workflows v1alpha1.WorkflowList
	if err := k8sClient.List(context.TODO(), &workflows, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("workflows: unable to list in namespace %q: %w", namespace, err)
	}

	sort.Slice(workflows.Items, func(i, j int) bool {
		return workflows.Items[j].CreationTimestamp.Before(&workflows.Items[i].CreationTimestamp)
	})

	if outputFmt == "json" {
		return printJSON(workflows.Items)
	}

	headers := []string{"NAME", "PHASE", "STEPS", "COMPLETED", "FAILED", "DURATION", "AGE"}
	rows := make([][]string, 0, len(workflows.Items))
	for _, wf := range workflows.Items {
		rows = append(rows, []string{
			wf.Name,
			wf.Status.Phase,
			fmt.Sprintf("%d", wf.Status.TotalSteps),
			fmt.Sprintf("%d", wf.Status.CompletedSteps),
			fmt.Sprintf("%d", wf.Status.FailedSteps),
			formatDuration(wf.Status.StartTime, wf.Status.CompletionTime),
			formatAge(wf.CreationTimestamp),
		})
	}
	printTable(headers, rows)
	return nil
}

func runWorkflowGet(cmd *cobra.Command, args []string) error {
	name := args[0]
	var wf v1alpha1.Workflow
	if err := k8sClient.Get(context.TODO(), client.ObjectKey{Namespace: namespace, Name: name}, &wf); err != nil {
		return fmt.Errorf("workflow %q not found in namespace %q", name, namespace)
	}

	if outputFmt == "json" {
		return printJSON(wf)
	}

	fmt.Printf("Name:         %s\n", wf.Name)
	fmt.Printf("Namespace:    %s\n", wf.Namespace)
	fmt.Printf("Phase:        %s\n", wf.Status.Phase)
	fmt.Printf("Duration:     %s\n", formatDuration(wf.Status.StartTime, wf.Status.CompletionTime))

	if len(wf.Spec.Parameters) > 0 {
		fmt.Println("Parameters:")
		for k, v := range wf.Spec.Parameters {
			fmt.Printf("  %s:   %s\n", k, v)
		}
	}

	fmt.Println()
	fmt.Println("Steps:")

	statusMap := make(map[string]*v1alpha1.StepStatus)
	for i := range wf.Status.StepStatuses {
		statusMap[wf.Status.StepStatuses[i].Name] = &wf.Status.StepStatuses[i]
	}

	stepHeaders := []string{"  NAME", "AGENT", "PHASE", "DURATION", "RETRIES", "JOB"}
	stepRows := make([][]string, 0, len(wf.Spec.Steps))
	for _, step := range wf.Spec.Steps {
		phase := "Pending"
		duration := "-"
		retries := "0"
		jobName := "-"

		if ss, ok := statusMap[step.Name]; ok {
			phase = ss.Phase
			duration = formatDuration(ss.StartTime, ss.CompletionTime)
			retries = fmt.Sprintf("%d", ss.RetryCount)
			if ss.JobName != "" {
				jobName = ss.JobName
			}
		}

		stepRows = append(stepRows, []string{
			"  " + step.Name,
			step.AgentRef.Name,
			phase,
			duration,
			retries,
			jobName,
		})
	}
	printTable(stepHeaders, stepRows)

	if len(wf.Status.Conditions) > 0 {
		fmt.Println()
		fmt.Println("Conditions:")
		condHeaders := []string{"  TYPE", "STATUS", "REASON", "MESSAGE"}
		condRows := make([][]string, 0, len(wf.Status.Conditions))
		for _, c := range wf.Status.Conditions {
			condRows = append(condRows, []string{
				"  " + c.Type,
				string(c.Status),
				c.Reason,
				c.Message,
			})
		}
		printTable(condHeaders, condRows)
	}

	return nil
}

func runWorkflowTrigger(cmd *cobra.Command, args []string) error {
	name := args[0]
	var wf v1alpha1.Workflow
	if err := k8sClient.Get(context.TODO(), client.ObjectKey{Namespace: namespace, Name: name}, &wf); err != nil {
		return fmt.Errorf("workflow %q not found in namespace %q", name, namespace)
	}

	params := make(map[string]string)
	for _, p := range triggerParams {
		parts := strings.SplitN(p, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid parameter format %q, expected key=value", p)
		}
		params[parts[0]] = parts[1]
	}

	if wf.Spec.Trigger != nil {
		return triggerTemplate(wf, params)
	}
	return triggerRerun(wf)
}

func triggerTemplate(tmpl v1alpha1.Workflow, params map[string]string) error {
	newName := fmt.Sprintf("%s-%s", tmpl.Name, randomString(5))
	if len(newName) > 253 {
		newName = newName[:253]
	}

	var specCopy v1alpha1.WorkflowSpec
	tmpl.Spec.DeepCopyInto(&specCopy)

	newWf := v1alpha1.Workflow{
		ObjectMeta: metav1.ObjectMeta{
			Name:      newName,
			Namespace: namespace,
			Labels:    tmpl.Labels,
		},
		Spec: specCopy,
	}
	newWf.Spec.Trigger = nil

	if newWf.Spec.Parameters == nil {
		newWf.Spec.Parameters = make(map[string]string)
	}
	for k, v := range params {
		newWf.Spec.Parameters[k] = v
	}

	if err := k8sClient.Create(context.TODO(), &newWf); err != nil {
		return fmt.Errorf("failed to create workflow instance: %w", err)
	}

	fmt.Printf("Workflow %s triggered in namespace %s\n", newName, namespace)
	return nil
}

func triggerRerun(wf v1alpha1.Workflow) error {
	wf.Status.Phase = ""
	wf.Status.StepStatuses = nil
	wf.Status.StartTime = nil
	wf.Status.CompletionTime = nil
	wf.Status.CompletedSteps = 0
	wf.Status.FailedSteps = 0
	wf.Status.Message = ""

	if err := k8sClient.Status().Update(context.TODO(), &wf); err != nil {
		return fmt.Errorf("failed to reset workflow status: %w", err)
	}

	if wf.Annotations == nil {
		wf.Annotations = make(map[string]string)
	}
	delete(wf.Annotations, "purko.io/cancel")
	for k := range wf.Annotations {
		if strings.HasPrefix(k, "purko.io/approve-") || strings.HasPrefix(k, "purko.io/deny-") {
			delete(wf.Annotations, k)
		}
	}
	wf.Annotations["purko.io/rerun"] = time.Now().UTC().Format(time.RFC3339)

	if err := k8sClient.Update(context.TODO(), &wf); err != nil {
		return fmt.Errorf("failed to set rerun annotation: %w", err)
	}

	fmt.Printf("Workflow %s triggered in namespace %s\n", wf.Name, namespace)
	return nil
}

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func runWorkflowLogs(cmd *cobra.Command, args []string) error {
	name := args[0]
	var wf v1alpha1.Workflow
	if err := k8sClient.Get(context.TODO(), client.ObjectKey{Namespace: namespace, Name: name}, &wf); err != nil {
		return fmt.Errorf("workflow %q not found in namespace %q", name, namespace)
	}

	var targetStep *v1alpha1.StepStatus
	if len(args) == 2 {
		stepName := args[1]
		for i := range wf.Status.StepStatuses {
			if wf.Status.StepStatuses[i].Name == stepName {
				targetStep = &wf.Status.StepStatuses[i]
				break
			}
		}
		if targetStep == nil {
			return fmt.Errorf("step %q not found in workflow %q", stepName, name)
		}
	} else {
		targetStep = findActiveStep(wf.Status.StepStatuses)
		if targetStep == nil {
			return fmt.Errorf("no active or completed steps found in workflow %q", name)
		}
	}

	if targetStep.JobName == "" {
		return fmt.Errorf("step %q has no job assigned yet", targetStep.Name)
	}

	podList, err := k8sClientset.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", targetStep.JobName),
	})
	if err != nil {
		return fmt.Errorf("unable to list pods for job %q: %w", targetStep.JobName, err)
	}
	if len(podList.Items) == 0 {
		fmt.Println("Logs unavailable — pod has been garbage collected")
		return nil
	}

	pod := podList.Items[0]
	logOpts := &corev1.PodLogOptions{Follow: followLogs}
	stream, err := k8sClientset.CoreV1().Pods(namespace).GetLogs(pod.Name, logOpts).Stream(context.TODO())
	if err != nil {
		return fmt.Errorf("unable to read logs from pod %q: %w", pod.Name, err)
	}
	defer stream.Close()

	_, err = io.Copy(os.Stdout, stream)
	return err
}

func findActiveStep(statuses []v1alpha1.StepStatus) *v1alpha1.StepStatus {
	for i := range statuses {
		if statuses[i].Phase == "Running" {
			return &statuses[i]
		}
	}
	var last *v1alpha1.StepStatus
	for i := range statuses {
		if statuses[i].Phase == "Succeeded" || statuses[i].Phase == "CompletedWithErrors" || statuses[i].Phase == "Failed" {
			last = &statuses[i]
		}
	}
	return last
}

func runWorkflowApprove(cmd *cobra.Command, args []string) error {
	return setStepAnnotation(args[0], args[1], "approve")
}

func runWorkflowDeny(cmd *cobra.Command, args []string) error {
	return setStepAnnotation(args[0], args[1], "deny")
}

func setStepAnnotation(wfName, stepName, action string) error {
	var wf v1alpha1.Workflow
	if err := k8sClient.Get(context.TODO(), client.ObjectKey{Namespace: namespace, Name: wfName}, &wf); err != nil {
		return fmt.Errorf("workflow %q not found in namespace %q", wfName, namespace)
	}

	stepFound := false
	for _, step := range wf.Spec.Steps {
		if step.Name == stepName {
			stepFound = true
			break
		}
	}
	if !stepFound {
		return fmt.Errorf("step %q not found in workflow %q", stepName, wfName)
	}

	var stepStatus *v1alpha1.StepStatus
	for i := range wf.Status.StepStatuses {
		if wf.Status.StepStatuses[i].Name == stepName {
			stepStatus = &wf.Status.StepStatuses[i]
			break
		}
	}
	if stepStatus == nil {
		return fmt.Errorf("step %q has not been reached yet", stepName)
	}
	if stepStatus.Phase != "Pending" {
		return fmt.Errorf("step %q is not pending approval (phase: %s)", stepName, stepStatus.Phase)
	}

	if wf.Annotations == nil {
		wf.Annotations = make(map[string]string)
	}
	annotationKey := fmt.Sprintf("purko.io/%s-%s", action, stepName)
	wf.Annotations[annotationKey] = "true"

	if err := k8sClient.Update(context.TODO(), &wf); err != nil {
		return fmt.Errorf("failed to set %s annotation: %w", action, err)
	}

	verb := "approved"
	if action == "deny" {
		verb = "denied"
	}
	fmt.Printf("Step %s %s in workflow %s\n", stepName, verb, wfName)
	return nil
}

func runWorkflowCancel(cmd *cobra.Command, args []string) error {
	name := args[0]
	var wf v1alpha1.Workflow
	if err := k8sClient.Get(context.TODO(), client.ObjectKey{Namespace: namespace, Name: name}, &wf); err != nil {
		return fmt.Errorf("workflow %q not found in namespace %q", name, namespace)
	}

	if wf.Status.Phase != "Running" && wf.Status.Phase != "Pending" {
		return fmt.Errorf("workflow %q is not running (phase: %s)", name, wf.Status.Phase)
	}

	if wf.Annotations == nil {
		wf.Annotations = make(map[string]string)
	}
	wf.Annotations["purko.io/cancel"] = "true"

	if err := k8sClient.Update(context.TODO(), &wf); err != nil {
		return fmt.Errorf("failed to cancel workflow: %w", err)
	}

	fmt.Printf("Workflow %s cancelled\n", name)
	return nil
}

func runWorkflowRerun(cmd *cobra.Command, args []string) error {
	name := args[0]
	var wf v1alpha1.Workflow
	if err := k8sClient.Get(context.TODO(), client.ObjectKey{Namespace: namespace, Name: name}, &wf); err != nil {
		return fmt.Errorf("workflow %q not found in namespace %q", name, namespace)
	}

	switch wf.Status.Phase {
	case "Succeeded", "CompletedWithErrors", "Failed", "Cancelled":
		// OK to rerun
	case "Running", "Pending":
		return fmt.Errorf("workflow %q is still running. Use 'purkoctl workflow cancel' first", name)
	default:
		return fmt.Errorf("workflow %q is in unexpected phase %q", name, wf.Status.Phase)
	}

	wf.Status.Phase = ""
	wf.Status.StepStatuses = nil
	wf.Status.StartTime = nil
	wf.Status.CompletionTime = nil
	wf.Status.CompletedSteps = 0
	wf.Status.FailedSteps = 0
	wf.Status.Message = ""

	if err := k8sClient.Status().Update(context.TODO(), &wf); err != nil {
		return fmt.Errorf("failed to reset workflow status: %w", err)
	}

	if wf.Annotations == nil {
		wf.Annotations = make(map[string]string)
	}
	delete(wf.Annotations, "purko.io/cancel")
	for k := range wf.Annotations {
		if strings.HasPrefix(k, "purko.io/approve-") || strings.HasPrefix(k, "purko.io/deny-") {
			delete(wf.Annotations, k)
		}
	}
	wf.Annotations["purko.io/rerun"] = time.Now().UTC().Format(time.RFC3339)

	if err := k8sClient.Update(context.TODO(), &wf); err != nil {
		return fmt.Errorf("failed to set rerun annotation: %w", err)
	}

	fmt.Printf("Workflow %s restarted\n", name)
	return nil
}
