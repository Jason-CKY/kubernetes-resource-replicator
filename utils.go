package main

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func LookupEnvOrBool(key string, defaultValue bool) bool {
	envVariable, exists := os.LookupEnv(key)
	if !exists {
		return defaultValue
	}
	value, err := strconv.ParseBool(envVariable)
	if err != nil {
		return defaultValue
	}
	return value
}

func LookupEnvOrDuration(key string, defaultValue time.Duration) time.Duration {
	envVariable, exists := os.LookupEnv(key)
	if !exists {
		return defaultValue
	}
	value, err := time.ParseDuration(envVariable)
	if err != nil {
		return defaultValue
	}
	return value
}

func getAllNamespaces(clientSet *kubernetes.Clientset) *v1.NamespaceList {
	namespaces, err := clientSet.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}
	return namespaces
}

func getAllRegexNamespaces(namespaces *v1.NamespaceList, pattern string) []v1.Namespace {
	// match with regex
	matchedNamespaces := make([]v1.Namespace, 0, 10)
	for _, namespace := range namespaces.Items {
		matched, err := regexp.MatchString(pattern, namespace.Name)
		if err != nil {
			panic(err.Error())
		}
		if matched {
			// log.Debugf("pattern=%v matched namespace=%v", pattern, namespace.Name)
			matchedNamespaces = append(matchedNamespaces, namespace)
		}
	}
	return matchedNamespaces
}

func copyAnnotations(annotation map[string]string) map[string]string {
	// copy a map
	copiedAnnotation := make(map[string]string)
	for k, v := range annotation {
		copiedAnnotation[k] = v
	}
	return copiedAnnotation
}

// fuction to check given string is in array or not
func arrayContains[T comparable](s []T, e T) bool {
	for _, v := range s {
		if v == e {
			return true
		}
	}
	return false
}

// Evaluate the regex in annotation and return a list of all namespaces that configmap is needed to replicate to
func getReplicateNamespaces(allNamespaces *v1.NamespaceList, obj metav1.ObjectMeta) ([]string, error) {
	output := make([]string, 0, 10)
	if metav1.HasAnnotation(obj, REPLICATE_REGEX) {
		// evaluate the regex on the namespace
		// append the names of the matched namespaces to output
		patterns := strings.Split(obj.Annotations[REPLICATE_REGEX], ",")
		for _, pattern := range patterns {
			namespaces := getAllRegexNamespaces(allNamespaces, pattern)
			for _, namespace := range namespaces {
				output = append(output, namespace.Name)
			}
		}
	} else if metav1.HasAnnotation(obj, REPLICATE_ALL_NAMESPACES) {
		// set output to all namespaces
		for _, namespace := range allNamespaces.Items {
			output = append(output, namespace.Name)
		}
	} else {
		return output, fmt.Errorf("neither %v or %v annotation found in configmap [namespace=%v][name=%v]", REPLICATE_REGEX, REPLICATE_ALL_NAMESPACES, obj.Namespace, obj.Name)
	}

	return output[:], nil
}

// remove all replicator annotations for resource comparison
func stripAllReplicatorAnnotations(annotation map[string]string) map[string]string {
	copied_annotation := copyAnnotations(annotation)
	delete(copied_annotation, REPLICATE_REGEX)
	delete(copied_annotation, REPLICATED_ANNOTATION)
	delete(copied_annotation, REPLICATE_ALL_NAMESPACES)
	delete(copied_annotation, LAST_APPLIED_CONFIGURATION)
	return copied_annotation
}
