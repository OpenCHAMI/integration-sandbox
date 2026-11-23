// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"crypto/rand"
	"math/big"
	"time"
)

func nativeRevisionUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	millis := uint64(time.Now().UnixMilli())
	for i := 5; i >= 0; i-- {
		b[i] = byte(millis)
		millis >>= 8
	}
	b[6] = b[6]&0x0f | 0x70
	b[8] = b[8]&0x3f | 0x80
	n := new(big.Int).SetBytes(b[:])
	mask := big.NewInt(31)
	alphabet := "0123456789abcdefghjkmnpqrstvwxyz"
	encoded := make([]byte, 26)
	for i := len(encoded) - 1; i >= 0; i-- {
		encoded[i] = alphabet[new(big.Int).And(n, mask).Int64()]
		n.Rsh(n, 5)
	}
	return "rev_" + string(encoded), nil
}
