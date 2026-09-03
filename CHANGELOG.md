# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v0.2.0] - 2026-09-04

### Added
- **Node Pressure Detection & Eviction**:
  - Automatic detection of sustained node pressure conditions (`MemoryPressure`, `DiskPressure`, `PIDPressure`, and `NotReady`/`Unknown` status).
  - Node cordoning (`Unschedulable = true`) upon detected sustained pressure.
  - Automatic pod evacuation/eviction and force-cleanup for non-DaemonSet pods on pressured nodes.
  - Configurable flags: `--enable-node-pressure`, `--node-pressure-duration`, `--node-cordon`, and `--node-eviction-force`.
- **Node Pressure CronJob**:
  - Added dedicated Helm chart template `node-pressure-cronjob.yaml` and configurable values under `nodePressure` in `values.yaml`.
  - Added node permissions (`get`, `list`, `watch`, `update`, `patch`) to the ClusterRole.
- **Codecov Integration & Testing**:
  - Integrated Codecov coverage reporting in CI workflow.
  - Increased test coverage across core cleaner and config packages to >94%.

### Changed
- Bumped Helm chart version to `0.2.0`.
- Updated documentation and README with node pressure eviction details and ArgoCD deployment examples.

---

## [v0.1.0] - 2026-09-03

### Added
- **Initial Release of `k8s-pod-cleanup`**:
  - Lightweight Go-based Kubernetes controller / CronJob for cleaning up stale, stuck, and unhealthy pods across cluster namespaces.
- **Unhealthy Pod Evaluation & Cleanup**:
  - Support for stale terminated/evicted pods (`Failed`, `Succeeded`, `Evicted`).
  - Detection of crash looping and pending stalled pods (`CrashLoopBackOff`, `ImagePullBackOff`, `ErrImagePull`, `CreateContainerError`).
  - Detection of stale unready running pods and stuck terminating pods (`DeletionTimestamp` exceeded).
- **Safety & Protection Rules**:
  - Namespace exclusion list (protecting `kube-system`, `kube-public`, `kube-node-lease`, and custom namespaces).
  - Pod opt-out annotation/label (`cleanup.k8s.io/ignore: "true"`).
  - Safe dry-run mode for evaluation and structured JSON logging without performing deletions.
- **Helm Chart & Deployment**:
  - Helm chart packaged with CronJob, ServiceAccount, ClusterRole, and ClusterRoleBinding templates.
  - Multi-architecture Docker image builds (`linux/amd64`, `linux/arm64`) published to GHCR.
  - Automated CI and Release workflows.
