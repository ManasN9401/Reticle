package inference

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/reticle/runtime/routing"
)

const groqTooLarge = `{"error":{"message":"Request too large for model ` + "`openai/gpt-oss-120b`" + ` in organization ` +
	"`org_x`" + ` service tier ` + "`on_demand`" + ` on tokens per minute (TPM): Limit 8000, Requested 10303, please reduce your message size and try again.","type":"tokens","code":"rate_limit_exceeded"}}`

// upstream is a stand-in provider that records what it was sent.
type upstream struct {
	server *httptest.Server
	auth   []string
	models []string
}

func newUpstream(t *testing.T, status int, body string) *upstream {
	u := &upstream{}
	u.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var request map[string]any
		_ = json.NewDecoder(r.Body).Decode(&request)
		u.auth = append(u.auth, r.Header.Get("Authorization"))
		u.models = append(u.models, request["model"].(string))
		if status >= 400 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = io.WriteString(w, body)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, chunk := range strings.Split(body, "|") {
			_, _ = io.WriteString(w, "data: "+chunk+"\n\n")
			w.(http.Flusher).Flush()
		}
	}))
	t.Cleanup(u.server.Close)
	return u
}

func route(id string, u *upstream, keyEnv string) routing.Model {
	return routing.Model{ID: id, APIBase: u.server.URL + "/v1", RequestModel: strings.TrimPrefix(id, "fixture/"), APIKeyEnv: keyEnv, Enabled: true}
}

func startProxy(t *testing.T) *Proxy {
	proxy, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = proxy.Close(context.Background()) })
	return proxy
}

