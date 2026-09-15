package k8s

import (
	"context"
	"encoding/json"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func TestKubectlLogArgsSelectsContainer(t *testing.T) {
	pod := Pod{Name: "api-abc-def", Namespace: "payments"}

	wantInitial := []string{"logs", "-f", "--tail=100", "-n", "payments", "api-abc-def", "-c", "metrics"}
	if got := kubectlLogArgs(pod, "metrics", false); !reflect.DeepEqual(got, wantInitial) {
		t.Fatalf("initial kubectl args = %v, want %v", got, wantInitial)
	}

	wantRestart := []string{"logs", "-f", "--tail=0", "-n", "payments", "api-abc-def", "-c", "metrics"}
	if got := kubectlLogArgs(pod, "metrics", true); !reflect.DeepEqual(got, wantRestart) {
		t.Fatalf("restart kubectl args = %v, want %v", got, wantRestart)
	}
}

func TestParsePodsIncludesContainers(t *testing.T) {
	output := []byte(`{
  "items": [{
    "metadata": {
      "name": "payments-api-7f8b9c6d5-x2j4k",
      "namespace": "payments",
      "uid": "pod-uid-1"
    },
    "spec": {
      "containers": [
        {"name": "api"},
        {"name": "metrics-sidecar"}
      ]
    },
    "status": {"phase": "Running"}
  }]
}`)

	pods, err := parsePods(output)
	if err != nil {
		t.Fatalf("parsePods() error = %v", err)
	}
	if len(pods) != 1 {
		t.Fatalf("len(parsePods()) = %d, want 1", len(pods))
	}

	pod := pods[0]
	if pod.UID != "pod-uid-1" {
		t.Errorf("pod UID = %q, want pod-uid-1", pod.UID)
	}
	if len(pod.Containers) != 2 || pod.Containers[0] != "api" || pod.Containers[1] != "metrics-sidecar" {
		t.Errorf("pod containers = %v, want [api metrics-sidecar]", pod.Containers)
	}
}

func TestCollectorDiscoversNewPodContainers(t *testing.T) {
	collector := NewCollector(nil, []string{"payments"}, nil)
	collector.discoveryInterval = 5 * time.Millisecond

	firstPod := Pod{
		Name:       "api-abc-def",
		UID:        "uid-api",
		Service:    "api",
		Namespace:  "payments",
		Status:     "Running",
		Containers: []string{"api", "metrics"},
	}
	secondPod := Pod{
		Name:       "worker-abc-def",
		UID:        "uid-worker",
		Service:    "worker",
		Namespace:  "payments",
		Status:     "Running",
		Containers: []string{"worker"},
	}

	var calls atomic.Int32
	collector.podSource = func(namespace string) ([]Pod, error) {
		if calls.Add(1) == 1 {
			return []Pod{firstPod}, nil
		}
		return []Pod{firstPod, secondPod}, nil
	}

	started := make(chan string, 4)
	collector.streamLogs = func(ctx context.Context, pod Pod, container string, restart bool) {
		started <- pod.Name + "/" + container
		<-ctx.Done()
	}

	if err := collector.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer collector.Stop()

	want := map[string]bool{
		"api-abc-def/api":       true,
		"api-abc-def/metrics":   true,
		"worker-abc-def/worker": true,
	}
	for len(want) > 0 {
		select {
		case stream := <-started:
			delete(want, stream)
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for streams: %v", want)
		}
	}
}

func TestCollectorStopsStreamsForRemovedPods(t *testing.T) {
	collector := NewCollector(nil, []string{"payments"}, nil)
	collector.discoveryInterval = 5 * time.Millisecond

	pod := Pod{
		Name:       "api-abc-def",
		UID:        "uid-api",
		Service:    "api",
		Namespace:  "payments",
		Status:     "Running",
		Containers: []string{"api"},
	}

	var calls atomic.Int32
	collector.podSource = func(namespace string) ([]Pod, error) {
		if calls.Add(1) == 1 {
			return []Pod{pod}, nil
		}
		return nil, nil
	}

	started := make(chan struct{}, 1)
	stopped := make(chan struct{}, 1)
	collector.streamLogs = func(ctx context.Context, pod Pod, container string, restart bool) {
		started <- struct{}{}
		<-ctx.Done()
		stopped <- struct{}{}
	}

	if err := collector.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer collector.Stop()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for stream to start")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for removed pod stream to stop")
	}
}

func TestCollectorRestartDoesNotReplayHistory(t *testing.T) {
	collector := NewCollector(nil, []string{"payments"}, nil)
	collector.discoveryInterval = 5 * time.Millisecond

	pod := Pod{
		Name:       "api-abc-def",
		UID:        "uid-api",
		Service:    "api",
		Namespace:  "payments",
		Status:     "Running",
		Containers: []string{"api"},
	}
	collector.podSource = func(namespace string) ([]Pod, error) {
		return []Pod{pod}, nil
	}

	restarts := make(chan bool, 4)
	collector.streamLogs = func(ctx context.Context, pod Pod, container string, restart bool) {
		select {
		case restarts <- restart:
		case <-ctx.Done():
		}
	}

	if err := collector.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer collector.Stop()

	select {
	case restart := <-restarts:
		if restart {
			t.Fatal("initial stream marked as restart")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for initial stream")
	}
	select {
	case restart := <-restarts:
		if !restart {
			t.Fatal("reconnected stream would replay its initial history")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for reconnected stream")
	}
}

func TestLogBufferIncludesContainerMetadata(t *testing.T) {
	pod := Pod{Name: "api-abc-def", Namespace: "payments", Service: "api"}
	buffer := newLogBuffer(nil, pod, "metrics", time.Hour)
	buffer.start("INFO metrics ready")

	var extra map[string]string
	if err := json.Unmarshal(buffer.entry.Extra, &extra); err != nil {
		t.Fatalf("decode log metadata: %v", err)
	}
	if extra["pod"] != pod.Name || extra["container"] != "metrics" {
		t.Fatalf("log metadata = %v, want pod and container", extra)
	}
}
