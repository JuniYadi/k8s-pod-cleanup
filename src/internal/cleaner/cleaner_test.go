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
			name: "ImagePullBackOff Container",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "image-pull-pod",
					Namespace:         "default",
					CreationTimestamp: metav1.Time{Time: now.Add(-10 * time.Minute)},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:         "app",
							RestartCount: 5,
							State: corev1.ContainerState{
								Waiting: &corev1.ContainerStateWaiting{
									Reason: "ImagePullBackOff",
								},
							},
						},
					},
				},
			},
			shouldDelete: true,
		},
		{
			name: "CreateContainerConfigError Container",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "config-err-pod",
					Namespace:         "default",
					CreationTimestamp: metav1.Time{Time: now.Add(-10 * time.Minute)},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:         "app",
							RestartCount: 5,
							State: corev1.ContainerState{
								Waiting: &corev1.ContainerStateWaiting{
									Reason: "CreateContainerConfigError",
								},
							},
						},
					},
				},
			},
			shouldDelete: true,
		},
		{
			name: "CreateContainerError Container",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "create-err-pod",
					Namespace:         "default",
					CreationTimestamp: metav1.Time{Time: now.Add(-10 * time.Minute)},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:         "app",
							RestartCount: 5,
							State: corev1.ContainerState{
								Waiting: &corev1.ContainerStateWaiting{
									Reason: "CreateContainerError",
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
			name: "Running Unready Pod Fresh Transition (Within Threshold)",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "unready-fresh-pod",
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
							LastTransitionTime: metav1.Time{Time: now.Add(-1 * time.Minute)},
						},
					},
				},
			},
			shouldDelete: false,
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
			name: "Opted-out Pod with Label",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "opted-out-label-pod",
					Namespace:         "default",
					CreationTimestamp: metav1.Time{Time: now.Add(-30 * time.Minute)},
					Labels: map[string]string{
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
		{
			name: "Terminating Pod within Grace Period",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "terminating-fresh-pod",
					Namespace:         "default",
					CreationTimestamp: metav1.Time{Time: now.Add(-30 * time.Minute)},
					DeletionTimestamp: &metav1.Time{Time: now.Add(-1 * time.Minute)},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			},
			shouldDelete: false,
		},
		{
			name: "Failed Pod within Threshold Duration",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "fresh-failed-pod",
					Namespace:         "default",
					CreationTimestamp: metav1.Time{Time: now.Add(-1 * time.Minute)},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodFailed,
				},
			},
			shouldDelete: false,
		},
		{
			name: "Pending Pod within Threshold Duration",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "fresh-pending-pod",
					Namespace:         "default",
					CreationTimestamp: metav1.Time{Time: now.Add(-1 * time.Minute)},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
				},
			},
			shouldDelete: false,
		},
		{
			name: "InitContainer Waiting with ErrImagePull",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "init-crash-pod",
					Namespace:         "default",
					CreationTimestamp: metav1.Time{Time: now.Add(-10 * time.Minute)},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
					InitContainerStatuses: []corev1.ContainerStatus{
						{
							Name:         "init-app",
							RestartCount: 4,
							State: corev1.ContainerState{
								Waiting: &corev1.ContainerStateWaiting{
									Reason: "ErrImagePull",
								},
							},
						},
					},
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

func TestCleanerRunGracefulDelete(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	cfg := &config.Config{
		ThresholdDuration: 5 * time.Minute,
		DryRun:            false,
		Force:             false, // Standard graceful deletion branch
	}

	client := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "bad-pod-graceful",
				Namespace:         "default",
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
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = client.CoreV1().Pods("default").Get(context.Background(), "bad-pod-graceful", metav1.GetOptions{})
	if err == nil {
		t.Errorf("expected pod to be deleted")
	}
}

func TestCleanerRunDryRunAndExplicitNamespaces(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	cfg := &config.Config{
		ThresholdDuration:  5 * time.Minute,
		RestartThreshold:   3,
		DryRun:             true,
		Namespaces:         []string{"target-ns", "kube-system"},
		ExcludedNamespaces: []string{"kube-system"},
	}

	client := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "target-ns"}},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "bad-pod-in-target",
				Namespace:         "target-ns",
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
		t.Fatalf("unexpected error: %v", err)
	}

	// Because DryRun is true, pod should still exist
	_, err = client.CoreV1().Pods("target-ns").Get(context.Background(), "bad-pod-in-target", metav1.GetOptions{})
	if err != nil {
		t.Errorf("expected pod to remain in dry-run mode, got err: %v", err)
	}
}

