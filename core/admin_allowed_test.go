package core

import (
	"net/http"
	"testing"

	"llm-router-go/types"
)

// adminAllowed 对 Authorization scheme 应大小写不敏感(Bearer/bearer/BEARER),
// 与 LLM 代理鉴权一致;错误 token 与缺失 scheme 均应拒绝。
func TestAdminAllowedBearerCaseInsensitive(t *testing.T) {
	s := &Server{}
	s.mu.Lock()
	s.cfg = types.Config{AuthToken: "secret-token", Host: "127.0.0.1", Port: 3000}
	s.mu.Unlock()

	cases := []struct {
		header string
		want   bool
	}{
		{"Bearer secret-token", true},
		{"bearer secret-token", true},
		{"BEARER secret-token", true},
		{"Bearer secret-token ", false}, // 尾部空格 → token 不匹配
		{"Bearer wrong", false},
		{"", false},
		{"secret-token", false}, // 无 scheme
	}
	for _, c := range cases {
		req, _ := http.NewRequest("GET", "/api/config", nil)
		if c.header != "" {
			req.Header.Set("Authorization", c.header)
		}
		if got := s.adminAllowed(req); got != c.want {
			t.Errorf("adminAllowed(%q) = %v, want %v", c.header, got, c.want)
		}
	}

	// x-api-key 与 x-admin-token 仍然有效
	req, _ := http.NewRequest("GET", "/api/config", nil)
	req.Header.Set("x-api-key", "secret-token")
	if !s.adminAllowed(req) {
		t.Error("x-api-key should be accepted")
	}
	req2, _ := http.NewRequest("GET", "/api/config", nil)
	req2.Header.Set("x-admin-token", "secret-token")
	if !s.adminAllowed(req2) {
		t.Error("x-admin-token should be accepted")
	}

	// OPTIONS 预检无需鉴权
	opt, _ := http.NewRequest("OPTIONS", "/api/config", nil)
	if !s.adminAllowed(opt) {
		t.Error("OPTIONS should be allowed")
	}
}

// 未配置 token 时管理接口无需鉴权。
func TestAdminAllowedNoToken(t *testing.T) {
	s := &Server{}
	s.mu.Lock()
	s.cfg = types.Config{AuthToken: "", Host: "127.0.0.1", Port: 3000}
	s.mu.Unlock()

	req, _ := http.NewRequest("GET", "/api/config", nil)
	if !s.adminAllowed(req) {
		t.Error("no token configured → admin allowed")
	}
}
