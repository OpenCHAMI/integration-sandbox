//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type lifecycleReaders struct {
	ctx       context.Context
	cancel    context.CancelFunc
	active    atomic.Int64
	completed atomic.Int64
	stopping  atomic.Bool
	window    atomic.Pointer[lifecycleWriteWindow]
	errs      chan error
	ready     chan struct{}
	wg        sync.WaitGroup
}

func lifecycleStartReaders(parent context.Context, c *lifecycleCase, target, pinned url.Values) *lifecycleReaders {
	ctx, cancel := context.WithCancel(parent)
	r := &lifecycleReaders{ctx: ctx, cancel: cancel, errs: make(chan error, 4), ready: make(chan struct{}, 4)}
	r.wg.Add(4)
	for range 4 {
		go func() {
			defer r.wg.Done()
			announced := false
			for ctx.Err() == nil && !r.stopping.Load() {
				r.active.Add(1)
				response, err := c.query(ctx, target)
				completedAt := time.Now()
				r.active.Add(-1)
				if ctx.Err() != nil {
					return
				}
				if err != nil || response.Status != http.StatusOK || (!c.matches(response.Body, "A") && !c.matches(response.Body, "B")) {
					r.errs <- errors.New("concurrent target render was not a complete A or B tuple")
					return
				}
				r.recordCompletion(completedAt)
				response, err = c.query(ctx, pinned)
				if ctx.Err() != nil {
					return
				}
				if err != nil || response.Status != http.StatusOK || !c.matches(response.Body, "A") {
					r.errs <- errors.New("pinned render changed during writes")
					return
				}
				if !announced {
					r.ready <- struct{}{}
					announced = true
				}
			}
		}()
	}
	for range 4 {
		select {
		case <-r.ready:
		case <-ctx.Done():
			return r
		case err := <-r.errs:
			r.errs <- err
			return r
		}
	}
	return r
}

func (r *lifecycleReaders) beforeWrite(t *testing.T) {
	t.Helper()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if r.active.Load() >= 2 && r.completed.Load() > 0 {
			return
		}
		select {
		case err := <-r.errs:
			t.Fatal(err)
		case <-r.ctx.Done():
			t.Fatalf("concurrent readers did not become active: %v", r.ctx.Err())
		case <-ticker.C:
		}
	}
}

func (r *lifecycleReaders) stop(t *testing.T) {
	t.Helper()
	// Stop scheduling requests, then drain every response before releasing writers.
	// Canceling an in-flight request would correctly poison the snapshot barrier.
	r.stopping.Store(true)
	r.wg.Wait()
	r.cancel()
	close(r.errs)
	for err := range r.errs {
		t.Error(err)
	}
}