func TestEvaluateNode(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	cfg := &config.Config{
		NodePressureDuration: 1 * time.Minute,
	}

	cleaner := NewCleaner(fake.NewSimpleClientset(), cfg)
	cleaner.SetNow(func() time.Time { return now })

	tests := []struct {
		name               string
		node               *corev1.Node
		expectedPressured  bool
		expectedPressCount int
	}{
		{
			name: "Healthy Node - No pressure",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "healthy-node"},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:               corev1.NodeReady,
							Status:             corev1.ConditionTrue,
							LastTransitionTime: metav1.Time{Time: now.Add(-10 * time.Minute)},
						},
						{
							Type:               corev1.NodeMemoryPressure,
							Status:             corev1.ConditionFalse,
							LastTransitionTime: metav1.Time{Time: now.Add(-10 * time.Minute)},
						},
					},
				},
			},
			expectedPressured:  false,
			expectedPressCount: 0,
		},
		{
			name: "MemoryPressure active for 30s (< 1m threshold)",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "brief-pressure-node"},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:               corev1.NodeReady,
							Status:             corev1.ConditionTrue,
							LastTransitionTime: metav1.Time{Time: now.Add(-10 * time.Minute)},
						},
						{
							Type:               corev1.NodeMemoryPressure,
							Status:             corev1.ConditionTrue,
							LastTransitionTime: metav1.Time{Time: now.Add(-30 * time.Second)},
						},
					},
				},
			},
			expectedPressured:  false,
			expectedPressCount: 0,
		},
		{
			name: "MemoryPressure active for 2m (>= 1m threshold)",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "mem-pressured-node"},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:               corev1.NodeReady,
							Status:             corev1.ConditionTrue,
							LastTransitionTime: metav1.Time{Time: now.Add(-10 * time.Minute)},
						},
						{
							Type:               corev1.NodeMemoryPressure,
							Status:             corev1.ConditionTrue,
							LastTransitionTime: metav1.Time{Time: now.Add(-2 * time.Minute)},
						},
					},
				},
			},
			expectedPressured:  true,
			expectedPressCount: 1,
		},
		{
			name: "Node NotReady for 5m",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "not-ready-node"},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:               corev1.NodeReady,
							Status:             corev1.ConditionFalse,
							LastTransitionTime: metav1.Time{Time: now.Add(-5 * time.Minute)},
						},
					},
				},
			},
			expectedPressured:  true,
			expectedPressCount: 1,
		},
		{
			name: "Multiple pressures: DiskPressure and PIDPressure for > 1m",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "multi-pressure-node"},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:               corev1.NodeDiskPressure,
							Status:             corev1.ConditionTrue,
							LastTransitionTime: metav1.Time{Time: now.Add(-3 * time.Minute)},
						},
						{
							Type:               corev1.NodePIDPressure,
							Status:             corev1.ConditionTrue,
							LastTransitionTime: metav1.Time{Time: now.Add(-2 * time.Minute)},
						},
					},
				},
			},
			expectedPressured:  true,
			expectedPressCount: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isPressured, pressures, _ := cleaner.EvaluateNode(tc.node)
			if isPressured != tc.expectedPressured {
				t.Errorf("expected isPressured %v, got %v", tc.expectedPressured, isPressured)
			}
			if len(pressures) != tc.expectedPressCount {
				t.Errorf("expected %d pressures, got %d (%v)", tc.expectedPressCount, len(pressures), pressures)
			}
		})
	}
}

