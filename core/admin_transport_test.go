package core

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"llm-router-go/types"
)

// TestAdminAPITransportGuard 校验管理接口对第三方网页 / DNS rebinding 的跨域
// 拦截，同时保证本机回环来源（本地管理面板）仍可用。
func TestAdminAPITransportGuard(t *testing.T) {
	s := &Server{}
	s.mu.Lock()
	s.cfg = types.Config{AuthToken: "", Host: "127.0.0.1", Port: 3000}
	s.mu.Unlock()

	t.Run("third-party cross-origin origin blocked", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
		req.Header.Set("Origin", "https://evil.example")
		s.handleAdminAPI(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got == "*" {
			t.Fatalf("must not send wildcard CORS on admin API, got %q", got)
		}
	})

	t.Run("rebound host without origin blocked", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
		req.Host = "evil.example:3000"
		s.handleAdminAPI(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})

	t.Run("loopback origin allowed", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
		req.RemoteAddr = "127.0.0.1:51000"
		req.Host = "127.0.0.1:3000"
		req.Header.Set("Origin", "http://127.0.0.1:3000")
		s.handleAdminAPI(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://127.0.0.1:3000" {
			t.Fatalf("loopback origin should be echoed, got %q", got)
		}
	})

	t.Run("forged loopback origin from lan blocked", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
		req.RemoteAddr = "192.168.1.20:51000"
		req.Host = "192.168.1.20:3000"
		req.Header.Set("Origin", "http://127.0.0.1:3000")
		s.handleAdminAPI(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403 for forged loopback origin", rec.Code)
		}
	})
}
