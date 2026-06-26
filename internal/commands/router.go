package commands

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/sync/errgroup"

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
			namespaces, err := r.allowedNamespaces(ctx)
			if err != nil {
				return nil, err
			}
			all, err := parallelMap(ctx, namespaces, r.kube.ListPods)
			return map[string]any{"pods": all}, err
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
			namespaces, err := r.allowedNamespaces(ctx)
			if err != nil {
				return nil, err
			}
			all, err := parallelMap(ctx, namespaces, r.kube.ListEvents)
			return map[string]any{"events": all}, err
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
			namespaces, err := r.allowedNamespaces(ctx)
			if err != nil {
				return nil, err
			}
			all, err := parallelMap(ctx, namespaces, r.kube.ListDeployments)
			return map[string]any{"deployments": all}, err
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
			namespaces, err := r.allowedNamespaces(ctx)
			if err != nil {
				return nil, err
			}
			all, err := parallelMap(ctx, namespaces, r.kube.ListServices)
			return map[string]any{"services": all}, err
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
	}
	// ValidateCommand rejects unknown actions before dispatch is called, so this
	// branch is unreachable in practice — kept as a compile-time exhaustion guard.
	return nil, fmt.Errorf("unsupported action %q", cmd.Action)
}

// parallelMap calls fn(ctx, ns) for each namespace concurrently and returns a
// flat []any of all results. The first error cancels remaining work via context.
func parallelMap[T any](
	ctx context.Context,
	namespaces []string,
	fn func(context.Context, string) ([]T, error),
) ([]any, error) {
	if len(namespaces) == 0 {
		return nil, nil
	}
	g, gCtx := errgroup.WithContext(ctx)
	results := make([][]T, len(namespaces))
	for i, ns := range namespaces {
		i, ns := i, ns
		g.Go(func() error {
			items, err := fn(gCtx, ns)
			if err != nil {
				return err
			}
			results[i] = items
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	var all []any
	for _, items := range results {
		for _, item := range items {
			all = append(all, item)
		}
	}
	return all, nil
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
