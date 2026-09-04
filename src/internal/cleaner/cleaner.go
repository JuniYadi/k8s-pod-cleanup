package cleaner

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/juniyadi/k8s-pod-cleanup/internal/config"
	"github.com/juniyadi/k8s-pod-cleanup/internal/metrics"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Decision represents the evaluation result for a pod.
type Decision struct {
	ShouldDelete bool
	Reason       string
	// Code is the low-cardinality metrics label for Reason. Reason itself
	// embeds durations and container names, so it can never be a label value.
	Code string
	Age  time.Duration
}

// Cleaner handles pod discovery, evaluation, and deletion.
type Cleaner struct {
	client  kubernetes.Interface
	cfg     *config.Config
	now     func() time.Time
	metrics *metrics.Recorder
}

// NewCleaner creates a new Cleaner instance.
func NewCleaner(client kubernetes.Interface, cfg *config.Config) *Cleaner {
	return &Cleaner{
		client:  client,
		cfg:     cfg,
		now:     time.Now,
		metrics: metrics.NewRecorder(cfg.DryRun),
	}
}

// Metrics returns the recorder holding this run's measurements.
func (c *Cleaner) Metrics() *metrics.Recorder {
	return c.metrics
}

// SetNow allows injecting current time for deterministic testing.
func (c *Cleaner) SetNow(nowFunc func() time.Time) {
	c.now = nowFunc
}

// Run executes a single cleaning cycle.
func (c *Cleaner) Run(ctx context.Context) error {
	slog.Info("Starting k8s-pod-cleanup run",
		"dryRun", c.cfg.DryRun,
		"force", c.cfg.Force,
		"thresholdDuration", c.cfg.ThresholdDuration.String(),
		"restartThreshold", c.cfg.RestartThreshold,
		"enableNodePressureEviction", c.cfg.EnableNodePressureEviction,
	)

	if c.cfg.EnableNodePressureEviction {
		if err := c.EvacuateHighPressureNodes(ctx); err != nil {
			slog.Error("Error during node pressure evaluation and evacuation", "error", err)
		}
	}
	var targetNamespaces []string
	if len(c.cfg.Namespaces) > 0 {
		targetNamespaces = c.cfg.Namespaces
	} else {
		nsList, err := c.client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		if err != nil {
			return fmt.Errorf("failed to list namespaces: %w", err)
		}
		for _, ns := range nsList.Items {
			if !c.isNamespaceExcluded(ns.Name) {
				targetNamespaces = append(targetNamespaces, ns.Name)
			}
		}
	}

	totalEvaluated := 0
	totalDeleted := 0

	for _, ns := range targetNamespaces {
		if c.isNamespaceExcluded(ns) {
			slog.Debug("Skipping excluded namespace", "namespace", ns)
			continue
		}

		pods, err := c.client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			slog.Error("Failed to list pods in namespace", "namespace", ns, "error", err)
			continue
		}

		for i := range pods.Items {
			pod := &pods.Items[i]
			totalEvaluated++
			c.metrics.AddPodEvaluated()

			decision := c.EvaluatePod(pod)
			if !decision.ShouldDelete {
				continue
			}

			slog.Info("Target pod flagged for cleanup",
				"namespace", pod.Namespace,
				"pod", pod.Name,
				"reason", decision.Reason,
				"age", decision.Age.Round(time.Second).String(),
				"dryRun", c.cfg.DryRun,
			)

			if c.cfg.DryRun {
				totalDeleted++
				c.metrics.AddPodDeleted(pod.Namespace, decision.Code)
				continue
			}

			deleteOpts := metav1.DeleteOptions{}
			if c.cfg.Force {
				var zero int64 = 0
				deleteOpts.GracePeriodSeconds = &zero
			}

			if err := c.client.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, deleteOpts); err != nil {
				c.metrics.AddPodDeleteError(pod.Namespace)
				slog.Error("Failed to delete pod",
					"namespace", pod.Namespace,
					"pod", pod.Name,
					"error", err,
				)
			} else {
				totalDeleted++
				c.metrics.AddPodDeleted(pod.Namespace, decision.Code)
				slog.Info("Successfully deleted pod",
					"namespace", pod.Namespace,
					"pod", pod.Name,
					"force", c.cfg.Force,
				)
			}
		}
	}

	slog.Info("Cleanup completed",
		"totalEvaluated", totalEvaluated,
		"totalDeleted", totalDeleted,
		"dryRun", c.cfg.DryRun,
	)

	return nil
}

