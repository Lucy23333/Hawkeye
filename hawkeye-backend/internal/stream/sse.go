package stream

import (
	"fmt"
	"net/http"
	"sync"
)

// 📢 定义一个广播站
type SSEBroker struct {
	Clients map[chan string]bool // 记录所有正在听广播的客户端
	mu      sync.Mutex           // 🔥 改名：通常习惯用 mu 代表 Mutex，避免和方法名混淆
}

// 全局唯一的广播站实例
var AlertBroker = &SSEBroker{
	Clients: make(map[chan string]bool),
}

// 🌐 这是给前端连接的 HTTP 接口
func (b *SSEBroker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 1. 设置 SSE 专用头信息
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// 2. 为这个连接创建一个专属频道
	msgChan := make(chan string)

	// 3. 把新听众加入列表
	// 🔥 修正：使用 b.mu.Lock()
	b.mu.Lock()
	b.Clients[msgChan] = true
	b.mu.Unlock()

	// 4. 监听连接断开 (比如用户关闭了浏览器)
	defer func() {
		// 🔥 修正：使用 b.mu.Lock()
		b.mu.Lock()
		delete(b.Clients, msgChan)
		b.mu.Unlock()
		close(msgChan)
	}()

	// 5. 核心循环：一旦频道里有消息，就推给浏览器
	flusher, _ := w.(http.Flusher)

	for {
		select {
		case msg := <-msgChan:
			// SSE 格式要求: "data: 你的内容\n\n"
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush() // 🔥 立即发送
		case <-r.Context().Done():
			// 浏览器断开了
			return
		}
	}
}

// 📣 广播方法：后端任何地方调用这个，所有网页都会收到
func (b *SSEBroker) Broadcast(message string) {
	// 🔥 修正：使用 b.mu.Lock()
	b.mu.Lock()
	defer b.mu.Unlock()
	for clientChan := range b.Clients {
		// 避免某个客户端卡死导致广播堵塞，使用 select 非阻塞发送
		select {
		case clientChan <- message:
		default:
		}
	}
}