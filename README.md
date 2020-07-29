# k8s-await-election

![Latest release](https://img.shields.io/github/v/release/linbit/k8s-await-election)

Ensure that only a single instance of a process is running in your Kubernetes cluster.

`k8s-await-election` leverages Kubernetes built-in [leader election](https://pkg.go.dev/k8s.io/client-go/tools/leaderelection?tab=doc)
capabilities to coordinate commands running in different pods. It acts as a gatekeeper, only
starting the command when the pod becomes a leader. 

## Usage
Just set `k8s-await-election` as entry point into your image. Configuration of the leader election
happens via environment variables. If no environment variables were passed, `k8s-await-election` 
will just start the command without waiting to become elected.

The relevant environment variables are

| Variable                             | Description                                                     |
|--------------------------------------|-----------------------------------------------------------------|
| `K8S_AWAIT_ELECTION_ENABLED`         | Set to any non-empty value to enable leader election            |
| `K8S_AWAIT_ELECTION_NAME`            | Name of the election processes. Useful for debugging            |
| `K8S_AWAIT_ELECTION_LOCK_NAME`       | Name of the `leases.coordination.k8s.io` resource               |
| `K8S_AWAIT_ELECTION_LOCK_NAMESPACE`  | Namespace of the  `leases.coordination.k8s.io`  resource        |
| `K8S_AWAIT_ELECTION_IDENTITY`        | Unique identity for each member of the election process         |
| `K8S_AWAIT_ELECTION_STATUS_ENDPOINT` | Optional: endpoint to report if the election process is running |

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
