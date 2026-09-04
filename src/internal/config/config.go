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
	Kubeconfig                 string
	Namespaces                 []string // if empty, scan all namespaces
	ExcludedNamespaces         []string
	IgnoreAnnotation           string
	ThresholdDuration          time.Duration
	RestartThreshold           int32
	DryRun                     bool
	Force                      bool
	LogLevel                   string
	TerminationThreshold       time.Duration
	EnableNodePressureEviction bool
	NodePressureDuration       time.Duration
	NodePressureForceDelete    bool
	NodePressureCordon         bool
	PushgatewayURL             string
	PushgatewayJob             string
	PushgatewayUsername        string
	PushgatewayPassword        string
}

// MetricsComponent identifies which of the two CronJobs is reporting. Both run
// the same binary, so without a distinct grouping key each push would overwrite
// the other's metrics in the Pushgateway.
func (c *Config) MetricsComponent() string {
	if c.EnableNodePressureEviction {
		return "node-pressure"
	}
	return "pod-cleanup"
}

// DefaultExcludedNamespaces is the default list of system namespaces to skip.
var DefaultExcludedNamespaces = []string{
	"kube-system",
	"kube-public",
	"kube-node-lease",
}

func ParseFlags() (*Config, error) {
	return ParseFlagsWithArgs(os.Args[1:])
}

// ParseFlagsWithArgs parses flags from given slice of arguments.
func ParseFlagsWithArgs(args []string) (*Config, error) {
	// Filter out go test flags if executed under test runner
	var cleanArgs []string
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-test.") {
			cleanArgs = append(cleanArgs, arg)
		}
	}

	cfg := &Config{}

	var namespacesStr string
	var excludedNamespacesStr string
	var restartThreshold int

	fs := flag.NewFlagSet("k8s-pod-cleanup", flag.ContinueOnError)

	fs.StringVar(&cfg.Kubeconfig, "kubeconfig", getEnv("KUBECONFIG", ""), "Path to a kubeconfig file. In-cluster config is used if omitted.")
	fs.StringVar(&namespacesStr, "namespaces", getEnv("NAMESPACES", ""), "Comma-separated list of target namespaces to clean (default: all).")
	fs.StringVar(&excludedNamespacesStr, "excluded-namespaces", getEnv("EXCLUDED_NAMESPACES", strings.Join(DefaultExcludedNamespaces, ",")), "Comma-separated list of namespaces to ignore.")
	fs.StringVar(&cfg.IgnoreAnnotation, "ignore-annotation", getEnv("IGNORE_ANNOTATION", "cleanup.k8s.io/ignore"), "Annotation or label key to opt-out pod from deletion if value is 'true'.")
	fs.DurationVar(&cfg.ThresholdDuration, "threshold-duration", getEnvDuration("THRESHOLD_DURATION", 5*time.Minute), "Minimum duration a pod must remain in an unhealthy state before being deleted.")
	fs.DurationVar(&cfg.TerminationThreshold, "terminating-threshold", getEnvDuration("TERMINATING_THRESHOLD", 5*time.Minute), "Minimum duration a pod stuck in Terminating status before deletion.")
	fs.IntVar(&restartThreshold, "restart-threshold", getEnvInt("RESTART_THRESHOLD", 3), "Minimum container restarts for CrashLoop conditions.")
	fs.BoolVar(&cfg.DryRun, "dry-run", getEnvBool("DRY_RUN", false), "Simulate pod deletion without performing actual API calls.")
	fs.BoolVar(&cfg.Force, "force", getEnvBool("FORCE", false), "Force delete pod immediately with zero grace period (gracePeriodSeconds=0).")
	fs.StringVar(&cfg.LogLevel, "log-level", getEnv("LOG_LEVEL", "info"), "Logging level: debug, info, warn, error.")
	fs.BoolVar(&cfg.EnableNodePressureEviction, "enable-node-pressure-eviction", getEnvBool("ENABLE_NODE_PRESSURE_EVICTION", false), "Enable eviction of pods from nodes under sustained resource pressure.")
	fs.DurationVar(&cfg.NodePressureDuration, "node-pressure-duration", getEnvDuration("NODE_PRESSURE_DURATION", 1*time.Minute), "Minimum duration a node must be under pressure before evicting pods.")
	fs.BoolVar(&cfg.NodePressureForceDelete, "node-pressure-force-delete", getEnvBool("NODE_PRESSURE_FORCE_DELETE", true), "Force delete pods immediately (gracePeriodSeconds=0) when evicting from pressured nodes.")
	fs.BoolVar(&cfg.NodePressureCordon, "node-pressure-cordon", getEnvBool("NODE_PRESSURE_CORDON", true), "Cordon node (mark unschedulable) when node pressure is detected.")
	fs.StringVar(&cfg.PushgatewayURL, "pushgateway-url", getEnv("PUSHGATEWAY_URL", ""), "Prometheus Pushgateway base URL. Metrics are not pushed when empty.")
	fs.StringVar(&cfg.PushgatewayJob, "pushgateway-job", getEnv("PUSHGATEWAY_JOB", "k8s-pod-cleanup"), "Prometheus job name used as the Pushgateway grouping key.")

	// Credentials are env-only on purpose: flags end up in the CronJob spec and
	// in the process command line, where anyone with pod read access can see them.
	cfg.PushgatewayUsername = getEnv("PUSHGATEWAY_USERNAME", "")
	cfg.PushgatewayPassword = getEnv("PUSHGATEWAY_PASSWORD", "")

	if err := fs.Parse(cleanArgs); err != nil {
		return nil, err
	}
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
