package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show purkoctl and operator version",
	RunE:  runVersion,
}

func runVersion(cmd *cobra.Command, args []string) error {
	fmt.Printf("Client:    %s\n", version)

	if err := setupK8sClient(); err != nil {
		fmt.Println("Operator:  not found (unable to connect to cluster)")
		return nil
	}

	deps, err := k8sClientset.AppsV1().Deployments("purko-system").List(
		context.TODO(),
		metav1.ListOptions{},
	)
	if err != nil {
		fmt.Println("Operator:  not found")
		return nil
	}

	for _, dep := range deps.Items {
		if !strings.Contains(dep.Name, "purko") && !strings.Contains(dep.Name, "operator") {
			continue
		}
		operatorVersion, podName := extractOperatorVersion(dep)
		fmt.Printf("Operator:  %s (%s/%s)\n", operatorVersion, dep.Namespace, podName)
		return nil
	}

	fmt.Println("Operator:  not found")
	return nil
}

func extractOperatorVersion(dep appsv1.Deployment) (string, string) {
	podName := dep.Name
	operatorVersion := "unknown"

	for _, c := range dep.Spec.Template.Spec.Containers {
		image := c.Image
		if parts := strings.SplitN(image, ":", 2); len(parts) == 2 {
			operatorVersion = parts[1]
		}
		break
	}

	return operatorVersion, podName
}
