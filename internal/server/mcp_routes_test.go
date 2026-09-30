package server

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/hoaxisr/awg-manager/internal/api"
	"github.com/hoaxisr/awg-manager/internal/storage"
)

func newMcpServer(t *testing.T, mcpEnabled bool) (*Server, *storage.McpKeyStore) {
	t.Helper()
	dir := t.TempDir()
	settings := storage.NewSettingsStore(dir)
	if _, err := settings.Load(); err != nil {
		t.Fatal(err)
	}
	if err := settings.Update(func(s *storage.Settings) error {
		s.McpEnabled = mcpEnabled
		// AuthEnabled must NOT influence /mcp either way: the endpoint is
		// reachable remotely through KeenDNS, so it always demands a key.
		s.AuthEnabled = false
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	keys := storage.NewMcpKeyStore(dir)
	if err := keys.Load(); err != nil {
		t.Fatal(err)
	}
	return &Server{settings: settings, mcpKeys: keys}, keys
}

func mcpRouteHandlers() *routeHandlers {
	return &routeHandlers{
		guarded:       func(f http.HandlerFunc) http.HandlerFunc { return f },
		serverHandler: &api.ServersHandler{},
		systemHandler: &api.SystemHandler{},
	}
}

func TestRegisterMcpRoutes_SkippedWithoutKeyStore(t *testing.T) {
	mux := http.NewServeMux()
	s := &Server{}
	s.registerMcpRoutes(mux, mcpRouteHandlers())

	for _, path := range []string{"/mcp", "/api/mcp/keys", "/api/mcp/keys/create", "/api/mcp/keys/revoke"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if _, pattern := mux.Handler(req); pattern != "" {
			t.Errorf("%s registered without a key store", path)
		}
	}
}

func TestRegisterMcpRoutes_RegistersKeyManagement(t *testing.T) {
	mux := http.NewServeMux()
	s, _ := newMcpServer(t, true)
	s.registerMcpRoutes(mux, mcpRouteHandlers())

	for _, path := range []string{"/mcp", "/api/mcp/keys", "/api/mcp/keys/create", "/api/mcp/keys/revoke"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if _, pattern := mux.Handler(req); pattern == "" {
			t.Errorf("%s not registered", path)
		}
	}
}

// /mcp is invisible while the feature is off — 404, with no auth hint that
// would tell a scanner the endpoint exists.
func TestMcpEndpoint_NotFoundWhenDisabled(t *testing.T) {
	mux := http.NewServeMux()
	s, keys := newMcpServer(t, false)
	s.registerMcpRoutes(mux, mcpRouteHandlers())

	_, plaintext, err := keys.Create("laptop", false)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer "+plaintext)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", rec.Code)
	}
	if h := rec.Header().Get("WWW-Authenticate"); h != "" {
		t.Fatalf("disabled endpoint leaked an auth hint: %q", h)
	}
}

// With MCP on but AuthEnabled off, a missing or wrong key is still a 401 —
// the daemon's session auth never applies to /mcp.
func TestMcpEndpoint_UnauthorizedWithoutValidKey(t *testing.T) {
	for _, tc := range []struct {
		name   string
		header string
	}{
		{"no header", ""},
		{"garbage token", "Bearer nope"},
		{"wrong scheme", "Basic " + storage.McpKeyPrefix + "x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			s, _ := newMcpServer(t, true)
			s.registerMcpRoutes(mux, mcpRouteHandlers())

			req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("code = %d, want 401", rec.Code)
			}
		})
	}
}

// «/mcp/» со слэшем обязан попадать в тот же middleware. Иначе запрос
// проваливается в catch-all SPA и клиент получает 200 с HTML вместо 401 —
// то есть неавторизованную страницу там, где ожидался отказ.
func TestMcpEndpoint_TrailingSlashIsGuarded(t *testing.T) {
	mux := http.NewServeMux()
	s, _ := newMcpServer(t, true)
	s.registerMcpRoutes(mux, mcpRouteHandlers())

	for _, path := range []string{"/mcp/", "/mcp/anything"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: code = %d, want 401", path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); strings.Contains(ct, "text/html") {
			t.Errorf("%s: ответ HTML (%q) — запрос ушёл в SPA", path, ct)
		}
	}
}

func TestSkipSlowRequestLogCoversMcp(t *testing.T) {
	// Both mounts, and everything under the trailing-slash one: a client
	// that normalises the URL would otherwise have every long-lived
	// Streamable-HTTP stream logged as a slow request.
	for _, path := range []string{"/mcp", "/mcp/", "/mcp/anything"} {
		if !skipSlowRequestLog(path) {
			t.Errorf("%s must be exempt from the slow-request log", path)
		}
	}
	if skipSlowRequestLog("/mcpsomething") {
		t.Error("/mcpsomething is not the MCP mount and must not be exempt")
	}
}

