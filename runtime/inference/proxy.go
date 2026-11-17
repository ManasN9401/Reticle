// Package inference is the runtime-owned inference proxy of RFC-052 (OpenAI-compatible scope).
//
// A worker that supports it sends every chat-completion request to an attempt-scoped loopback
// endpoint instead of to a provider. The proxy forwards the request to the attempt's current
// route with that route's credential, so the worker never holds a provider secret. When the
// provider rejects a request before any of the response has been relayed, the dispatcher (through
// Session.Failover) records what the failure proves about the route and may name another route;
// the proxy then sends the same request there. Only the pending request is repeated: the worker
// executes tools only after a complete response, so no tool call is ever replayed.
package inference

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/reticle/runtime/routing"
)

// maxRequestBytes bounds one request body: a long conversation with a few images.
const maxRequestBytes = 32 << 20

// maxErrorBytes bounds how much of a provider's error response is read and relayed.
const maxErrorBytes = 64 << 10

// Endpoint returns the OpenAI-compatible base URL and upstream model name for a route, or ok=false
// when the route must keep the worker's direct path. Local Ollama is excluded on purpose: its
// OpenAI-compatible endpoint ignores num_ctx, so proxied local models would lose their context size.
func Endpoint(m routing.Model) (base, model string, ok bool) {
	switch {
	case m.APIBase != "":
		model = m.RequestModel
		if model == "" {
			_, model, _ = strings.Cut(m.ID, "/")
		}
		return strings.TrimRight(m.APIBase, "/"), model, model != ""
	case strings.HasPrefix(m.ID, "groq/"):
		return "https://api.groq.com/openai/v1", strings.TrimPrefix(m.ID, "groq/"), true
	case strings.HasPrefix(m.ID, "openrouter/"):
		return "https://openrouter.ai/api/v1", strings.TrimPrefix(m.ID, "openrouter/"), true
	}
	return "", "", false
}

// Eligible reports whether a route can be served by the proxy now: it has an OpenAI-compatible
// endpoint and, when it names a credential, that credential is configured.
func Eligible(m routing.Model) bool {
	if _, _, ok := Endpoint(m); !ok {
		return false
	}
	return m.APIKeyEnv == "" || os.Getenv(m.APIKeyEnv) != ""
}

// Session is one worker attempt's use of the proxy.
type Session struct {
	token string
	mu    sync.Mutex
	route routing.Model
	// Failover is called with the route that failed and a description of the failure in the form
	// the dispatcher's classifier reads. It returns the next route, or ok=false to stop and relay
	// the failure to the worker. It is called with no proxy lock held.
	Failover  func(failed routing.Model, failure string) (next routing.Model, ok bool)
	failovers int
	// exhausted is set when the last provider failure was relayed to the worker, so the dispatcher
	// knows that failure's lessons have already been recorded.
	exhausted bool
}

// Route is the route the session will use for its next request.
func (s *Session) Route() routing.Model {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.route
}

// Failovers is how many times a request moved to another route.
func (s *Session) Failovers() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.failovers
}

// Exhausted reports whether the most recent provider failure was relayed to the worker.
func (s *Session) Exhausted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exhausted
}

// Proxy serves attempt sessions on a loopback listener.
type Proxy struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	listener net.Listener
	server   *http.Server
	client   *http.Client
}

// New starts the proxy on 127.0.0.1 at a free port.
func New() (*Proxy, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("start inference proxy: %w", err)
	}
	p := &Proxy{
		sessions: make(map[string]*Session),
		listener: listener,
		// No overall timeout: a streamed reply can legitimately take many minutes. The worker
		// bounds the wait for the first event, and its disconnect cancels the upstream request.
		client: &http.Client{Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 10 * time.Minute,
			IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 15 * time.Second,
		}},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", p.handleChat)
	p.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 * 1024}
	go func() { _ = p.server.Serve(listener) }()
	return p, nil
}

// URL is the base URL a worker passes as its api_base.
func (p *Proxy) URL() string { return "http://" + p.listener.Addr().String() + "/v1" }

// Begin opens a session starting on route. The returned environment is all a worker needs.
func (p *Proxy) Begin(route routing.Model) (*Session, []string, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, nil, fmt.Errorf("create inference credential: %w", err)
	}
	session := &Session{token: hex.EncodeToString(tokenBytes), route: route}
	p.mu.Lock()
	p.sessions[session.token] = session
	p.mu.Unlock()
	return session, []string{"RETICLE_INFERENCE_URL=" + p.URL(), "RETICLE_INFERENCE_TOKEN=" + session.token}, nil
}

// End revokes a session; later requests with its token are refused.
func (p *Proxy) End(session *Session) {
	if session == nil {
		return
	}
	p.mu.Lock()
	delete(p.sessions, session.token)
	p.mu.Unlock()
}

// Close stops the listener.
func (p *Proxy) Close(ctx context.Context) error { return p.server.Shutdown(ctx) }

func (p *Proxy) authenticate(r *http.Request) *Session {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" || len(token) > 256 {
		return nil
	}
	p.mu.RLock()
	session := p.sessions[token]
	p.mu.RUnlock()
	if session == nil || subtle.ConstantTimeCompare([]byte(token), []byte(session.token)) != 1 {
		return nil
	}
	return session
}

// forwardedHeaders are the worker's headers a provider may use (OpenRouter's attribution).
var forwardedHeaders = []string{"HTTP-Referer", "X-Title", "Accept"}

