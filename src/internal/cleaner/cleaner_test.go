package cleaner

import (
	"context"
	"testing"
	"time"

	"github.com/juniyadi/k8s-pod-cleanup/internal/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestEvaluatePod(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	cfg := &config.Config{
		ThresholdDuration:    5 * time.Minute,
		TerminationThreshold: 5 * time.Minute,
		RestartThreshold:     3,
		IgnoreAnnotation:     "cleanup.k8s.io/ignore",
	}

	cleaner := NewCleaner(fake.NewSimpleClientset(), cfg)
	cleaner.SetNow(func() time.Time { return now })

	tests := []struct {
		name         string
		pod          *corev1.Pod
		shouldDelete bool
	}{
		{
			name: "Healthy Running Pod",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "healthy-pod",
					Namespace:         "default",
					CreationTimestamp: metav1.Time{Time: now.Add(-10 * time.Minute)},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			shouldDelete: false,
		},
		{
			name: "CrashLoopBackOff Pod Exceeding Restart and Duration",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "crashloop-pod",
					Namespace:         "default",
					CreationTimestamp: metav1.Time{Time: now.Add(-10 * time.Minute)},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:         "app",
							RestartCount: 4,
							State: corev1.ContainerState{
								Waiting: &corev1.ContainerStateWaiting{
									Reason: "CrashLoopBackOff",
								},
							},
						},
					},
				},
			},
			shouldDelete: true,
		},
		{
			name: "CrashLoopBackOff Pod with Low Restarts (Below Threshold)",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "crashloop-low-restarts",
					Namespace:         "default",
					CreationTimestamp: metav1.Time{Time: now.Add(-10 * time.Minute)},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:         "app",
							RestartCount: 2,
							State: corev1.ContainerState{
								Waiting: &corev1.ContainerStateWaiting{
									Reason: "CrashLoopBackOff",
								},
							},
						},
					},
				},
			},
			shouldDelete: false,
		},
		{
			name: "Evicted Pod Older than Threshold",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "evicted-pod",
					Namespace:         "default",
					CreationTimestamp: metav1.Time{Time: now.Add(-15 * time.Minute)},
				},
				Status: corev1.PodStatus{
					Phase:  corev1.PodFailed,
					Reason: "Evicted",
				},
			},
			shouldDelete: true,
		},
		{
			name: "Pending Pod Stuck for Long Time",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "pending-stuck-pod",
					Namespace:         "default",
					CreationTimestamp: metav1.Time{Time: now.Add(-20 * time.Minute)},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
				},
			},
			shouldDelete: true,
		},
		{
			name: "Running Unready Pod Longer than Threshold",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "unready-running-pod",
					Namespace:         "default",
					CreationTimestamp: metav1.Time{Time: now.Add(-30 * time.Minute)},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					Conditions: []corev1.PodCondition{
						{
							Type:               corev1.PodReady,
							Status:             corev1.ConditionFalse,
							Reason:             "ContainersNotReady",
							LastTransitionTime: metav1.Time{Time: now.Add(-10 * time.Minute)},
						},
					},
				},
			},
			shouldDelete: true,
		},
		{
			name: "Opted-out Pod with Annotation",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "opted-out-pod",
					Namespace:         "default",
					CreationTimestamp: metav1.Time{Time: now.Add(-30 * time.Minute)},
					Annotations: map[string]string{
						"cleanup.k8s.io/ignore": "true",
					},
				},
				Status: corev1.PodStatus{
					Phase:  corev1.PodFailed,
					Reason: "Evicted",
				},
			},
			shouldDelete: false,
		},
		{
			name: "Terminating Pod Stuck Beyond Threshold",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "terminating-pod",
					Namespace:         "default",
					CreationTimestamp: metav1.Time{Time: now.Add(-30 * time.Minute)},
					DeletionTimestamp: &metav1.Time{Time: now.Add(-10 * time.Minute)},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			},
			shouldDelete: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			decision := cleaner.EvaluatePod(tc.pod)
			if decision.ShouldDelete != tc.shouldDelete {
				t.Fatalf("expected shouldDelete=%v, got %v (reason: %s)", tc.shouldDelete, decision.ShouldDelete, decision.Reason)
			}
		})
	}
}

func TestCleanerRun(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	cfg := &config.Config{
		ThresholdDuration:    5 * time.Minute,
		TerminationThreshold: 5 * time.Minute,
		RestartThreshold:     3,
		DryRun:               false,
		Force:                true,
		ExcludedNamespaces:   []string{"kube-system"},
	}

	client := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kube-system"}},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "bad-pod",
				Namespace:         "default",
				CreationTimestamp: metav1.Time{Time: now.Add(-10 * time.Minute)},
			},
			Status: corev1.PodStatus{
				Phase:  corev1.PodFailed,
				Reason: "Evicted",
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "system-bad-pod",
				Namespace:         "kube-system",
				CreationTimestamp: metav1.Time{Time: now.Add(-10 * time.Minute)},
			},
			Status: corev1.PodStatus{
				Phase:  corev1.PodFailed,
				Reason: "Evicted",
			},
		},
	)

	cleaner := NewCleaner(client, cfg)
	cleaner.SetNow(func() time.Time { return now })

	err := cleaner.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error during cleaner run: %v", err)
	}

	// bad-pod in default should be deleted
	_, err = client.CoreV1().Pods("default").Get(context.Background(), "bad-pod", metav1.GetOptions{})
	if err == nil {
		t.Errorf("expected bad-pod in default namespace to be deleted")
	}

	// system-bad-pod in kube-system should remain
	_, err = client.CoreV1().Pods("kube-system").Get(context.Background(), "system-bad-pod", metav1.GetOptions{})
	if err != nil {
		t.Errorf("expected system-bad-pod in kube-system to not be deleted, got err: %v", err)
	}
}
