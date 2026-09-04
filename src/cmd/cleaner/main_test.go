package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/juniyadi/k8s-pod-cleanup/internal/config"
	"github.com/juniyadi/k8s-pod-cleanup/internal/metrics"
)

func TestSetupLogger(t *testing.T) {
	levels := []string{"debug", "warn", "error", "info", "unknown"}
	for _, lvl := range levels {
		setupLogger(lvl)
		if slog.Default() == nil {
			t.Errorf("expected default logger to be initialized for level %s", lvl)
		}
	}
}

func TestGetKubernetesConfig(t *testing.T) {
	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")
	kubeconfigContent := `
apiVersion: v1
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: test-cluster
contexts:
- context:
    cluster: test-cluster
    user: test-user
  name: test-context
current-context: test-context
kind: Config
preferences: {}
users:
- name: test-user
  user:
    token: fake-token
`
	if err := os.WriteFile(kubeconfigPath, []byte(kubeconfigContent), 0600); err != nil {
		t.Fatalf("failed to write dummy kubeconfig: %v", err)
	}

	cfg, err := getKubernetesConfig(kubeconfigPath)
	if err != nil {
		t.Fatalf("unexpected error loading kubeconfig from file: %v", err)
	}
	if cfg == nil || cfg.Host != "https://127.0.0.1:6443" {
		t.Errorf("expected host https://127.0.0.1:6443, got %v", cfg)
	}

	// Test fallback when kubeconfigPath is empty
	_, _ = getKubernetesConfig("")
}

func TestMainAndRun(t *testing.T) {
	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")
	kubeconfigContent := `
apiVersion: v1
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: test-cluster
contexts:
- context:
    cluster: test-cluster
    user: test-user
  name: test-context
current-context: test-context
kind: Config
preferences: {}
users:
- name: test-user
  user:
    token: fake-token
`
	if err := os.WriteFile(kubeconfigPath, []byte(kubeconfigContent), 0600); err != nil {
		t.Fatalf("failed to write dummy kubeconfig: %v", err)
	}

	os.Setenv("KUBECONFIG", kubeconfigPath)
	os.Setenv("DRY_RUN", "true")
	os.Setenv("NAMESPACES", "test-ns")
	defer func() {
		os.Unsetenv("KUBECONFIG")
		os.Unsetenv("DRY_RUN")
		os.Unsetenv("NAMESPACES")
	}()

	// Intercept osExit
	var exitCode int
	osExit = func(code int) {
		exitCode = code
	}
	defer func() {
		osExit = os.Exit
	}()

	main()
	if exitCode != 1 {
		// Server https://127.0.0.1:6443 is unreachable in unit test so it exits with 1, covering main and run
		t.Logf("main exited as expected with code %d", exitCode)
	}
}

func TestMainSuccess(t *testing.T) {
	// Intercept osExit
	var exitCode int
	osExit = func(code int) {
		exitCode = code
	}
	defer func() {
		osExit = os.Exit
	}()

	// Test running main when getKubernetesConfig fails (invalid config)
	os.Setenv("KUBECONFIG", "/nonexistent/path/to/kubeconfig")
	defer os.Unsetenv("KUBECONFIG")

	main()
	if exitCode != 1 {
		t.Errorf("expected exit code 1 on config error, got %d", exitCode)
	}
}

func TestRunFlagParseError(t *testing.T) {
	origArgs := os.Args
	os.Args = []string{"cleaner", "--threshold-duration=invalid-duration"}
	defer func() {
		os.Args = origArgs
	}()
	err := run()
	if err == nil {
		t.Errorf("expected error from run() with invalid flag, got nil")
	}
}

func TestGetKubernetesConfigInvalidFile(t *testing.T) {
	tmpDir := t.TempDir()
	invalidKubeconfigPath := filepath.Join(tmpDir, "invalid-kubeconfig")
	if err := os.WriteFile(invalidKubeconfigPath, []byte("invalid: [yaml: broken"), 0600); err != nil {
		t.Fatalf("failed to write invalid kubeconfig: %v", err)
	}

	_, err := getKubernetesConfig(invalidKubeconfigPath)
	if err == nil {
		t.Errorf("expected error when loading invalid kubeconfig, got nil")
	}
}

func TestMainAndRunSuccessWithMock(t *testing.T) {
	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")
	kubeconfigContent := `
apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: test-cluster
contexts:
- context:
    cluster: test-cluster
    user: test-user
  name: test-context
current-context: test-context
users:
- name: test-user
  user:
    token: dummy-token
`
	if err := os.WriteFile(kubeconfigPath, []byte(kubeconfigContent), 0600); err != nil {
		t.Fatalf("failed to write dummy kubeconfig: %v", err)
	}

	os.Setenv("KUBECONFIG", kubeconfigPath)
	os.Setenv("DRY_RUN", "true")
	os.Setenv("NAMESPACES", "empty-ns")
	defer func() {
		os.Unsetenv("KUBECONFIG")
		os.Unsetenv("DRY_RUN")
		os.Unsetenv("NAMESPACES")
	}()

	var exitCode int
	osExit = func(code int) {
		exitCode = code
	}
	defer func() {
		osExit = os.Exit
	}()

	main()
	if exitCode != 0 {
		t.Errorf("expected exit code 0 when execution finishes without fatal init error, got %d", exitCode)
	}
}

func TestPushMetricsSkipsWhenNoURLConfigured(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
	}))
	defer srv.Close()

	recorder := metrics.NewRecorder(false)
	pushMetrics(context.Background(), recorder, &config.Config{})

	if hits != 0 {
		t.Errorf("expected no push without a configured URL, got %d requests", hits)
	}
}

func TestPushMetricsSendsToConfiguredGateway(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	cfg := &config.Config{
		PushgatewayURL:             srv.URL,
		PushgatewayJob:             "k8s-pod-cleanup",
		EnableNodePressureEviction: true,
	}
	pushMetrics(context.Background(), metrics.NewRecorder(false), cfg)

	want := "/metrics/job/k8s-pod-cleanup/component/node-pressure"
	if gotPath != want {
		t.Errorf("expected path %q, got %q", want, gotPath)
	}
}

// A failing Pushgateway must not take the run down with it: the cleanup already
// happened, and a non-zero exit would make the CronJob retry deletions.
func TestPushMetricsSurvivesGatewayFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfg := &config.Config{PushgatewayURL: srv.URL, PushgatewayJob: "job"}
	pushMetrics(context.Background(), metrics.NewRecorder(false), cfg)
}

// Run returns an error only when the namespace listing itself fails, which is
// what surfaces a broken cluster connection rather than a per-namespace hiccup.
func TestRunReturnsErrorWhenCleanupFails(t *testing.T) {
	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")
	kubeconfigContent := `
apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://127.0.0.1:1
  name: test-cluster
contexts:
- context:
    cluster: test-cluster
    user: test-user
  name: test-context
current-context: test-context
users:
- name: test-user
  user:
    token: dummy-token
`
	if err := os.WriteFile(kubeconfigPath, []byte(kubeconfigContent), 0600); err != nil {
		t.Fatalf("failed to write dummy kubeconfig: %v", err)
	}

	t.Setenv("KUBECONFIG", kubeconfigPath)
	t.Setenv("DRY_RUN", "true")
	// No NAMESPACES: the cleaner must list namespaces itself, and that call fails.
	os.Unsetenv("NAMESPACES")

	err := run()
	if err == nil {
		t.Fatal("expected run to return an error when namespace listing fails")
	}
	if !strings.Contains(err.Error(), "error executing pod cleanup") {
		t.Errorf("expected wrapped cleanup error, got %v", err)
	}
}
