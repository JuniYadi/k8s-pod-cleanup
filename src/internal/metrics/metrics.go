// Package metrics records what a single cleanup run did and ships the result to
// a Prometheus Pushgateway.
//
// Every metric here is a Gauge rather than a Counter. The binary runs as an
// ephemeral CronJob: each run reports only its own work, and the Pushgateway
// replaces the previous group on every push. A value that goes 5 -> 2 between
// runs is therefore a normal observation, not a counter reset — Counter
// semantics would make rate() and increase() produce nonsense. Aggregate across
// runs with sum_over_time() instead.
package metrics

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/push"
	"github.com/prometheus/common/expfmt"
)

const namespace = "k8s_pod_cleanup"

// Reason codes labelling why a pod was deleted. These are deliberately a small
// fixed set: the free-text reason (which embeds durations and container names)
// stays in the logs, because using it as a label value would mint new
// Prometheus series on every run.
const (
	ReasonTerminatingStuck = "terminating_stuck"
	ReasonFailedOrEvicted  = "failed_or_evicted"
	ReasonPendingStalled   = "pending_stalled"
	ReasonCrashOrImage     = "crashloop_or_image_error"
	ReasonUnready          = "unready"
)

// Node pressure condition codes.
const (
	ConditionMemoryPressure = "memory_pressure"
	ConditionDiskPressure   = "disk_pressure"
	ConditionPIDPressure    = "pid_pressure"
	ConditionNotReady       = "not_ready"
)

// Recorder holds the gauges for one run.
type Recorder struct {
	registry *prometheus.Registry

	duration prometheus.Gauge
	lastRun  prometheus.Gauge
	success  prometheus.Gauge
	dryRun   prometheus.Gauge

	podsEvaluated   prometheus.Gauge
	podsDeleted     *prometheus.GaugeVec
	podDeleteErrors *prometheus.GaugeVec

	nodesEvaluated    prometheus.Gauge
	nodesPressured    prometheus.Gauge
	pressureCondition *prometheus.GaugeVec
	nodesCordoned     prometheus.Gauge
	podsEvacuated     *prometheus.GaugeVec
}

// NewRecorder builds a Recorder on its own registry. The default registry is
// deliberately not used: its go_* and process_* collectors describe a process
// that has already exited by the time anyone scrapes them, and they would sit
// stale in the Pushgateway.
func NewRecorder(dryRun bool) *Recorder {
	r := &Recorder{
		registry: prometheus.NewRegistry(),

		duration: gauge("duration_seconds", "Wall-clock duration of the last run."),
		lastRun:  gauge("last_run_timestamp_seconds", "Unix timestamp when the last run finished."),
		success:  gauge("run_success", "1 if the last run completed without error, 0 otherwise."),
		dryRun:   gauge("dry_run", "1 if the last run was a dry run, 0 otherwise."),

		podsEvaluated:   gauge("pods_evaluated", "Pods evaluated against the cleanup rules during the last run."),
		podsDeleted:     gaugeVec("pods_deleted", "Pods deleted during the last run.", "namespace", "reason"),
		podDeleteErrors: gaugeVec("pod_delete_errors", "Pod deletions that failed during the last run.", "namespace"),

		nodesEvaluated:    gauge("nodes_evaluated", "Nodes evaluated for sustained pressure during the last run."),
		nodesPressured:    gauge("nodes_pressured", "Nodes found under sustained pressure during the last run."),
		pressureCondition: gaugeVec("node_pressure_conditions", "Active sustained pressure conditions by type. A node under two conditions counts once in each.", "condition"),
		nodesCordoned:     gauge("nodes_cordoned", "Nodes cordoned during the last run."),
		podsEvacuated:     gaugeVec("pods_evacuated", "Pods evacuated from pressured nodes during the last run.", "namespace"),
	}

	r.registry.MustRegister(
		r.duration, r.lastRun, r.success, r.dryRun,
		r.podsEvaluated, r.podsDeleted, r.podDeleteErrors,
		r.nodesEvaluated, r.nodesPressured, r.pressureCondition, r.nodesCordoned, r.podsEvacuated,
	)

	r.dryRun.Set(boolValue(dryRun))
	return r
}

func gauge(name, help string) prometheus.Gauge {
	return prometheus.NewGauge(prometheus.GaugeOpts{Namespace: namespace, Name: name, Help: help})
}

func gaugeVec(name, help string, labels ...string) *prometheus.GaugeVec {
	return prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: namespace, Name: name, Help: help}, labels)
}

func boolValue(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// AddPodEvaluated counts one pod checked against the cleanup rules.
func (r *Recorder) AddPodEvaluated() { r.podsEvaluated.Inc() }

// AddPodDeleted counts one pod deleted (or, in dry-run, one that would have been).
func (r *Recorder) AddPodDeleted(ns, reason string) {
	r.podsDeleted.WithLabelValues(ns, reason).Inc()
}

// AddPodDeleteError counts one failed deletion attempt.
func (r *Recorder) AddPodDeleteError(ns string) { r.podDeleteErrors.WithLabelValues(ns).Inc() }

// AddNodeEvaluated counts one node checked for sustained pressure.
func (r *Recorder) AddNodeEvaluated() { r.nodesEvaluated.Inc() }

// AddNodePressured counts one node under sustained pressure, along with each of
// its active condition types.
func (r *Recorder) AddNodePressured(conditions []string) {
	r.nodesPressured.Inc()
	for _, c := range conditions {
		r.pressureCondition.WithLabelValues(c).Inc()
	}
}

// AddNodeCordoned counts one node marked unschedulable.
func (r *Recorder) AddNodeCordoned() { r.nodesCordoned.Inc() }

// AddPodEvacuated counts one pod removed from a pressured node.
func (r *Recorder) AddPodEvacuated(ns string) { r.podsEvacuated.WithLabelValues(ns).Inc() }

// Finish records the outcome of the run. Call it before Push.
func (r *Recorder) Finish(d time.Duration, ok bool) {
	r.duration.Set(d.Seconds())
	r.lastRun.SetToCurrentTime()
	r.success.Set(boolValue(ok))
}

// PushOptions describes where to send the metrics.
type PushOptions struct {
	URL       string
	Job       string
	Component string
	Username  string
	Password  string
}

// Push sends the recorded metrics to the Pushgateway.
//
// It uses PushContext (HTTP PUT), which replaces the entire group. Add (POST)
// would merge instead, leaving series from namespaces that no longer exist
// lingering in the gateway forever.
func (r *Recorder) Push(ctx context.Context, opts PushOptions) error {
	p := push.New(opts.URL, opts.Job).
		Grouping("component", opts.Component).
		Gatherer(r.registry).
		// Text rather than the library's default protobuf: the Pushgateway
		// accepts both, and text makes what we send readable with curl.
		Format(expfmt.NewFormat(expfmt.TypeTextPlain))

	if opts.Username != "" {
		p = p.BasicAuth(opts.Username, opts.Password)
	}

	return p.PushContext(ctx)
}
