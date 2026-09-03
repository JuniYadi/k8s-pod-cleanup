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

---

## 🚀 Installation

### Option 1: Direct Helm OCI Install

```bash
# Install with Dry-Run mode enabled for safety verification
helm install k8s-pod-cleanup oci://ghcr.io/juniyadi/k8s-pod-cleanup/charts/k8s-pod-cleanup \
  --version 0.1.0 \
  --namespace kube-system \
  --set cleanup.dryRun=true

# Deploy active deletion mode
helm upgrade --install k8s-pod-cleanup oci://ghcr.io/juniyadi/k8s-pod-cleanup/charts/k8s-pod-cleanup \
  --version 0.1.0 \
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
    repoURL: ghcr.io/juniyadi/k8s-pod-cleanup/charts
    chart: k8s-pod-cleanup
    targetRevision: 0.1.0
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
