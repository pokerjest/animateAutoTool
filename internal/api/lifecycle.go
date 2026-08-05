package api

import (
	"context"
	"log"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"
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
var backgroundTaskSequence atomic.Uint64

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
	log.Printf("API background tasks: started accepting=true")
}

// GoBackground starts tracked request-independent work. New work is rejected
// after shutdown begins, and WaitBackgroundTasks blocks until accepted work
// has finished before SQLite is closed.
func GoBackground(run func(context.Context), taskNames ...string) bool {
	if run == nil {
		log.Printf("WARN: API background task rejected reason=nil_runner")
		return false
	}
	taskName := strings.TrimSpace(strings.Join(taskNames, " "))
	if taskName == "" {
		if pc, _, _, ok := runtime.Caller(1); ok {
			if fn := runtime.FuncForPC(pc); fn != nil {
				taskName = fn.Name()
			}
		}
	}
	if taskName == "" {
		taskName = "anonymous"
	}
	appBackgroundTasks.mu.Lock()
	if !appBackgroundTasks.started {
		appBackgroundTasks.ctx, appBackgroundTasks.cancel = context.WithCancel(context.Background())
		appBackgroundTasks.accepting = true
		appBackgroundTasks.started = true
		log.Printf("API background tasks: implicitly started accepting=true")
	}
	if !appBackgroundTasks.accepting || appBackgroundTasks.ctx == nil {
		appBackgroundTasks.mu.Unlock()
		log.Printf("WARN: API background task rejected task=%s reason=shutdown_in_progress", taskName)
		return false
	}
	ctx := appBackgroundTasks.ctx
	taskID := backgroundTaskSequence.Add(1)
	appBackgroundTasks.wg.Add(1)
	appBackgroundTasks.mu.Unlock()

	startedAt := time.Now()
	log.Printf("API background task started task_id=%d task=%s", taskID, taskName)
	go func() {
		defer appBackgroundTasks.wg.Done()
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Printf(
					"ERROR: API background task panic task_id=%d task=%s duration=%s recovery_action=inspect_stack_and_retry panic=%v\n%s",
					taskID,
					taskName,
					time.Since(startedAt).Round(time.Millisecond),
					recovered,
					debug.Stack(),
				)
				return
			}
			log.Printf(
				"API background task completed task_id=%d task=%s duration=%s context=%v",
				taskID,
				taskName,
				time.Since(startedAt).Round(time.Millisecond),
				ctx.Err(),
			)
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
	log.Printf("API background tasks: stop requested accepting=false")
}

func WaitBackgroundTasks() {
	log.Printf("API background tasks: waiting for accepted tasks")
	appBackgroundTasks.wg.Wait()
	log.Printf("API background tasks: all accepted tasks finished")
}