func TestEvacuateHighPressureNodes(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	cfg := &config.Config{
		EnableNodePressureEviction: true,
		NodePressureDuration:       1 * time.Minute,
		NodePressureForceDelete:    true,
		NodePressureCordon:         true,
		IgnoreAnnotation:           "cleanup.k8s.io/ignore",
		ExcludedNamespaces:         []string{"kube-system"},
	}

	pressuredNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "pressured-node"},
		Spec:       corev1.NodeSpec{Unschedulable: false},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:               corev1.NodeMemoryPressure,
					Status:             corev1.ConditionTrue,
					LastTransitionTime: metav1.Time{Time: now.Add(-2 * time.Minute)},
				},
			},
		},
	}

	healthyNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "healthy-node"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:               corev1.NodeReady,
					Status:             corev1.ConditionTrue,
					LastTransitionTime: metav1.Time{Time: now.Add(-10 * time.Minute)},
				},
			},
		},
	}

	podOnPressuredNode := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app-pod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			NodeName: "pressured-node",
		},
	}

	podOnHealthyNode := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "healthy-app-pod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			NodeName: "healthy-node",
		},
	}

	daemonSetPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ds-pod",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind: "DaemonSet",
					Name: "logging-agent",
				},
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "pressured-node",
		},
	}

	optedOutPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "opted-out-pod",
			Namespace: "default",
			Annotations: map[string]string{
				"cleanup.k8s.io/ignore": "true",
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "pressured-node",
		},
	}

	systemPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "coredns-pod",
			Namespace: "kube-system",
		},
		Spec: corev1.PodSpec{
			NodeName: "pressured-node",
		},
	}

	client := fake.NewSimpleClientset(
		pressuredNode,
		healthyNode,
		podOnPressuredNode,
		podOnHealthyNode,
		daemonSetPod,
		optedOutPod,
		systemPod,
	)

	cleaner := NewCleaner(client, cfg)
	cleaner.SetNow(func() time.Time { return now })

	err := cleaner.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error during run: %v", err)
	}

	// 1. Node should be cordoned (Unschedulable = true)
	updatedNode, err := client.CoreV1().Nodes().Get(context.Background(), "pressured-node", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get pressured node: %v", err)
	}
	if !updatedNode.Spec.Unschedulable {
		t.Errorf("expected pressured node to be cordoned (Unschedulable=true)")
	}

	// 2. App pod on pressured node should be deleted
	_, err = client.CoreV1().Pods("default").Get(context.Background(), "app-pod", metav1.GetOptions{})
	if err == nil {
		t.Errorf("expected app-pod on pressured node to be deleted")
	}

	// 3. Healthy pod on healthy node should remain
	_, err = client.CoreV1().Pods("default").Get(context.Background(), "healthy-app-pod", metav1.GetOptions{})
	if err != nil {
		t.Errorf("expected healthy-app-pod to remain, got err: %v", err)
	}

	// 4. DaemonSet pod on pressured node should remain
	_, err = client.CoreV1().Pods("default").Get(context.Background(), "ds-pod", metav1.GetOptions{})
	if err != nil {
		t.Errorf("expected ds-pod (DaemonSet) to remain, got err: %v", err)
	}

	// 5. Opted-out pod on pressured node should remain
	_, err = client.CoreV1().Pods("default").Get(context.Background(), "opted-out-pod", metav1.GetOptions{})
	if err != nil {
		t.Errorf("expected opted-out-pod to remain, got err: %v", err)
	}

	// 6. System pod in kube-system should remain
	_, err = client.CoreV1().Pods("kube-system").Get(context.Background(), "coredns-pod", metav1.GetOptions{})
	if err != nil {
		t.Errorf("expected coredns-pod in kube-system to remain, got err: %v", err)
	}
}

func TestEvacuateHighPressureNodesDryRun(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	cfg := &config.Config{
		EnableNodePressureEviction: true,
		NodePressureDuration:       1 * time.Minute,
		NodePressureForceDelete:    false,
		NodePressureCordon:         true,
		DryRun:                     true,
	}

	pressuredNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "pressured-node-dryrun"},
		Spec:       corev1.NodeSpec{Unschedulable: false},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:               corev1.NodeDiskPressure,
					Status:             corev1.ConditionTrue,
					LastTransitionTime: metav1.Time{Time: now.Add(-2 * time.Minute)},
				},
			},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dryrun-pod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			NodeName: "pressured-node-dryrun",
		},
	}

	client := fake.NewSimpleClientset(pressuredNode, pod)
	cleaner := NewCleaner(client, cfg)
	cleaner.SetNow(func() time.Time { return now })

	err := cleaner.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Node should NOT be modified in dry-run
	node, _ := client.CoreV1().Nodes().Get(context.Background(), "pressured-node-dryrun", metav1.GetOptions{})
	if node.Spec.Unschedulable {
		t.Errorf("expected node to not be cordoned in dry-run")
	}

	// Pod should still exist in dry-run
	_, err = client.CoreV1().Pods("default").Get(context.Background(), "dryrun-pod", metav1.GetOptions{})
	if err != nil {
		t.Errorf("expected pod to remain in dry-run, got err: %v", err)
	}
}

func TestIsCrashOrImageError(t *testing.T) {
	errors := []string{"CrashLoopBackOff", "ImagePullBackOff", "ErrImagePull", "CreateContainerConfigError", "CreateContainerError"}
	for _, e := range errors {
		if !isCrashOrImageError(e) {
			t.Errorf("expected %s to be recognized as crash/image error", e)
		}
	}
	if isCrashOrImageError("RandomOtherReason") {
		t.Errorf("expected RandomOtherReason to not be recognized")
	}
}
