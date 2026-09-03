package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/juniyadi/k8s-pod-cleanup/internal/cleaner"
	"github.com/juniyadi/k8s-pod-cleanup/internal/config"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var osExit = os.Exit

func main() {
	if err := run(); err != nil {
		slog.Error("Application terminated with error", "error", err)
		osExit(1)
	}
}

func run() error {
	cfg, err := config.ParseFlags()
	if err != nil {
		return fmt.Errorf("failed to parse flags: %w", err)
	}

	setupLogger(cfg.LogLevel)

	k8sConfig, err := getKubernetesConfig(cfg.Kubeconfig)
	if err != nil {
		return fmt.Errorf("failed to initialize Kubernetes client config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes clientset: %w", err)
	}

	appCleaner := cleaner.NewCleaner(clientset, cfg)
	ctx := context.Background()

	if err := appCleaner.Run(ctx); err != nil {
		return fmt.Errorf("error executing pod cleanup: %w", err)
	}

	slog.Info("k8s-pod-cleanup execution finished successfully")
	return nil
}

func setupLogger(levelStr string) {
	var level slog.Level
	switch levelStr {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	}))
	slog.SetDefault(logger)
}

func getKubernetesConfig(kubeconfigPath string) (*rest.Config, error) {
	if kubeconfigPath != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	}

	// Try in-cluster first
	cfg, err := rest.InClusterConfig()
	if err == nil {
		return cfg, nil
	}

	// Fallback to local default kubeconfig (e.g. ~/.kube/config)
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides).ClientConfig()
}
