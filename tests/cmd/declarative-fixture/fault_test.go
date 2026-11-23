package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

func TestFaultProxyCommitsBeforeSingleFailure(t *testing.T) {
	var mu sync.Mutex
	state := ""
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodGet {
			io.WriteString(w, state)
			return
		}
		if r.URL.Path == "/reject" {
			http.Error(w, "no", 409)
			return
		}
		state = "committed"
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, "created")
	}))
	defer backend.Close()
	target, _ := url.Parse(backend.URL)
	proxy := httptest.NewServer(newFaultProxy(target))
	defer proxy.Close()
	get, err := http.Get(proxy.URL + "/resource")
	if err != nil {
		t.Fatal(err)
	}
	get.Body.Close()
	if get.StatusCode != 200 {
		t.Fatal("GET interrupted")
	}
	reject, err := http.Post(proxy.URL+"/reject", "text/plain", strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	reject.Body.Close()
	if reject.StatusCode != 409 {
		t.Fatal("failed mutation changed")
	}
	first, err := http.Post(proxy.URL+"/resource", "text/plain", strings.NewReader("secret"))
	if err != nil {
		t.Fatal(err)
	}
	first.Body.Close()
	if first.StatusCode != 502 {
		t.Fatalf("first committed write status = %d", first.StatusCode)
	}
	read, err := http.Get(backend.URL + "/resource")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(read.Body)
	read.Body.Close()
	if string(body) != "committed" {
		t.Fatal("fault occurred before commit")
	}
	second, err := http.Post(proxy.URL+"/resource", "text/plain", strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	second.Body.Close()
	if second.StatusCode != 201 {
		t.Fatalf("replay status = %d", second.StatusCode)
	}
}

func TestFaultProxyDoesNotInterruptSafeMethods(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer backend.Close()
	target, _ := url.Parse(backend.URL)
	proxy := httptest.NewServer(newFaultProxy(target))
	defer proxy.Close()
	for _, method := range []string{http.MethodHead, http.MethodOptions, http.MethodTrace} {
		request, err := http.NewRequest(method, proxy.URL+"/resource", nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusNoContent {
			t.Fatalf("%s interrupted: %d", method, response.StatusCode)
		}
	}
	response, err := http.Post(proxy.URL+"/resource", "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadGateway {
		t.Fatalf("mutation status = %d", response.StatusCode)
	}
}
