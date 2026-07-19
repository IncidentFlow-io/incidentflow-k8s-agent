package kube

import (
	"context"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PodDescription is the structured output for k8s.describe_pod.
// Does NOT include env vars, secrets, imageID, containerID, managedFields, or raw annotations.
type PodDescription struct {
	Metadata   PodMeta              `json:"metadata"`
	Status     DescribedStatus      `json:"status"`
	Containers []DescribedContainer `json:"containers"`
	Resources  PodResources         `json:"resources,omitempty"`
	Probes     []ContainerProbes    `json:"probes,omitempty"`
	Events     []Event              `json:"events"`
}

type PodMeta struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Owner     string `json:"owner,omitempty"` // "ReplicaSet/xxx", "DaemonSet/xxx", etc.
	Node      string `json:"node,omitempty"`
	PodIP     string `json:"pod_ip,omitempty"`
	Age       string `json:"age"`
}

type DescribedStatus struct {
	Phase      string         `json:"phase"`
	Ready      bool           `json:"ready"`
	Conditions []PodCondition `json:"conditions,omitempty"`
	Reason     string         `json:"reason,omitempty"`
	Message    string         `json:"message,omitempty"`
}

type PodCondition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

type DescribedContainer struct {
	Name          string          `json:"name"`
	Image         string          `json:"image"` // digest stripped
	Ready         bool            `json:"ready"`
	State         ContainerState  `json:"state"`
	LastState     *ContainerState `json:"last_state,omitempty"`
	RestartCount  int32           `json:"restart_count"`
	LastRestartAt string          `json:"last_restart_at,omitempty"`
}

type ContainerState struct {
	Running    *StateRunning    `json:"running,omitempty"`
	Waiting    *StateWaiting    `json:"waiting,omitempty"`
	Terminated *StateTerminated `json:"terminated,omitempty"`
}

type StateRunning struct {
	StartedAt string `json:"started_at,omitempty"`
}

type StateWaiting struct {
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

type StateTerminated struct {
	ExitCode   int32  `json:"exit_code"`
	Reason     string `json:"reason,omitempty"`
	Message    string `json:"message,omitempty"`
	FinishedAt string `json:"finished_at,omitempty"`
}

type ContainerResourceSpec struct {
	CPU    string `json:"cpu,omitempty"`
	Memory string `json:"memory,omitempty"`
}

type ContainerResourceEntry struct {
	Name     string                `json:"name"`
	Requests ContainerResourceSpec `json:"requests,omitempty"`
	Limits   ContainerResourceSpec `json:"limits,omitempty"`
}

type PodResources struct {
	QoSClass   string                   `json:"qos_class,omitempty"`
	Containers []ContainerResourceEntry `json:"containers,omitempty"`
}

type ProbeConfig struct {
	Type                string `json:"type"` // "http", "tcp", "exec", "grpc"
	Path                string `json:"path,omitempty"`
	Port                string `json:"port,omitempty"`
	InitialDelaySeconds int32  `json:"initial_delay_seconds,omitempty"`
	PeriodSeconds       int32  `json:"period_seconds,omitempty"`
	TimeoutSeconds      int32  `json:"timeout_seconds,omitempty"`
	SuccessThreshold    int32  `json:"success_threshold,omitempty"`
	FailureThreshold    int32  `json:"failure_threshold,omitempty"`
}

type ContainerProbes struct {
	Name      string       `json:"name"`
	Readiness *ProbeConfig `json:"readiness,omitempty"`
	Liveness  *ProbeConfig `json:"liveness,omitempty"`
	Startup   *ProbeConfig `json:"startup,omitempty"`
}

func stripImageDigest(image string) string {
	if i := strings.Index(image, "@"); i >= 0 {
		return image[:i]
	}
	return image
}

// DescribePod fetches a pod and its recent events and returns a structured description.
func (s *Service) DescribePod(ctx context.Context, namespace, name string) (PodDescription, error) {
	pod, err := s.client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return PodDescription{}, err
	}
	events := s.fetchPodEvents(ctx, namespace, name)
	return toPodDescription(*pod, events), nil
}

