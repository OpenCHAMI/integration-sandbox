//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"net/http/httptrace"
	"sync"
	"time"
)

type lifecycleWriteWindow struct {
	mu                     sync.Mutex
	start, end, completion time.Time
	startCount             int64
	invalid                bool
	started                chan struct{}
	firstByte              chan struct{}
}

func (r *lifecycleReaders) writerContext(ctx context.Context) (context.Context, *lifecycleWriteWindow) {
	window := &lifecycleWriteWindow{started: make(chan struct{}), firstByte: make(chan struct{})}
	r.window.Store(window)
	trace := &httptrace.ClientTrace{
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			at := time.Now()
			window.mu.Lock()
			defer window.mu.Unlock()
			if info.Err != nil || !window.start.IsZero() || !window.end.IsZero() {
				window.invalid = true
				return
			}
			window.start = at
			window.startCount = r.completed.Load()
			close(window.started)
		},
		GotFirstResponseByte: func() {
			at := time.Now()
			window.mu.Lock()
			defer window.mu.Unlock()
			if window.end.IsZero() {
				window.end = at
				close(window.firstByte)
			}
		},
	}
	return httptrace.WithClientTrace(ctx, trace), window
}

func (r *lifecycleReaders) recordCompletion(at time.Time) {
	window := r.window.Load()
	if window == nil {
		r.completed.Add(1)
		return
	}
	window.mu.Lock()
	defer window.mu.Unlock()
	count := r.completed.Add(1)
	if !window.start.IsZero() && at.After(window.start) && count > window.startCount && (window.completion.IsZero() || at.Before(window.completion)) {
		window.completion = at
	}
}

func (window *lifecycleWriteWindow) finish(err error) bool {
	window.mu.Lock()
	defer window.mu.Unlock()
	if err != nil && window.end.IsZero() {
		window.end = time.Now()
	}
	return !window.invalid && !window.start.IsZero() && !window.end.IsZero() && window.completion.After(window.start) && window.completion.Before(window.end)
}
