package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
)

type publicJWK struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n,omitempty"`
	E   string `json:"e,omitempty"`
	Crv string `json:"crv,omitempty"`
	X   string `json:"x,omitempty"`
	Y   string `json:"y,omitempty"`
}

type fixtureKeys struct {
	rsa    *rsa.PrivateKey
	wrong  *rsa.PrivateKey
	ec     *ecdsa.PrivateKey
	rsaJWK publicJWK
	ecJWK  publicJWK
}

func rsaPublicJWK(key *rsa.PublicKey, kid string) publicJWK {
	enc := base64.RawURLEncoding.EncodeToString
	return publicJWK{Kty: "RSA", Kid: kid, Use: "sig", Alg: "RS256", N: enc(key.N.Bytes()), E: enc(big.NewInt(int64(key.E)).Bytes())}
}

func keyID(key any) (string, error) {
	bytes, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return "", fmt.Errorf("encode public key: %w", err)
	}
	sum := sha256.Sum256(bytes)
	return "fixture-" + base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

func generateKeys(dir string) (*fixtureKeys, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create fixture key directory: %w", err)
	}
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generate RSA key: %w", err)
	}
	wrong, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generate invalid-signature key: %w", err)
	}
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate EC key: %w", err)
	}
	rsaID, err := keyID(&rsaKey.PublicKey)
	if err != nil {
		return nil, err
	}
	ecID, err := keyID(&ec.PublicKey)
	if err != nil {
		return nil, err
	}
	for _, item := range []struct {
		name string
		key  any
	}{{rsaID, rsaKey}, {ecID, ec}, {rsaID + "-invalid", wrong}} {
		encoded, err := x509.MarshalPKCS8PrivateKey(item.key)
		if err != nil {
			return nil, fmt.Errorf("encode fixture key: %w", err)
		}
		path := filepath.Join(dir, item.name+".pem")
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return nil, fmt.Errorf("create fixture key: %w", err)
		}
		writeErr := pem.Encode(file, &pem.Block{Type: "PRIVATE KEY", Bytes: encoded})
		closeErr := file.Close()
		if writeErr != nil {
			return nil, fmt.Errorf("write fixture key: %w", writeErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close fixture key: %w", closeErr)
		}
	}
	enc := base64.RawURLEncoding.EncodeToString
	return &fixtureKeys{rsa: rsaKey, wrong: wrong, ec: ec, rsaJWK: rsaPublicJWK(&rsaKey.PublicKey, rsaID), ecJWK: publicJWK{Kty: "EC", Kid: ecID, Use: "sig", Alg: "ES256", Crv: "P-256", X: enc(ec.X.FillBytes(make([]byte, 32))), Y: enc(ec.Y.FillBytes(make([]byte, 32)))}}, nil
}