// fetchPodEvents lists events for a specific pod. Uses field selector when possible;
// falls back to full namespace listing + in-process filter.
func (s *Service) fetchPodEvents(ctx context.Context, namespace, podName string) []Event {
	items, err := s.client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: "involvedObject.kind=Pod,involvedObject.name=" + podName,
	})
	if err == nil {
		out := make([]Event, 0, len(items.Items))
		for _, e := range items.Items {
			out = append(out, toEventItem(e))
		}
		return sortAndLimitEvents(out, 20)
	}
	// Fallback: list all and filter
	all, err2 := s.client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err2 != nil {
		return nil
	}
	var out []Event
	for _, e := range all.Items {
		if e.InvolvedObject.Kind == "Pod" && e.InvolvedObject.Name == podName {
			out = append(out, toEventItem(e))
		}
	}
	return sortAndLimitEvents(out, 20)
}

func toEventItem(e corev1.Event) Event {
	lastSeen := e.LastTimestamp.Time
	if lastSeen.IsZero() {
		lastSeen = e.EventTime.Time
	}
	return Event{
		Namespace: e.Namespace,
		Name:      e.Name,
		Type:      e.Type,
		Reason:    e.Reason,
		Message:   e.Message,
		Object:    fmt.Sprintf("%s/%s", e.InvolvedObject.Kind, e.InvolvedObject.Name),
		Count:     e.Count,
		LastSeen:  lastSeen.Format("2006-01-02T15:04:05Z07:00"),
	}
}

func sortAndLimitEvents(events []Event, limit int) []Event {
	// Warnings first, then newest first within each group.
	sort.Slice(events, func(i, j int) bool {
		wi := events[i].Type == "Warning"
		wj := events[j].Type == "Warning"
		if wi != wj {
			return wi
		}
		return events[i].LastSeen > events[j].LastSeen
	})
	if len(events) > limit {
		return events[:limit]
	}
	return events
}

