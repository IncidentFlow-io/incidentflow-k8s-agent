package kube

import (
	"bytes"
	"context"
	"fmt"
	"io"

	corev1 "k8s.io/api/core/v1"
)

func (s *Service) GetPodLogs(ctx context.Context, namespace, pod, container string, tailLines int64, maxBytes int64) (LogResult, error) {
	opts := &corev1.PodLogOptions{TailLines: &tailLines}
	if container != "" {
		opts.Container = container
	}
	req := s.client.CoreV1().Pods(namespace).GetLogs(pod, opts)
	stream, err := req.Stream(ctx)
	if err != nil {
		return LogResult{}, err
	}
	defer stream.Close()

	var buf bytes.Buffer
	limit := maxBytes + 1
	if _, err := io.CopyN(&buf, stream, limit); err != nil && err != io.EOF {
		return LogResult{}, fmt.Errorf("read pod logs: %w", err)
	}
	logs := buf.String()
	truncated := int64(len(logs)) > maxBytes
	if truncated {
		logs = logs[:maxBytes]
	}
	return LogResult{Logs: logs, Truncated: truncated}, nil
}
