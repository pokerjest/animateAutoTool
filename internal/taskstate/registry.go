package taskstate

import (
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/event"
)

type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusError     Status = "error"
)

type Task struct {
	TaskID    string    `json:"task_id"`
	Kind      string    `json:"kind"`
	Title     string    `json:"title"`
	Status    Status    `json:"status"`
	Message   string    `json:"message"`
	Current   int64     `json:"current,omitempty"`
	Total     int64     `json:"total,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// maxTrackedTasks caps retained task history. Active tasks are never evicted,
// so the registry may temporarily exceed this limit while more than this many
// tasks are queued or running.
const maxTrackedTasks = 200

type Registry struct {
	mu    sync.RWMutex
	tasks map[string]Task
}

func NewRegistry() *Registry {
	return &Registry{tasks: make(map[string]Task)}
}

var Global = NewRegistry()

func (r *Registry) Start(taskID, kind, title, message string) Task {
	return r.update(Task{TaskID: taskID, Kind: kind, Title: title, Status: StatusRunning, Message: message})
}

func (r *Registry) Progress(taskID, message string, current, total int64) Task {
	previous, _ := r.Get(taskID)
	previous.TaskID = taskID
	previous.Status = StatusRunning
	previous.Message = message
	previous.Current = current
	previous.Total = total
	return r.update(previous)
}

func (r *Registry) Complete(taskID, message string) Task {
	previous, _ := r.Get(taskID)
	previous.TaskID = taskID
	previous.Status = StatusCompleted
	previous.Message = message
	if previous.Total > 0 {
		previous.Current = previous.Total
	}
	return r.update(previous)
}

func (r *Registry) Fail(taskID string, err error) Task {
	message := "任务执行失败"
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		message = strings.TrimSpace(err.Error())
	}
	previous, _ := r.Get(taskID)
	previous.TaskID = taskID
	previous.Status = StatusError
	previous.Message = message
	log.Printf("Task failed: id=%s kind=%s title=%q error=%s", taskID, previous.Kind, previous.Title, message)
	return r.update(previous)
}

func (r *Registry) Get(taskID string) (Task, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	task, ok := r.tasks[taskID]
	return task, ok
}

func (r *Registry) List() []Task {
	r.mu.RLock()
	items := make([]Task, 0, len(r.tasks))
	for _, task := range r.tasks {
		items = append(items, task)
	}
	r.mu.RUnlock()
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
	if len(items) > 50 {
		items = items[:50]
	}
	return items
}

func (r *Registry) Reset() {
	r.mu.Lock()
	r.tasks = make(map[string]Task)
	r.mu.Unlock()
}

func (r *Registry) update(task Task) Task {
	task.TaskID = strings.TrimSpace(task.TaskID)
	if task.TaskID == "" {
		return task
	}
	task.UpdatedAt = time.Now().UTC()
	r.mu.Lock()
	r.tasks[task.TaskID] = task
	if len(r.tasks) > maxTrackedTasks {
		r.evictOldestLocked(maxTrackedTasks)
	}
	r.mu.Unlock()
	event.GlobalBus.Publish(event.EventTaskUpdate, task)
	return task
}

// evictOldestLocked removes the oldest terminal tasks until the registry is at
// the requested size. Queued and running tasks must remain addressable by ID.
// Callers must hold r.mu.
func (r *Registry) evictOldestLocked(keep int) {
	if len(r.tasks) <= keep {
		return
	}
	items := make([]Task, 0, len(r.tasks))
	for _, task := range r.tasks {
		if task.Status == StatusCompleted || task.Status == StatusError {
			items = append(items, task)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt.Before(items[j].UpdatedAt) })
	remove := len(r.tasks) - keep
	if remove > len(items) {
		remove = len(items)
	}
	for _, task := range items[:remove] {
		delete(r.tasks, task.TaskID)
	}
}
