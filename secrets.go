package main

import (
	"context"

	"github.com/google/go-cmp/cmp"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
)

type SourceSecret struct {
	secret           v1.Secret
	targetNamespaces []string
}

type ReplicatedSecret struct {
	secret          v1.Secret
	sourceNamespace string
}

// function to list all namespaces and source secrets and replicate them to the relevant namespaces
// also scans and deletes any orphaned secrets.
// It is optimized by only querying once for the list of source secrets, replicated secrets, and namespaces to be replicated to
func processSecrets(clientSet *kubernetes.Clientset, allNamespaces *v1.NamespaceList) {
	// Get all secrets
	allSecrets := getAllSecrets(clientSet)
	sourceSecrets, replicatedSecrets := getSourceAndReplicatedSecrets(allSecrets, allNamespaces)
	log.Debugf("There are %d secrets with the relevant annotations in the cluster", len(sourceSecrets))

	// Replicating source secrets
	for _, sourceSecret := range sourceSecrets {
		// replicate to all relevant namespaces
		for _, replicateNamespace := range sourceSecret.targetNamespaces {
			replicateSecretToNamespace(clientSet, sourceSecret.secret, replicateNamespace, replicatedSecrets)
		}
		log.Debugf("Finished replicating all namespaces for secret %v", sourceSecret.secret.Name)
	}

	// Deleting orphaned secrets
	for _, replicatedSecret := range replicatedSecrets {
		// check if source secret still exists or regex still valid
		_, err := getSecretInSourceSecrets(replicatedSecret, sourceSecrets)
		if err != nil {
			if errors.IsNotFound(err) {
				deleteSecret(clientSet, replicatedSecret.secret)
			} else {
				panic(err.Error())
			}
		}
	}
}

// Get all secrets from all namespaces
func getAllSecrets(clientSet *kubernetes.Clientset) *v1.SecretList {
	allSecrets, err := clientSet.CoreV1().Secrets("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}
	return allSecrets
}

// Checks if given secret is a source secret by checking the annotations
func isSourceSecret(secret v1.Secret) bool {
	return metav1.HasAnnotation(secret.ObjectMeta, REPLICATE_REGEX) || metav1.HasAnnotation(secret.ObjectMeta, REPLICATE_ALL_NAMESPACES)
}

// Checks if given secret is a replicated secret by checking the annotations
func isReplicatedSecret(secret v1.Secret) bool {
	return metav1.HasAnnotation(secret.ObjectMeta, REPLICATED_ANNOTATION)
}

// fuction that takes in all secrets and returns a list of source Secrets and a list of replicatedSecrets
func getSourceAndReplicatedSecrets(allSecrets *v1.SecretList, allNamespaces *v1.NamespaceList) ([]SourceSecret, []ReplicatedSecret) {
	// initialize array for SourceSecrets and ReplicatedSecrets
	sourceSecrets := make([]SourceSecret, 0, 10)
	replicatedSecrets := make([]ReplicatedSecret, 0, 10)

	for _, secret := range allSecrets.Items {
		if isSourceSecret(secret) {
			// Filter for all source secrets
			targetNamespaces, err := getReplicateNamespaces(allNamespaces, secret.ObjectMeta)
			if err != nil {
				panic(err.Error())
			}
			sourceSecrets = append(sourceSecrets, SourceSecret{secret: secret, targetNamespaces: targetNamespaces})
		} else if isReplicatedSecret(secret) {
			// Filter for all replicated secrets
			replicatedSecrets = append(replicatedSecrets, ReplicatedSecret{secret: secret, sourceNamespace: secret.Annotations[REPLICATED_ANNOTATION]})
		}
	}

	return sourceSecrets, replicatedSecrets
}

// Get secret in array of SourceSecrets, error if not found or the replicatedSecret Namespace is no longer valid to be replicated into (i.e. regex changed in the source secret).
// Used to search for the source secret given a replicated one.
func getSecretInSourceSecrets(replicatedSecret ReplicatedSecret, sourceSecrets []SourceSecret) (v1.Secret, error) {
	for _, sourceSecret := range sourceSecrets {
		if sourceSecret.secret.Name == replicatedSecret.secret.Name &&
			replicatedSecret.sourceNamespace == sourceSecret.secret.Namespace &&
			arrayContains(sourceSecret.targetNamespaces, replicatedSecret.secret.Namespace) {
			return sourceSecret.secret, nil
		}
	}
	return v1.Secret{}, errors.NewNotFound(schema.GroupResource{}, "")
}

// Get secret in array of replicatedSecrets, error if not found.
// Used to search for the replicated secret given a sourceSecret.
func getSecretInReplicatedSecrets(secret v1.Secret, replicatedSecrets []ReplicatedSecret, namespace string) (v1.Secret, error) {
	for _, replicatedSecret := range replicatedSecrets {
		if replicatedSecret.secret.Name == secret.Name &&
			replicatedSecret.sourceNamespace == secret.Namespace &&
			replicatedSecret.secret.Namespace == namespace {
			return replicatedSecret.secret, nil
		}
	}
	return v1.Secret{}, errors.NewNotFound(schema.GroupResource{}, "")
}

// Replicate source secret to target namespace
// Creates the replicate secret if it does not exist, and update it if it exists and is not the same
func replicateSecretToNamespace(clientSet *kubernetes.Clientset, secret v1.Secret, namespace string, replicatedSecrets []ReplicatedSecret) {
	// do nothing if the target namespace is the same as the source secret namespace
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
			// Create secret if it does not exist
			log.Infof("Replicating [resource=secret][ns=%v][name=%v] to %v namespace...\n", secret.Namespace, secret.Name, namespace)
			_, err := clientSet.CoreV1().Secrets(namespace).Create(context.TODO(), copied_secret, metav1.CreateOptions{})
			if err != nil {
				panic(err.Error())
			}
		} else {
			panic(err.Error())
		}
	} else {
		// Check if secret value is the same if it exists
		// and updates the secret if it is changed
		if !checkSecretEquality(*copied_secret, existing_secret) {
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
func checkSecretEquality(originalSecret v1.Secret, replicatedSecret v1.Secret) bool {
	originalAnnotation := stripAllReplicatorAnnotations(originalSecret.Annotations)
	replicatedAnnotation := stripAllReplicatorAnnotations(replicatedSecret.Annotations)

	return cmp.Equal(originalSecret.Data, replicatedSecret.Data) &&
		cmp.Equal(originalAnnotation, replicatedAnnotation) &&
		cmp.Equal(originalSecret.Labels, replicatedSecret.Labels)
}

// deletes secret
func deleteSecret(clientSet *kubernetes.Clientset, secret v1.Secret) {
	log.Infof("Deleting secret %v in namespace %v...", secret.Name, secret.Namespace)
	err := clientSet.CoreV1().Secrets(secret.Namespace).Delete(context.TODO(), secret.Name, metav1.DeleteOptions{})
	if err != nil {
		panic(err.Error())
	}
}
