package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

type jwks struct {
	Keys []json.RawMessage `json:"keys"`
}

type facade struct {
	issuer   string
	keys     *fixtureKeys
	upstream *url.URL
	client   *http.Client
	proxy    *httputil.ReverseProxy
}

func transport() *http.Transport {
	return &http.Transport{DialContext: (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext, MaxIdleConns: 20, MaxIdleConnsPerHost: 10, IdleConnTimeout: 30 * time.Second, ResponseHeaderTimeout: 10 * time.Second, TLSHandshakeTimeout: 5 * time.Second}
}

func reverseProxy(upstream *url.URL) *httputil.ReverseProxy {
	proxy := &httputil.ReverseProxy{Transport: transport(), ErrorLog: log.New(io.Discard, "", 0), Rewrite: func(request *httputil.ProxyRequest) { request.SetURL(upstream) }}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
	}
	return proxy
}

func newFacade(upstream *url.URL, issuer, dir string) (http.Handler, error) {
	keys, err := generateKeys(dir)
	if err != nil {
		return nil, fmt.Errorf("initialize fixture keys: %w", err)
	}
	f := &facade{issuer: issuer, keys: keys, upstream: upstream, client: &http.Client{Transport: transport(), Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}, proxy: reverseProxy(upstream)}
	mux := http.NewServeMux()
	mux.HandleFunc("/__fixtures/health", health)
	mux.HandleFunc("/__fixtures/token", f.token)
	mux.HandleFunc("/.well-known/jwks.json", f.jwks)
	mux.Handle("/", f.proxy)
	return mux, nil
}

func health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, "{\"status\":\"ok\"}\n")
}

func (f *facade) jwks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	endpoint := *f.upstream
	endpoint.Path = "/.well-known/jwks.json"
	endpoint.RawPath = ""
	endpoint.RawQuery = ""
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, endpoint.String(), nil)
	if err != nil {
		http.Error(w, "JWKS unavailable", http.StatusBadGateway)
		return
	}
	response, err := f.client.Do(req)
	if err != nil {
		http.Error(w, "JWKS unavailable", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		http.Error(w, "JWKS unavailable", http.StatusBadGateway)
		return
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024+1))
	if err != nil || len(data) > 1024*1024 {
		http.Error(w, "invalid JWKS", http.StatusBadGateway)
		return
	}
	var set jwks
	if err := json.Unmarshal(data, &set); err != nil || set.Keys == nil {
		http.Error(w, "invalid JWKS", http.StatusBadGateway)
		return
	}
	for _, raw := range set.Keys {
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || fields == nil {
			http.Error(w, "invalid JWKS", http.StatusBadGateway)
			return
		}
		if !validBackendPublicJWK(raw) {
			http.Error(w, "invalid JWKS", http.StatusBadGateway)
			return
		}
		for _, private := range []string{"d", "p", "q", "dp", "dq", "qi", "oth", "k"} {
			if _, exists := fields[private]; exists {
				http.Error(w, "invalid JWKS", http.StatusBadGateway)
				return
			}
		}
		if encoded, exists := fields["kid"]; exists {
			var id string
			if err := json.Unmarshal(encoded, &id); err != nil || id == f.keys.rsaJWK.Kid || id == f.keys.ecJWK.Kid {
				http.Error(w, "invalid JWKS", http.StatusBadGateway)
				return
			}
		}
	}
	for _, key := range []publicJWK{f.keys.rsaJWK, f.keys.ecJWK} {
		raw, err := json.Marshal(key)
		if err != nil {
			http.Error(w, "invalid JWKS", http.StatusBadGateway)
			return
		}
		set.Keys = append(set.Keys, raw)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(set)
}