func (c *Cleaner) isNamespaceExcluded(ns string) bool {
	for _, excluded := range c.cfg.ExcludedNamespaces {
		if strings.EqualFold(ns, excluded) {
			return true
		}
	}
	return false
}

// EvaluatePod evaluates whether a pod meets the deletion criteria.
func (c *Cleaner) EvaluatePod(pod *corev1.Pod) Decision {
	// 1. Opt-out Annotation / Label Check
	if c.isOptedOut(pod) {
		return Decision{ShouldDelete: false, Reason: "Opt-out annotation/label present"}
	}

	now := c.now()

	// 2. Terminating Stuck Pods
	if pod.DeletionTimestamp != nil {
		terminatingDuration := now.Sub(pod.DeletionTimestamp.Time)
		if terminatingDuration >= c.cfg.TerminationThreshold {
			return Decision{
				ShouldDelete: true,
				Reason:       fmt.Sprintf("Stuck in Terminating state for %s", terminatingDuration.Round(time.Second)),
				Code:         metrics.ReasonTerminatingStuck,
				Age:          terminatingDuration,
			}
		}
		return Decision{ShouldDelete: false, Reason: "Terminating within grace period"}
	}

	podAge := now.Sub(pod.CreationTimestamp.Time)

	// 3. Evicted / Failed / Succeeded Phases
	if pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Reason == "Evicted" {
		if podAge >= c.cfg.ThresholdDuration {
			return Decision{
				ShouldDelete: true,
				Reason:       fmt.Sprintf("Phase is %s (Reason: %s) older than threshold", pod.Status.Phase, pod.Status.Reason),
				Code:         metrics.ReasonFailedOrEvicted,
				Age:          podAge,
			}
		}
		return Decision{ShouldDelete: false, Reason: "Failed/Evicted but within threshold"}
	}

	// 4. Stalled Pending Pods
	if pod.Status.Phase == corev1.PodPending {
		if podAge >= c.cfg.ThresholdDuration {
			return Decision{
				ShouldDelete: true,
				Reason:       fmt.Sprintf("Pending phase stuck for %s", podAge.Round(time.Second)),
				Code:         metrics.ReasonPendingStalled,
				Age:          podAge,
			}
		}
		return Decision{ShouldDelete: false, Reason: "Pending within threshold"}
	}

	// 5. Container Status Inspection: CrashLoopBackOff, ImagePullBackOff, ErrImagePull
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Waiting != nil {
			reason := cs.State.Waiting.Reason
			if isCrashOrImageError(reason) && cs.RestartCount >= c.cfg.RestartThreshold {
				if podAge >= c.cfg.ThresholdDuration {
					return Decision{
						ShouldDelete: true,
						Reason:       fmt.Sprintf("Container %s waiting with %s (restarts: %d)", cs.Name, reason, cs.RestartCount),
						Code:         metrics.ReasonCrashOrImage,
						Age:          podAge,
					}
				}
			}
		}
	}

	for _, ics := range pod.Status.InitContainerStatuses {
		if ics.State.Waiting != nil {
			reason := ics.State.Waiting.Reason
			if isCrashOrImageError(reason) && ics.RestartCount >= c.cfg.RestartThreshold {
				if podAge >= c.cfg.ThresholdDuration {
					return Decision{
						ShouldDelete: true,
						Reason:       fmt.Sprintf("InitContainer %s waiting with %s (restarts: %d)", ics.Name, reason, ics.RestartCount),
						Code:         metrics.ReasonCrashOrImage,
						Age:          podAge,
					}
				}
			}
		}
	}

	// 6. Running but Ready Condition is False
	if pod.Status.Phase == corev1.PodRunning {
		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionFalse {
				unreadyDuration := now.Sub(cond.LastTransitionTime.Time)
				if unreadyDuration >= c.cfg.ThresholdDuration {
					return Decision{
						ShouldDelete: true,
						Reason:       fmt.Sprintf("Running but unready for %s (Reason: %s)", unreadyDuration.Round(time.Second), cond.Reason),
						Code:         metrics.ReasonUnready,
						Age:          unreadyDuration,
					}
				}
			}
		}
	}

	return Decision{ShouldDelete: false, Reason: "Pod is healthy or within acceptable thresholds"}
}

