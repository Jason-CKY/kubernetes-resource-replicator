package main

import (
	"context"
	"flag"
	"path/filepath"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		log.Infof("Checking...")
		replicateSecrets(clientSet)
		deleteOrphanedSecrets(clientSet)
		time.Sleep(configLoopDuration)
	}
}

func processSecrets(clientSet *kubernetes.Clientset) {
	//TODO: taking >10s per loop to process, optimize it by
	// only querying once for the list of source secrets, replicated secrets, and namespaces to be replicated to
}

func replicateSecrets(clientSet *kubernetes.Clientset) {
	secrets := getAllSourceSecrets(clientSet)
	log.Debugf("There are %d secrets with the relevant annotations in the cluster", len(secrets.Items))

	for i := 0; i < len(secrets.Items); i++ {
		replicatingNamespaces, err := getReplicateNamespaces(clientSet, secrets.Items[i].ObjectMeta)
		if err != nil {
			panic(err.Error())
		}
		for j := 0; j < len(replicatingNamespaces); j++ {
			if secrets.Items[i].Namespace != replicatingNamespaces[j] {
				replicateSecretToNamespace(clientSet, secrets.Items[i], replicatingNamespaces[j])
			}
		}
	}
}

// Delete secrets that does not have a source anymore
// find all secrets with REPLICATED_ANNOTATION annotation, check if source namespace with same name secret exists,
// and delete secret if does not exist
func deleteOrphanedSecrets(clientSet *kubernetes.Clientset) {
	// map to hold if secret still exists in source namespace, to prevent unnecessary api calls to kubernetes
	secretExistMapping := make(map[string]bool)
	replicatedSecrets := getAllReplicatedSecrets(clientSet)
	for _, replicatedSecret := range replicatedSecrets.Items {
		if secretExists, ok := secretExistMapping[replicatedSecret.Name]; ok {
			if !secretExists {
				deleteSecret(clientSet, replicatedSecret)
			}
		} else {
			// check if secret exists
			_, err := clientSet.CoreV1().Secrets(replicatedSecret.Annotations[REPLICATED_ANNOTATION]).Get(context.TODO(), replicatedSecret.Name, v1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					secretExistMapping[replicatedSecret.Name] = false
					deleteSecret(clientSet, replicatedSecret)
				} else {
					panic(err.Error())
				}
			} else {
				secretExistMapping[replicatedSecret.Name] = true
			}
		}
		if secretExistMapping[replicatedSecret.Name] {
			// check if annotations has changed to not include this current namespace anymore
			// this is only relevant for regex replication as the annotations may be changed to no longer fit
			// the current replica's namespace
			originalSecret, err := clientSet.CoreV1().Secrets(replicatedSecret.Annotations[REPLICATED_ANNOTATION]).Get(context.TODO(), replicatedSecret.Name, v1.GetOptions{})
			if err != nil {
				panic(err.Error())
			}
			if v1.HasAnnotation(originalSecret.ObjectMeta, REPLICATE_REGEX) {
				deleteReplica := true
				patterns := strings.Split(originalSecret.Annotations[REPLICATE_REGEX], ",")
				for _, pattern := range patterns {
					namespaces := getAllRegexNamespaces(clientSet, pattern)
					for _, namespace := range namespaces {
						if replicatedSecret.Namespace == namespace.Name {
							deleteReplica = false
						}
					}
				}
				if deleteReplica {
					deleteSecret(clientSet, replicatedSecret)
				}
			}
		}
	}
}
