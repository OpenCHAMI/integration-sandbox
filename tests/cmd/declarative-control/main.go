// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"reflect"
	"strings"
	"syscall"
	"time"
)

var errInput = errors.New("invalid control request")

func decodeRequest[T any](raw json.RawMessage) (T, error) {
	var request T
	if len(raw) == 0 || len(raw) > 16384 || raw[0] != '{' || !uniqueJSON(raw) {
		return request, errInput
	}
	var keys map[string]json.RawMessage
	if json.Unmarshal(raw, &keys) != nil {
		return request, errInput
	}
	typ := reflect.TypeOf(request)
	if typ == nil || typ.Kind() != reflect.Struct {
		return request, errInput
	}
	allowed := make(map[string]struct{}, typ.NumField())
	for i := range typ.NumField() {
		field := typ.Field(i)
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "" {
			name = field.Name
		}
		if name != "-" {
			allowed[name] = struct{}{}
		}
	}
	for key := range keys {
		if _, ok := allowed[key]; !ok {
			return request, errInput
		}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&request) != nil || dec.Decode(&struct{}{}) != io.EOF {
		return request, errInput
	}
	return request, nil
}

func main() {
	if len(os.Args) == 3 && os.Args[1] == "recover-watch" {
		c, err := configFromEnv()
		if err == nil {
			err = c.recoverWatch(os.Args[2], os.Stdin, os.Stdout)
		}
		if err != nil {
			os.Exit(1)
		}
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "--help" {
		fmt.Fprintln(os.Stdout, "Usage: declarative-control ACTION SINGLE_JSON_ARGUMENT")
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	if err := execute(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "declarative-control: operation failed")
		os.Exit(1)
	}
}

func execute(ctx context.Context, args []string, out io.Writer) error {
	if len(args) != 2 || len(args[1]) > 16384 || !json.Valid([]byte(args[1])) || !uniqueJSON([]byte(args[1])) {
		return errInput
	}
	request := bytes.TrimSpace([]byte(args[1]))
	if len(request) == 0 || request[0] != '{' {
		return errInput
	}
	handler := actions[args[0]]
	if handler == nil {
		return errInput
	}
	c, err := configFromEnv()
	if err != nil {
		return err
	}
	ctx = context.WithValue(ctx, controlActionKey{}, args[0])
	result, err := handler(ctx, c, json.RawMessage(request))
	if err != nil || !json.Valid(result) || len(result) == 0 || result[0] != '{' {
		return errInput
	}
	_, err = fmt.Fprintln(out, string(result))
	return err
}
