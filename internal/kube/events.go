package kube

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (s *Service) ListEvents(ctx context.Context, namespace string) ([]Event, error) {
	items, err := s.client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]Event, 0, len(items.Items))
	for _, event := range items.Items {
		out = append(out, toEvent(event))
	}
	return out, nil
}

func toEvent(event corev1.Event) Event {
	lastSeen := event.LastTimestamp.Time
	if lastSeen.IsZero() {
		lastSeen = event.EventTime.Time
	}
	return Event{
		Namespace: event.Namespace,
		Name:      event.Name,
		Type:      event.Type,
		Reason:    event.Reason,
		Message:   event.Message,
		Object:    fmt.Sprintf("%s/%s", event.InvolvedObject.Kind, event.InvolvedObject.Name),
		ObjectUID: string(event.InvolvedObject.UID),
		Count:     event.Count,
		LastSeen:  lastSeen.Format("2006-01-02T15:04:05Z07:00"),
	}
}
