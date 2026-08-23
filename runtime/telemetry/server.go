package telemetry

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/hyperparallel/runtime/events"
	"github.com/hyperparallel/runtime/routing"
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
	rootDir   string
	clients   map[*websocket.Conn]bool
	mu        sync.Mutex
	connected chan struct{}
	once      sync.Once
}

func NewServer(bus *events.Bus, addr string, rootDir string) *Server {
	return &Server{
		bus:       bus,
		addr:      addr,
		rootDir:   rootDir,
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
	
	// Serve artifacts from the isolated sessions via generic HTTP
	hyperFS := http.FileServer(http.Dir(filepath.Join(s.rootDir, ".hyperparallel", "sessions")))
	http.HandleFunc("/artifacts/", func(w http.ResponseWriter, r *http.Request) {
		http.StripPrefix("/artifacts/", hyperFS).ServeHTTP(w, r)
	})

	// JSON API to fetch the outputs (the content of src/)
	http.HandleFunc("/api/outputs/", func(w http.ResponseWriter, r *http.Request) {
		execID := strings.TrimPrefix(r.URL.Path, "/api/outputs/")
		if execID == "" {
			http.Error(w, "missing execID", http.StatusBadRequest)
			return
		}

		srcDir := filepath.Join(s.rootDir, ".hyperparallel", "sessions", execID, "src")
		
		type OutputFile struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		
		var files []OutputFile
		_ = filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // Ignore missing directory or unreadable files for now, return empty array
			}
			if !d.IsDir() {
				rel, _ := filepath.Rel(srcDir, path)
				content, _ := os.ReadFile(path)
				files = append(files, OutputFile{
					Path:    filepath.ToSlash(rel),
					Content: string(content),
				})
			}
			return nil
		})
		
		if files == nil {
			files = []OutputFile{} // Ensure we return [] instead of null
		}
		
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(files)
	})
	
	// JSON API to fetch and toggle available models
	http.HandleFunc("/api/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(routing.AvailableModels)
	})
	
	http.HandleFunc("/api/models/toggle", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			ModelKey string `json:"model_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		for i := range routing.AvailableModels {
			if routing.AvailableModels[i].Key() == payload.ModelKey {
				routing.AvailableModels[i].Enabled = !routing.AvailableModels[i].Enabled
				w.WriteHeader(http.StatusOK)
				return
			}
		}
		http.Error(w, "Model not found", http.StatusNotFound)
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
