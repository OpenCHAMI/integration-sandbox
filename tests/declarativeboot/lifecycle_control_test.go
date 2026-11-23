//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"reflect"
	"testing"
)

type lifecycleController struct {
	bin string
	run CommandRunner
}

func (control lifecycleController) action(ctx context.Context, action string) error {
	if control.bin == "" || control.run == nil || (action != "service-stop" && action != "service-start") {
		return errors.New("invalid lifecycle control configuration")
	}
	body, err := json.Marshal(struct {
		Service string `json:"service"`
	}{Service: "smd"})
	if err != nil {
		return errors.New("encode lifecycle control")
	}
	if _, err := control.run(ctx, control.bin, action, string(body)); err != nil {
		return errors.New("lifecycle control failed")
	}
	return nil
}

func lifecycleControlRun(ctx context.Context, args ...string) ([]byte, error) {
	if len(args) != 3 {
		return nil, errors.New("invalid lifecycle control arguments")
	}
	return nil, exec.CommandContext(ctx, args[0], args[1:]...).Run()
}

func TestLifecycleHelpersControlArguments(t *testing.T) {
	var got []string
	control := lifecycleController{bin: "/owned/control", run: func(_ context.Context, args ...string) ([]byte, error) {
		got = append([]string(nil), args...)
		return nil, nil
	}}
	if err := control.action(context.Background(), "service-stop"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"/owned/control", "service-stop", `{"service":"smd"}`}) {
		t.Fatal("control did not receive the expected action and JSON arguments")
	}
	if err := control.action(context.Background(), "unsupported"); err == nil {
		t.Fatal("unsupported control action accepted")
	}
}
