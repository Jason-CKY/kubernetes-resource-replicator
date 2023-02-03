# kubernetes-resource-replicator
Replicates kubernetes configmaps and secrets to selected namespaces

This package is written as a practice for writing golang applications. It heavily references from [kubernetes-replicator](https://github.com/mittwald/kubernetes-replicator) and [imagepullsecret-patcher](https://github.com/titansoft-pte-ltd/imagepullsecret-patcher). If you require this functionality in your cluster, the more production ready version of the replicator is to use [kubernetes-replicator](https://github.com/mittwald/kubernetes-replicator).

Instead of being a kubernetes controller, this will be more like a running process that endlessly loops and watches for changes in 

## Implementation

Every `CONFIG_LOOP_DURATION` duration, this application checks for all secrets and configmaps with the `resource-replicator/replicate-to` or `resource-replicator/all-namespaces` annotation, and replicates it to the intended namespaces. It will also ensure that the secret/configmap data is the same as the source (i.e. when you change the value of the source secret/configmap it will propagate the change to all the replicated resources).

Below is a table of available configurations:

| Config name          | ENV     | Default Value | Description |
|--------------|-----------|------------|---------|
| loop duration | CONFIG_LOOP_DURATION      | 10s        | duration string which defines how often namespaces are checked, see https://golang.org/pkg/time/#ParseDuration for more examples
| debug logs | CONFIG_DEBUG      | false        | show debug logs


## Usage

### Replicates with annotations

#### Name-based

This allows you to either specify your target namespaces by name or by regular expression (which should match the namespace name). To use name-based push replication, add a replicator.v1.mittwald.de/replicate-to annotation to your secret or configmap. The value of this annotation should contain a comma separated list of permitted namespaces or regular expressions. (Example: `namespace-1,my-ns-2,app-ns-[0-9]*` will replicate only into the namespaces `namespace-1` and `my-ns-2` as well as any namespace that matches the regular expression `app-ns-[0-9]*`).

Example:

```yaml
apiVersion: v1
kind: Secret
metadata:
  annotations:
    resource-replicator/replicate-to: "my-ns-1,namespace-[0-9]*"
data:
  key1: <value>
```

#### Cluster-wide access

Use `resource-replicator/all-namespaces` annotation for the resource to be replicated to all namespaces.

```yaml
apiVersion: v1
kind: Secret
metadata:
  annotations:
    resource-replicator/all-namespaces: "true"
data:
  key1: <value>
```

### Cleaning up abandoned resource

Once the source resource has been deleted, all the replicated resources will also be cleaned up by this process. 

Updating the source resource's replication annotation will also update the replicated resource.
Example:

Updating

```yaml
apiVersion: v1
kind: Secret
metadata:
  annotations:
    resource-replicator/replicate-to: "my-ns-1,namespace-[0-9]*"
data:
  key1: <value>
```

to

```yaml
apiVersion: v1
kind: Secret
metadata:
  annotations:
    resource-replicator/replicate-to: "namespace-[0-9]*"
data:
  key1: <value>
```

will cause the secret in `my-ns-1` to be removed.
