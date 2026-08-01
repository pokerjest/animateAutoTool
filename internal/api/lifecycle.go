package api

import (
	"context"
	"log"
	"sync"
)

type backgroundTaskRegistry struct {
	mu        sync.Mutex
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	accepting bool
	started   bool
}

var appBackgroundTasks backgroundTaskRegistry

// StartBackgroundTasks installs the application context used by API work that
// outlives its initiating HTTP request.
func StartBackgroundTasks(parent context.Context) {
	if parent == nil {
		parent = context.Background()
	}
	appBackgroundTasks.mu.Lock()
	defer appBackgroundTasks.mu.Unlock()
	if appBackgroundTasks.cancel != nil {
		appBackgroundTasks.cancel()
	}
	appBackgroundTasks.ctx, appBackgroundTasks.cancel = context.WithCancel(parent)
	appBackgroundTasks.accepting = true
	appBackgroundTasks.started = true
}

// GoBackground starts tracked request-independent work. New work is rejected
// after shutdown begins, and WaitBackgroundTasks blocks until accepted work
// has finished before SQLite is closed.
func GoBackground(run func(context.Context)) bool {
	if run == nil {
		return false
	}
	appBackgroundTasks.mu.Lock()
	if !appBackgroundTasks.started {
		appBackgroundTasks.ctx, appBackgroundTasks.cancel = context.WithCancel(context.Background())
		appBackgroundTasks.accepting = true
		appBackgroundTasks.started = true
	}
	if !appBackgroundTasks.accepting || appBackgroundTasks.ctx == nil {
		appBackgroundTasks.mu.Unlock()
		return false
	}
	ctx := appBackgroundTasks.ctx
	appBackgroundTasks.wg.Add(1)
	appBackgroundTasks.mu.Unlock()

	go func() {
		defer appBackgroundTasks.wg.Done()
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Printf("API background task panic: %v", recovered)
			}
		}()
		run(ctx)
	}()
	return true
}

func StopBackgroundTasks() {
	appBackgroundTasks.mu.Lock()
	appBackgroundTasks.accepting = false
	cancel := appBackgroundTasks.cancel
	appBackgroundTasks.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func WaitBackgroundTasks() {
	appBackgroundTasks.wg.Wait()
}
