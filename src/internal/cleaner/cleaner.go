package cleaner

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/juniyadi/k8s-pod-cleanup/internal/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Decision represents the evaluation result for a pod.
type Decision struct {
	ShouldDelete bool
	Reason       string
	Age          time.Duration
}

// Cleaner handles pod discovery, evaluation, and deletion.
type Cleaner struct {
	client kubernetes.Interface
	cfg    *config.Config
	now    func() time.Time
}

// NewCleaner creates a new Cleaner instance.
func NewCleaner(client kubernetes.Interface, cfg *config.Config) *Cleaner {
	return &Cleaner{
		client: client,
		cfg:    cfg,
		now:    time.Now,
	}
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
	)

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
				continue
			}

			deleteOpts := metav1.DeleteOptions{}
			if c.cfg.Force {
				var zero int64 = 0
				deleteOpts.GracePeriodSeconds = &zero
			}

			if err := c.client.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, deleteOpts); err != nil {
				slog.Error("Failed to delete pod",
					"namespace", pod.Namespace,
					"pod", pod.Name,
					"error", err,
				)
			} else {
				totalDeleted++
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
	default:
		return false
	}
}
