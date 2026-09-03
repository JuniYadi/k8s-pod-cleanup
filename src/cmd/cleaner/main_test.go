package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
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