func (c *Cleaner) isOptedOut(pod *corev1.Pod) bool {
	if pod.Annotations != nil {
		if val, exists := pod.Annotations[c.cfg.IgnoreAnnotation]; exists && strings.EqualFold(val, "true") {
			return true
		}
	}
	if pod.Labels != nil {
		if val, exists := pod.Labels[c.cfg.IgnoreAnnotation]; exists && strings.EqualFold(val, "true") {
			return true
		}
	}
	return false
}

func isCrashOrImageError(reason string) bool {
	switch reason {
	case "CrashLoopBackOff", "ImagePullBackOff", "ErrImagePull", "CreateContainerConfigError", "CreateContainerError":
		return true
	}
	return false
}

// NodePressureInfo contains details of a node under sustained resource pressure.
type NodePressureInfo struct {
	NodeName      string
	Conditions    []string
	PressureSince time.Time
	Duration      time.Duration
}

// Pressure is one active sustained pressure condition on a node.
type Pressure struct {
	// Code is the low-cardinality metrics label (e.g. "memory_pressure").
	Code string
	// Detail is the human-readable form, including how long it has been active.
	Detail string
}

// pressureCodes maps Kubernetes node condition types to their metrics labels.
var pressureCodes = map[corev1.NodeConditionType]string{
	corev1.NodeMemoryPressure: metrics.ConditionMemoryPressure,
	corev1.NodeDiskPressure:   metrics.ConditionDiskPressure,
	corev1.NodePIDPressure:    metrics.ConditionPIDPressure,
}

// EvaluateNode checks if a node is experiencing sustained pressure beyond configured duration.
func (c *Cleaner) EvaluateNode(node *corev1.Node) (bool, []Pressure, time.Duration) {
	var activePressures []Pressure
	var longestDuration time.Duration

	// Iterated as a slice, not over pressureCodes, to keep the reported order stable.
	pressureConditionTypes := []corev1.NodeConditionType{
		corev1.NodeMemoryPressure,
		corev1.NodeDiskPressure,
		corev1.NodePIDPressure,
	}

	now := c.now()

	for _, cond := range node.Status.Conditions {
		// Check standard pressure conditions (MemoryPressure, DiskPressure, PIDPressure == True)
		for _, pt := range pressureConditionTypes {
			if cond.Type == pt && cond.Status == corev1.ConditionTrue {
				duration := now.Sub(cond.LastTransitionTime.Time)
				if duration >= c.cfg.NodePressureDuration {
					activePressures = append(activePressures, Pressure{
						Code:   pressureCodes[pt],
						Detail: fmt.Sprintf("%s(active for %s)", cond.Type, duration.Round(time.Second)),
					})
					if duration > longestDuration {
						longestDuration = duration
					}
				}
			}
		}

		// Check Node Ready condition (Ready == False or Ready == Unknown)
		if cond.Type == corev1.NodeReady && cond.Status != corev1.ConditionTrue {
			duration := now.Sub(cond.LastTransitionTime.Time)
			if duration >= c.cfg.NodePressureDuration {
				activePressures = append(activePressures, Pressure{
					Code:   metrics.ConditionNotReady,
					Detail: fmt.Sprintf("NodeNotReady[status=%s](active for %s)", cond.Status, duration.Round(time.Second)),
				})
				if duration > longestDuration {
					longestDuration = duration
				}
			}
		}
	}

	return len(activePressures) > 0, activePressures, longestDuration
}

// pressureDetails renders pressures for log output.
func pressureDetails(pressures []Pressure) string {
	details := make([]string, 0, len(pressures))
	for _, p := range pressures {
		details = append(details, p.Detail)
	}
	return strings.Join(details, ", ")
}

// pressureCodeList extracts the metrics labels from pressures.
func pressureCodeList(pressures []Pressure) []string {
	codes := make([]string, 0, len(pressures))
	for _, p := range pressures {
		codes = append(codes, p.Code)
	}
	return codes
}

