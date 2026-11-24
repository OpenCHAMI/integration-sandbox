// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

func main() {
	body, err := json.Marshal(os.Args[1:])
	if err != nil {
		os.Exit(1)
	}
	client := http.Client{Timeout: 100 * time.Second}
	response, err := client.Post(os.Getenv("QUIESCE_FAKE_DOCKER_URL"), "application/json", bytes.NewReader(body))
	if err != nil {
		os.Exit(1)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		fmt.Fprint(os.Stderr, "simulated-issuer-stderr-secret")
		os.Exit(1)
	}
	if _, err := io.Copy(os.Stdout, response.Body); err != nil {
		os.Exit(1)
	}
}