// TestOAuthProtectedResourceMetadataIs404 — этот URL называет сам заголовок
// WWW-Authenticate у 401. OAuth в v1 намеренно не поддерживается, значит
// путь обязан отвечать честным JSON-404, а не проваливаться в catch-all SPA
// с 200 text/html, по которому клиент не отличит страницу от метаданных.
func TestOAuthProtectedResourceMetadataIs404(t *testing.T) {
	mux := http.NewServeMux()
	s, _ := newMcpServer(t, true)
	s.registerMcpRoutes(mux, mcpRouteHandlers())

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want JSON", ct)
	}
	if !strings.Contains(rec.Body.String(), "NOT_FOUND") {
		t.Fatalf("body = %q", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "bearer MCP key") {
		t.Fatalf("with MCP enabled the 404 should say why OAuth is absent: %q", rec.Body.String())
	}
}

// TestOAuthProtectedResourceMetadataHidesHintWhenMcpDisabled — при
// выключенном MCP ответ неотличим от 404 самого /mcp: подсказка про
// bearer-ключ выдала бы, что на роутере есть выключенный MCP-эндпоинт.
func TestOAuthProtectedResourceMetadataHidesHintWhenMcpDisabled(t *testing.T) {
	mux := http.NewServeMux()
	s, _ := newMcpServer(t, false)
	s.registerMcpRoutes(mux, mcpRouteHandlers())

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "OAuth") || strings.Contains(body, "MCP") {
		t.Fatalf("404 with MCP disabled leaks the endpoint: %q", body)
	}
	if !strings.Contains(body, `"message":"not found"`) {
		t.Fatalf("body = %q, want the anonymous not-found", body)
	}
}

// Сервер MCP собирается лениво (F541) — первый запрос с верным ключом при
// включённом MCP обязан получить полностью собранный сервер со списком
// инструментов.
func TestMcpEndpoint_LazyServerAnswersToolsList(t *testing.T) {
	mux := http.NewServeMux()
	s, keys := newMcpServer(t, true)
	s.registerMcpRoutes(mux, mcpRouteHandlers())
	_, plaintext, err := keys.Create("laptop", false)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	req.Header.Set("Authorization", "Bearer "+plaintext)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"list_tunnels"`) {
		t.Fatalf("в ответе нет инструментов: %s", rec.Body.String())
	}
}

// lazyHandler не строит обработчик до первого запроса и строит его ровно
// один раз, даже при одновременных запросах.
func TestLazyHandler_BuildsOnceOnFirstRequest(t *testing.T) {
	var builds atomic.Int32
	l := &lazyHandler{build: func() http.Handler {
		builds.Add(1)
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	}}
	if n := builds.Load(); n != 0 {
		t.Fatalf("построен до первого запроса: %d", n)
	}

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			l.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
			if rec.Code != http.StatusTeapot {
				t.Errorf("code = %d, want 418", rec.Code)
			}
		}()
	}
	wg.Wait()
	if n := builds.Load(); n != 1 {
		t.Fatalf("построений %d, ждали 1", n)
	}
}

// При выключенном MCP ни регистрация маршрута, ни отклонённый запрос не
// собирают сервер (F541). Сборка — ~1,1 МБ живой кучи на 64-битной сборке,
// без неё прирост около нуля (замер 29.09: 1130 КБ против −45 КБ), так что
// порог 400 КБ не дрожит от шума, а жадную сборку ловит.
func TestRegisterMcpRoutes_DisabledDoesNotBuildServer(t *testing.T) {
	s, _ := newMcpServer(t, false)
	h := mcpRouteHandlers()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	mux := http.NewServeMux()
	s.registerMcpRoutes(mux, h)
	mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}")))

	runtime.GC()
	runtime.ReadMemStats(&after)
	if grown := int64(after.HeapAlloc) - int64(before.HeapAlloc); grown > 400<<10 {
		t.Fatalf("куча выросла на %d КБ — сервер MCP собран при выключенном MCP", grown>>10)
	}
	runtime.KeepAlive(mux)
}

// Паника при сборке не хоронит обработчик до перезапуска: следующий запрос
// собирает заново.
func TestLazyHandler_RetriesAfterPanickedBuild(t *testing.T) {
	calls := 0
	l := &lazyHandler{build: func() http.Handler {
		calls++
		if calls == 1 {
			panic("сборка упала")
		}
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	}}

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("ждали панику первой сборки")
			}
		}()
		l.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	}()

	rec := httptest.NewRecorder()
	l.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusTeapot {
		t.Fatalf("code = %d, want 418: после паники обработчик не пересобран", rec.Code)
	}
}
