package router

import (
	"net/http"
	"regexp"
	"testing"

	"llm-router-go/types"
)

var uuidShape = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-8[0-9a-f]{3}-[0-9a-f]{12}$`)

func TestDerivedOpencodeSessionStableUUIDShape(t *testing.T) {
	p := types.Provider{ID: "p-1", BaseURL: "https://opencode.ai/zen/go/v1"}
	a, b := derivedOpencodeSession(p), derivedOpencodeSession(p)
	if a != b {
		t.Fatalf("session id should be deterministic: %q vs %q", a, b)
	}
	if !uuidShape.MatchString(a) {
		t.Fatalf("session id not UUID-shaped: %q", a)
	}
	// 不同服务商派生不同会话，避免共享同一上游路由会话
	p2 := p
	p2.ID = "p-2"
	if a == derivedOpencodeSession(p2) {
		t.Fatalf("different providers should derive different session ids")
	}
}

func TestApplyOpencodeSession(t *testing.T) {
	oc := types.Provider{ID: "p-1", BaseURL: "https://opencode.ai/zen/go/v1"}
	other := types.Provider{ID: "p-2", BaseURL: "https://api.deepseek.com"}

	h := http.Header{}
	applyOpencodeSession(h, other, nil)
	if _, ok := h["X-Opencode-Session"]; ok {
		t.Fatalf("non-opencode upstream must not get the session header")
	}

	h = http.Header{}
	applyOpencodeSession(h, oc, nil)
	if got := h.Get("x-opencode-session"); got != derivedOpencodeSession(oc) {
		t.Fatalf("derived session expected, got %q", got)
	}

	// 客户端自带则透传（保留真实会话亲和）
	in := &http.Request{Header: http.Header{}}
	in.Header.Set("x-opencode-session", "client-session")
	h = http.Header{}
	applyOpencodeSession(h, oc, in)
	if got := h.Get("x-opencode-session"); got != "client-session" {
		t.Fatalf("client session should pass through, got %q", got)
	}
}

func TestIsOpencodeGateway(t *testing.T) {
	yes := []string{"https://opencode.ai/zen/go/v1", "https://GO.opencode.ai/x", "https://my-opencode.example.com/v1"}
	no := []string{"https://api.deepseek.com", "https://openrouter.ai/api/v1", ""}
	for _, s := range yes {
		if !isOpencodeGateway(s) {
			t.Fatalf("expected %q to be detected as opencode gateway", s)
		}
	}
	for _, s := range no {
		if isOpencodeGateway(s) {
			t.Fatalf("expected %q NOT to be detected as opencode gateway", s)
		}
	}
}
