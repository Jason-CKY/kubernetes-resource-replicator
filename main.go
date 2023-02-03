package main

import (
	"context"
	"flag"
	"path/filepath"
	"time"

	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
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

func main() {
	var kubeconfig *string
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
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
	// use the current context in kubeconfig
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err.Error())
	}

	// create the clientset
	clientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	for {
		replicateSecrets(clientSet)
		deleteOrphanedSecrets(clientSet)
		time.Sleep(configLoopDuration)
	}
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
func deleteOrphanedSecrets(clientSet *kubernetes.Clientset) {
	// find all secrets with REPLICATED_ANNOTATION annotation, check if source namespace with same name secret exists,
	// and delete secret if does not exist

	// map to hold if secret still exists in source namespace, to prevent unnecessary api calls to kubernetes
	secretExistMapping := make(map[string]bool)
	replicatedSecrets := getAllReplicatedSecrets(clientSet)
	for i := 0; i < len(replicatedSecrets.Items); i++ {
		replicatedSecret := replicatedSecrets.Items[i]
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

	}
}
