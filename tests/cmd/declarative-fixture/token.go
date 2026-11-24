package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type tokenRequest struct {
	Kind   string   `json:"kind"`
	Scopes []string `json:"scopes"`
}
type tokenClaims struct {
	Scope string `json:"scope"`
	jwt.RegisteredClaims
}

func (f *facade) token(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request tokenRequest
	if err := decoder.Decode(&request); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	for _, scope := range request.Scopes {
		if scope == "" || strings.ContainsAny(scope, " \t\r\n") {
			http.Error(w, "invalid scope", http.StatusBadRequest)
			return
		}
	}
	now := time.Now()
	claims := tokenClaims{Scope: strings.Join(request.Scopes, " "), RegisteredClaims: jwt.RegisteredClaims{Issuer: f.issuer, Subject: "declarative-fixture", Audience: jwt.ClaimStrings{"boot-service"}, ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)), NotBefore: jwt.NewNumericDate(now.Add(-time.Minute)), IssuedAt: jwt.NewNumericDate(now.Add(-time.Minute))}}
	method, key, kid := jwt.SigningMethodRS256, any(f.keys.rsa), f.keys.rsaJWK.Kid
	var signingMethod jwt.SigningMethod = method
	switch request.Kind {
	case "wrong-issuer":
		claims.Issuer = f.issuer + "/wrong"
	case "wrong-audience":
		claims.Audience = jwt.ClaimStrings{"wrong-audience"}
	case "expired":
		claims.ExpiresAt = jwt.NewNumericDate(now.Add(-time.Minute))
		claims.NotBefore = jwt.NewNumericDate(now.Add(-time.Hour))
		claims.IssuedAt = jwt.NewNumericDate(now.Add(-time.Hour))
	case "wrong-algorithm":
		signingMethod, key, kid = jwt.SigningMethodES256, f.keys.ec, f.keys.ecJWK.Kid
	case "bad-signature":
		key = f.keys.wrong
	default:
		http.Error(w, "invalid fixture kind", http.StatusBadRequest)
		return
	}
	token := jwt.NewWithClaims(signingMethod, claims)
	token.Header["kid"] = kid
	value, err := token.SignedString(key)
	if err != nil {
		http.Error(w, "fixture signing failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(struct {
		Token string `json:"token"`
	}{value})
}
