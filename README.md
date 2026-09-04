# Kubernetes Pod Cleanup (`k8s-pod-cleanup`)

[![CI](https://github.com/JuniYadi/k8s-pod-cleanup/actions/workflows/ci.yaml/badge.svg)](https://github.com/JuniYadi/k8s-pod-cleanup/actions/workflows/ci.yaml)
[![Release](https://github.com/JuniYadi/k8s-pod-cleanup/actions/workflows/release.yaml/badge.svg)](https://github.com/JuniYadi/k8s-pod-cleanup/actions/workflows/release.yaml)
[![codecov](https://codecov.io/gh/JuniYadi/k8s-pod-cleanup/graph/badge.svg?token=W72D5NQWJO)](https://codecov.io/gh/JuniYadi/k8s-pod-cleanup)

A lightweight, automated Kubernetes CronJob written in Go that scans cluster namespaces and cleans up unhealthy or stuck Pods that waste cluster compute and IP resources.

---

## 🎯 Features

- **CrashLoop & Image Pull Detection**: Automatically detects pods with container states in `CrashLoopBackOff`, `ImagePullBackOff`, `ErrImagePull`, or `CreateContainerError` exceeding the restart threshold (default: 3) and duration threshold (default: 5m).
- **Stuck Pending & Evicted Pods**: Removes pods stuck in `Pending` or stale `Evicted` / `Failed` / `Error` state.
- **Unready Running Pods**: Identifies `Running` pods whose `Ready` condition remains `False` continuously for longer than the grace threshold (does **never** touch healthy `Running` pods).
- **Stuck Terminating Pods**: Cleans up pods stuck in `Terminating` lifecycle beyond threshold.
- **Node High-Pressure Evacuation & Cordoning**: Automatically detects nodes experiencing sustained resource pressure (`MemoryPressure`, `DiskPressure`, `PIDPressure`, or `NotReady` for $\ge 1\text{m}$), cordons the node (`Unschedulable = true`), and evacuates pods with forced cleanup fallback so workloads are rescheduled on healthy nodes.
- **Safety Exclusions**:
  - Automatically skips critical namespaces (`kube-system`, `kube-public`, `kube-node-lease`, and custom namespaces).
  - Pod opt-out via annotation/label `cleanup.k8s.io/ignore: "true"`.
- **Dry-Run Mode**: Full simulation logging with structured JSON (`log/slog`) before performing actual deletions.
- **Graceful or Force Deletion**: Choose between standard graceful termination or immediate force deletion (`--force`).
- **Prometheus Metrics**: Optionally pushes per-run metrics (pods evaluated/deleted by namespace and reason, nodes pressured/cordoned, run duration and outcome) to a Prometheus Pushgateway at the end of every run.

---

## 🚀 Installation

### Option 1: Direct Helm OCI Install

```bash
helm install k8s-pod-cleanup oci://ghcr.io/juniyadi/k8s-pod-cleanup/charts/k8s-pod-cleanup \
  --version 0.2.0 \
  --namespace kube-system \
  --set cleanup.dryRun=true

# Deploy active deletion mode
helm upgrade --install k8s-pod-cleanup oci://ghcr.io/juniyadi/k8s-pod-cleanup/charts/k8s-pod-cleanup \
  --version 0.2.0 \
  --namespace kube-system \
  --set cleanup.dryRun=false \
  --set cleanup.thresholdDuration=5m
```

---

### Option 2: ArgoCD Application

You can deploy `k8s-pod-cleanup` declaratively using ArgoCD via Git repository or direct OCI Helm registry.

#### A. ArgoCD using OCI Helm Chart (Recommended)

Create an `Application` manifest pointing to the GHCR OCI repository:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: k8s-pod-cleanup
  namespace: argocd
  finalizers:
    - resources-finalizer.argocd.argoproj.io
spec:
  project: default
  source:
    chart: k8s-pod-cleanup
    targetRevision: 0.2.0
    helm:
      releaseName: k8s-pod-cleanup
      values: |
        schedule: "*/5 * * * *"
        cleanup:
          dryRun: true
          force: false
          thresholdDuration: "5m"
          terminatingThreshold: "5m"
          restartThreshold: 3
          excludedNamespaces:
            - kube-system
            - kube-public
            - kube-node-lease
        nodePressureEviction:
          enabled: true
          schedule: "*/2 * * * *"
          dryRun: false
          pressureDuration: "1m"
          forceDelete: true
          cordon: true
  destination:
    server: https://kubernetes.default.svc
    namespace: kube-system
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
```

#### B. ArgoCD using Git Repository (Directory/Helm Source)

If you maintain this repository directly in your GitOps structure:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: k8s-pod-cleanup
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/juniyadi/k8s-pod-cleanup.git
    targetRevision: main
    path: chart/k8s-pod-cleanup
    helm:
      values: |
        schedule: "*/10 * * * *"
        cleanup:
          dryRun: false
          thresholdDuration: "10m"
        nodePressureEviction:
          enabled: true
          schedule: "*/2 * * * *"
          dryRun: false
          pressureDuration: "1m"
          forceDelete: true
          cordon: true
  destination:
    server: https://kubernetes.default.svc
    namespace: kube-system
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
```

---

## ⚙️ Configuration Parameters

| Parameter                      | Description                                            | Default                                       |
| :----------------------------- | :----------------------------------------------------- | :-------------------------------------------- |
| `schedule`                     | Cron schedule expression                               | `"*/5 * * * *"`                               |
| `image.repository`             | Container image repository                             | `ghcr.io/juniyadi/k8s-pod-cleanup`            |
| `image.tag`                    | Container image tag                                    | `1.0.0`                                       |
| `cleanup.dryRun`               | Simulate pod cleanup without deleting                  | `false`                                       |
| `cleanup.force`                | Force delete pods with 0 grace period                  | `false`                                       |
| `cleanup.thresholdDuration`    | Age/duration required before deleting an unhealthy pod | `"5m"`                                        |
| `cleanup.terminatingThreshold` | Threshold duration for stuck terminating pods          | `"5m"`                                        |
| `cleanup.restartThreshold`     | Minimum restart count for CrashLoop pods               | `3`                                           |
| `cleanup.ignoreAnnotation`     | Annotation or label key used to ignore pods            | `"cleanup.k8s.io/ignore"`                     |
| `cleanup.namespaces`           | Target namespaces list (empty list scans all)          | `[]`                                          |
| `cleanup.excludedNamespaces`   | Namespaces excluded from evaluation                    | `[kube-system, kube-public, kube-node-lease]` |
| `cleanup.logLevel`             | Logging verbosity (`debug`, `info`, `warn`, `error`)   | `"info"`                                      |
| `nodePressureEviction.enabled`           | Enable dedicated fast CronJob for node pressure evacuation | `true`                                        |
| `nodePressureEviction.schedule`          | Cron schedule for node pressure checks                 | `"*/2 * * * *"`                               |
| `nodePressureEviction.dryRun`            | Dry-run simulation for node pressure evaluation        | `false`                                       |
| `nodePressureEviction.pressureDuration`  | Sustained node pressure duration threshold             | `"1m"`                                        |
| `nodePressureEviction.forceDelete`       | Force delete pods (gracePeriodSeconds=0) on bad nodes  | `true`                                        |
| `nodePressureEviction.cordon`            | Mark pressured node unschedulable                      | `true`                                        |
| `metrics.enabled`                        | Push run metrics to a Prometheus Pushgateway           | `false`                                       |
| `metrics.pushgateway.url`                | Pushgateway base URL                                   | `"http://prometheus-pushgateway.monitoring.svc:9091"` |
| `metrics.pushgateway.jobName`            | Prometheus job name used as the grouping key           | `"k8s-pod-cleanup"`                           |
| `metrics.pushgateway.auth.existingSecret`| Secret with `PUSHGATEWAY_USERNAME` / `PUSHGATEWAY_PASSWORD` | `""`                                     |

---

## 📊 Prometheus Metrics

A CronJob pod exits long before Prometheus could scrape it, so metrics are pushed to a [Pushgateway](https://github.com/prometheus/pushgateway) at the end of each run instead.

```bash
helm upgrade --install k8s-pod-cleanup oci://ghcr.io/juniyadi/charts/k8s-pod-cleanup \
  --namespace kube-system \
  --set metrics.enabled=true \
  --set metrics.pushgateway.url=http://prometheus-pushgateway.monitoring.svc:9091
```

If your Pushgateway requires basic auth, create a Secret and reference it. Credentials are passed as environment variables, never as container args, so they do not show up in the pod spec:

```bash
kubectl create secret generic pushgateway-creds -n kube-system \
  --from-literal=PUSHGATEWAY_USERNAME=metrics \
  --from-literal=PUSHGATEWAY_PASSWORD='...'

helm upgrade --install ... --set metrics.pushgateway.auth.existingSecret=pushgateway-creds
```

### Exposed Metrics

Both CronJobs run the same binary, so each pushes under its own `component` grouping label (`pod-cleanup` or `node-pressure`) to avoid overwriting the other's group.

| Metric | Labels | Description |
| :----- | :----- | :---------- |
| `k8s_pod_cleanup_pods_evaluated`             | —                   | Pods checked against the cleanup rules |
| `k8s_pod_cleanup_pods_deleted`               | `namespace`, `reason` | Pods deleted (or, in dry-run, that would have been) |
| `k8s_pod_cleanup_pod_delete_errors`          | `namespace`         | Deletion attempts that failed |
| `k8s_pod_cleanup_nodes_evaluated`            | —                   | Nodes checked for sustained pressure |
| `k8s_pod_cleanup_nodes_pressured`            | —                   | Nodes found under sustained pressure |
| `k8s_pod_cleanup_node_pressure_conditions`   | `condition`         | Active pressure conditions by type |
| `k8s_pod_cleanup_nodes_cordoned`             | —                   | Nodes marked unschedulable |
| `k8s_pod_cleanup_pods_evacuated`             | `namespace`         | Pods removed from pressured nodes |
| `k8s_pod_cleanup_duration_seconds`           | —                   | Wall-clock duration of the run |
| `k8s_pod_cleanup_last_run_timestamp_seconds` | —                   | When the run finished |
| `k8s_pod_cleanup_run_success`                | —                   | `1` if the run completed without error |
| `k8s_pod_cleanup_dry_run`                    | —                   | `1` if the run was a dry run |

`reason` is one of `terminating_stuck`, `failed_or_evicted`, `pending_stalled`, `crashloop_or_image_error`, `unready`. `condition` is one of `memory_pressure`, `disk_pressure`, `pid_pressure`, `not_ready`.

### Querying

Every metric is a **Gauge describing a single run**, not a Counter. Each run reports only its own work and replaces the previous group in the Pushgateway, so a value going from `5` to `2` is a normal observation rather than a counter reset — `rate()` and `increase()` would produce nonsense here. Aggregate across runs with `sum_over_time()`:

```promql
# Pods deleted in the last hour, by reason
sum by (reason) (sum_over_time(k8s_pod_cleanup_pods_deleted[1h]))

# Alert: cleanup has not reported in 30 minutes
time() - k8s_pod_cleanup_last_run_timestamp_seconds > 1800

# Alert: last run failed
k8s_pod_cleanup_run_success == 0
```

A push failure is logged but never fails the run — the cleanup itself already happened, and exiting non-zero would make the CronJob retry deletions to fix a monitoring problem. Alert on `k8s_pod_cleanup_last_run_timestamp_seconds` going stale to catch that case.

---

## 🛡️ Protecting Specific Pods from Cleanup

Add the ignore annotation or label to your Pod or Deployment template:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: critical-debug-pod
  annotations:
    cleanup.k8s.io/ignore: "true"
spec:
  containers:
    - name: app
      image: broken-image:v1
```

---

## 🛠️ Local Development & Testing

```bash
# Run unit tests with race detection
make test

# Build local binary
make build

# Build local docker image
make docker-build

# Run dry-run locally against current kubectl context
make run
```
