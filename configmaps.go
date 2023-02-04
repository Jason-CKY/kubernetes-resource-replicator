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

type SourceConfigmap struct {
	configmap        v1.ConfigMap
	targetNamespaces []string
}

type ReplicatedConfigmap struct {
	configmap       v1.ConfigMap
	sourceNamespace string
}

// function to list all namespaces and source configmaps and replicate them to the relevant namespaces
// also scans and deletes any orphaned configmaps.
// It is optimized by only querying once for the list of source configmaps, replicated configmaps, and namespaces to be replicated to
func processConfigmaps(clientSet *kubernetes.Clientset, allNamespaces *v1.NamespaceList) {
	// Get all configmaps
	allConfigmaps := getAllConfigmaps(clientSet)
	sourceConfigmaps, replicatedConfigmaps := getSourceAndReplicatedConfigmaps(allConfigmaps, allNamespaces)
	log.Debugf("There are %d configmaps with the relevant annotations in the cluster", len(sourceConfigmaps))

	// Replicating source configmaps
	for _, sourceConfigmap := range sourceConfigmaps {
		// replicate to all relevant namespaces
		for _, replicateNamespace := range sourceConfigmap.targetNamespaces {
			replicateConfigmapToNamespace(clientSet, sourceConfigmap.configmap, replicateNamespace, replicatedConfigmaps)
		}
		log.Debugf("Finished replicating all namespaces for configmap %v", sourceConfigmap.configmap.Name)
	}

	// Deleting orphaned configmaps
	for _, replicatedConfigmap := range replicatedConfigmaps {
		// check if source configmap still exists or regex still valid
		_, err := getConfigmapInSourceConfigmaps(replicatedConfigmap, sourceConfigmaps)
		if err != nil {
			if errors.IsNotFound(err) {
				deleteConfigmap(clientSet, replicatedConfigmap.configmap)
			} else {
				panic(err.Error())
			}
		}
	}
}

