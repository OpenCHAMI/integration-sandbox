// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

func cleanStop(v container) bool {
	return v.stopStateComplete && !v.State.Running && !v.State.Paused && v.State.ExitCode == 0 && !v.State.OOMKilled
}
