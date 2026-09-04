# Domain Model: Kubernetes Pod Cleanup

## Glossary

### Target Pod
A Kubernetes `Pod` resource evaluated across cluster namespaces to determine whether it is consuming resources or blocking scheduling without providing healthy service.

### Unhealthy Pod Conditions
- **Stale Terminated / Evicted Pod**: A Pod with phase `Failed`, `Succeeded`, or `Evicted` status whose age exceeds the configured threshold.
- **CrashLoop / Pending Stalled Pod**: A Pod exhibiting continuous container restart failures (`CrashLoopBackOff`, `ImagePullBackOff`, `ErrImagePull`, `CreateContainerError`, or unresolvable `Pending`) where container restart counts exceed the restart threshold and duration exceeds the threshold duration.
- **Stale Unready Running Pod**: A Pod in `Running` phase whose `Ready` condition is `False` continuously for longer than the grace threshold. Pods in `Running` with `Ready == True` are strictly invariant and never marked unhealthy.
- **Stale Terminating Pod**: A Pod stuck in termination lifecycle with non-nil `DeletionTimestamp` exceeding `terminating-threshold`.

### Node Pressure Condition
A Kubernetes `Node` undergoing sustained resource pressure (`MemoryPressure`, `DiskPressure`, `PIDPressure`, or `Ready` condition in `False`/`Unknown` status) where `time.Since(condition.LastTransitionTime)` exceeds the configured `node-pressure-duration` (default `1m`).
When sustained node pressure is detected:
- The node is cordoned (`Unschedulable = true`) to prevent new workloads from landing.
- Non-DaemonSet pods on the node in non-excluded namespaces are evacuated (with force deletion fallback) so controllers recreate them on healthy nodes.

### Exclusion Rule
Rules protecting Pods from deletion:
- **Namespace Exclusion**: Blacklisted namespaces (`kube-system`, `kube-public`, `kube-node-lease`, and user-configured namespaces).
- **Annotation Opt-Out**: Pods possessing the label/annotation `cleanup.k8s.io/ignore: "true"`.

### Execution Mode
- **Dry-Run Mode**: Evaluation is executed and candidate Pods are logged with structured JSON details without calling Kubernetes deletion API.
- **Active Deletion Mode**: Evaluated unhealthy Pods are deleted from the cluster.

### Deletion Strategy
- **Graceful Deletion**: Deletes pod respecting Kubernetes grace period seconds (default).
- **Force Deletion**: Deletes pod immediately with zero grace period (`gracePeriodSeconds = 0`) via `--force`.

### Run Metrics
Measurements describing a single execution of the binary, pushed to a Prometheus Pushgateway when `--pushgateway-url` is set. Every metric is a Gauge scoped to that one run, never a Counter: the Pushgateway replaces the whole group on each push, so a value that decreases between runs is a normal observation rather than a counter reset.
- **Reason Code**: The low-cardinality label (`terminating_stuck`, `failed_or_evicted`, `pending_stalled`, `crashloop_or_image_error`, `unready`) paired with each free-text deletion reason. The free-text form embeds durations and container names and belongs only in logs.
- **Component**: Which CronJob is reporting (`pod-cleanup` or `node-pressure`). Both run the same binary, so this is part of the Pushgateway grouping key and keeps one job's metrics from overwriting the other's.

### Cleaner CronJob
A containerized Go CLI binary executed periodically by the Kubernetes CronJob controller.
