package main

import (
	"crypto/elliptic"
	"encoding/base64"
	"encoding/json"
	"math/big"
)

type backendPublicJWK struct {
	Kty string `json:"kty"`
	N   string `json:"n"`
	E   string `json:"e"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

func decodeJWKBytes(value string) ([]byte, bool) {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(value)
	return decoded, err == nil && len(decoded) != 0 && base64.RawURLEncoding.EncodeToString(decoded) == value
}

func validBackendPublicJWK(raw json.RawMessage) bool {
	var key backendPublicJWK
	if err := json.Unmarshal(raw, &key); err != nil {
		return false
	}
	switch key.Kty {
	case "RSA":
		n, validN := decodeJWKBytes(key.N)
		e, validE := decodeJWKBytes(key.E)
		if !validN || !validE || n[0] == 0 || e[0] == 0 || len(e) > 4 {
			return false
		}
		modulus := new(big.Int).SetBytes(n)
		exponent := new(big.Int).SetBytes(e)
		return modulus.BitLen() >= 1024 && modulus.Bit(0) == 1 && exponent.IsInt64() && exponent.Int64() >= 3 && exponent.Int64() <= 2147483647 && exponent.Bit(0) == 1 && modulus.Cmp(exponent) > 0
	case "EC":
		var curve elliptic.Curve
		switch key.Crv {
		case "P-256":
			curve = elliptic.P256()
		case "P-384":
			curve = elliptic.P384()
		case "P-521":
			curve = elliptic.P521()
		default:
			return false
		}
		x, validX := decodeJWKBytes(key.X)
		y, validY := decodeJWKBytes(key.Y)
		size := (curve.Params().BitSize + 7) / 8
		return validX && validY && len(x) == size && len(y) == size && curve.IsOnCurve(new(big.Int).SetBytes(x), new(big.Int).SetBytes(y))
	default:
		return false
	}
}
