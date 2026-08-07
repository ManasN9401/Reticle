package telemetry

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/hyperparallel/runtime/events"
)

//go:embed ui/*
var uiFS embed.FS

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type Server struct {
	bus       *events.Bus
	addr      string
	clients   map[*websocket.Conn]bool
	mu        sync.Mutex
	connected chan struct{}
	once      sync.Once
}

func NewServer(bus *events.Bus, addr string) *Server {
	return &Server{
		bus:       bus,
		addr:      addr,
		clients:   make(map[*websocket.Conn]bool),
		connected: make(chan struct{}),
	}
}

func (s *Server) Start() error {
	// Serve embedded UI files
	subFS, err := fs.Sub(uiFS, "ui")
	if err != nil {
		return fmt.Errorf("failed to create sub filesystem: %w", err)
	}
	
	fsHandler := http.FileServer(http.FS(subFS))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		fsHandler.ServeHTTP(w, r)
	})
	http.HandleFunc("/ws", s.wsHandler)

	// Subscribe to all events and broadcast
	s.bus.SubscribeAll(func(e events.RuntimeEvent) {
		s.broadcast(e)
	})

	fmt.Printf("Telemetry Server running on http://localhost%s\n", s.addr)
	
	go func() {
		if err := http.ListenAndServe(s.addr, nil); err != nil {
			fmt.Printf("Telemetry Server error: %v\n", err)
		}
	}()

	return nil
}

func (s *Server) WaitForClient() {
	fmt.Printf("Telemetry Server waiting for browser connection at http://localhost%s...\n", s.addr)
	<-s.connected
	fmt.Println("Client connected! Resuming execution...")
}

func (s *Server) wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Printf("WebSocket upgrade failed: %v\n", err)
		return
	}

	s.mu.Lock()
	s.clients[conn] = true
	s.mu.Unlock()

	s.once.Do(func() {
		close(s.connected)
	})

	// Request waitlist and workflow state so it broadcasts to the new client
	if s.bus != nil {
		s.bus.Publish(events.EventType("WaitlistStateRequested"), events.Component("telemetry_ui"), nil)
		s.bus.Publish(events.EventType("WorkflowStateRequested"), events.Component("telemetry_ui"), nil)
	}

	// Keep connection alive, listen for messages
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			s.mu.Lock()
			delete(s.clients, conn)
			s.mu.Unlock()
			conn.Close()
			break
		}
		
		var payload map[string]any
		if err := json.Unmarshal(msg, &payload); err == nil {
			if action, ok := payload["action"].(string); ok && (action == "enqueue" || action == "remove") {
				s.bus.Publish(events.EventType("WaitlistCommand"), events.Component("telemetry_ui"), payload)
			}
		}
	}
}

func (s *Server) broadcast(e events.RuntimeEvent) {
	data, err := json.Marshal(e)
	if err != nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for conn := range s.clients {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			conn.Close()
			delete(s.clients, conn)
		}
	}
}
