package main

import (
	"context"
	"os"
	"regexp"
	"strconv"
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
