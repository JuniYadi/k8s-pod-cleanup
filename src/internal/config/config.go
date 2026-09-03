package config

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

// Config represents runtime settings for the cleaner.
type Config struct {
	Kubeconfig                  string
	Namespaces                  []string // if empty, scan all namespaces
	ExcludedNamespaces          []string
	IgnoreAnnotation            string
	ThresholdDuration           time.Duration
	RestartThreshold            int32
	DryRun                      bool
	Force                       bool
	LogLevel                    string
	TerminationThreshold        time.Duration
	EnableNodePressureEviction  bool
	NodePressureDuration        time.Duration
	NodePressureForceDelete     bool
	NodePressureCordon          bool
}

// DefaultExcludedNamespaces is the default list of system namespaces to skip.
var DefaultExcludedNamespaces = []string{
	"kube-system",
	"kube-public",
	"kube-node-lease",
}
func ParseFlags() (*Config, error) {
	cfg := &Config{}

	var namespacesStr string
	var excludedNamespacesStr string
	var restartThreshold int

	flag.StringVar(&cfg.Kubeconfig, "kubeconfig", getEnv("KUBECONFIG", ""), "Path to a kubeconfig file. In-cluster config is used if omitted.")
	flag.StringVar(&namespacesStr, "namespaces", getEnv("NAMESPACES", ""), "Comma-separated list of target namespaces to clean (default: all).")
	flag.StringVar(&excludedNamespacesStr, "excluded-namespaces", getEnv("EXCLUDED_NAMESPACES", strings.Join(DefaultExcludedNamespaces, ",")), "Comma-separated list of namespaces to ignore.")
	flag.StringVar(&cfg.IgnoreAnnotation, "ignore-annotation", getEnv("IGNORE_ANNOTATION", "cleanup.k8s.io/ignore"), "Annotation or label key to opt-out pod from deletion if value is 'true'.")
	flag.DurationVar(&cfg.ThresholdDuration, "threshold-duration", getEnvDuration("THRESHOLD_DURATION", 5*time.Minute), "Minimum duration a pod must remain in an unhealthy state before being deleted.")
	flag.DurationVar(&cfg.TerminationThreshold, "terminating-threshold", getEnvDuration("TERMINATING_THRESHOLD", 5*time.Minute), "Minimum duration a pod stuck in Terminating status before deletion.")
	flag.IntVar(&restartThreshold, "restart-threshold", getEnvInt("RESTART_THRESHOLD", 3), "Minimum container restarts for CrashLoop conditions.")
	flag.BoolVar(&cfg.DryRun, "dry-run", getEnvBool("DRY_RUN", false), "Simulate pod deletion without performing actual API calls.")
	flag.BoolVar(&cfg.Force, "force", getEnvBool("FORCE", false), "Force delete pod immediately with zero grace period (gracePeriodSeconds=0).")
	flag.StringVar(&cfg.LogLevel, "log-level", getEnv("LOG_LEVEL", "info"), "Logging level: debug, info, warn, error.")
	flag.BoolVar(&cfg.EnableNodePressureEviction, "enable-node-pressure-eviction", getEnvBool("ENABLE_NODE_PRESSURE_EVICTION", false), "Enable eviction of pods from nodes under sustained resource pressure.")
	flag.DurationVar(&cfg.NodePressureDuration, "node-pressure-duration", getEnvDuration("NODE_PRESSURE_DURATION", 1*time.Minute), "Minimum duration a node must be under pressure before evicting pods.")
	flag.BoolVar(&cfg.NodePressureForceDelete, "node-pressure-force-delete", getEnvBool("NODE_PRESSURE_FORCE_DELETE", true), "Force delete pods immediately (gracePeriodSeconds=0) when evicting from pressured nodes.")
	flag.BoolVar(&cfg.NodePressureCordon, "node-pressure-cordon", getEnvBool("NODE_PRESSURE_CORDON", true), "Cordon node (mark unschedulable) when node pressure is detected.")

	flag.Parse()
	cfg.RestartThreshold = int32(restartThreshold)

	if namespacesStr != "" {
		cfg.Namespaces = splitAndTrim(namespacesStr)
	}

	if excludedNamespacesStr != "" {
		cfg.ExcludedNamespaces = splitAndTrim(excludedNamespacesStr)
	}

	return cfg, nil
}

func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func getEnv(key, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok {
		return val
	}
	return defaultVal
}

func getEnvDuration(key string, defaultVal time.Duration) time.Duration {
	if val, ok := os.LookupEnv(key); ok {
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if val, ok := os.LookupEnv(key); ok {
		var n int
		if _, err := fmt.Sscanf(val, "%d", &n); err == nil {
			return n
		}
	}
	return defaultVal
}

func getEnvBool(key string, defaultVal bool) bool {
	if val, ok := os.LookupEnv(key); ok {
		val = strings.ToLower(strings.TrimSpace(val))
		return val == "true" || val == "1" || val == "yes"
	}
	return defaultVal
}