// EvacuateHighPressureNodes discovers nodes under sustained pressure, cordons them, and cleans up pods.
func (c *Cleaner) EvacuateHighPressureNodes(ctx context.Context) error {
	slog.Info("Evaluating cluster nodes for sustained pressure",
		"nodePressureDuration", c.cfg.NodePressureDuration.String(),
		"forceDelete", c.cfg.NodePressureForceDelete,
		"cordon", c.cfg.NodePressureCordon,
	)

	nodes, err := c.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list cluster nodes: %w", err)
	}

	pressuredNodesCount := 0
	totalPodsEvacuated := 0

	for i := range nodes.Items {
		node := &nodes.Items[i]
		c.metrics.AddNodeEvaluated()
		isPressured, pressures, maxDuration := c.EvaluateNode(node)
		if !isPressured {
			continue
		}

		pressuredNodesCount++
		c.metrics.AddNodePressured(pressureCodeList(pressures))
		slog.Warn("Node identified under sustained resource pressure",
			"node", node.Name,
			"pressures", pressureDetails(pressures),
			"duration", maxDuration.Round(time.Second).String(),
			"dryRun", c.cfg.DryRun,
		)

		// Cordon node if enabled
		if c.cfg.NodePressureCordon && !node.Spec.Unschedulable {
			if c.cfg.DryRun {
				c.metrics.AddNodeCordoned()
				slog.Info("Dry-run: would cordon node", "node", node.Name)
			} else {
				nodeCopy := node.DeepCopy()
				nodeCopy.Spec.Unschedulable = true
				if _, err := c.client.CoreV1().Nodes().Update(ctx, nodeCopy, metav1.UpdateOptions{}); err != nil {
					slog.Error("Failed to cordon node", "node", node.Name, "error", err)
				} else {
					c.metrics.AddNodeCordoned()
					slog.Info("Successfully cordoned pressured node", "node", node.Name)
				}
			}
		}

		// Find and evict/delete pods running on this node
		pods, err := c.client.CoreV1().Pods("").List(ctx, metav1.ListOptions{
			FieldSelector: fmt.Sprintf("spec.nodeName=%s", node.Name),
		})
		if err != nil {
			slog.Error("Failed to list pods on pressured node", "node", node.Name, "error", err)
			continue
		}

		for j := range pods.Items {
			pod := &pods.Items[j]

			// Explicit check for nodeName matching (fake client doesn't filter by fieldSelector automatically)
			if pod.Spec.NodeName != node.Name {
				continue
			}
			// Skip excluded namespaces (e.g. kube-system)
			if c.isNamespaceExcluded(pod.Namespace) {
				slog.Debug("Skipping pod on pressured node in excluded namespace",
					"namespace", pod.Namespace,
					"pod", pod.Name,
					"node", node.Name,
				)
				continue
			}

			// Skip pods marked with ignore annotation/label
			if c.isOptedOut(pod) {
				slog.Debug("Skipping opted-out pod on pressured node",
					"namespace", pod.Namespace,
					"pod", pod.Name,
					"node", node.Name,
				)
				continue
			}

			// Skip DaemonSet pods (they cannot be rescheduled elsewhere)
			if isDaemonSetPod(pod) {
				slog.Debug("Skipping DaemonSet pod on pressured node",
					"namespace", pod.Namespace,
					"pod", pod.Name,
					"node", node.Name,
				)
				continue
			}

			slog.Warn("Evacuating pod from pressured node",
				"namespace", pod.Namespace,
				"pod", pod.Name,
				"node", node.Name,
				"pressures", pressureDetails(pressures),
				"force", c.cfg.NodePressureForceDelete,
				"dryRun", c.cfg.DryRun,
			)

			if c.cfg.DryRun {
				totalPodsEvacuated++
				c.metrics.AddPodEvacuated(pod.Namespace)
				continue
			}

			deleteOpts := metav1.DeleteOptions{}
			if c.cfg.NodePressureForceDelete {
				var zero int64 = 0
				deleteOpts.GracePeriodSeconds = &zero
			}

			if err := c.client.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, deleteOpts); err != nil {
				c.metrics.AddPodDeleteError(pod.Namespace)
				slog.Error("Failed to evacuate pod from pressured node",
					"namespace", pod.Namespace,
					"pod", pod.Name,
					"node", node.Name,
					"error", err,
				)
			} else {
				totalPodsEvacuated++
				c.metrics.AddPodEvacuated(pod.Namespace)
				slog.Info("Successfully evacuated pod from pressured node",
					"namespace", pod.Namespace,
					"pod", pod.Name,
					"node", node.Name,
					"force", c.cfg.NodePressureForceDelete,
				)
			}
		}
	}

	slog.Info("Node pressure evaluation completed",
		"pressuredNodesCount", pressuredNodesCount,
		"totalPodsEvacuated", totalPodsEvacuated,
		"dryRun", c.cfg.DryRun,
	)

	return nil
}

func isDaemonSetPod(pod *corev1.Pod) bool {
	for _, ownerRef := range pod.OwnerReferences {
		if ownerRef.Kind == "DaemonSet" {
			return true
		}
	}
	return false
}
