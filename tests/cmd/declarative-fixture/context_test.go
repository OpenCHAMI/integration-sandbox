package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestJWKSFetchPropagatesCancellation(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(canceled)
	}))
	defer backend.Close()
	target, _ := url.Parse(backend.URL)
	handler, err := newFacade(target, "http://tokensmith:27780", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/.well-known/jwks.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		response, err := http.DefaultClient.Do(request)
		if err == nil {
			response.Body.Close()
		}
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("backend request did not start")
	}
	cancel()
	select {
	case <-canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("backend did not observe cancellation")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("client did not finish")
	}
}

func TestFaultProxyAllowsReadsDuringPendingMutation(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			close(started)
			<-release
			w.WriteHeader(http.StatusCreated)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()
	target, _ := url.Parse(backend.URL)
	proxy := httptest.NewServer(newFaultProxy(target))
	defer proxy.Close()
	done := make(chan int, 1)
	go func() {
		response, err := http.Post(proxy.URL+"/resource", "text/plain", nil)
		if err != nil {
			done <- 0
			return
		}
		response.Body.Close()
		done <- response.StatusCode
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("mutation did not start")
	}
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get(proxy.URL + "/resource")
	close(release)
	if err != nil {
		t.Fatalf("read was blocked: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatal("read interrupted")
	}
	select {
	case status := <-done:
		if status != http.StatusBadGateway {
			t.Fatalf("mutation status = %d", status)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("mutation did not finish")
	}
}
