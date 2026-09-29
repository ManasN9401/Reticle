package telemetry

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
	"github.com/reticle/runtime/events"
	"github.com/reticle/runtime/mcp"
	runtimeplugin "github.com/reticle/runtime/plugin"
	"github.com/reticle/runtime/routing"
)

//go:embed ui/*
var uiFS embed.FS

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return sameOrigin(r)
	},
}

type Server struct {
	bus       *events.Bus
	addr      string
	rootDir   string
	clients   map[*websocket.Conn]chan []byte
	mu        sync.Mutex
	connected chan struct{}
	once      sync.Once
	mcp       *mcp.Manager
	plugins   *runtimeplugin.Manager
}

func (s *Server) SetExtensionManagers(mcpManager *mcp.Manager, pluginManager *runtimeplugin.Manager) {
	s.mcp = mcpManager
	s.plugins = pluginManager
}

func NewServer(bus *events.Bus, addr string, rootDir string) *Server {
	return &Server{
		bus:       bus,
		addr:      addr,
		rootDir:   rootDir,
		clients:   make(map[*websocket.Conn]chan []byte),
		connected: make(chan struct{}),
	}
}

func (s *Server) Start() error {
	host, port, err := net.SplitHostPort(s.addr)
	if err != nil {
		return err
	}
	if host == "" {
		host = "127.0.0.1"
	}
	if !localHost(host) {
		return fmt.Errorf("remote control listener is unsupported; use loopback")
	}
	s.addr = net.JoinHostPort(host, port)
	token, err := controlToken(s.rootDir)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	// Serve embedded UI files
	subFS, err := fs.Sub(uiFS, "ui")
	if err != nil {
		return fmt.Errorf("failed to create sub filesystem: %w", err)
	}

	fsHandler := http.FileServer(http.FS(subFS))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		if r.URL.Path != "/" && r.URL.Path != "/index.html" {
			http.NotFound(w, r)
			return
		}
		fsHandler.ServeHTTP(w, r)
	})

	// Serve artifacts from the isolated sessions via generic HTTP
	hyperFS := http.FileServer(http.Dir(filepath.Join(s.rootDir, ".reticle", "sessions")))
	mux.HandleFunc("/artifacts/", func(w http.ResponseWriter, r *http.Request) {
		relative := strings.TrimPrefix(r.URL.Path, "/artifacts/")
		target, err := SafePath(filepath.Join(s.rootDir, ".reticle", "sessions"), relative)
		if err != nil {
			http.Error(w, "Invalid artifact", 400)
			return
		}
		info, err := os.Stat(target)
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Disposition", "attachment")
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
		http.StripPrefix("/artifacts/", hyperFS).ServeHTTP(w, r)
	})

	// JSON API to fetch the outputs (the content of src/)
	mux.HandleFunc("/api/outputs/", func(w http.ResponseWriter, r *http.Request) {
		execID := strings.TrimPrefix(r.URL.Path, "/api/outputs/")
		if execID == "" {
			http.Error(w, "missing execID", http.StatusBadRequest)
			return
		}

		if strings.ContainsAny(execID, "/\\:") || execID == "." || execID == ".." {
			http.Error(w, "Invalid execution ID", 400)
			return
		}
		srcDir, err := SafePath(filepath.Join(s.rootDir, ".reticle", "sessions"), execID+"/src")
		if err != nil {
			http.Error(w, "Invalid execution path", 400)
			return
		}
		totalBytes := int64(0)

		type OutputFile struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}

		var files []OutputFile
		_ = filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // Ignore missing directory or unreadable files for now, return empty array
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if !d.IsDir() {
				rel, _ := filepath.Rel(srcDir, path)
				info, err := d.Info()
				if err != nil || info.Size() > 1024*1024 || totalBytes+info.Size() > 8*1024*1024 {
					return nil
				}
				content, err := os.ReadFile(path)
				if err != nil || !utf8.Valid(content) || strings.ContainsRune(string(content), 0) {
					return nil
				}
				totalBytes += info.Size()
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

	mux.HandleFunc("/api/upload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 50<<20)
		err := r.ParseMultipartForm(2 << 20) // 50 MB max memory
		if err != nil {
			http.Error(w, "Failed to parse form: "+err.Error(), http.StatusBadRequest)
			return
		}

		defer r.MultipartForm.RemoveAll()
		stagingDir := filepath.Join(s.rootDir, ".reticle", "waitlist_staging")
		os.MkdirAll(stagingDir, 0755)

		type UploadedFile struct {
			ID       string `json:"id"`
			Filename string `json:"filename"`
			MimeType string `json:"mime_type"`
			Path     string `json:"path"`
		}

		var uploaded []UploadedFile

		for _, fheaders := range r.MultipartForm.File {
			for _, hdr := range fheaders {
				file, err := hdr.Open()
				if err != nil {
					continue
				}

				fileID := fmt.Sprintf("%d_%s", time.Now().UnixNano(), hdr.Filename)
				fileID = strings.ReplaceAll(fileID, " ", "_")
				fileID = strings.ReplaceAll(fileID, "/", "_")
				fileID = strings.ReplaceAll(fileID, "\\", "_")
				fileID = strings.ReplaceAll(fileID, ":", "_")
				if strings.ContainsAny(hdr.Filename, "/\\:") || hdr.Filename == ".." {
					file.Close()
					http.Error(w, "Invalid filename", 400)
					return
				}

				dstPath := filepath.Join(stagingDir, fileID)
				dst, err := os.Create(dstPath)
				if err == nil {
					_, copyErr := io.Copy(dst, file)
					closeErr := dst.Close()
					if copyErr != nil || closeErr != nil {
						file.Close()
						os.Remove(dstPath)
						http.Error(w, "Upload failed", 500)
						return
					}

					uploaded = append(uploaded, UploadedFile{
						ID:       fileID,
						Filename: hdr.Filename,
						MimeType: hdr.Header.Get("Content-Type"),
						Path:     dstPath,
					})
				}
				file.Close()
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(uploaded)
	})

	// JSON API to fetch and toggle available models
	mux.HandleFunc("/api/models", func(w http.ResponseWriter, r *http.Request) {
		routing.ModelsMutex.RLock()
		defer routing.ModelsMutex.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(routing.AvailableModels)
	})

	mux.HandleFunc("/api/models/toggle", func(w http.ResponseWriter, r *http.Request) {
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
		routing.ModelsMutex.Lock()
		defer routing.ModelsMutex.Unlock()
		for i := range routing.AvailableModels {
			if routing.AvailableModels[i].Key() == payload.ModelKey {
				routing.AvailableModels[i].Enabled = !routing.AvailableModels[i].Enabled
				w.WriteHeader(http.StatusOK)
				return
			}
		}
		http.Error(w, "Model not found", http.StatusNotFound)
	})

	mux.HandleFunc("/api/mcp/servers", s.handleMCPServers)
	mux.HandleFunc("/api/mcp/servers/", s.handleMCPServer)
	mux.HandleFunc("/api/plugins", s.handlePlugins)
	mux.HandleFunc("/api/plugins/", s.handlePlugin)

	mux.HandleFunc("/ws", s.wsHandler)

	// Subscribe to all events and broadcast
	dispose := s.bus.SubscribeAll(func(e events.RuntimeEvent) {
		s.broadcast(e)
	})

	fmt.Printf("Telemetry Server running on http://%s\n", s.addr)

	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		dispose()
		return err
	}
	server := &http.Server{Handler: protected(token, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.bus.Accepting() {
			http.Error(w, "Runtime unavailable; restart required", http.StatusServiceUnavailable)
			return
		}
		mux.ServeHTTP(w, r)
	})), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 130 * time.Second, IdleTimeout: 60 * time.Second}
	s.bus.Subscribe("RuntimeShutdown", func(events.RuntimeEvent) {
		dispose()
		_ = server.Close()
		s.mu.Lock()
		for conn, ch := range s.clients {
			_ = conn.Close()
			close(ch)
			delete(s.clients, conn)
		}
		s.mu.Unlock()
	})
	go server.Serve(listener)

	return nil
}

