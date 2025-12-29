package event

import (
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
	b.mu.RLock()
	wrappers := b.handlers[topic]
	b.mu.RUnlock()

	// 异步执行所有 Handler，避免阻塞发布者
	evt := Event{Type: topic, Payload: payload}
	for _, w := range wrappers {
		go w.Handler(evt)
	}
}
