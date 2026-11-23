package main

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestFacadeForwardsExchangeAndTracksJWKSRotation(t *testing.T) {
	var revision atomic.Int32
	backendKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	backendToken := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"iss": "http://tokensmith:27780", "aud": "boot-service", "exp": time.Now().Add(time.Hour).Unix(), "scope": "read:boot"})
	backendToken.Header["kid"] = "backend-a"
	signed, err := backendToken.SignedString(backendKey)
	if err != nil {
		t.Fatal(err)
	}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/jwks.json" {
			key := rsaPublicJWK(&backendKey.PublicKey, "backend-a")
			if revision.Load() != 0 {
				key.Kid = "backend-b"
			}
			json.NewEncoder(w).Encode(jwks{Keys: []json.RawMessage{mustJSON(t, key)}})
			return
		}
		if r.URL.Path == "/oauth/token" && r.Method == http.MethodPost {
			body, _ := io.ReadAll(r.Body)
			if string(body) == "grant_type=client_credentials" {
				json.NewEncoder(w).Encode(struct {
					Token string `json:"access_token"`
				}{signed})
				return
			}
		}
		http.NotFound(w, r)
	}))
	defer backend.Close()
	upstream, _ := url.Parse(backend.URL)
	h, err := newFacade(upstream, "http://tokensmith:27780", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	response, err := http.Post(server.URL+"/oauth/token", "application/x-www-form-urlencoded", strings.NewReader("grant_type=client_credentials"))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	var exchange struct {
		Token string `json:"access_token"`
	}
	if response.StatusCode != 200 || json.Unmarshal(body, &exchange) != nil || exchange.Token != signed {
		t.Fatal("exchange was not forwarded")
	}
	first := readJWKS(t, server.URL)
	key, ok := publicKey(t, first.Keys[0]).(*rsa.PublicKey)
	if !ok || key.N.Cmp(backendKey.N) != 0 || key.E != backendKey.E {
		t.Fatal("backend public key bytes changed")
	}
	if _, err := jwt.Parse(exchange.Token, func(*jwt.Token) (any, error) { return key, nil }, jwt.WithIssuer("http://tokensmith:27780"), jwt.WithAudience("boot-service")); err != nil {
		t.Fatalf("backend signed exchange failed verification: %v", err)
	}
	revision.Store(1)
	second := readJWKS(t, server.URL)
	if len(first.Keys) != 3 || len(second.Keys) != 3 {
		t.Fatal("expected backend and two fixture keys")
	}
	if first.Keys[0].Kid != "backend-a" || second.Keys[0].Kid != "backend-b" {
		t.Fatal("backend key rotation not reflected")
	}
	if first.Keys[1].Kid != second.Keys[1].Kid || first.Keys[2].Kid != second.Keys[2].Kid {
		t.Fatal("fixture key IDs changed")
	}
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