func (s *Server) handleMCPServers(w http.ResponseWriter, r *http.Request) {
	if s.mcp == nil {
		http.Error(w, "MCP manager unavailable", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeControlJSON(w, http.StatusOK, s.mcp.List())
	case http.MethodPost:
		var config mcp.ServerConfig
		if err := decodeControlJSON(w, r, &config); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()
		status, err := s.mcp.Put(ctx, config)
		if err != nil {
			writeControlError(w, http.StatusBadRequest, err)
			return
		}
		writeControlJSON(w, http.StatusCreated, status)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleMCPServer(w http.ResponseWriter, r *http.Request) {
	if s.mcp == nil {
		http.Error(w, "MCP manager unavailable", http.StatusServiceUnavailable)
		return
	}
	relative := strings.TrimPrefix(r.URL.Path, "/api/mcp/servers/")
	parts := strings.Split(relative, "/")
	if len(parts) == 0 || parts[0] == "" || strings.ContainsAny(parts[0], "\\:") {
		http.Error(w, "Invalid server id", http.StatusBadRequest)
		return
	}
	id := parts[0]
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodPut:
			var config mcp.ServerConfig
			if err := decodeControlJSON(w, r, &config); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			config.ID = id
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
			defer cancel()
			status, err := s.mcp.Put(ctx, config)
			if err != nil {
				writeControlError(w, http.StatusBadRequest, err)
				return
			}
			writeControlJSON(w, http.StatusOK, status)
		case http.MethodDelete:
			if err := s.mcp.Delete(id); err != nil {
				writeControlError(w, http.StatusNotFound, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}
	if len(parts) != 2 || (parts[1] == "tools" && r.Method != http.MethodGet) || (parts[1] != "tools" && r.Method != http.MethodPost) {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	var status mcp.ServerStatus
	var err error
	switch parts[1] {
	case "enable":
		status, err = s.mcp.SetEnabled(ctx, id, true)
	case "disable":
		status, err = s.mcp.SetEnabled(ctx, id, false)
	case "test":
		status, err = s.mcp.Test(ctx, id)
	case "tools":
		tools, toolsErr := s.mcp.Tools(id)
		if toolsErr != nil {
			writeControlError(w, http.StatusNotFound, toolsErr)
			return
		}
		writeControlJSON(w, http.StatusOK, tools)
		return
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeControlError(w, http.StatusBadRequest, err)
		return
	}
	writeControlJSON(w, http.StatusOK, status)
}

func (s *Server) handlePlugins(w http.ResponseWriter, r *http.Request) {
	if s.plugins == nil {
		http.Error(w, "Plugin manager unavailable", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeControlJSON(w, http.StatusOK, s.plugins.List())
	case http.MethodPost:
		var request struct {
			Path string `json:"path"`
		}
		if err := decodeControlJSON(w, r, &request); err != nil || request.Path == "" {
			http.Error(w, "A plugin directory path is required", http.StatusBadRequest)
			return
		}
		view, err := s.plugins.Install(request.Path)
		if err != nil {
			writeControlError(w, http.StatusBadRequest, err)
			return
		}
		writeControlJSON(w, http.StatusCreated, view)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handlePlugin(w http.ResponseWriter, r *http.Request) {
	if s.plugins == nil {
		http.Error(w, "Plugin manager unavailable", http.StatusServiceUnavailable)
		return
	}
	relative := strings.TrimPrefix(r.URL.Path, "/api/plugins/")
	parts := strings.Split(relative, "/")
	if len(parts) == 0 || parts[0] == "" || strings.ContainsAny(parts[0], "\\:") {
		http.Error(w, "Invalid plugin id", http.StatusBadRequest)
		return
	}
	id := parts[0]
	if len(parts) == 1 && r.Method == http.MethodDelete {
		if err := s.plugins.Delete(id); err != nil {
			writeControlError(w, http.StatusBadRequest, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if len(parts) != 2 || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	var view runtimeplugin.View
	var err error
	switch parts[1] {
	case "enable":
		view, err = s.plugins.Enable(r.Context(), id)
	case "disable":
		view, err = s.plugins.Disable(id)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeControlError(w, http.StatusBadRequest, err)
		return
	}
	writeControlJSON(w, http.StatusOK, view)
}

func decodeControlJSON(w http.ResponseWriter, r *http.Request, value any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("invalid JSON request")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("request contains trailing JSON")
	}
	return nil
}

func writeControlJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeControlError(w http.ResponseWriter, status int, err error) {
	message := strings.TrimSpace(err.Error())
	if len(message) > 1024 {
		message = message[:1024]
	}
	writeControlJSON(w, status, map[string]string{"error": message})
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
	outbound := make(chan []byte, 64)
	s.clients[conn] = outbound
	s.mu.Unlock()

	go func() {
		for data := range outbound {
			conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
			if conn.WriteMessage(websocket.TextMessage, data) != nil {
				conn.Close()
				return
			}
		}
	}()
	conn.SetReadLimit(1024 * 1024)
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
			if channel, ok := s.clients[conn]; ok {
				delete(s.clients, conn)
				close(channel)
			}
			s.mu.Unlock()
			conn.Close()
			break
		}

		var payload map[string]any
		if err := json.Unmarshal(msg, &payload); err == nil {
			if action, ok := payload["action"].(string); ok && (action == "enqueue" || action == "remove" || action == "kill" || action == "pause" || action == "resume" || action == "update_settings") {
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

	for conn, ch := range s.clients {
		select {
		case ch <- data:
		default:
			conn.Close()
			close(ch)
			delete(s.clients, conn)
		}
	}
}
