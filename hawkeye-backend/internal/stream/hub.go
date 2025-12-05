package stream

import "sync"

var (
	StreamChannels = make(map[string][]chan []byte)
	StreamMu       sync.RWMutex
)

// AddViewer 注册一个新的观众通道
func AddViewer(deviceID string) chan []byte {
	StreamMu.Lock()
	defer StreamMu.Unlock()
	ch := make(chan []byte, 10)
	StreamChannels[deviceID] = append(StreamChannels[deviceID], ch)
	return ch
}

// RemoveViewer 移除观众
func RemoveViewer(deviceID string, ch chan []byte) {
	StreamMu.Lock()
	defer StreamMu.Unlock()
	channels := StreamChannels[deviceID]
	for i, c := range channels {
		if c == ch {
			StreamChannels[deviceID] = append(channels[:i], channels[i+1:]...)
			close(c)
			break
		}
	}
}

// BroadcastFrame 向所有观众广播帧
func BroadcastFrame(deviceID string, imgData []byte) {
	StreamMu.RLock()
	defer StreamMu.RUnlock()
	for _, ch := range StreamChannels[deviceID] {
		select {
		case ch <- imgData:
		default:
			// 如果通道满了，丢弃该帧以防止阻塞
		}
	}
}