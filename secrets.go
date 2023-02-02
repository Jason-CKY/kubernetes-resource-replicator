package main

import (
	"context"

	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// list all secrets and filter by annotation
// return all secrets with the `resource-replicator/replicate-to` or `resource-replicator/all-namespaces` annotation
func getAllSourceSecrets(clientSet *kubernetes.Clientset) *v1.SecretList {
	allSecrets, err := clientSet.CoreV1().Secrets("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}
	secrets := v1.SecretList{}

	for i := 0; i < len(allSecrets.Items); i++ {
		secret := allSecrets.Items[i]
		if metav1.HasAnnotation(secret.ObjectMeta, "resource-replicator/replicate-to") || metav1.HasAnnotation(secret.ObjectMeta, "resource-replicator/all-namespaces") {
			secrets.Items = append(secrets.Items, secret)
			log.Info(secret.Annotations["resource-replication/replicate-to"])
		}
	}
	return &secrets
}
