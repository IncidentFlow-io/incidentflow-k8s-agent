package commands

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"golang.org/x/sync/errgroup"

	"github.com/incidentflow/incidentflow-k8s-agent/internal/observability"
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
		ctx, span := observability.Tracer.Start(ctx, "k8s.api.list_namespaces")
		defer span.End()
		namespaces, err := r.kube.ListNamespaces(ctx)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			return nil, err
		}
		filtered := make([]any, 0, len(namespaces))
		for _, namespace := range namespaces {
			if r.guard.Check(namespace.Name) == nil {
				filtered = append(filtered, namespace)
			}
		}
		span.SetAttributes(attribute.Int("items.count", len(filtered)))
		span.SetStatus(codes.Ok, "")
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
			all, err := parallelMapTraced(ctx, "k8s.api.list_pods", namespaces, r.kube.ListPods)
			return map[string]any{"pods": all}, err
		}
		ctx, span := observability.Tracer.Start(ctx, "k8s.api.list_pods")
		defer span.End()
		span.SetAttributes(attribute.String("k8s.namespace", params.Namespace))
		pods, err := r.kube.ListPods(ctx, params.Namespace)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetAttributes(attribute.Int("items.count", len(pods)))
			span.SetStatus(codes.Ok, "")
		}
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
		ctx, span := observability.Tracer.Start(ctx, "k8s.api.get_pod")
		defer span.End()
		span.SetAttributes(
			attribute.String("k8s.namespace", params.Namespace),
			attribute.String("k8s.pod", params.Pod),
		)
		pod, err := r.kube.GetPod(ctx, params.Namespace, params.Pod)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}
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
		ctx, span := observability.Tracer.Start(ctx, "k8s.api.get_pod_logs")
		defer span.End()
		tail := r.limits.TailLines(params.TailLines)
		span.SetAttributes(
			attribute.String("k8s.namespace", params.Namespace),
			attribute.String("k8s.pod", params.Pod),
			attribute.String("k8s.container", params.Container),
			attribute.Int64("k8s.tail_lines", tail),
		)
		logs, err := r.kube.GetPodLogs(ctx, params.Namespace, params.Pod, params.Container, tail, r.limits.MaxLogBytes)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}
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
			all, err := parallelMapTraced(ctx, "k8s.api.list_events", namespaces, r.kube.ListEvents)
			return map[string]any{"events": all}, err
		}
		ctx, span := observability.Tracer.Start(ctx, "k8s.api.list_events")
		defer span.End()
		span.SetAttributes(attribute.String("k8s.namespace", params.Namespace))
		events, err := r.kube.ListEvents(ctx, params.Namespace)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetAttributes(attribute.Int("items.count", len(events)))
			span.SetStatus(codes.Ok, "")
		}
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
			all, err := parallelMapTraced(ctx, "k8s.api.list_deployments", namespaces, r.kube.ListDeployments)
			return map[string]any{"deployments": all}, err
		}
		ctx, span := observability.Tracer.Start(ctx, "k8s.api.list_deployments")
		defer span.End()
		span.SetAttributes(attribute.String("k8s.namespace", params.Namespace))
		deployments, err := r.kube.ListDeployments(ctx, params.Namespace)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetAttributes(attribute.Int("items.count", len(deployments)))
			span.SetStatus(codes.Ok, "")
		}
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
			all, err := parallelMapTraced(ctx, "k8s.api.list_services", namespaces, r.kube.ListServices)
			return map[string]any{"services": all}, err
		}
		ctx, span := observability.Tracer.Start(ctx, "k8s.api.list_services")
		defer span.End()
		span.SetAttributes(attribute.String("k8s.namespace", params.Namespace))
		services, err := r.kube.ListServices(ctx, params.Namespace)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetAttributes(attribute.Int("items.count", len(services)))
			span.SetStatus(codes.Ok, "")
		}
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
		ctx, span := observability.Tracer.Start(ctx, "k8s.api.get_rollout_status")
		defer span.End()
		span.SetAttributes(
			attribute.String("k8s.namespace", params.Namespace),
			attribute.String("k8s.deployment", params.Deployment),
		)
		status, err := r.kube.GetRolloutStatus(ctx, params.Namespace, params.Deployment)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}
		return map[string]any{"rollout": status}, err

	case ActionDescribePod:
		params, err := decodeParams[apiv1.DescribePodParams](cmd)
		if err != nil {
			return nil, invalidParams(err)
		}
		if params.Namespace == "" || params.Pod == "" {
			return nil, invalidParams(errors.New("namespace and pod are required"))
		}
		if err := r.guard.Check(params.Namespace); err != nil {
			return nil, namespaceDenied(err)
		}
		ctx, span := observability.Tracer.Start(ctx, "k8s.api.describe_pod")
		defer span.End()
		span.SetAttributes(
			attribute.String("k8s.namespace", params.Namespace),
			attribute.String("k8s.pod", params.Pod),
		)
		desc, err := r.kube.DescribePod(ctx, params.Namespace, params.Pod)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetAttributes(attribute.Int("events.count", len(desc.Events)))
			span.SetStatus(codes.Ok, "")
		}
		return map[string]any{"description": desc}, err
	}
	// ValidateCommand rejects unknown actions before dispatch is called.
	return nil, fmt.Errorf("unsupported action %q", cmd.Action)
}

// parallelMapTraced calls fn(ctx, ns) for each namespace concurrently,
// creating a span for each call, and returns a flat []any of all results.
func parallelMapTraced[T any](
	ctx context.Context,
	spanName string,
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
			nsCtx, nsSpan := observability.Tracer.Start(gCtx, spanName)
			defer nsSpan.End()
			nsSpan.SetAttributes(attribute.String("k8s.namespace", ns))
			items, err := fn(nsCtx, ns)
			if err != nil {
				nsSpan.SetStatus(codes.Error, err.Error())
				return err
			}
			nsSpan.SetAttributes(attribute.Int("items.count", len(items)))
			nsSpan.SetStatus(codes.Ok, "")
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

// parallelMap is kept for compatibility with existing tests.
func parallelMap[T any](
	ctx context.Context,
	namespaces []string,
	fn func(context.Context, string) ([]T, error),
) ([]any, error) {
	return parallelMapTraced(ctx, "k8s.api.call", namespaces, fn)
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
