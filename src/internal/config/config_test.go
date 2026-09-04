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
	os.Setenv("ENABLE_NODE_PRESSURE_EVICTION", "true")
	os.Setenv("NODE_PRESSURE_DURATION", "2m")
	os.Setenv("NODE_PRESSURE_FORCE_DELETE", "true")
	os.Setenv("NODE_PRESSURE_CORDON", "true")
	defer func() {
		os.Unsetenv("NAMESPACES")
		os.Unsetenv("EXCLUDED_NAMESPACES")
		os.Unsetenv("DRY_RUN")
		os.Unsetenv("FORCE")
		os.Unsetenv("THRESHOLD_DURATION")
		os.Unsetenv("RESTART_THRESHOLD")
		os.Unsetenv("LOG_LEVEL")
		os.Unsetenv("ENABLE_NODE_PRESSURE_EVICTION")
		os.Unsetenv("NODE_PRESSURE_DURATION")
		os.Unsetenv("NODE_PRESSURE_FORCE_DELETE")
		os.Unsetenv("NODE_PRESSURE_CORDON")
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

	if !cfg.EnableNodePressureEviction {
		t.Errorf("expected enable-node-pressure-eviction to be true")
	}

	if cfg.NodePressureDuration != 2*time.Minute {
		t.Errorf("expected 2m node pressure duration, got %v", cfg.NodePressureDuration)
	}

	if !cfg.NodePressureForceDelete {
		t.Errorf("expected node-pressure-force-delete to be true")
	}

	if !cfg.NodePressureCordon {
		t.Errorf("expected node-pressure-cordon to be true")
	}
}

func TestConfigDefaultsAndHelpers(t *testing.T) {
	// Test getEnvInt fallback
	os.Setenv("RESTART_THRESHOLD_INVALID", "invalid-number")
	defer os.Unsetenv("RESTART_THRESHOLD_INVALID")

	val := getEnvInt("RESTART_THRESHOLD_INVALID", 99)
	if val != 99 {
		t.Errorf("expected fallback 99, got %d", val)
	}

	// Test getEnvBool cases
	os.Setenv("TEST_BOOL_1", "1")
	os.Setenv("TEST_BOOL_YES", "yes")
	os.Setenv("TEST_BOOL_FALSE", "false")
	defer func() {
		os.Unsetenv("TEST_BOOL_1")
		os.Unsetenv("TEST_BOOL_YES")
		os.Unsetenv("TEST_BOOL_FALSE")
	}()

	if !getEnvBool("TEST_BOOL_1", false) {
		t.Errorf("expected true for '1'")
	}
	if !getEnvBool("TEST_BOOL_YES", false) {
		t.Errorf("expected true for 'yes'")
	}
	if getEnvBool("TEST_BOOL_FALSE", true) {
		t.Errorf("expected false for 'false'")
	}
	if !getEnvBool("TEST_BOOL_NONEXISTENT", true) {
		t.Errorf("expected default true for nonexistent key")
	}

	// Test getEnvDuration fallback on invalid
	os.Setenv("TEST_DURATION_INVALID", "not-a-duration")
	defer os.Unsetenv("TEST_DURATION_INVALID")
	dur := getEnvDuration("TEST_DURATION_INVALID", 7*time.Minute)
	if dur != 7*time.Minute {
		t.Errorf("expected 7m, got %v", dur)
	}
}

// Both CronJobs run the same binary, so the component is what keeps their
// Pushgateway groups apart. Getting this wrong means one job silently
// overwrites the other's metrics.
func TestMetricsComponent(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want string
	}{
		{"node pressure job", &Config{EnableNodePressureEviction: true}, "node-pressure"},
		{"pod cleanup job", &Config{EnableNodePressureEviction: false}, "pod-cleanup"},
		{"zero value defaults to pod cleanup", &Config{}, "pod-cleanup"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.MetricsComponent(); got != tc.want {
				t.Errorf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestMetricsComponentFromParsedFlags(t *testing.T) {
	cfg, err := ParseFlagsWithArgs([]string{"--enable-node-pressure-eviction=true"})
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if got := cfg.MetricsComponent(); got != "node-pressure" {
		t.Errorf("expected node-pressure, got %q", got)
	}
}

func TestPushgatewayFlagsAndCredentials(t *testing.T) {
	t.Setenv("PUSHGATEWAY_USERNAME", "metrics")
	t.Setenv("PUSHGATEWAY_PASSWORD", "secret")

	cfg, err := ParseFlagsWithArgs([]string{
		"--pushgateway-url=http://pgw:9091",
		"--pushgateway-job=custom-job",
	})
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	if cfg.PushgatewayURL != "http://pgw:9091" {
		t.Errorf("expected url http://pgw:9091, got %q", cfg.PushgatewayURL)
	}
	if cfg.PushgatewayJob != "custom-job" {
		t.Errorf("expected job custom-job, got %q", cfg.PushgatewayJob)
	}
	// Credentials are env-only: a flag would expose them in the pod spec.
	if cfg.PushgatewayUsername != "metrics" || cfg.PushgatewayPassword != "secret" {
		t.Errorf("expected credentials from env, got %q/%q", cfg.PushgatewayUsername, cfg.PushgatewayPassword)
	}
}

func TestPushgatewayDefaults(t *testing.T) {
	cfg, err := ParseFlagsWithArgs(nil)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	// An empty URL is what disables the push entirely.
	if cfg.PushgatewayURL != "" {
		t.Errorf("expected empty default url, got %q", cfg.PushgatewayURL)
	}
	if cfg.PushgatewayJob != "k8s-pod-cleanup" {
		t.Errorf("expected default job k8s-pod-cleanup, got %q", cfg.PushgatewayJob)
	}
}
