package main

import (
	"bytes"
	"encoding/json"
	"sync"
)

const sseFrameMax = 1 << 20 // 1 MiB safety cap per frame

// chatSSE manages broadcast fan-out for one chat's SSE subscribers.
type chatSSE struct {
	mu   sync.Mutex
	subs map[chan []byte]struct{}
}

func newChatSSE() *chatSSE {
	return &chatSSE{subs: make(map[chan []byte]struct{})}
}

func (c *chatSSE) subscribe(buf int) chan []byte {
	if buf < 4 {
		buf = 4
	}
	ch := make(chan []byte, buf)
	c.mu.Lock()
	c.subs[ch] = struct{}{}
	c.mu.Unlock()
	return ch
}

func (c *chatSSE) unsubscribe(ch chan []byte) {
	c.mu.Lock()
	_, registered := c.subs[ch]
	if registered {
		delete(c.subs, ch)
	}
	c.mu.Unlock()
	if registered {
		close(ch)
	}
}

func (c *chatSSE) broadcast(frame []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for ch := range c.subs {
		select {
		case ch <- frame:
		default:
			// drop if subscriber is slow
		}
	}
}

// sseHub routes events to per-chat subscriber groups.
type sseHub struct {
	mu    sync.Mutex
	chats map[string]*chatSSE
}

func newSSEHub() *sseHub {
	return &sseHub{chats: make(map[string]*chatSSE)}
}

func (h *sseHub) room(chatID string) *chatSSE {
	h.mu.Lock()
	defer h.mu.Unlock()
	r, ok := h.chats[chatID]
	if !ok {
		r = newChatSSE()
		h.chats[chatID] = r
	}
	return r
}

func (h *sseHub) removeChat(chatID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r, ok := h.chats[chatID]
	if !ok {
		return
	}
	delete(h.chats, chatID)
	r.mu.Lock()
	for ch := range r.subs {
		close(ch)
	}
	r.subs = nil
	r.mu.Unlock()
}

// formatSSE builds one Server-Sent Events frame: "event: <type>\ndata: <json>\n\n".
func formatSSE(eventType string, payload any) []byte {
	data, err := json.Marshal(payload)
	if err != nil {
		data, _ = json.Marshal(map[string]string{"error": err.Error()})
	}
	if len(data) > sseFrameMax {
		data, _ = json.Marshal(map[string]string{"error": "event payload too large"})
	}
	var buf bytes.Buffer
	buf.WriteString("event: ")
	buf.WriteString(eventType)
	buf.WriteString("\ndata: ")
	buf.Write(data)
	buf.WriteString("\n\n")
	return buf.Bytes()
}
