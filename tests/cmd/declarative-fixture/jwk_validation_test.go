package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
)

func TestFacadeValidatesBackendPublicKeyMaterial(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	rsaJWK := rsaPublicJWK(&rsaKey.PublicKey, "")
	enc := base64.RawURLEncoding.EncodeToString
	rsaJSON := mustJSON(t, rsaJWK)
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
	cases := []struct {
		name string
		keys []json.RawMessage
		want int
	}{
		{"empty key", []json.RawMessage{json.RawMessage(`{}`)}, 502},
		{"mixed valid and invalid", []json.RawMessage{rsaJSON, json.RawMessage(`{}`)}, 502},
		{"unknown type", []json.RawMessage{json.RawMessage(`{"kty":"other"}`)}, 502},
		{"unsupported OKP", []json.RawMessage{json.RawMessage(`{"kty":"OKP","crv":"Ed25519","x":"` + enc(make([]byte, 32)) + `"}`)}, 502},
		{"missing RSA exponent", []json.RawMessage{json.RawMessage(`{"kty":"RSA","n":"` + rsaJWK.N + `"}`)}, 502},
		{"invalid RSA base64", []json.RawMessage{json.RawMessage(`{"kty":"RSA","n":"%%%","e":"AQAB"}`)}, 502},
		{"tiny RSA modulus", []json.RawMessage{json.RawMessage(`{"kty":"RSA","n":"AQ","e":"Aw"}`)}, 502},
		{"even RSA modulus", []json.RawMessage{json.RawMessage(`{"kty":"RSA","n":"` + enc(append(rsaKey.N.Bytes()[:len(rsaKey.N.Bytes())-1], 0)) + `","e":"AQAB"}`)}, 502},
		{"RSA exponent one", []json.RawMessage{json.RawMessage(`{"kty":"RSA","n":"` + rsaJWK.N + `","e":"AQ"}`)}, 502},
		{"RSA exponent even", []json.RawMessage{json.RawMessage(`{"kty":"RSA","n":"` + rsaJWK.N + `","e":"Ag"}`)}, 502},
		{"RSA exponent too large", []json.RawMessage{json.RawMessage(`{"kty":"RSA","n":"` + rsaJWK.N + `","e":"gAAAAQ"}`)}, 502},
		{"noncanonical RSA integer", []json.RawMessage{json.RawMessage(`{"kty":"RSA","n":"` + enc(append([]byte{0}, rsaKey.N.Bytes()...)) + `","e":"AQAB"}`)}, 502},
		{"padded RSA encoding", []json.RawMessage{json.RawMessage(`{"kty":"RSA","n":"` + rsaJWK.N + `=","e":"AQAB"}`)}, 502},
		{"unknown EC curve", []json.RawMessage{json.RawMessage(`{"kty":"EC","crv":"unknown","x":"AA","y":"AA"}`)}, 502},
		{"invalid EC base64", []json.RawMessage{json.RawMessage(`{"kty":"EC","crv":"P-256","x":"%%%","y":"AA"}`)}, 502},
		{"incorrect EC coordinate size", []json.RawMessage{json.RawMessage(`{"kty":"EC","crv":"P-256","x":"AA","y":"AA"}`)}, 502},
		{"off curve point", []json.RawMessage{json.RawMessage(`{"kty":"EC","crv":"P-256","x":"` + enc(make([]byte, 32)) + `","y":"` + enc(make([]byte, 32)) + `"}`)}, 502},
		{"valid RSA without kid", []json.RawMessage{rsaJSON}, 200},
	}
	for _, private := range []string{"d", "p", "q", "dp", "dq", "qi", "oth", "k"} {
		data := json.RawMessage(`{"kty":"RSA","n":"` + rsaJWK.N + `","e":"` + rsaJWK.E + `","` + private + `":"private"}`)
		cases = append(cases, struct {
			name string
			keys []json.RawMessage
			want int
		}{"private " + private, []json.RawMessage{data}, 502})
	}
	for _, curve := range []struct {
		name  string
		curve elliptic.Curve
		size  int
	}{{"P-256", elliptic.P256(), 32}, {"P-384", elliptic.P384(), 48}, {"P-521", elliptic.P521(), 66}} {
		key, err := ecdsa.GenerateKey(curve.curve, rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		value := struct {
			Kty string `json:"kty"`
			Crv string `json:"crv"`
			X   string `json:"x"`
			Y   string `json:"y"`
		}{Kty: "EC", Crv: curve.name, X: enc(key.X.FillBytes(make([]byte, curve.size))), Y: enc(key.Y.FillBytes(make([]byte, curve.size)))}
		cases = append(cases, struct {
			name string
			keys []json.RawMessage
			want int
		}{"valid " + curve.name + " without kid", []json.RawMessage{mustJSON(t, value)}, 200})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(jwks{Keys: tc.keys})
			if err != nil {
				t.Fatal(err)
			}
			document.Store(string(data))
			response, err := http.Get(server.URL + "/.well-known/jwks.json")
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != tc.want {
				t.Fatalf("status = %d, want %d", response.StatusCode, tc.want)
			}
		})
	}
}
