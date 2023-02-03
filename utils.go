package main

import (
	"context"
	"os"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"
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

func getAllRegexNamespaces(clientSet *kubernetes.Clientset, pattern string) *v1.NamespaceList {
	// TODO: match with regex
	namespaces, err := clientSet.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}
	for index, namespace := range namespaces.Items {
		log.Infof("%v at index %v", namespace.Name, index)
	}
	return namespaces
}

func copyAnnotations(annotation map[string]string) map[string]string {
	// copy a map
	copiedAnnotation := make(map[string]string)
	for k, v := range annotation {
		copiedAnnotation[k] = v
	}
	return copiedAnnotation
}
