//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/openchami/integration-sandbox/tests/declarativeboot"
)

var errRequest = errors.New("boot HTTP request failed")

type streams struct {
	input  io.Reader
	output io.Writer
}

func run(ctx context.Context, args []string, stream streams) error {
	flags := flag.NewFlagSet("boot-http", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	method := flags.String("method", http.MethodGet, "")
	status := flags.Bool("status", false, "")
	if err := flags.Parse(args); err != nil || flags.NArg() != 1 || (*method != http.MethodGet && *method != http.MethodPost) {
		return errRequest
	}
	u, err := url.Parse(flags.Arg(0))
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return errRequest
	}
	client, err := declarativeboot.NewHTTPClient(u.Scheme+"://"+u.Host, 20*time.Second)
	if err != nil {
		return errRequest
	}
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}
	request := declarativeboot.Request{Method: *method, Path: path}
	if *method == http.MethodPost {
		request.Body, err = io.ReadAll(io.LimitReader(stream.input, 16385))
		if err != nil || len(request.Body) > 16384 {
			return errRequest
		}
		request.Headers = http.Header{"Content-Type": {"application/json"}}
	}
	response, err := client.Do(ctx, request)
	if err != nil {
		return errRequest
	}
	if *status || (response.Status >= 200 && response.Status < 300) {
		if *status && len(response.Body) > 16384 {
			return errRequest
		}
		if _, err := stream.output.Write(response.Body); err != nil {
			return err
		}
	}
	if *status {
		if _, err := fmt.Fprintf(stream.output, "\n__SBX_HTTP_STATUS__%d\n", response.Status); err != nil {
			return err
		}
	}
	if response.Status < 200 || response.Status >= 300 {
		return errRequest
	}
	return nil
}

func main() {
	if err := run(context.Background(), os.Args[1:], streams{input: os.Stdin, output: os.Stdout}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
