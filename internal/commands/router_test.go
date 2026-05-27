package commands

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/incidentflow/incidentflow-k8s-agent/internal/kube"
	"github.com/incidentflow/incidentflow-k8s-agent/internal/security"
	apiv1 "github.com/incidentflow/incidentflow-k8s-agent/pkg/api/v1"
)

type fakeKube struct{}

func (fakeKube) ListNamespaces(context.Context) ([]kube.Namespace, error) {
	return []kube.Namespace{{Name: "prod"}, {Name: "kube-system"}}, nil
}
func (fakeKube) ListPods(_ context.Context, namespace string) ([]kube.Pod, error) {
	return []kube.Pod{{Name: "p", Namespace: namespace}}, nil
}
func (fakeKube) GetPod(context.Context, string, string) (kube.Pod, error) {
	return kube.Pod{Name: "p"}, nil
}
func (fakeKube) GetPodLogs(context.Context, string, string, string, int64, int64) (kube.LogResult, error) {
	return kube.LogResult{Logs: "ok"}, nil
}
func (fakeKube) ListEvents(context.Context, string) ([]kube.Event, error) { return nil, nil }
func (fakeKube) ListDeployments(context.Context, string) ([]kube.Deployment, error) {
	return nil, nil
}
func (fakeKube) ListServices(context.Context, string) ([]kube.ServiceResource, error) {
	return nil, nil
}
func (fakeKube) GetRolloutStatus(context.Context, string, string) (kube.RolloutStatus, error) {
	return kube.RolloutStatus{Complete: true}, nil
}

func TestRouterRejectsUnsupportedAction(t *testing.T) {
	router := NewRouter(fakeKube{}, security.NewNamespaceGuard(nil), security.Limits{DefaultTailLines: 200, MaxTailLines: 1000, MaxLogBytes: 100})
	resp := router.Handle(context.Background(), apiv1.Command{ID: "req", Type: apiv1.MessageTypeCommand, Action: "k8s.delete_pod"})
	if resp.Status != apiv1.StatusError || resp.Error.Code != apiv1.ErrUnsupportedAction {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestRouterDeniesNamespace(t *testing.T) {
	params, _ := json.Marshal(apiv1.ListPodsParams{Namespace: "kube-system"})
	router := NewRouter(fakeKube{}, security.NewNamespaceGuard(nil), security.Limits{DefaultTailLines: 200, MaxTailLines: 1000, MaxLogBytes: 100})
	resp := router.Handle(context.Background(), apiv1.Command{ID: "req", Type: apiv1.MessageTypeCommand, Action: ActionListPods, Params: params})
	if resp.Status != apiv1.StatusError || resp.Error.Code != apiv1.ErrNamespaceDenied {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestRouterFiltersDeniedNamespaces(t *testing.T) {
	router := NewRouter(fakeKube{}, security.NewNamespaceGuard(nil), security.Limits{DefaultTailLines: 200, MaxTailLines: 1000, MaxLogBytes: 100})
	resp := router.Handle(context.Background(), apiv1.Command{ID: "req", Type: apiv1.MessageTypeCommand, Action: ActionListNamespaces})
	if resp.Status != apiv1.StatusSuccess {
		t.Fatalf("unexpected response: %#v", resp)
	}
	data := resp.Data.(map[string]any)
	namespaces := data["namespaces"].([]any)
	if len(namespaces) != 1 {
		t.Fatalf("expected one namespace after filtering, got %d", len(namespaces))
	}
}
