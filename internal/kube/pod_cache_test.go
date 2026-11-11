package kube

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
)

func TestWatchHealthAndFreshnessAreIndependent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stream := watch.NewRaceFreeFake()
	cached := &podCache{}
	previousProgress := time.Now().Add(-time.Minute).UnixNano()
	cached.progress.Store(previousProgress)
	tracked := cached.trackWatch(stream)
	defer tracked.Stop()

	observe := func(kind watch.EventType, timestamp *atomic.Int64) {
		t.Helper()
		stream.Action(kind, &corev1.Pod{})
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-tracked.ResultChan():
		}
		if err := wait.PollUntilContextCancel(ctx, time.Millisecond, true, func(context.Context) (bool, error) {
			return timestamp.Load() > previousProgress, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	observe(watch.Modified, &cached.lastEvent)
	if !cached.watching.Load() || cached.progress.Load() != previousProgress {
		t.Fatal("an active watch and a Pod event must not renew confirmed freshness")
	}
	observe(watch.Bookmark, &cached.progress)
	if cached.lastBookmark.Load() == 0 {
		t.Fatal("bookmark should update its timestamp")
	}
	tracked.Stop()
	if cached.watching.Load() || cached.progress.Load() != 0 || cached.lastBookmark.Load() == 0 {
		t.Fatal("stopping the watch should invalidate freshness and retain activity history")
	}
}

func TestPodInformerConcurrentReadAvoidsAPIAtScale(t *testing.T) {
	for _, count := range []int{3000, 10000} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			objects := make([]runtime.Object, count)
			for i := range objects {
				objects[i] = &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf("pod-%05d", i), Namespace: "shop",
					},
					Spec: corev1.PodSpec{NodeName: fmt.Sprintf("node-%03d", i%300)},
				}
			}
			client := fake.NewSimpleClientset(objects...)
			service := NewService(client)
			service.StartPodCache(ctx, []string{"shop"})
			cached := service.podCaches["shop"]
			if err := wait.PollUntilContextCancel(ctx, 10*time.Millisecond, true, func(context.Context) (bool, error) {
				return cached.watching.Load() && cached.informer.HasSynced(), nil
			}); err != nil {
				t.Fatal(err)
			}
			client.ClearActions()
			// Simulate a recent bookmark immediately before the request burst.
			cached.progress.Store(time.Now().UnixNano())
			var reads sync.WaitGroup
			for range 100 {
				reads.Go(func() {
					pods, hit, err := service.ListPods(ctx, "shop", MaxPodCacheAge)
					if err != nil || !hit || len(pods) != count {
						t.Errorf("expected %d cached pods, got %d, hit=%v err=%v", count, len(pods), hit, err)
					}
				})
			}
			reads.Wait()
			if actions := client.Actions(); len(actions) != 0 {
				t.Fatalf("cached requests sent %d Kubernetes calls", len(actions))
			}
		})
	}
}

func TestPodInformerServesWatchUpdatesAndFallsBackWhenStale(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "shop"}}
	client := fake.NewSimpleClientset(pod)
	service := NewService(client)
	service.StartPodCache(ctx, []string{"shop"})

	cached := service.podCaches["shop"]
	if err := wait.PollUntilContextCancel(ctx, 10*time.Millisecond, true, func(context.Context) (bool, error) {
		return cached.usable(time.Now(), MaxPodCacheAge), nil
	}); err != nil {
		t.Fatal(err)
	}
	if pods, hit, err := service.ListPodsForHealth(ctx, "shop", MaxPodCacheAge); err != nil || !hit || len(pods) != 1 {
		t.Fatalf("the initial pending pod should appear in health: pods=%v hit=%v err=%v", pods, hit, err)
	}
	pod.Status.Phase = corev1.PodRunning
	if _, err := client.CoreV1().Pods("shop").Update(ctx, pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := wait.PollUntilContextCancel(ctx, 10*time.Millisecond, true, func(ctx context.Context) (bool, error) {
		pods, hit, err := service.ListPods(ctx, "shop", MaxPodCacheAge)
		return hit && len(pods) == 1 && pods[0].Phase == "Running", err
	}); err != nil {
		t.Fatal(err)
	}
	client.ClearActions()
	if pods, hit, err := service.ListPodsForHealth(ctx, "shop", MaxPodCacheAge); err != nil || !hit || len(pods) != 0 {
		t.Fatalf("the running pod should be removed from the health index: pods=%v hit=%v err=%v", pods, hit, err)
	}
	if _, hit, err := service.ListPods(ctx, "shop", MaxPodCacheAge); err != nil || !hit || len(client.Actions()) != 0 {
		t.Fatalf("healthy cache should avoid a Kubernetes request: hit=%v err=%v actions=%v", hit, err, client.Actions())
	}
	if _, hit, err := service.ListPods(ctx, "shop", 0); err != nil || hit {
		t.Fatalf("fresh request should read live: hit=%v err=%v", hit, err)
	}
	client.ClearActions()
	cached.progress.Store(time.Now().Add(-2 * MaxPodCacheAge).UnixNano())
	if _, hit, err := service.ListPods(ctx, "shop", MaxPodCacheAge); err != nil || hit {
		t.Fatalf("stale cache should read live: hit=%v err=%v", hit, err)
	}
	actions := client.Actions()
	if len(actions) != 1 || !actions[0].Matches("list", "pods") {
		t.Fatalf("expected one live pod LIST, got %v", actions)
	}
}