func (p *Proxy) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
		return
	}
	session := p.authenticate(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "invalid_credential", "unknown or revoked inference session")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBytes+1))
	if err != nil || len(raw) > maxRequestBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "inference request exceeds the proxy's 32 MiB limit")
		return
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is not a JSON object")
		return
	}
	for {
		route := session.Route()
		response, failure := p.send(r, route, body)
		if failure == nil {
			session.mu.Lock()
			session.exhausted = false
			session.mu.Unlock()
			relay(w, response)
			return
		}
		var nextRoute routing.Model
		decided := false
		if session.Failover != nil && r.Context().Err() == nil {
			nextRoute, decided = session.Failover(route, failure.text)
		}
		moved := decided && nextRoute.Key() != route.Key()
		session.mu.Lock()
		if moved {
			session.route = nextRoute
			session.failovers++
		} else {
			session.exhausted = true
		}
		session.mu.Unlock()
		if !moved {
			failure.relay(w)
			return
		}
	}
}

// upstreamFailure is a provider rejection that has not been relayed yet.
type upstreamFailure struct {
	status int
	header http.Header
	body   []byte
	text   string // for the dispatcher's classifier
}

func (f *upstreamFailure) relay(w http.ResponseWriter) {
	if f.status == 0 {
		// The provider could not be reached: answer like a gateway, so the worker's client raises a
		// connection-class error rather than a malformed-response one.
		writeError(w, http.StatusBadGateway, "provider_unreachable", f.text)
		return
	}
	if contentType := f.header.Get("Content-Type"); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	if retryAfter := f.header.Get("Retry-After"); retryAfter != "" {
		w.Header().Set("Retry-After", retryAfter)
	}
	w.WriteHeader(f.status)
	_, _ = w.Write(f.body)
}

func (p *Proxy) send(incoming *http.Request, route routing.Model, body map[string]any) (*http.Response, *upstreamFailure) {
	base, model, ok := Endpoint(route)
	if !ok {
		return nil, &upstreamFailure{text: FailureText(0, "", "route has no OpenAI-compatible endpoint: "+route.ID)}
	}
	outgoing := make(map[string]any, len(body))
	for key, value := range body {
		outgoing[key] = value
	}
	outgoing["model"] = model
	payload, err := json.Marshal(outgoing)
	if err != nil {
		return nil, &upstreamFailure{text: FailureText(0, "", err.Error())}
	}
	request, err := http.NewRequestWithContext(incoming.Context(), http.MethodPost, base+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, &upstreamFailure{text: FailureText(0, "", err.Error())}
	}
	request.Header.Set("Content-Type", "application/json")
	for _, name := range forwardedHeaders {
		if value := incoming.Header.Get(name); value != "" {
			request.Header.Set(name, value)
		}
	}
	if route.APIKeyEnv != "" {
		request.Header.Set("Authorization", "Bearer "+os.Getenv(route.APIKeyEnv))
	}
	response, err := p.client.Do(request)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, &upstreamFailure{text: FailureText(0, "", "request cancelled by the worker")}
		}
		return nil, &upstreamFailure{text: FailureText(0, "", err.Error())}
	}
	if response.StatusCode < 400 {
		return response, nil
	}
	defer response.Body.Close()
	errorBody, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorBytes))
	return nil, &upstreamFailure{status: response.StatusCode, header: response.Header.Clone(), body: errorBody,
		text: FailureText(response.StatusCode, response.Header.Get("Retry-After"), string(errorBody))}
}

// relay streams a successful provider response to the worker as it arrives.
func relay(w http.ResponseWriter, response *http.Response) {
	defer response.Body.Close()
	for _, name := range []string{"Content-Type", "Cache-Control"} {
		if value := response.Header.Get(name); value != "" {
			w.Header().Set(name, value)
		}
	}
	w.WriteHeader(response.StatusCode)
	flusher, _ := w.(http.Flusher)
	buffer := make([]byte, 32*1024)
	for {
		n, err := response.Body.Read(buffer)
		if n > 0 {
			if _, writeErr := w.Write(buffer[:n]); writeErr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			// A stream cut part-way is not retried: some of it has reached the worker. The worker
			// sees the broken stream and handles it as it would a direct one.
			return
		}
	}
}

// FailureText describes a provider failure in the form the dispatcher's classifier reads from a
// worker's stderr: a retry-safety marker (the pending request is the only thing repeated, so no
// effect is replayed) and a final exception line naming the error class, as LiteLLM would.
func FailureText(status int, retryAfter, body string) string {
	class := "APIConnectionError"
	switch {
	case status == 400:
		class = "BadRequestError"
	case status == 401:
		class = "AuthenticationError"
	case status == 403:
		class = "PermissionDeniedError"
	case status == 404:
		class = "NotFoundError"
	case status == 408:
		class = "Timeout"
	case status == 413 || status == 429:
		class = "RateLimitError"
	case status == 422:
		class = "UnprocessableEntityError"
	case status >= 500:
		class = "ServiceUnavailableError"
	case status >= 400:
		class = "APIError"
	}
	detail := strings.Join(strings.Fields(body), " ")
	if len(detail) > 4000 {
		detail = detail[:4000]
	}
	if retryAfter != "" {
		detail += " (Retry-After: " + retryAfter + ")"
	}
	if status > 0 {
		detail = fmt.Sprintf("status code: %d, %s", status, detail)
	}
	return "[RETICLE_RETRY_SAFE: NO_EFFECTS]\nlitellm." + class + ": " + class + ": inference proxy - " + detail
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": code, "message": message, "type": code}})
}