// Get all configmaps from all namespaces
func getAllConfigmaps(clientSet *kubernetes.Clientset) *v1.ConfigMapList {
	allConfigmaps, err := clientSet.CoreV1().ConfigMaps("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}
	return allConfigmaps
}

// Checks if given configmap is a source configmap by checking the annotations
func isSourceConfigmap(configmap v1.ConfigMap) bool {
	return metav1.HasAnnotation(configmap.ObjectMeta, REPLICATE_REGEX) || metav1.HasAnnotation(configmap.ObjectMeta, REPLICATE_ALL_NAMESPACES)
}

// Checks if given configmap is a replicated configmap by checking the annotations
func isReplicatedConfigmap(configmap v1.ConfigMap) bool {
	return metav1.HasAnnotation(configmap.ObjectMeta, REPLICATED_ANNOTATION)
}

// fuction that takes in all configmaps and returns a list of source Configmaps and a list of replicatedConfigmaps
func getSourceAndReplicatedConfigmaps(allConfigmaps *v1.ConfigMapList, allNamespaces *v1.NamespaceList) ([]SourceConfigmap, []ReplicatedConfigmap) {
	// initialize array for SourceConfigmaps and ReplicatedConfigmaps
	sourceConfigmaps := make([]SourceConfigmap, 0, 10)
	replicatedConfigmaps := make([]ReplicatedConfigmap, 0, 10)

	for _, configmap := range allConfigmaps.Items {
		if isSourceConfigmap(configmap) {
			// Filter for all source configmaps
			targetNamespaces, err := getReplicateNamespaces(allNamespaces, configmap.ObjectMeta)
			if err != nil {
				panic(err.Error())
			}
			sourceConfigmaps = append(sourceConfigmaps, SourceConfigmap{configmap: configmap, targetNamespaces: targetNamespaces})
		} else if isReplicatedConfigmap(configmap) {
			// Filter for all replicated configmaps
			replicatedConfigmaps = append(replicatedConfigmaps, ReplicatedConfigmap{configmap: configmap, sourceNamespace: configmap.Annotations[REPLICATED_ANNOTATION]})
		}
	}

	return sourceConfigmaps, replicatedConfigmaps
}

// Get configmap in array of SourceConfigmaps, error if not found or the replicatedConfigmap Namespace is no longer valid to be replicated into (i.e. regex changed in the source configmap).
// Used to search for the source configmap given a replicated one.
func getConfigmapInSourceConfigmaps(replicatedConfigmap ReplicatedConfigmap, sourceConfigmaps []SourceConfigmap) (v1.ConfigMap, error) {
	for _, sourceConfigmap := range sourceConfigmaps {
		if sourceConfigmap.configmap.Name == replicatedConfigmap.configmap.Name &&
			replicatedConfigmap.sourceNamespace == sourceConfigmap.configmap.Namespace &&
			arrayContains(sourceConfigmap.targetNamespaces, replicatedConfigmap.configmap.Namespace) {
			return sourceConfigmap.configmap, nil
		}
	}
	return v1.ConfigMap{}, errors.NewNotFound(schema.GroupResource{}, "")
}

// Get configmap in array of replicatedConfigmaps, error if not found.
// Used to search for the replicated configmap given a sourceConfigmap.
func getConfigmapInReplicatedConfigmaps(configmap v1.ConfigMap, replicatedConfigmaps []ReplicatedConfigmap, namespace string) (v1.ConfigMap, error) {
	for _, replicatedConfigmap := range replicatedConfigmaps {
		if replicatedConfigmap.configmap.Name == configmap.Name &&
			replicatedConfigmap.sourceNamespace == configmap.Namespace &&
			replicatedConfigmap.configmap.Namespace == namespace {
			return replicatedConfigmap.configmap, nil
		}
	}
	return v1.ConfigMap{}, errors.NewNotFound(schema.GroupResource{}, "")
}

// Replicate source configmap to target namespace
// Creates the replicate configmap if it does not exist, and update it if it exists and is not the same
func replicateConfigmapToNamespace(clientSet *kubernetes.Clientset, configmap v1.ConfigMap, namespace string, replicatedConfigmaps []ReplicatedConfigmap) {
	// do nothing if the target namespace is the same as the source configmap namespace
	if namespace == configmap.Namespace {
		return
	}
	// Remove annotation
	copied_configmap := configmap.DeepCopy()
	delete(copied_configmap.Annotations, REPLICATE_REGEX)
	delete(copied_configmap.Annotations, REPLICATE_ALL_NAMESPACES)
	// add replicated-from annotation
	copied_configmap.Annotations[REPLICATED_ANNOTATION] = configmap.Namespace
	copied_configmap.Namespace = namespace
	copied_configmap.ResourceVersion = ""

	existing_configmap, err := getConfigmapInReplicatedConfigmaps(configmap, replicatedConfigmaps, namespace)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create configmap if it does not exist
			log.Infof("Replicating [resource=configmap][ns=%v][name=%v] to %v namespace...\n", configmap.Namespace, configmap.Name, namespace)
			_, err := clientSet.CoreV1().ConfigMaps(namespace).Create(context.TODO(), copied_configmap, metav1.CreateOptions{})
			if err != nil {
				panic(err.Error())
			}
		} else {
			panic(err.Error())
		}
	} else {
		// Check if configmap value is the same if it exists
		// and updates the configmap if it is changed
		if !checkConfigmapEquality(*copied_configmap, existing_configmap) {
			// updates configmap
			updated_configmap := existing_configmap.DeepCopy()
			updated_configmap.Annotations = copied_configmap.Annotations
			updated_configmap.Data = copied_configmap.Data
			updated_configmap.Labels = copied_configmap.Labels
			log.Infof("Updating [resource=configmap][ns=%v][name=%v] to %v namespace...\n", configmap.Namespace, configmap.Name, namespace)
			_, err := clientSet.CoreV1().ConfigMaps(namespace).Update(context.TODO(), updated_configmap, metav1.UpdateOptions{})
			if err != nil {
				panic(err.Error())
			}
		}
	}
}

// checks 2 configmaps if they are the same
// this function checks the values, labels, and annotations
func checkConfigmapEquality(originalConfigmap v1.ConfigMap, replicatedConfigmap v1.ConfigMap) bool {
	originalAnnotation := stripAllReplicatorAnnotations(originalConfigmap.Annotations)
	replicatedAnnotation := stripAllReplicatorAnnotations(replicatedConfigmap.Annotations)

	return cmp.Equal(originalConfigmap.Data, replicatedConfigmap.Data) &&
		cmp.Equal(originalAnnotation, replicatedAnnotation) &&
		cmp.Equal(originalConfigmap.Labels, replicatedConfigmap.Labels)
}

// deletes configmap
func deleteConfigmap(clientSet *kubernetes.Clientset, configmap v1.ConfigMap) {
	log.Infof("Deleting configmap %v in namespace %v...", configmap.Name, configmap.Namespace)
	err := clientSet.CoreV1().ConfigMaps(configmap.Namespace).Delete(context.TODO(), configmap.Name, metav1.DeleteOptions{})
	if err != nil {
		panic(err.Error())
	}
}