func toPodDescription(pod corev1.Pod, events []Event) PodDescription {
	// Metadata
	meta := PodMeta{
		Name:      pod.Name,
		Namespace: pod.Namespace,
		Node:      pod.Spec.NodeName,
		PodIP:     pod.Status.PodIP,
		Age:       age(pod.CreationTimestamp.Time),
	}
	if len(pod.OwnerReferences) > 0 {
		ref := pod.OwnerReferences[0]
		meta.Owner = ref.Kind + "/" + ref.Name
	}

	// Status
	allReady := len(pod.Status.ContainerStatuses) > 0
	for _, cs := range pod.Status.ContainerStatuses {
		if !cs.Ready {
			allReady = false
			break
		}
	}
	var conditions []PodCondition
	for _, c := range pod.Status.Conditions {
		conditions = append(conditions, PodCondition{
			Type:    string(c.Type),
			Status:  string(c.Status),
			Reason:  c.Reason,
			Message: c.Message,
		})
	}
	status := DescribedStatus{
		Phase:      string(pod.Status.Phase),
		Ready:      allReady,
		Conditions: conditions,
		Reason:     pod.Status.Reason,
		Message:    pod.Status.Message,
	}

	// Spec image map (no digest)
	specImages := make(map[string]string, len(pod.Spec.Containers))
	for _, c := range pod.Spec.Containers {
		specImages[c.Name] = stripImageDigest(c.Image)
	}

	// Containers
	containers := make([]DescribedContainer, 0, len(pod.Status.ContainerStatuses))
	for _, cs := range pod.Status.ContainerStatuses {
		image := specImages[cs.Name]
		if image == "" {
			image = stripImageDigest(cs.Image)
		}
		dc := DescribedContainer{
			Name:          cs.Name,
			Image:         image,
			Ready:         cs.Ready,
			State:         toContainerState(cs.State),
			RestartCount:  cs.RestartCount,
			LastRestartAt: lastRestartAt(cs),
		}
		if isNonEmptyState(cs.LastTerminationState) {
			ls := toContainerState(cs.LastTerminationState)
			dc.LastState = &ls
		}
		containers = append(containers, dc)
	}

	// Resources
	resContainers := make([]ContainerResourceEntry, 0, len(pod.Spec.Containers))
	for _, c := range pod.Spec.Containers {
		entry := ContainerResourceEntry{Name: c.Name}
		if cpu := c.Resources.Requests.Cpu(); cpu != nil && !cpu.IsZero() {
			entry.Requests.CPU = cpu.String()
		}
		if mem := c.Resources.Requests.Memory(); mem != nil && !mem.IsZero() {
			entry.Requests.Memory = mem.String()
		}
		if cpu := c.Resources.Limits.Cpu(); cpu != nil && !cpu.IsZero() {
			entry.Limits.CPU = cpu.String()
		}
		if mem := c.Resources.Limits.Memory(); mem != nil && !mem.IsZero() {
			entry.Limits.Memory = mem.String()
		}
		resContainers = append(resContainers, entry)
	}
	resources := PodResources{
		QoSClass:   string(pod.Status.QOSClass),
		Containers: resContainers,
	}

	// Probes (config only — no env, no command args beyond type)
	var probes []ContainerProbes
	for _, c := range pod.Spec.Containers {
		cp := ContainerProbes{Name: c.Name}
		cp.Readiness = toProbeConfig(c.ReadinessProbe)
		cp.Liveness = toProbeConfig(c.LivenessProbe)
		cp.Startup = toProbeConfig(c.StartupProbe)
		if cp.Readiness != nil || cp.Liveness != nil || cp.Startup != nil {
			probes = append(probes, cp)
		}
	}

	return PodDescription{
		Metadata:   meta,
		Status:     status,
		Containers: containers,
		Resources:  resources,
		Probes:     probes,
		Events:     events,
	}
}

func isNonEmptyState(s corev1.ContainerState) bool {
	return s.Running != nil || s.Waiting != nil || s.Terminated != nil
}

func toContainerState(s corev1.ContainerState) ContainerState {
	cs := ContainerState{}
	if s.Running != nil {
		cs.Running = &StateRunning{
			StartedAt: s.Running.StartedAt.Format("2006-01-02T15:04:05Z07:00"),
		}
	}
	if s.Waiting != nil {
		cs.Waiting = &StateWaiting{
			Reason:  s.Waiting.Reason,
			Message: s.Waiting.Message,
		}
	}
	if s.Terminated != nil {
		cs.Terminated = &StateTerminated{
			ExitCode:   s.Terminated.ExitCode,
			Reason:     s.Terminated.Reason,
			Message:    s.Terminated.Message,
			FinishedAt: s.Terminated.FinishedAt.Format("2006-01-02T15:04:05Z07:00"),
		}
	}
	return cs
}

func toProbeConfig(probe *corev1.Probe) *ProbeConfig {
	if probe == nil {
		return nil
	}
	cfg := &ProbeConfig{
		InitialDelaySeconds: probe.InitialDelaySeconds,
		PeriodSeconds:       probe.PeriodSeconds,
		TimeoutSeconds:      probe.TimeoutSeconds,
		SuccessThreshold:    probe.SuccessThreshold,
		FailureThreshold:    probe.FailureThreshold,
	}
	switch {
	case probe.HTTPGet != nil:
		cfg.Type = "http"
		cfg.Path = probe.HTTPGet.Path
		cfg.Port = probe.HTTPGet.Port.String()
	case probe.TCPSocket != nil:
		cfg.Type = "tcp"
		cfg.Port = probe.TCPSocket.Port.String()
	case probe.GRPC != nil:
		cfg.Type = "grpc"
		cfg.Port = fmt.Sprintf("%d", probe.GRPC.Port)
	case probe.Exec != nil:
		cfg.Type = "exec"
	}
	return cfg
}
