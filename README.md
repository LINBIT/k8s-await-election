# k8s-await-election

![Latest release](https://img.shields.io/github/v/release/linbit/k8s-await-election)

Ensure that only a single instance of a process is running in your Kubernetes cluster.

`k8s-await-election` leverages Kubernetes built-in [leader election](https://pkg.go.dev/k8s.io/client-go/tools/leaderelection?tab=doc)
capabilities to coordinate commands running in different pods. It acts as a gatekeeper, only
starting the command when the pod becomes a leader. 

### Why leader election?
Some applications aren't natively able to run in a replicated way.
For example, they maintain some internal state which is only synchronized at the start.

Running such an application as a single replica is not ideal. Should the node the pod
is running on go offline unexpectedly, Kubernetes will take its time to reschedule
(5min+ by default).

Leader election is able to elect a new leader in about 10-15 seconds in such an event.

## Usage
Just set `k8s-await-election` as entry point into your image. Configuration of the leader election
happens via environment variables. If no environment variables were passed, `k8s-await-election` 
will just start the command without waiting to become elected.

The relevant environment variables are

| Variable                                | Description                                                       |
|-----------------------------------------|-------------------------------------------------------------------|
| `K8S_AWAIT_ELECTION_ENABLED`            | Set to any non-empty value to enable leader election              |
| `K8S_AWAIT_ELECTION_NAME`               | Name of the election processes. Useful for debugging              |
| `K8S_AWAIT_ELECTION_LOCK_NAME`          | Name of the `leases.coordination.k8s.io` resource                 |
| `K8S_AWAIT_ELECTION_LOCK_NAMESPACE`     | Namespace of the  `leases.coordination.k8s.io`  resource          |
| `K8S_AWAIT_ELECTION_IDENTITY`           | Unique identity for each member of the election process           |
| `K8S_AWAIT_ELECTION_STATUS_ENDPOINT`    | Optional: endpoint to report if the election process is running   |
| `K8S_AWAIT_ELECTION_SERVICE_NAME`       | Optional: set the service to update. [On Service Updates]         |
| `K8S_AWAIT_ELECTION_SERVICE_NAMESPACE`  | Optional: set the service namespace.                              |
| `K8S_AWAIT_ELECTION_SERVICE_PORTS_JSON` | Optional: set to json array of endpoint ports.                    |
| `K8S_AWAIT_ELECTION_POD_IP`             | Optional: IP of the pod, which will be used to update the service |
| `K8S_AWAIT_ELECTION_NODE_NAME`          | Optional: Node name, will be used to update the service           |

[On Service Updates]: #service-updates

Most of the time you will want to use this process in a Deployment spec or similar context. Here is
an example:

```yaml
apiVersion: apps/v1                                                                                                                                           
kind: Deployment                                                                                                                                              
metadata:                                                                                                                                                     
  name: my-singleton-with-replicas
spec:
  replicas: 5
  selector:
    matchLabels:
      app: my-singleton-server
  template:
    metadata:
      labels:
        app: my-singleton-server
    spec:
      containers:          
      - name: my-singleton-server
        args:                
        - my-singleton-server                      
        env:                              
        - name: K8S_AWAIT_ELECTION_ENABLED
          value: "1"         
        - name: K8S_AWAIT_ELECTION_NAME           
          value: linstor-controller                 
        - name: K8S_AWAIT_ELECTION_LOCK_NAME                                                
          value: piraeus-op-cs      
        - name: K8S_AWAIT_ELECTION_LOCK_NAMESPACE
          value: default    
        - name: K8S_AWAIT_ELECTION_IDENTITY
          valueFrom:    
            fieldRef:   
              apiVersion: v1
              fieldPath: metadata.name
        - name: K8S_AWAIT_ELECTION_STATUS_ENDPOINT
          value: :9999
```

### Service Updates

`k8s-await-election` can also be used to select which pod should receive traffic for a service.
This is done by updating the endpoint resource associated with a service whenever a new leader is elected.
This leader will be the only pod receiving traffic via the service.
To enable this feature, set the `K8S_AWAIT_ELECTION_SERVICE_*` variables.
See [the full example](./examples/singleton-service.yml)

#### Why update service endpoints?

For deployments that provide some kind of external API (for example a REST API), we would
also like to automatically re-route traffic to the current leader.

This is normally done via the readiness state of the pod: only ready pods associated with
a service receive traffic. Because we only start the application if we are elected, only
one pod is ever "ready" in the sense that it should receive traffic.

This means that if we wanted to use the automatic service configuration via selectors, we
run into some issues. The "ready" state of a pod has a kind of dual use. Consider a rolling upgrade of a deployment:

1. A new pod starts.
2. It won't become leader as the old one is still running
3. Since the app starts, it is never "ready" to receive traffic
4. the deployment controller sees the pod is not ready, and does not continue with upgrading
 
`k8s-await-election` has all the information it needs to tell Kubernetes which pod should receive
traffic. This works around the above issue, at the cost of a non-usable readiness probe.
