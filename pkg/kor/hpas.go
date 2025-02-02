package kor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/utils/strings/slices"

	"github.com/yonahd/kor/pkg/filters"
)

func getDeploymentNames(clientset kubernetes.Interface, namespace string) ([]string, error) {
	deployments, err := clientset.AppsV1().Deployments(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(deployments.Items))
	for _, deployment := range deployments.Items {
		names = append(names, deployment.Name)
	}
	return names, nil
}

func getStatefulSetNames(clientset kubernetes.Interface, namespace string) ([]string, error) {
	statefulSets, err := clientset.AppsV1().StatefulSets(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(statefulSets.Items))
	for _, statefulSet := range statefulSets.Items {
		names = append(names, statefulSet.Name)
	}
	return names, nil
}

func extractUnusedHpas(clientset kubernetes.Interface, namespace string, filterOpts *filters.Options) ([]string, error) {
	deploymentNames, err := getDeploymentNames(clientset, namespace)
	if err != nil {
		return nil, err
	}
	statefulsetNames, err := getStatefulSetNames(clientset, namespace)
	if err != nil {
		return nil, err
	}
	hpas, err := clientset.AutoscalingV2().HorizontalPodAutoscalers(namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: filterOpts.IncludeLabels})
	if err != nil {
		return nil, err
	}

	var diff []string
	for _, hpa := range hpas.Items {
		if pass, _ := filter.Run(filterOpts); pass {
			continue
		}

		switch hpa.Spec.ScaleTargetRef.Kind {
		case "Deployment":
			if !slices.Contains(deploymentNames, hpa.Spec.ScaleTargetRef.Name) {
				diff = append(diff, hpa.Name)
			}
		case "StatefulSet":
			if !slices.Contains(statefulsetNames, hpa.Spec.ScaleTargetRef.Name) {
				diff = append(diff, hpa.Name)
			}
		}
	}
	return diff, nil
}

func processNamespaceHpas(clientset kubernetes.Interface, namespace string, filterOpts *filters.Options) ([]string, error) {
	unusedHpas, err := extractUnusedHpas(clientset, namespace, filterOpts)
	if err != nil {
		return nil, err
	}
	return unusedHpas, nil
}

func GetUnusedHpas(filterOpts *filters.Options, clientset kubernetes.Interface, outputFormat string, opts Opts) (string, error) {
	var outputBuffer bytes.Buffer
	namespaces := filterOpts.Namespaces(clientset)
	response := make(map[string]map[string][]string)

	for _, namespace := range namespaces {
		diff, err := processNamespaceHpas(clientset, namespace, filterOpts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to process namespace %s: %v\n", namespace, err)
			continue
		}

		if opts.DeleteFlag {
			if diff, err = DeleteResource(diff, clientset, namespace, "HPA", opts.NoInteractive); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to delete HPA %s in namespace %s: %v\n", diff, namespace, err)
			}
		}
		output := FormatOutput(namespace, diff, "HPAs", opts)
		if output != "" {
			outputBuffer.WriteString(output)
			outputBuffer.WriteString("\n")

			resourceMap := make(map[string][]string)
			resourceMap["Hpa"] = diff
			response[namespace] = resourceMap
		}
	}

	jsonResponse, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return "", err
	}

	unusedHpas, err := unusedResourceFormatter(outputFormat, outputBuffer, opts, jsonResponse)
	if err != nil {
		fmt.Printf("err: %v\n", err)
	}

	return unusedHpas, nil
}
