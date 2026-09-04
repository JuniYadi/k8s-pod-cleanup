package metrics

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// capture starts a Pushgateway stand-in and returns the single request it received.
type capture struct {
	method  string
	path    string
	body    string
	user    string
	pass    string
	hasAuth bool
}

func pushTo(t *testing.T, r *Recorder, opts PushOptions, status int) *capture {
	t.Helper()

	got := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		got.method = req.Method
		got.path = req.URL.Path
		got.body = string(body)
		got.user, got.pass, got.hasAuth = req.BasicAuth()
		w.WriteHeader(status)
	}))
	defer srv.Close()

	opts.URL = srv.URL
	err := r.Push(context.Background(), opts)

	if status >= 400 && err == nil {
		t.Fatalf("expected an error for HTTP %d, got nil", status)
	}
	if status < 400 && err != nil {
		t.Fatalf("unexpected push error: %v", err)
	}
	return got
}

func TestPushSendsRecordedMetrics(t *testing.T) {
	r := NewRecorder(false)
	r.AddPodEvaluated()
	r.AddPodEvaluated()
	r.AddPodDeleted("prod", ReasonCrashOrImage)
	r.AddPodDeleted("prod", ReasonCrashOrImage)
	r.AddPodDeleted("staging", ReasonPendingStalled)
	r.AddPodDeleteError("prod")
	r.Finish(1500*time.Millisecond, true)

	got := pushTo(t, r, PushOptions{Job: "k8s-pod-cleanup", Component: "pod-cleanup"}, http.StatusAccepted)

	// PUT replaces the whole group. POST (client_golang's Add) would merge and
	// leave series from vanished namespaces behind forever.
	if got.method != http.MethodPut {
		t.Errorf("expected PUT, got %s", got.method)
	}

	wantPath := "/metrics/job/k8s-pod-cleanup/component/pod-cleanup"
	if got.path != wantPath {
		t.Errorf("expected path %q, got %q", wantPath, got.path)
	}

	wantLines := []string{
		`k8s_pod_cleanup_pods_evaluated 2`,
		`k8s_pod_cleanup_pods_deleted{namespace="prod",reason="crashloop_or_image_error"} 2`,
		`k8s_pod_cleanup_pods_deleted{namespace="staging",reason="pending_stalled"} 1`,
		`k8s_pod_cleanup_pod_delete_errors{namespace="prod"} 1`,
		`k8s_pod_cleanup_duration_seconds 1.5`,
		`k8s_pod_cleanup_run_success 1`,
		`k8s_pod_cleanup_dry_run 0`,
	}
	for _, want := range wantLines {
		if !strings.Contains(got.body, want) {
			t.Errorf("pushed body missing %q\ngot:\n%s", want, got.body)
		}
	}

	if !strings.Contains(got.body, "k8s_pod_cleanup_last_run_timestamp_seconds ") {
		t.Errorf("pushed body missing last run timestamp\ngot:\n%s", got.body)
	}

	// The default registry's go_*/process_* collectors describe a process that
	// has already exited by scrape time, so they must not be pushed.
	if strings.Contains(got.body, "go_goroutines") || strings.Contains(got.body, "process_cpu") {
		t.Errorf("pushed body contains default collectors\ngot:\n%s", got.body)
	}
}

func TestPushNodeMetrics(t *testing.T) {
	r := NewRecorder(true)
	r.AddNodeEvaluated()
	r.AddNodeEvaluated()
	// One node under two conditions: counted once as a node, once per condition.
	r.AddNodePressured([]string{ConditionMemoryPressure, ConditionDiskPressure})
	r.AddNodeCordoned()
	r.AddPodEvacuated("prod")
	r.Finish(2*time.Second, false)

	got := pushTo(t, r, PushOptions{Job: "k8s-pod-cleanup", Component: "node-pressure"}, http.StatusAccepted)

	if got.path != "/metrics/job/k8s-pod-cleanup/component/node-pressure" {
		t.Errorf("unexpected path %q", got.path)
	}

	wantLines := []string{
		`k8s_pod_cleanup_nodes_evaluated 2`,
		`k8s_pod_cleanup_nodes_pressured 1`,
		`k8s_pod_cleanup_node_pressure_conditions{condition="memory_pressure"} 1`,
		`k8s_pod_cleanup_node_pressure_conditions{condition="disk_pressure"} 1`,
		`k8s_pod_cleanup_nodes_cordoned 1`,
		`k8s_pod_cleanup_pods_evacuated{namespace="prod"} 1`,
		`k8s_pod_cleanup_run_success 0`,
		`k8s_pod_cleanup_dry_run 1`,
	}
	for _, want := range wantLines {
		if !strings.Contains(got.body, want) {
			t.Errorf("pushed body missing %q\ngot:\n%s", want, got.body)
		}
	}
}

func TestPushBasicAuth(t *testing.T) {
	r := NewRecorder(false)
	r.Finish(time.Second, true)

	opts := PushOptions{Job: "job", Component: "pod-cleanup", Username: "user", Password: "secret"}
	got := pushTo(t, r, opts, http.StatusAccepted)

	if !got.hasAuth {
		t.Fatal("expected basic auth to be sent")
	}
	if got.user != "user" || got.pass != "secret" {
		t.Errorf("expected user/secret, got %s/%s", got.user, got.pass)
	}
}

func TestPushWithoutCredentialsSendsNoAuth(t *testing.T) {
	r := NewRecorder(false)
	r.Finish(time.Second, true)

	got := pushTo(t, r, PushOptions{Job: "job", Component: "pod-cleanup"}, http.StatusAccepted)

	if got.hasAuth {
		t.Error("expected no basic auth header when no username is configured")
	}
}

func TestPushReturnsErrorOnGatewayFailure(t *testing.T) {
	r := NewRecorder(false)
	r.Finish(time.Second, true)

	// pushTo fails the test itself if no error comes back.
	pushTo(t, r, PushOptions{Job: "job", Component: "pod-cleanup"}, http.StatusInternalServerError)
}

func TestPushReturnsErrorWhenGatewayUnreachable(t *testing.T) {
	r := NewRecorder(false)
	r.Finish(time.Second, true)

	opts := PushOptions{URL: "http://127.0.0.1:1", Job: "job", Component: "pod-cleanup"}
	if err := r.Push(context.Background(), opts); err == nil {
		t.Error("expected an error when the Pushgateway is unreachable")
	}
}
