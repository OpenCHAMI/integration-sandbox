package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type testKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
	D   string `json:"d"`
}
type testKeys struct {
	Keys []testKey `json:"keys"`
}

func readJWKS(t *testing.T, base string) testKeys {
	t.Helper()
	response, err := http.Get(base + "/.well-known/jwks.json")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("JWKS status %d", response.StatusCode)
	}
	var keys testKeys
	if err := json.NewDecoder(response.Body).Decode(&keys); err != nil {
		t.Fatal(err)
	}
	for _, key := range keys.Keys {
		if key.D != "" {
			t.Fatal("private key in JWKS")
		}
	}
	return keys
}

func publicKey(t *testing.T, key testKey) any {
	t.Helper()
	decode := func(value string) *big.Int {
		bytes, err := base64.RawURLEncoding.DecodeString(value)
		if err != nil {
			t.Fatal(err)
		}
		return new(big.Int).SetBytes(bytes)
	}
	switch key.Kty {
	case "RSA":
		return &rsa.PublicKey{N: decode(key.N), E: int(decode(key.E).Int64())}
	case "EC":
		if key.Crv != "P-256" {
			t.Fatal("wrong curve")
		}
		return &ecdsa.PublicKey{Curve: elliptic.P256(), X: decode(key.X), Y: decode(key.Y)}
	default:
		t.Fatal("unknown key type")
		return nil
	}
}

func TestTokenFixturesHaveExactlyOneDefect(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { ioWriteJSON(w, `{"keys":[]}`) }))
	defer backend.Close()
	upstream, _ := url.Parse(backend.URL)
	h, err := newFacade(upstream, "http://tokensmith:27780", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	keys := readJWKS(t, server.URL)
	for _, kind := range []string{"wrong-issuer", "wrong-audience", "expired", "wrong-algorithm", "bad-signature"} {
		t.Run(kind, func(t *testing.T) {
			response, err := http.Post(server.URL+"/__fixtures/token", "application/json", strings.NewReader(`{"kind":"`+kind+`","scopes":["read:boot","propose:boot","promote:boot"]}`))
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != 200 {
				t.Fatalf("token endpoint status = %d", response.StatusCode)
			}
			var body struct {
				Token string `json:"token"`
			}
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			claims := jwt.MapClaims{}
			token, err := jwt.ParseWithClaims(body.Token, claims, func(token *jwt.Token) (any, error) {
				kid, _ := token.Header["kid"].(string)
				for _, key := range keys.Keys {
					if key.Kid == kid {
						return publicKey(t, key), nil
					}
				}
				return nil, errors.New("unknown kid")
			}, jwt.WithoutClaimsValidation())
			if kind == "bad-signature" {
				if !errors.Is(err, jwt.ErrTokenSignatureInvalid) {
					t.Fatalf("expected signature failure, got %v", err)
				}
			} else if err != nil {
				t.Fatalf("signature validation: %v", err)
			}
			wantAlg := "RS256"
			if kind == "wrong-algorithm" {
				wantAlg = "ES256"
			}
			if token.Method.Alg() != wantAlg {
				t.Fatalf("algorithm = %s", token.Method.Alg())
			}
			issuer, err := claims.GetIssuer()
			if err != nil || (issuer != "http://tokensmith:27780") != (kind == "wrong-issuer") {
				t.Fatal("incorrect issuer defect")
			}
			aud, err := claims.GetAudience()
			if err != nil || (len(aud) != 1 || aud[0] != "boot-service") != (kind == "wrong-audience") {
				t.Fatal("incorrect audience defect")
			}
			exp, err := claims.GetExpirationTime()
			if err != nil || exp == nil || exp.Before(time.Now()) != (kind == "expired") {
				t.Fatal("incorrect expiry defect")
			}
			nbf, err := claims.GetNotBefore()
			if err != nil || nbf == nil || nbf.After(time.Now()) {
				t.Fatal("invalid not-before")
			}
			if claims["scope"] != "read:boot propose:boot promote:boot" {
				t.Fatal("requested scopes changed")
			}
		})
	}
}

func ioWriteJSON(w http.ResponseWriter, data string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(data))
}
