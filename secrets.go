package main

import (
	"context"
	"fmt"

	"github.com/google/go-cmp/cmp"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// list all secrets and filter by annotation
// return all secrets with the REPLICATE_REGEX or REPLICATE_ALL_NAMESPACES annotation
func getAllSourceSecrets(clientSet *kubernetes.Clientset) *v1.SecretList {
	allSecrets, err := clientSet.CoreV1().Secrets("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}
	secrets := v1.SecretList{}

	for i := 0; i < len(allSecrets.Items); i++ {
		secret := allSecrets.Items[i]
		if metav1.HasAnnotation(secret.ObjectMeta, REPLICATE_REGEX) || metav1.HasAnnotation(secret.ObjectMeta, REPLICATE_ALL_NAMESPACES) {
			secrets.Items = append(secrets.Items, secret)
		}
	}
	return &secrets
}

// list all secrets and filter by annotation
// return all secrets with the REPLICATED_ANNOTATION annotation
func getAllReplicatedSecrets(clientSet *kubernetes.Clientset) *v1.SecretList {
	allSecrets, err := clientSet.CoreV1().Secrets("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}
	secrets := v1.SecretList{}

	for i := 0; i < len(allSecrets.Items); i++ {
		secret := allSecrets.Items[i]
		if metav1.HasAnnotation(secret.ObjectMeta, REPLICATED_ANNOTATION) {
			secrets.Items = append(secrets.Items, secret)
		}
	}
	return &secrets
}

// Evaluate the regex in annotation and return a list of all namespaces that secret is needed to replicate to
func getReplicateNamespaces(clientSet *kubernetes.Clientset, obj metav1.ObjectMeta) ([]string, error) {
	output := make([]string, 0, 10)
	if metav1.HasAnnotation(obj, REPLICATE_REGEX) {
		// TODO: evaluate the regex on the namespace
		fmt.Println(REPLICATE_REGEX)
	} else if metav1.HasAnnotation(obj, REPLICATE_ALL_NAMESPACES) {
		// set output to all namespaces
		namespaces := getAllNamespaces(clientSet)
		for i := 0; i < len(namespaces.Items); i++ {
			output = append(output, namespaces.Items[i].Name)
		}
	} else {
		return output, fmt.Errorf("neither %v or %v annotation found in resource", REPLICATE_REGEX, REPLICATE_ALL_NAMESPACES)
	}

	return output[:], nil
}

func replicateSecretToNamespace(clientSet *kubernetes.Clientset, secret v1.Secret, namespace string) {
	// Remove annotation
	copied_secret := secret.DeepCopy()
	delete(copied_secret.Annotations, REPLICATE_REGEX)
	delete(copied_secret.Annotations, REPLICATE_ALL_NAMESPACES)
	// add replicated-from annotation
	copied_secret.Annotations[REPLICATED_ANNOTATION] = secret.Namespace
	copied_secret.Namespace = namespace
	copied_secret.ResourceVersion = ""
	_, err := clientSet.CoreV1().Secrets(namespace).Create(context.TODO(), copied_secret, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			// checks if secret is the same
			existing_secret, err := clientSet.CoreV1().Secrets(namespace).Get(context.TODO(), secret.Name, metav1.GetOptions{})
			if err != nil {
				panic(err.Error())
			}
			if !checkSecretEquality(copied_secret, existing_secret) {
				// updates secret
				updated_secret := existing_secret.DeepCopy()
				updated_secret.Annotations = copied_secret.Annotations
				updated_secret.Data = copied_secret.Data
				updated_secret.Labels = copied_secret.Labels
				_, err := clientSet.CoreV1().Secrets(namespace).Update(context.TODO(), updated_secret, metav1.UpdateOptions{})
				if err != nil {
					panic(err.Error())
				}
				log.Infof("Updating [resource=secret][ns=%v][name=%v] to %v namespace...\n", secret.Namespace, secret.Name, namespace)
			}
		} else {
			panic(err.Error())
		}
	} else {
		log.Infof("Replicating [resource=secret][ns=%v][name=%v] to %v namespace...\n", secret.Namespace, secret.Name, namespace)
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
