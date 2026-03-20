package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/coder/websocket"
	"github.com/zevro-ai/remote-control-on-demand/internal/claudechat"
	"github.com/zevro-ai/remote-control-on-demand/internal/codex"
	"github.com/zevro-ai/remote-control-on-demand/internal/session"
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
	unsubClaude  func()
	unsubCodex   func()
}

func newHub() *Hub {
	return &Hub{
		clients: make(map[*wsClient]bool),
	}
}

func (h *Hub) start(sessionMgr *session.Manager, claudeMgr *claudechat.Manager, codexMgr *codex.Manager) {
	h.unsubSession = sessionMgr.Subscribe(func(n session.Notification) {
		h.broadcast(wsMessage{Type: "notification", Line: n.Message})
	})

	h.unsubClaude = claudeMgr.Subscribe(func(e claudechat.Event) {
		switch e.Type {
		case claudechat.EventSessionCreated:
			h.broadcast(wsMessage{
				Type:      "claude_session_added",
				SessionID: e.SessionID,
				Session:   toClaudeSessionResponse(e.Session),
			})
		case claudechat.EventSessionClosed:
			h.broadcast(wsMessage{
				Type:      "claude_session_removed",
				SessionID: e.SessionID,
			})
		case claudechat.EventMessageReceived:
			if e.Message != nil {
				h.broadcast(wsMessage{
					Type:      "claude_message",
					SessionID: e.SessionID,
					Message:   toClaudeMessagePayload(*e.Message),
				})
			}
		case claudechat.EventMessageDelta:
			h.broadcast(wsMessage{
				Type:      "claude_message_delta",
				SessionID: e.SessionID,
				Delta:     e.Delta,
			})
		case claudechat.EventBusyChanged:
			busy := e.Busy
			h.broadcast(wsMessage{
				Type:      "claude_busy",
				SessionID: e.SessionID,
				Busy:      &busy,
			})
		case claudechat.EventToolUseStart:
			if e.ToolCall != nil {
				h.broadcast(wsMessage{
					Type:      "claude_tool_start",
					SessionID: e.SessionID,
					ToolCall: &toolCallPayload{
						Index: e.ToolCall.Index,
						ID:    e.ToolCall.ID,
						Name:  e.ToolCall.Name,
					},
				})
			}
		case claudechat.EventToolUseDelta:
			if e.ToolCall != nil {
				h.broadcast(wsMessage{
					Type:      "claude_tool_delta",
					SessionID: e.SessionID,
					ToolCall: &toolCallPayload{
						Index:       e.ToolCall.Index,
						PartialJSON: e.ToolCall.PartialJSON,
					},
				})
			}
		case claudechat.EventToolUseFinish:
			if e.ToolCall != nil {
				h.broadcast(wsMessage{
					Type:      "claude_tool_finish",
					SessionID: e.SessionID,
					ToolCall: &toolCallPayload{
						Index: e.ToolCall.Index,
					},
				})
			}
		}
	})

	h.unsubCodex = codexMgr.Subscribe(func(e codex.Event) {
		switch e.Type {
		case codex.EventSessionCreated:
			h.broadcast(wsMessage{
				Type:      "codex_session_added",
				SessionID: e.SessionID,
				Session:   toCodexSessionResponse(e.Session),
			})
		case codex.EventSessionClosed:
			h.broadcast(wsMessage{
				Type:      "codex_session_removed",
				SessionID: e.SessionID,
			})
		case codex.EventMessageReceived:
			if e.Message != nil {
				h.broadcast(wsMessage{
					Type:      "codex_message",
					SessionID: e.SessionID,
					Message:   toCodexMessagePayload(*e.Message),
				})
			}
		case codex.EventMessageDelta:
			if e.Delta != "" {
				h.broadcast(wsMessage{
					Type:      "codex_message_delta",
					SessionID: e.SessionID,
					Delta:     e.Delta,
				})
			}
		case codex.EventBusyChanged:
			busy := e.Busy
			h.broadcast(wsMessage{
				Type:      "codex_busy",
				SessionID: e.SessionID,
				Busy:      &busy,
			})
		case codex.EventItemStarted:
			if e.Item != nil {
				h.broadcast(wsMessage{
					Type:      "codex_item_started",
					SessionID: e.SessionID,
					ToolCall: &toolCallPayload{
						Index: e.Item.Index,
						ID:    e.Item.ID,
						Name:  e.Item.Type,
					},
					Delta: e.Item.Command,
				})
			}
		case codex.EventItemCompleted:
			if e.Item != nil {
				h.broadcast(wsMessage{
					Type:      "codex_item_completed",
					SessionID: e.SessionID,
					ToolCall: &toolCallPayload{
						Index: e.Item.Index,
						ID:    e.Item.ID,
						Name:  e.Item.Type,
					},
					Delta: e.Item.Text,
				})
			}
		case codex.EventError:
			if e.Error != "" {
				h.broadcast(wsMessage{
					Type:      "codex_error",
					SessionID: e.SessionID,
					Line:      e.Error,
				})
			}
		}
	})
}

func (h *Hub) stop() {
	if h.unsubSession != nil {
		h.unsubSession()
	}
	if h.unsubClaude != nil {
		h.unsubClaude()
	}
	if h.unsubCodex != nil {
		h.unsubCodex()
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

func (h *Hub) broadcastToSession(sessionID string, msg wsMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	h.mu.Lock()
	clients := make([]*wsClient, 0)
	for c := range h.clients {
		c.subsMu.Lock()
		if c.subs[sessionID] {
			clients = append(clients, c)
		}
		c.subsMu.Unlock()
	}
	h.mu.Unlock()

	for _, c := range clients {
		select {
		case c.send <- data:
		default:
		}
	}
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"localhost:*", "127.0.0.1:*"},
	})
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
	sess, ok := s.sessionMgr.Get(sessionID)
	if !ok {
		return
	}

	// Take the snapshot before subscribing so we don't duplicate lines that land
	// between the backfill and live-stream boundaries.
	snapshot := sess.OutputBuf.Lines(50)

	lineCh := make(chan string, 256)
	unsub := sess.OutputBuf.Subscribe(func(line string) {
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
