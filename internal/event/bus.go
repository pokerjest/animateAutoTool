package event

import (
	"log"
	"runtime/debug"
	"sync"

	"github.com/google/uuid"
)

// EventType 定义事件类型
type EventType string

const (
	// 定义常用事件
	EventScanProgress     EventType = "scan_progress"
	EventScanComplete     EventType = "scan_complete"
	EventMetadataUpdated  EventType = "metadata_updated"
	EventDownloadProgress EventType = "download_progress"
	EventDownloadReady    EventType = "download_ready"
	EventLibraryIssue     EventType = "library_issue"
	EventScanRun          EventType = "scan_run"
	EventSubscriptionRun  EventType = "subscription_run"
	EventSchedulerRun     EventType = "scheduler_run"
	EventTaskUpdate       EventType = "task_update"
)

// Event 代表一个系统事件
type Event struct {
	Type    EventType
	Payload interface{}
}

// Handler 处理事件的函数签名
type Handler func(event Event)

// Bus 事件总线接口
type Bus interface {
	Subscribe(topic EventType, handler Handler) string // 返回 Subscription ID
	Unsubscribe(topic EventType, subID string)
	Publish(topic EventType, payload interface{})
	// Wait 停止接收新事件并阻塞到已派发的异步 Handler 执行完毕。
	Wait()
}

// HandlerWrapper 包装 Handler 以便识别
type HandlerWrapper struct {
	ID      string
	Handler Handler
}

// InMemoryBus 简单的内存事件总线实现
type InMemoryBus struct {
	mu       sync.RWMutex
	handlers map[EventType][]HandlerWrapper
	wg       sync.WaitGroup
	closed   bool
}

// GlobalBus 全局单例
var GlobalBus Bus = NewInMemoryBus()

func NewInMemoryBus() *InMemoryBus {
	return &InMemoryBus{
		handlers: make(map[EventType][]HandlerWrapper),
	}
}

func (b *InMemoryBus) Subscribe(topic EventType, handler Handler) string {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := uuid.New().String()
	wrapper := HandlerWrapper{ID: id, Handler: handler}
	b.handlers[topic] = append(b.handlers[topic], wrapper)
	return id
}

func (b *InMemoryBus) Unsubscribe(topic EventType, subID string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	wrappers := b.handlers[topic]
	for i, w := range wrappers {
		if w.ID == subID {
			// Remove
			b.handlers[topic] = append(wrappers[:i], wrappers[i+1:]...)
			break
		}
	}
}

func (b *InMemoryBus) Publish(topic EventType, payload interface{}) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	wrappers := append([]HandlerWrapper(nil), b.handlers[topic]...)
	// Add while holding the same lock used by Wait. This guarantees that Wait
	// either observes this publication or closes the bus before it begins.
	b.wg.Add(len(wrappers))
	b.mu.Unlock()

	// 异步执行所有 Handler，避免阻塞发布者
	evt := Event{Type: topic, Payload: payload}
	for _, w := range wrappers {
		handler := w.Handler
		subID := w.ID
		go func() {
			defer b.wg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Printf(
						"ERROR: EventBus: handler panic topic=%s sub_id=%s recovery_action=continue_other_handlers panic=%v\n%s",
						topic,
						subID,
						r,
						debug.Stack(),
					)
				}
			}()
			handler(evt)
		}()
	}
}

// Wait stops accepting new publications and blocks until every handler that
// was already dispatched has completed. A bus is not reusable after Wait.
func (b *InMemoryBus) Wait() {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	b.wg.Wait()
}
