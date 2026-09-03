package config

import (
	"os"
	"testing"
	"time"
)

func TestConfigEnvOverrides(t *testing.T) {
	os.Setenv("NAMESPACES", "ns1, ns2,ns3 ")
	os.Setenv("EXCLUDED_NAMESPACES", "kube-system,custom-system")
	os.Setenv("DRY_RUN", "true")
	os.Setenv("FORCE", "true")
	os.Setenv("THRESHOLD_DURATION", "10m")
	os.Setenv("RESTART_THRESHOLD", "5")
	os.Setenv("LOG_LEVEL", "debug")
	defer func() {
		os.Unsetenv("NAMESPACES")
		os.Unsetenv("EXCLUDED_NAMESPACES")
		os.Unsetenv("DRY_RUN")
		os.Unsetenv("FORCE")
		os.Unsetenv("THRESHOLD_DURATION")
		os.Unsetenv("RESTART_THRESHOLD")
		os.Unsetenv("LOG_LEVEL")
	}()

	cfg, err := ParseFlags()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Namespaces) != 3 || cfg.Namespaces[0] != "ns1" || cfg.Namespaces[1] != "ns2" || cfg.Namespaces[2] != "ns3" {
		t.Errorf("expected namespaces [ns1 ns2 ns3], got %v", cfg.Namespaces)
	}

	if len(cfg.ExcludedNamespaces) != 2 || cfg.ExcludedNamespaces[1] != "custom-system" {
		t.Errorf("expected excluded namespaces [kube-system custom-system], got %v", cfg.ExcludedNamespaces)
	}

	if !cfg.DryRun {
		t.Errorf("expected dry-run to be true")
	}

	if !cfg.Force {
		t.Errorf("expected force to be true")
	}

	if cfg.ThresholdDuration != 10*time.Minute {
		t.Errorf("expected 10m threshold duration, got %v", cfg.ThresholdDuration)
	}

	if cfg.RestartThreshold != 5 {
		t.Errorf("expected restart threshold 5, got %d", cfg.RestartThreshold)
	}

	if cfg.LogLevel != "debug" {
		t.Errorf("expected log level debug, got %s", cfg.LogLevel)
	}
}
