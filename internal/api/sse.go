package api

import (
	"encoding/json"
	"log"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/event"
)

// SSEHandler 处理 Server-Sent Events 连接
func SSEHandler(c *gin.Context) {
	// 1. 设置 Header
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Transfer-Encoding", "chunked")

	// 2. 创建一个 Channel 来接收事件
	// 注意：EventBus 是多播的，我们需要一个由 EventBus 驱动的临时 Bridge
	// 这里为了简单，我们让 GlobalBus 支持 SubscribeChannel 或者我们在内部搞一个转发
	// 但我们的 InMemoryBus 目前是 callback 模式。
	// 我们可以创建一个 Callback，将收到的消息写入 clientChan

	clientChan := make(chan event.Event, 10)

	// 定义一个闭包 Handler，用于转发消息
	bridgeHandler := func(e event.Event) {
		// 非阻塞发送，避免慢客户端阻塞总线
		select {
		case clientChan <- e:
		default:
			// Client channel full, drop message or log?
		}
	}

	// 3. 订阅感兴趣的事件
	// 目前订阅所有核心事件
	topics := []event.EventType{
		event.EventScanProgress,
		event.EventScanComplete,
		event.EventMetadataUpdated,
		event.EventDownloadProgress,
	}

	var subIDs = make(map[event.EventType]string)

	for _, t := range topics {
		id := event.GlobalBus.Subscribe(t, bridgeHandler)
		subIDs[t] = id
	}

	// 4. 发送初始连接成功消息 (可选)
	c.SSEvent("message", "connected")
	c.Writer.Flush()

	// 5. 循环推送
	// 监听 clientChan 和 Context.Done (客户端断开)
	defer func() {
		// 清理 Subscription
		for t, id := range subIDs {
			event.GlobalBus.Unsubscribe(t, id)
		}
		close(clientChan)
		log.Println("SSE Client disconnected")
	}()

	for {
		select {
		case evt := <-clientChan:
			// 序列化 Payload
			data, err := json.Marshal(evt.Payload)
			if err != nil {
				log.Printf("SSE JSON Marshal error: %v", err)
				continue
			}
			// 发送事件，事件名即为 Topic
			c.SSEvent(string(evt.Type), string(data))
			c.Writer.Flush()

		case <-c.Writer.CloseNotify():
			return
		}
	}
}
