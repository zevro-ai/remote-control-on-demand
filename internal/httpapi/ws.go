package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/coder/websocket"
	"github.com/zevro-ai/remote-control-on-demand/internal/chat"
	"github.com/zevro-ai/remote-control-on-demand/internal/provider"
)

type wsClient struct {
	conn   *websocket.Conn
	send   chan []byte
	subs   map[string]bool // session IDs subscribed to
	subsMu sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
}

type Hub struct {
	mu           sync.Mutex
	clients      map[*wsClient]bool
	unsubSession func()
	unsubChat    []func()
}

func newHub() *Hub {
	return &Hub{
		clients: make(map[*wsClient]bool),
	}
}

func (h *Hub) start(runtimeProvider provider.RuntimeProvider, registry *provider.Registry) {
	if runtimeProvider != nil {
		h.unsubSession = runtimeProvider.Subscribe(func(notification provider.RuntimeNotification) {
			h.broadcast(wsMessage{Type: "notification", Line: notification.Message})
		})
	}

	if registry == nil {
		return
	}

	for _, p := range registry.ChatProviders() {
		providerID := p.Metadata().ID
		unsub := p.Subscribe(func(e chat.Event) {
			h.handleChatEvent(providerID, e)
		})
		h.unsubChat = append(h.unsubChat, unsub)
	}
}

func (h *Hub) handleChatEvent(providerID string, e chat.Event) {
	msg := wsMessage{
		Provider:  providerID,
		SessionID: e.SessionID,
	}

	switch e.Type {
	case chat.EventSessionCreated:
		msg.Type = "chat_session_added"
		msg.Session = toChatSessionResponse(e.Session, providerID)
	case chat.EventSessionClosed:
		msg.Type = "chat_session_removed"
	case chat.EventMessageReceived:
		if e.Message == nil {
			return
		}
		msg.Type = "chat_message"
		msg.Message = toMessagePayload(*e.Message)
	case chat.EventMessageDelta:
		msg.Type = "chat_message_delta"
		msg.Delta = e.Delta
	case chat.EventBusyChanged:
		msg.Type = "chat_busy"
		busy := e.Busy
		msg.Busy = &busy
	case chat.EventToolUseStart:
		if e.ToolCall == nil {
			return
		}
		msg.Type = "chat_tool_start"
		msg.ToolCall = &toolCallPayload{
			Index: e.ToolCall.Index,
			ID:    e.ToolCall.ID,
			Name:  e.ToolCall.Name,
		}
	case chat.EventToolUseDelta:
		if e.ToolCall == nil {
			return
		}
		msg.Type = "chat_tool_delta"
		msg.ToolCall = &toolCallPayload{
			Index:       e.ToolCall.Index,
			PartialJSON: e.ToolCall.PartialJSON,
		}
	case chat.EventToolUseFinish:
		if e.ToolCall == nil {
			return
		}
		msg.Type = "chat_tool_finish"
		msg.ToolCall = &toolCallPayload{
			Index: e.ToolCall.Index,
		}
	default:
		return
	}

	h.broadcast(msg)
}

func (h *Hub) stop() {
	if h.unsubSession != nil {
		h.unsubSession()
	}
	for _, unsub := range h.unsubChat {
		unsub()
	}
	h.mu.Lock()
	for c := range h.clients {
		c.cancel()
	}
	h.mu.Unlock()
}

func (h *Hub) addClient(c *wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[c] = true
}

func (h *Hub) removeClient(c *wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, c)
}

func (h *Hub) broadcast(msg wsMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	h.mu.Lock()
	clients := make([]*wsClient, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.Unlock()

	for _, c := range clients {
		select {
		case c.send <- data:
		default:
			// Client too slow, drop message
		}
	}
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		log.Printf("websocket accept error: %v", err)
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	client := &wsClient{
		conn:   conn,
		send:   make(chan []byte, 256),
		subs:   make(map[string]bool),
		ctx:    ctx,
		cancel: cancel,
	}

	s.hub.addClient(client)

	go s.wsWriter(client)
	s.wsReader(client)

	s.hub.removeClient(client)
	cancel()
	conn.Close(websocket.StatusNormalClosure, "")
}

func (s *Server) wsWriter(c *wsClient) {
	for {
		select {
		case <-c.ctx.Done():
			return
		case data := <-c.send:
			err := c.conn.Write(c.ctx, websocket.MessageText, data)
			if err != nil {
				c.cancel()
				return
			}
		}
	}
}

func (s *Server) wsReader(c *wsClient) {
	for {
		_, data, err := c.conn.Read(c.ctx)
		if err != nil {
			return
		}

		var msg wsClientMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		switch msg.Type {
		case "subscribe":
			if msg.SessionID != "" {
				c.subsMu.Lock()
				c.subs[msg.SessionID] = true
				c.subsMu.Unlock()

				// Subscribe to RingBuffer for this session's live output
				s.subscribeClientToSession(c, msg.SessionID)
			}
		case "unsubscribe":
			if msg.SessionID != "" {
				c.subsMu.Lock()
				delete(c.subs, msg.SessionID)
				c.subsMu.Unlock()
			}
		}
	}
}

func (s *Server) subscribeClientToSession(c *wsClient, sessionID string) {
	if s.runtimeProvider == nil {
		return
	}

	sess, ok := s.runtimeProvider.GetSession(sessionID)
	if !ok {
		return
	}

	// Take the snapshot before subscribing so we don't duplicate lines that land
	// between the backfill and live-stream boundaries.
	snapshot := sess.SnapshotLogs(50)

	lineCh := make(chan string, 256)
	unsub := sess.SubscribeLogs(func(line string) {
		select {
		case lineCh <- line:
		default:
		}
	})

	for _, line := range snapshot {
		data, _ := json.Marshal(wsMessage{Type: "log", SessionID: sessionID, Line: line})
		select {
		case c.send <- data:
		default:
		}
	}

	// Forward live lines in a goroutine
	go func() {
		defer unsub()
		for {
			select {
			case <-c.ctx.Done():
				return
			case line := <-lineCh:
				c.subsMu.Lock()
				subscribed := c.subs[sessionID]
				c.subsMu.Unlock()
				if !subscribed {
					return
				}
				data, _ := json.Marshal(wsMessage{Type: "log", SessionID: sessionID, Line: line})
				select {
				case c.send <- data:
				default:
				}
			}
		}
	}()
}
