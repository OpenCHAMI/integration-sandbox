package main

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestFixturePublicKeysMatchPrivateFiles(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { ioWriteJSON(w, `{"keys":[]}`) }))
	defer backend.Close()
	target, _ := url.Parse(backend.URL)
	dir := t.TempDir()
	handler, err := newFacade(target, "http://tokensmith:27780", dir)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	keys := readJWKS(t, server.URL)
	if len(keys.Keys) != 2 {
		t.Fatal("expected two fixture public keys")
	}
	for _, key := range keys.Keys {
		info, err := os.Stat(filepath.Join(dir, key.Kid+".pem"))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatal("fixture key file is not private")
		}
		data, err := os.ReadFile(filepath.Join(dir, key.Kid+".pem"))
		if err != nil {
			t.Fatal(err)
		}
		block, _ := pem.Decode(data)
		if block == nil {
			t.Fatal("invalid private key file")
		}
		private, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			t.Fatal(err)
		}
		switch parsed := private.(type) {
		case *rsa.PrivateKey:
			pub, ok := publicKey(t, key).(*rsa.PublicKey)
			if !ok || pub.N.Cmp(parsed.N) != 0 || pub.E != parsed.E {
				t.Fatal("RSA public bytes differ")
			}
		case *ecdsa.PrivateKey:
			pub, ok := publicKey(t, key).(*ecdsa.PublicKey)
			if !ok || pub.X.Cmp(parsed.X) != 0 || pub.Y.Cmp(parsed.Y) != 0 {
				t.Fatal("EC public bytes differ")
			}
		default:
			t.Fatal("unexpected private key type")
		}
	}
}

func TestFacadeRejectsMalformedJWKSAndFixtureRequests(t *testing.T) {
	var document atomic.Value
	document.Store(`{"keys":[]}`)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { ioWriteJSON(w, document.Load().(string)) }))
	defer backend.Close()
	target, _ := url.Parse(backend.URL)
	handler, err := newFacade(target, "http://tokensmith:27780", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	for _, data := range []string{`{"keys":[{"kid":"private","kty":"RSA","d":"secret"}]}`, `{"keys":[{"kid":42}]}`, `{"keys":null}`, `not-json`} {
		document.Store(data)
		response, err := http.Get(server.URL + "/.well-known/jwks.json")
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusBadGateway {
			t.Fatalf("unsafe JWKS status %d", response.StatusCode)
		}
	}
	for _, data := range []string{`{"kind":"valid","scopes":[]}`, `{"kind":"expired","scopes":["bad scope"]}`, `{"kind":"expired"} {}`, `{"kind":"expired","extra":1}`} {
		response, err := http.Post(server.URL+"/__fixtures/token", "application/json", strings.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("invalid request status %d", response.StatusCode)
		}
	}
}

func TestFacadePreservesBackendPublicKeyWithoutID(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	public := rsaPublicJWK(&key.PublicKey, "")
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(jwks{Keys: []json.RawMessage{mustJSON(t, struct {
			Kty string `json:"kty"`
			N   string `json:"n"`
			E   string `json:"e"`
		}{Kty: public.Kty, N: public.N, E: public.E})}})
	}))
	defer backend.Close()
	target, _ := url.Parse(backend.URL)
	handler, err := newFacade(target, "http://tokensmith:27780", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	keys := readJWKS(t, server.URL)
	if len(keys.Keys) != 3 || keys.Keys[0].Kid != "" || keys.Keys[0].N != public.N || keys.Keys[0].E != public.E {
		t.Fatal("backend public key was not preserved")
	}
}

func TestFaultProxySerializesConcurrentMutations(t *testing.T) {
	var writes atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writes.Add(1); w.WriteHeader(http.StatusCreated) }))
	defer backend.Close()
	target, _ := url.Parse(backend.URL)
	proxy := httptest.NewServer(newFaultProxy(target))
	defer proxy.Close()
	const count = 12
	var wg sync.WaitGroup
	statuses := make(chan int, count)
	for range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			response, err := http.Post(proxy.URL+"/resource", "text/plain", strings.NewReader(""))
			if err != nil {
				statuses <- 0
				return
			}
			response.Body.Close()
			statuses <- response.StatusCode
		}()
	}
	wg.Wait()
	close(statuses)
	failed, succeeded := 0, 0
	for status := range statuses {
		switch status {
		case 502:
			failed++
		case 201:
			succeeded++
		default:
			t.Fatalf("unexpected status %d", status)
		}
	}
	if failed != 1 || succeeded != count-1 || writes.Load() != count {
		t.Fatalf("fault count = %d, success = %d, writes = %d", failed, succeeded, writes.Load())
	}
}
