package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-cmp/cmp"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
)

//TODO: fuction that takes in all secrets and returns a list of source Secrets and a list of replicatedSecrets

// Evaluate the regex in annotation and return a list of all namespaces that secret is needed to replicate to
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
		return output, fmt.Errorf("neither %v or %v annotation found in secret [namespace=%v][name=%v]", REPLICATE_REGEX, REPLICATE_ALL_NAMESPACES, obj.Namespace, obj.Name)
	}

	return output[:], nil
}

// Get secret in array of SourceSecrets, error if not found
func getSecretInSourceSecrets(replicatedSecret ReplicatedSecret, sourceSecrets []SourceSecret) (*v1.Secret, error) {
	for _, sourceSecret := range sourceSecrets {
		if sourceSecret.secret.Name == replicatedSecret.secret.Name &&
			replicatedSecret.sourceNamespace == sourceSecret.secret.Namespace &&
			arrayContains(sourceSecret.targetNamespaces, replicatedSecret.secret.Namespace) {
			return &sourceSecret.secret, nil
		}
	}
	return &v1.Secret{}, errors.NewNotFound(schema.GroupResource{}, "")
}

func getSecretInReplicatedSecrets(secret v1.Secret, replicatedSecrets []ReplicatedSecret, namespace string) (*v1.Secret, error) {
	for _, replicatedSecret := range replicatedSecrets {
		if replicatedSecret.secret.Name == secret.Name &&
			replicatedSecret.sourceNamespace == secret.Namespace &&
			replicatedSecret.secret.Namespace == namespace {
			return &replicatedSecret.secret, nil
		}
	}
	return &v1.Secret{}, errors.NewNotFound(schema.GroupResource{}, "")
}

func replicateSecretToNamespace(clientSet *kubernetes.Clientset, secret v1.Secret, namespace string, replicatedSecrets []ReplicatedSecret) {

	if namespace == secret.Namespace {
		return
	}
	// Remove annotation
	copied_secret := secret.DeepCopy()
	delete(copied_secret.Annotations, REPLICATE_REGEX)
	delete(copied_secret.Annotations, REPLICATE_ALL_NAMESPACES)
	// add replicated-from annotation
	copied_secret.Annotations[REPLICATED_ANNOTATION] = secret.Namespace
	copied_secret.Namespace = namespace
	copied_secret.ResourceVersion = ""

	existing_secret, err := getSecretInReplicatedSecrets(secret, replicatedSecrets, namespace)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Infof("Replicating [resource=secret][ns=%v][name=%v] to %v namespace...\n", secret.Namespace, secret.Name, namespace)
			_, err := clientSet.CoreV1().Secrets(namespace).Create(context.TODO(), copied_secret, metav1.CreateOptions{})
			if err != nil {
				panic(err.Error())
			}
		} else {
			log.Info(errors.IsNotFound(err))
			panic(err.Error())
		}
	} else {
		if !checkSecretEquality(copied_secret, existing_secret) {
			// updates secret
			updated_secret := existing_secret.DeepCopy()
			updated_secret.Annotations = copied_secret.Annotations
			updated_secret.Data = copied_secret.Data
			updated_secret.Labels = copied_secret.Labels
			log.Infof("Updating [resource=secret][ns=%v][name=%v] to %v namespace...\n", secret.Namespace, secret.Name, namespace)
			_, err := clientSet.CoreV1().Secrets(namespace).Update(context.TODO(), updated_secret, metav1.UpdateOptions{})
			if err != nil {
				panic(err.Error())
			}
		}
	}
}

// checks 2 secrets if they are the same
// this function checks the values, labels, and annotations
func checkSecretEquality(originalSecret *v1.Secret, replicatedSecret *v1.Secret) bool {
	originalAnnotation := stripAllReplicatorAnnotations(originalSecret.Annotations)
	replicatedAnnotation := stripAllReplicatorAnnotations(replicatedSecret.Annotations)

	return cmp.Equal(originalSecret.Data, replicatedSecret.Data) &&
		cmp.Equal(originalAnnotation, replicatedAnnotation) &&
		cmp.Equal(originalSecret.Labels, replicatedSecret.Labels)
}

func stripAllReplicatorAnnotations(annotation map[string]string) map[string]string {
	copied_annotation := copyAnnotations(annotation)
	delete(copied_annotation, REPLICATE_REGEX)
	delete(copied_annotation, REPLICATED_ANNOTATION)
	delete(copied_annotation, REPLICATE_ALL_NAMESPACES)
	delete(copied_annotation, LAST_APPLIED_CONFIGURATION)
	return copied_annotation
}

func deleteSecret(clientSet *kubernetes.Clientset, secret v1.Secret) {
	log.Infof("Deleting secret %v in namespace %v...", secret.Name, secret.Namespace)
	err := clientSet.CoreV1().Secrets(secret.Namespace).Delete(context.TODO(), secret.Name, metav1.DeleteOptions{})
	if err != nil {
		panic(err.Error())
	}
}
