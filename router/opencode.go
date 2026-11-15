package router

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"llm-router-go/types"
)

// opencode Zen GO 网关（如 https://opencode.ai/zen/go/v1）要求每个请求携带
// x-opencode-session 会话头，缺失时返回 400 [type: MissingSessionID]。deepcode
// 等客户端不会带这个头，而路由器出站会重建请求头（即便 opencode CLI 自带也会
// 被丢弃），故在此统一补齐：客户端带了则原样透传（保留真实会话亲和），否则用
// 服务商派生的确定性会话 ID——同一服务商始终同一会话、跨重启不变。
func isOpencodeGateway(baseURL string) bool {
	host := strings.ToLower(strings.TrimSpace(baseURL))
	if u, err := url.Parse(strings.TrimSpace(baseURL)); err == nil && u.Hostname() != "" {
		host = strings.ToLower(u.Hostname())
	}
	return strings.Contains(host, "opencode")
}

func derivedOpencodeSession(p types.Provider) string {
	sum := sha256.Sum256([]byte("opencode-session:" + p.ID + ":" + p.BaseURL))
	h := fmt.Sprintf("%x", sum)
	return h[0:8] + "-" + h[8:12] + "-4" + h[13:16] + "-8" + h[17:20] + "-" + h[20:32]
}

// applyOpencodeSession 按需给上游请求补 x-opencode-session。incoming 为 nil
// 表示无入站请求可透传（如后台模型列表探测）。
func applyOpencodeSession(h http.Header, p types.Provider, incoming *http.Request) {
	if !isOpencodeGateway(p.BaseURL) {
		return
	}
	if incoming != nil {
		if v := strings.TrimSpace(incoming.Header.Get("x-opencode-session")); v != "" {
			h.Set("x-opencode-session", v)
			return
		}
	}
	h.Set("x-opencode-session", derivedOpencodeSession(p))
}
