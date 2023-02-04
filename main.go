package main

import (
	"context"
	"flag"
	"path/filepath"
	"time"

	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

var (
	// Config
	configDebug        bool          = false
	configLoopDuration time.Duration = 10 * time.Second
)

const (
	REPLICATE_REGEX            string = "resource-replicator/replicate-to"
	REPLICATE_ALL_NAMESPACES   string = "resource-replicator/all-namespaces"
	REPLICATED_ANNOTATION      string = "resource-replicator/replicated-from"
	LAST_APPLIED_CONFIGURATION string = "kubectl.kubernetes.io/last-applied-configuration"
)

type SourceSecret struct {
	secret           v1.Secret
	targetNamespaces []string
}

type ReplicatedSecret struct {
	secret          v1.Secret
	sourceNamespace string
}

func getKubernetesConfig() *rest.Config {
	var config *rest.Config
	if home := homedir.HomeDir(); home != "" {
		kubeconfig := filepath.Join(home, ".kube", "config")
		log.Infof("Using kubeconfig file at %v", kubeconfig)
		// use the current context in kubeconfig
		_config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			panic(err.Error())
		}
		config = _config
	} else {
		log.Infof("Using incluster config...")
		_config, err := rest.InClusterConfig()
		if err != nil {
			panic(err.Error())
		}
		config = _config
	}
	return config
}

func main() {
	flag.BoolVar(&configDebug, "configDebug", LookupEnvOrBool("CONFIG_DEBUG", configDebug), "show DEBUG logs")
	flag.DurationVar(&configLoopDuration, "configLoopDuration", LookupEnvOrDuration("CONFIG_LOOP_DURATION", configLoopDuration), "duration string which defines how often namespaces are checked, see https://golang.org/pkg/time/#ParseDuration for more examples")

	flag.Parse()

	// setup logrus
	if configDebug {
		log.SetLevel(log.DebugLevel)
	}
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp:          true,
		DisableLevelTruncation: true,
	})

	log.Info("Application started")
	log.Debug("config loop duration: ", configLoopDuration)

	// create the clientset
	config := getKubernetesConfig()
	clientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	for {
		log.Info("Checking...")
		processSecrets(clientSet)
		log.Debug("Finished one loop")
		time.Sleep(configLoopDuration)
	}
}

func processSecrets(clientSet *kubernetes.Clientset) {
	// taking >10s per loop to process, optimize it by
	// only querying once for the list of source secrets, replicated secrets, and namespaces to be replicated to

	// Get all secrets
	allSecrets, err := clientSet.CoreV1().Secrets("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}
	allNamespaces := getAllNamespaces(clientSet)
	sourceSecrets := make([]SourceSecret, 0, 10)
	replicatedSecrets := make([]ReplicatedSecret, 0, 10)
	for _, secret := range allSecrets.Items {
		if metav1.HasAnnotation(secret.ObjectMeta, REPLICATE_REGEX) || metav1.HasAnnotation(secret.ObjectMeta, REPLICATE_ALL_NAMESPACES) {
			// Filter for all source secrets
			targetNamespaces, err := getReplicateNamespaces(allNamespaces, secret.ObjectMeta)
			if err != nil {
				panic(err.Error())
			}
			sourceSecrets = append(sourceSecrets, SourceSecret{secret: secret, targetNamespaces: targetNamespaces})
		} else if metav1.HasAnnotation(secret.ObjectMeta, REPLICATED_ANNOTATION) {
			// Filter for all replicated secrets
			replicatedSecrets = append(replicatedSecrets, ReplicatedSecret{secret: secret, sourceNamespace: secret.Annotations[REPLICATED_ANNOTATION]})
		}
	}
	log.Debugf("There are %d secrets with the relevant annotations in the cluster", len(sourceSecrets))

	for _, sourceSecret := range sourceSecrets {
		// replicate to all relevant namespaces
		for _, replicateNamespace := range sourceSecret.targetNamespaces {
			replicateSecretToNamespace(clientSet, sourceSecret.secret, replicateNamespace, replicatedSecrets)
		}
		log.Debugf("Finished replicating all namespaces for secret %v", sourceSecret.secret.Name)
	}
	for _, replicatedSecret := range replicatedSecrets {
		// check if source secret still exists
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
