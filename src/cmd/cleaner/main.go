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

func main() {
	cfg, err := config.ParseFlags()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to parse flags: %v\n", err)
		os.Exit(1)
	}

	setupLogger(cfg.LogLevel)

	k8sConfig, err := getKubernetesConfig(cfg.Kubeconfig)
	if err != nil {
		slog.Error("Failed to initialize Kubernetes client config", "error", err)
		os.Exit(1)
	}

	clientset, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		slog.Error("Failed to create Kubernetes clientset", "error", err)
		os.Exit(1)
	}

	appCleaner := cleaner.NewCleaner(clientset, cfg)
	ctx := context.Background()

	if err := appCleaner.Run(ctx); err != nil {
		slog.Error("Error executing pod cleanup", "error", err)
		os.Exit(1)
	}

	slog.Info("k8s-pod-cleanup execution finished successfully")
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
