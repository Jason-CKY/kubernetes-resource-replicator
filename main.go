package main

import (
	"flag"
	"os"
	"path/filepath"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
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
	home := homedir.HomeDir()
	kubeconfig := filepath.Join(home, ".kube", "config")
	if _, err := os.Stat(kubeconfig); err == nil {
		// if $HOME/.kube/config file exists
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
		allNamespaces := getAllNamespaces(clientSet)
		loop(clientSet, allNamespaces)
		log.Debugf("End of loop!")

		time.Sleep(configLoopDuration)
	}
}

// main loop function that uses goroutines to process secrets and configmaps
// includes waitGroup to block code execution until the loop function full completes.
// This is to ensure the loop is fully executed before the loop delay is executed
func loop(clientSet *kubernetes.Clientset, allNamespaces *v1.NamespaceList) {
	var wg sync.WaitGroup
	wg.Add(2)
	go processSecrets(clientSet, allNamespaces, &wg)
	go processConfigmaps(clientSet, allNamespaces, &wg)
	wg.Wait()
}
