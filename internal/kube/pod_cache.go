package kube

import (
	"context"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/incidentflow/incidentflow-k8s-agent/internal/telemetry"
)

const MaxPodCacheAge = 5 * time.Second
const podHealthIndex = "health-summary"

type podCache struct {
	informer           cache.SharedIndexInformer
	lister             corelisters.PodNamespaceLister
	watching           atomic.Bool
	progress           atomic.Int64
	lastEvent          atomic.Int64
	lastBookmark       atomic.Int64
	lastSuccessfulList atomic.Int64
}

func (c *podCache) usable(now time.Time, maxAge time.Duration) bool {
	progress := c.progress.Load()
	age := now.Sub(time.Unix(0, progress))
	return maxAge > 0 && c.watching.Load() && c.informer.HasSynced() &&
		progress != 0 && age >= 0 && age <= maxAge
}

// StartPodCache starts one informer per allowed namespace before serving commands.
func (s *Service) StartPodCache(ctx context.Context, namespaces []string) {
	s.podCaches = make(map[string]*podCache, len(namespaces))
	telemetry.PodCacheRequests.WithLabelValues("hit")
	telemetry.PodCacheRequests.WithLabelValues("miss")
	telemetry.PodAPIRequestsAvoided.WithLabelValues("pods")
	for _, namespace := range namespaces {
		pods := s.client.CoreV1().Pods(namespace)
		c := &podCache{}
		watchAttempts := 0
		var listStarted time.Time
		telemetry.InformerWatchReconnects.WithLabelValues("pods", namespace)
		c.informer = cache.NewSharedIndexInformer(&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				if options.Continue == "" {
					listStarted = time.Now()
					// A LIST with resourceVersion=0 can return an arbitrarily old snapshot.
					options.ResourceVersion = ""
					options.ResourceVersionMatch = ""
				}
				list, err := pods.List(ctx, options)
				if err == nil && list.Continue == "" && !listStarted.IsZero() {
					c.lastSuccessfulList.Store(time.Now().UnixNano())
					c.progress.Store(listStarted.UnixNano())
				}
				return list, err
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				if watchAttempts > 0 {
					telemetry.InformerWatchReconnects.WithLabelValues("pods", namespace).Inc()
				}
				watchAttempts++
				options.AllowWatchBookmarks = true
				stream, err := pods.Watch(ctx, options)
				if err != nil {
					return nil, err
				}
				return c.trackWatch(stream), nil
			},
		}, &corev1.Pod{}, 0, cache.Indexers{
			cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
			podHealthIndex: func(obj any) ([]string, error) {
				if podInHealthSummary(obj.(*corev1.Pod)) {
					return []string{"included"}, nil
				}
				return nil, nil
			},
		})
		c.lister = corelisters.NewPodLister(c.informer.GetIndexer()).Pods(namespace)
		// Keep only the fields used by ListPods.
		_ = c.informer.SetTransform(func(obj any) (any, error) {
			p, ok := obj.(*corev1.Pod)
			if !ok {
				return obj, nil
			}
			images := make([]corev1.Container, len(p.Spec.Containers))
			for i, container := range p.Spec.Containers {
				images[i] = corev1.Container{Name: container.Name, Image: container.Image}
			}
			return &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: p.Name, Namespace: p.Namespace, UID: p.UID,
					ResourceVersion: p.ResourceVersion, CreationTimestamp: p.CreationTimestamp,
					Labels: p.Labels,
				},
				Spec:   corev1.PodSpec{NodeName: p.Spec.NodeName, Containers: images},
				Status: corev1.PodStatus{Phase: p.Status.Phase, ContainerStatuses: p.Status.ContainerStatuses},
			}, nil
		})
		s.podCaches[namespace] = c
		go c.informer.Run(ctx.Done())
		go c.reportHealth(ctx, namespace)
	}
}

type trackedWatch struct {
	watch.Interface
	done <-chan struct{}
}

func (w trackedWatch) Stop() {
	w.Interface.Stop()
	<-w.done
}

func (c *podCache) trackWatch(stream watch.Interface) watch.Interface {
	out := make(chan watch.Event)
	done := make(chan struct{})
	proxy := watch.NewProxyWatcher(out)
	c.watching.Store(true)
	go func() {
		defer close(done)
		defer close(out)
		defer stream.Stop()
		defer func() {
			c.watching.Store(false)
			c.progress.Store(0)
		}()
		for {
			select {
			case <-proxy.StopChan():
				return
			case event, ok := <-stream.ResultChan():
				if !ok {
					return
				}
				if event.Type == watch.Error {
					c.watching.Store(false)
				}
				select {
				case <-proxy.StopChan():
					return
				case out <- event:
					now := time.Now().UnixNano()
					switch event.Type {
					case watch.Bookmark:
						c.lastBookmark.Store(now)
						c.progress.Store(now)
					case watch.Added, watch.Modified, watch.Deleted:
						c.lastEvent.Store(now)
					}
				}
			}
		}
	}()
	// Finish resetting this stream before the reflector starts another one.
	return trackedWatch{Interface: proxy, done: done}
}

func (c *podCache) reportHealth(ctx context.Context, namespace string) {
	health := telemetry.InformerSynced.WithLabelValues("pods", namespace)
	watchHealth := telemetry.InformerWatchHealthy.WithLabelValues("pods", namespace)
	lastEvent := telemetry.InformerLastActivity.WithLabelValues("pods", namespace, "event")
	lastBookmark := telemetry.InformerLastActivity.WithLabelValues("pods", namespace, "bookmark")
	lastList := telemetry.InformerLastActivity.WithLabelValues("pods", namespace, "list")
	objects := telemetry.InformerObjects.WithLabelValues("pods", namespace)
	bytes := telemetry.InformerPayloadBytes.WithLabelValues("pods", namespace)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	defer health.Set(0)
	defer watchHealth.Set(0)
	var nextStoreSample time.Time
	for {
		watching := c.watching.Load()
		if watching {
			watchHealth.Set(1)
		} else {
			watchHealth.Set(0)
		}
		if watching && c.informer.HasSynced() {
			health.Set(1)
		} else {
			health.Set(0)
		}
		if progress := c.progress.Load(); progress > 0 {
			telemetry.InformerLastSync.WithLabelValues("pods", namespace).Set(float64(progress) / 1e9)
		}
		lastEvent.Set(float64(c.lastEvent.Load()) / 1e9)
		lastBookmark.Set(float64(c.lastBookmark.Load()) / 1e9)
		lastList.Set(float64(c.lastSuccessfulList.Load()) / 1e9)
		if now := time.Now(); !now.Before(nextStoreSample) {
			items := c.informer.GetStore().List()
			size := 0
			for _, item := range items {
				size += item.(*corev1.Pod).Size()
			}
			objects.Set(float64(len(items)))
			bytes.Set(float64(size))
			nextStoreSample = now.Add(15 * time.Second)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
