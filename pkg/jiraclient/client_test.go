package jiraclient

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"
)

func TestDoRetries429AfterRetryAfter(t *testing.T) {
	calls := 0
	client, requests := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "8")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("slow down"))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	var slept []time.Duration
	client.sleep = recordSleep(&slept)

	request, err := client.NewRequest(context.Background(), http.MethodPut, "/rest/api/3/issue/INIT-1", map[string]string{"summary": "Title"})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Do(request, nil); err != nil {
		t.Fatal(err)
	}

	if calls != 2 || !reflect.DeepEqual(slept, []time.Duration{8 * time.Second}) {
		t.Fatalf("calls = %d, waits = %v; want 2 calls and a wait of 8s", calls, slept)
	}
	if len(*requests) != 2 || (*requests)[0].body != (*requests)[1].body || (*requests)[0].body != `{"summary":"Title"}` {
		t.Fatalf("requests = %+v; want the JSON body sent twice", *requests)
	}
}

func TestDoRetriesServiceUnavailableWithBackoff(t *testing.T) {
	calls := 0
	client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	var slept []time.Duration
	client.sleep = recordSleep(&slept)

	request, err := client.NewRequest(context.Background(), http.MethodGet, "/rest/api/3/issue/INIT-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Do(request, nil); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || !reflect.DeepEqual(slept, []time.Duration{time.Second}) {
		t.Fatalf("calls = %d, waits = %v; want 2 calls and a wait of 1s", calls, slept)
	}
}

func TestDoStopsRetryingA429(t *testing.T) {
	calls := 0
	client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Retry-After", "8")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("slow down"))
	})
	var slept []time.Duration
	client.sleep = recordSleep(&slept)

	request, err := client.NewRequest(context.Background(), http.MethodGet, "/rest/api/3/issue/INIT-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	err = client.Do(request, nil)
	if err == nil || calls != maxRetries+1 || len(slept) != maxRetries {
		t.Fatalf("calls = %d, waits = %d, err = %v; want %d calls", calls, len(slept), err, maxRetries+1)
	}
}

func TestDoStopsWhenTheContextEndsDuringTheWait(t *testing.T) {
	client, requests := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "8")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client.sleep = func(ctx context.Context, _ time.Duration) error {
		cancel()
		return ctx.Err()
	}

	request, err := client.NewRequest(ctx, http.MethodGet, "/rest/api/3/issue/INIT-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	err = client.Do(request, nil)
	if !errors.Is(err, context.Canceled) || len(*requests) != 1 {
		t.Fatalf("requests = %d, err = %v; want one request and context.Canceled", len(*requests), err)
	}
}

func recordSleep(slept *[]time.Duration) func(context.Context, time.Duration) error {
	return func(_ context.Context, delay time.Duration) error {
		*slept = append(*slept, delay)
		return nil
	}
}
