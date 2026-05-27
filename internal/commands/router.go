package commands

import (
	"context"
	"errors"
	"fmt"

	"github.com/incidentflow/incidentflow-k8s-agent/internal/security"
	apiv1 "github.com/incidentflow/incidentflow-k8s-agent/pkg/api/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

type Router struct {
	kube   Kubernetes
	guard  security.NamespaceGuard
	limits security.Limits
}

func NewRouter(kube Kubernetes, guard security.NamespaceGuard, limits security.Limits) *Router {
	return &Router{kube: kube, guard: guard, limits: limits}
}

func (r *Router) Handle(ctx context.Context, cmd apiv1.Command) apiv1.Response {
	if err := ValidateCommand(cmd); err != nil {
		code := apiv1.ErrInvalidCommand
		if !IsAllowedAction(cmd.Action) {
			code = apiv1.ErrUnsupportedAction
		}
		return apiv1.Failure(cmd.ID, code, err.Error())
	}

	data, err := r.dispatch(ctx, cmd)
	if err != nil {
		code, message := classifyError(err)
		return apiv1.Failure(cmd.ID, code, message)
	}
	return apiv1.Success(cmd.ID, data)
}

func (r *Router) dispatch(ctx context.Context, cmd apiv1.Command) (any, error) {
	switch cmd.Action {
	case ActionListNamespaces:
		namespaces, err := r.kube.ListNamespaces(ctx)
		if err != nil {
			return nil, err
		}
		filtered := make([]any, 0, len(namespaces))
		for _, namespace := range namespaces {
			if r.guard.Check(namespace.Name) == nil {
				filtered = append(filtered, namespace)
			}
		}
		return map[string]any{"namespaces": filtered}, nil
	case ActionListPods:
		params, err := decodeParams[apiv1.ListPodsParams](cmd)
		if err != nil {
			return nil, invalidParams(err)
		}
		if err := r.guard.Check(params.Namespace); err != nil {
			return nil, namespaceDenied(err)
		}
		if params.Namespace == "" {
			var all []any
			namespaces, err := r.allowedNamespaces(ctx)
			if err != nil {
				return nil, err
			}
			for _, namespace := range namespaces {
				pods, err := r.kube.ListPods(ctx, namespace)
				if err != nil {
					return nil, err
				}
				for _, pod := range pods {
					all = append(all, pod)
				}
			}
			return map[string]any{"pods": all}, nil
		}
		pods, err := r.kube.ListPods(ctx, params.Namespace)
		return map[string]any{"pods": pods}, err
	case ActionGetPod:
		params, err := decodeParams[apiv1.GetPodParams](cmd)
		if err != nil {
			return nil, invalidParams(err)
		}
		if params.Namespace == "" || params.Pod == "" {
			return nil, invalidParams(errors.New("namespace and pod are required"))
		}
		if err := r.guard.Check(params.Namespace); err != nil {
			return nil, namespaceDenied(err)
		}
		pod, err := r.kube.GetPod(ctx, params.Namespace, params.Pod)
		return map[string]any{"pod": pod}, err
	case ActionGetPodLogs:
		params, err := decodeParams[apiv1.GetPodLogsParams](cmd)
		if err != nil {
			return nil, invalidParams(err)
		}
		if params.Namespace == "" || params.Pod == "" {
			return nil, invalidParams(errors.New("namespace and pod are required"))
		}
		if err := r.guard.Check(params.Namespace); err != nil {
			return nil, namespaceDenied(err)
		}
		tail := r.limits.TailLines(params.TailLines)
		logs, err := r.kube.GetPodLogs(ctx, params.Namespace, params.Pod, params.Container, tail, r.limits.MaxLogBytes)
		return logs, err
	case ActionListEvents:
		params, err := decodeParams[apiv1.ListEventsParams](cmd)
		if err != nil {
			return nil, invalidParams(err)
		}
		if err := r.guard.Check(params.Namespace); err != nil {
			return nil, namespaceDenied(err)
		}
		if params.Namespace == "" {
			var all []any
			namespaces, err := r.allowedNamespaces(ctx)
			if err != nil {
				return nil, err
			}
			for _, namespace := range namespaces {
				events, err := r.kube.ListEvents(ctx, namespace)
				if err != nil {
					return nil, err
				}
				for _, event := range events {
					all = append(all, event)
				}
			}
			return map[string]any{"events": all}, nil
		}
		events, err := r.kube.ListEvents(ctx, params.Namespace)
		return map[string]any{"events": events}, err
	case ActionListDeployments:
		params, err := decodeParams[apiv1.ListDeploymentsParams](cmd)
		if err != nil {
			return nil, invalidParams(err)
		}
		if err := r.guard.Check(params.Namespace); err != nil {
			return nil, namespaceDenied(err)
		}
		if params.Namespace == "" {
			var all []any
			namespaces, err := r.allowedNamespaces(ctx)
			if err != nil {
				return nil, err
			}
			for _, namespace := range namespaces {
				deployments, err := r.kube.ListDeployments(ctx, namespace)
				if err != nil {
					return nil, err
				}
				for _, deployment := range deployments {
					all = append(all, deployment)
				}
			}
			return map[string]any{"deployments": all}, nil
		}
		deployments, err := r.kube.ListDeployments(ctx, params.Namespace)
		return map[string]any{"deployments": deployments}, err
	case ActionListServices:
		params, err := decodeParams[apiv1.ListServicesParams](cmd)
		if err != nil {
			return nil, invalidParams(err)
		}
		if err := r.guard.Check(params.Namespace); err != nil {
			return nil, namespaceDenied(err)
		}
		if params.Namespace == "" {
			var all []any
			namespaces, err := r.allowedNamespaces(ctx)
			if err != nil {
				return nil, err
			}
			for _, namespace := range namespaces {
				services, err := r.kube.ListServices(ctx, namespace)
				if err != nil {
					return nil, err
				}
				for _, service := range services {
					all = append(all, service)
				}
			}
			return map[string]any{"services": all}, nil
		}
		services, err := r.kube.ListServices(ctx, params.Namespace)
		return map[string]any{"services": services}, err
	case ActionGetRolloutStatus:
		params, err := decodeParams[apiv1.GetRolloutStatusParams](cmd)
		if err != nil {
			return nil, invalidParams(err)
		}
		if params.Namespace == "" || params.Deployment == "" {
			return nil, invalidParams(errors.New("namespace and deployment are required"))
		}
		if err := r.guard.Check(params.Namespace); err != nil {
			return nil, namespaceDenied(err)
		}
		status, err := r.kube.GetRolloutStatus(ctx, params.Namespace, params.Deployment)
		return map[string]any{"rollout": status}, err
	default:
		return nil, fmt.Errorf("unsupported action %q", cmd.Action)
	}
}

func (r *Router) allowedNamespaces(ctx context.Context) ([]string, error) {
	namespaces, err := r.kube.ListNamespaces(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(namespaces))
	for _, namespace := range namespaces {
		if r.guard.Check(namespace.Name) == nil {
			out = append(out, namespace.Name)
		}
	}
	return out, nil
}

type codedError struct {
	code string
	err  error
}

func (e codedError) Error() string { return e.err.Error() }
func (e codedError) Unwrap() error { return e.err }

func invalidParams(err error) error {
	return codedError{code: apiv1.ErrInvalidParams, err: err}
}

func namespaceDenied(err error) error {
	return codedError{code: apiv1.ErrNamespaceDenied, err: err}
}

func classifyError(err error) (string, string) {
	var coded codedError
	if errors.As(err, &coded) {
		return coded.code, coded.err.Error()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return apiv1.ErrTimeout, "command timed out"
	}
	if apierrors.IsNotFound(err) {
		return apiv1.ErrNotFound, err.Error()
	}
	if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
		return apiv1.ErrRBACDenied, "Agent cannot access the requested Kubernetes resource"
	}
	return apiv1.ErrInternal, err.Error()
}