func ask(t *testing.T, proxy *Proxy, token string) (int, string) {
	request, _ := http.NewRequest(http.MethodPost, proxy.URL()+"/chat/completions",
		strings.NewReader(`{"model":"openai/whatever","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	return response.StatusCode, string(body)
}

func token(t *testing.T, environment []string) string {
	for _, item := range environment {
		if value, ok := strings.CutPrefix(item, "RETICLE_INFERENCE_TOKEN="); ok {
			return value
		}
	}
	t.Fatal("no token in the session environment")
	return ""
}

func TestEndpointsCoverOpenAICompatibleRoutesOnly(t *testing.T) {
	cases := []struct {
		model      routing.Model
		base, name string
		ok         bool
	}{
		{routing.Model{ID: "groq/openai/gpt-oss-120b"}, "https://api.groq.com/openai/v1", "openai/gpt-oss-120b", true},
		{routing.Model{ID: "openrouter/meta/llama-3:free"}, "https://openrouter.ai/api/v1", "meta/llama-3:free", true},
		{routing.Model{ID: "nvidia/writer/palmyra", APIBase: "https://integrate.api.nvidia.com/v1/"}, "https://integrate.api.nvidia.com/v1", "writer/palmyra", true},
		{routing.Model{ID: "mistral/x", APIBase: "https://api.mistral.ai/v1", RequestModel: "mistral-large"}, "https://api.mistral.ai/v1", "mistral-large", true},
		{routing.Model{ID: "ollama/qwen2.5-coder:7b"}, "", "", false},
		{routing.Model{ID: "gemini/gemini-2.0-flash"}, "", "", false},
	}
	for _, c := range cases {
		base, name, ok := Endpoint(c.model)
		if base != c.base || name != c.name || ok != c.ok {
			t.Errorf("%s: got %q %q %v, want %q %q %v", c.model.ID, base, name, ok, c.base, c.name, c.ok)
		}
	}
	t.Setenv("FIXTURE_UNSET_KEY", "")
	if Eligible(routing.Model{ID: "groq/x", APIKeyEnv: "FIXTURE_UNSET_KEY"}) {
		t.Error("a route whose credential is not configured cannot be proxied")
	}
}

func TestARequestIsForwardedWithTheRoutesCredentialAndStreamed(t *testing.T) {
	t.Setenv("FIXTURE_KEY", "secret-a")
	provider := newUpstream(t, 200, `{"a":1}|{"b":2}|[DONE]`)
	proxy := startProxy(t)
	session, environment, err := proxy.Begin(route("fixture/model-a", provider, "FIXTURE_KEY"))
	if err != nil {
		t.Fatal(err)
	}
	status, body := ask(t, proxy, token(t, environment))
	if status != 200 || body != "data: {\"a\":1}\n\ndata: {\"b\":2}\n\ndata: [DONE]\n\n" {
		t.Fatalf("got %d %q", status, body)
	}
	if provider.auth[0] != "Bearer secret-a" || provider.models[0] != "model-a" {
		t.Fatalf("upstream saw auth %q model %q", provider.auth[0], provider.models[0])
	}
	if strings.Contains(strings.Join(environment, " "), "secret-a") {
		t.Fatal("the provider credential must never be in the worker's environment")
	}
	if session.Failovers() != 0 || session.Exhausted() {
		t.Fatal("a successful request is neither a failover nor exhausted")
	}
}

func TestARejectedRequestMovesToTheRouteTheDispatcherNames(t *testing.T) {
	t.Setenv("FIXTURE_KEY_A", "secret-a")
	t.Setenv("FIXTURE_KEY_B", "secret-b")
	small := newUpstream(t, 413, groqTooLarge)
	large := newUpstream(t, 200, `{"ok":true}|[DONE]`)
	proxy := startProxy(t)
	session, environment, _ := proxy.Begin(route("fixture/small", small, "FIXTURE_KEY_A"))
	var reported []string
	session.Failover = func(failed routing.Model, failure string) (routing.Model, bool) {
		reported = append(reported, failed.ID+": "+failure)
		return route("fixture/large", large, "FIXTURE_KEY_B"), true
	}
	status, body := ask(t, proxy, token(t, environment))
	if status != 200 || !strings.Contains(body, `{"ok":true}`) {
		t.Fatalf("got %d %q", status, body)
	}
	if len(reported) != 1 || !strings.Contains(reported[0], "fixture/small: [RETICLE_RETRY_SAFE: NO_EFFECTS]\nlitellm.RateLimitError") ||
		!strings.Contains(reported[0], "Limit 8000, Requested 10303") {
		t.Fatalf("the dispatcher was told %q", reported)
	}
	if large.auth[0] != "Bearer secret-b" || large.models[0] != "large" {
		t.Fatalf("the new route got auth %q model %q", large.auth[0], large.models[0])
	}
	if session.Failovers() != 1 || session.Route().ID != "fixture/large" || session.Exhausted() {
		t.Fatalf("failovers %d route %s exhausted %v", session.Failovers(), session.Route().ID, session.Exhausted())
	}
	// The next request starts on the route that worked.
	if status, _ := ask(t, proxy, token(t, environment)); status != 200 || len(small.models) != 1 || len(large.models) != 2 {
		t.Fatalf("second request: %d, small %d, large %d", status, len(small.models), len(large.models))
	}
}

func TestWhenNoRouteIsLeftTheProvidersErrorReachesTheWorker(t *testing.T) {
	small := newUpstream(t, 413, groqTooLarge)
	proxy := startProxy(t)
	session, environment, _ := proxy.Begin(route("fixture/small", small, ""))
	session.Failover = func(routing.Model, string) (routing.Model, bool) { return routing.Model{}, false }
	status, body := ask(t, proxy, token(t, environment))
	if status != 413 || !strings.Contains(body, "Request too large") {
		t.Fatalf("got %d %q", status, body)
	}
	if !session.Exhausted() || session.Failovers() != 0 {
		t.Fatal("a relayed failure marks the session exhausted")
	}
}

func TestOnlyALiveSessionsTokenIsAccepted(t *testing.T) {
	provider := newUpstream(t, 200, `{}|[DONE]`)
	proxy := startProxy(t)
	session, environment, _ := proxy.Begin(route("fixture/a", provider, ""))
	if status, _ := ask(t, proxy, "not-a-token"); status != http.StatusUnauthorized {
		t.Fatalf("unknown token: %d", status)
	}
	proxy.End(session)
	if status, _ := ask(t, proxy, token(t, environment)); status != http.StatusUnauthorized {
		t.Fatalf("ended session: %d", status)
	}
	if len(provider.models) != 0 {
		t.Fatal("nothing reaches a provider without a live session")
	}
}

func TestAnUnreachableProviderIsReportedAsAConnectionFailure(t *testing.T) {
	gone := newUpstream(t, 200, "")
	gone.server.Close()
	proxy := startProxy(t)
	session, environment, _ := proxy.Begin(route("fixture/gone", gone, ""))
	var failure string
	session.Failover = func(_ routing.Model, text string) (routing.Model, bool) {
		failure = text
		return routing.Model{}, false
	}
	if status, _ := ask(t, proxy, token(t, environment)); status != http.StatusBadGateway {
		t.Fatalf("got %d", status)
	}
	if !strings.Contains(failure, "litellm.APIConnectionError") {
		t.Fatalf("failure text %q", failure)
	}
}
