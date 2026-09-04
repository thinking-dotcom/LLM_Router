package core

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"llm-router-go/config"
	"llm-router-go/router"
	"llm-router-go/types"
)

const maxLogs = 5000
const maxBodySize = 10 * 1024 * 1024 // 10MB max request body
const logSaveDebounceMs = 5000

// apiKeyMask 是 HTTP 管理接口 GET 掩码 API Key / AuthToken 用的哨兵值：POST 回写
// 遇到该值时保留服务端现有密钥/令牌，防止「读出掩码配置→原样 POST」把真实值覆盖掉。
const apiKeyMask = "****"

// safeHTTPClient 已移除：从未被使用，上游请求统一走 router.upstreamHTTPClient。

type Server struct {
	mu           sync.RWMutex
	cfg          types.Config
	logs         []types.LogEntry
	httpServer   *http.Server
	running      bool
	startError   string
	saveTimer    *time.Timer
	activeModels map[string]*activeModel // model → 在途请求计数 + 最近活跃时刻

	// saveMu 串行化「落盘+换内存」整个保存序列：桌面端 App.SaveConfig 与
	// HTTP /api/config POST 都汇入 SaveConfig，并发时磁盘/内存可能乱序不一致。
	saveMu sync.Mutex
	// logsMu 串行化 logs.json 落盘：AddLog 的防抖 AfterFunc goroutine 与
	// ClearLogs/Stop 的直接落盘可能并发写同一文件，OS 层交错会产生损坏的 JSON
	logsMu sync.Mutex
	stopCh chan struct{} // 关闭后通知所有周期性 goroutine 退出（Restart 不再泄漏）
}

// activeModel 跟踪某模型的在途请求数：请求开始 +1、结束 -1。
// 概览页光波动画据此判断"该模型是否正在被请求"。
type activeModel struct {
	inflight int
	last     time.Time
}

func NewServer() *Server {
	s := &Server{
		cfg:          config.LoadConfig(),
		activeModels: make(map[string]*activeModel),
	}
	s.loadLogs()
	return s
}

func (s *Server) AddLog(e types.LogEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// 尾部追加（内存中 oldest→newest），避免头插导致的 O(n) 拷贝
	s.logs = append(s.logs, e)
	if len(s.logs) > maxLogs {
		s.logs = s.logs[len(s.logs)-maxLogs:]
	}
	// 延迟保存
	if s.saveTimer != nil {
		s.saveTimer.Stop()
	}
	s.saveTimer = time.AfterFunc(logSaveDebounceMs*time.Millisecond, s.saveLogs)
}

func (s *Server) GetLogs() []types.LogEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := min(200, len(s.logs))
	out := make([]types.LogEntry, n)
	for i := 0; i < n; i++ {
		out[i] = s.logs[len(s.logs)-1-i]
	}
	return out
}

// markModelActive 记录某模型有一个请求进入在途状态（请求开始时调用）。
// 返回的 cleanup 由请求方在结束时 defer 调用，把在途计数减一并刷新活跃时刻：
// 概览页的光波动画只在"该模型有请求正在进行（或刚结束片刻）"时亮起，
// 停止请求后动画随之熄灭，而不是像固定时间窗口那样再续亮一段时间。
func (s *Server) markModelActive(model string) func() {
	if model == "" {
		return func() {}
	}
	s.mu.Lock()
	if s.activeModels == nil {
		s.activeModels = make(map[string]*activeModel)
	}
	a := s.activeModels[model]
	if a == nil {
		a = &activeModel{}
		s.activeModels[model] = a
	}
	a.inflight++
	a.last = time.Now()
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if cur := s.activeModels[model]; cur != nil {
			if cur.inflight > 0 {
				cur.inflight--
			}
			cur.last = time.Now()
		}
	}
}

// ActiveModels 返回仍在处理请求（在途）或刚结束 withinSeconds 秒内的模型名集合，
// 供桌面端概览页判断哪些模型处于"正在使用"状态（光波动画）。
// 请求进行中（inflight>0）始终返回；请求结束后仅保留 withinSeconds 的缓冲，
// 避免快速连续请求之间的间隔导致动画闪烁。过期且无在途请求的条目顺手清理。
func (s *Server) ActiveModels(withinSeconds int) []string {
	if withinSeconds <= 0 {
		withinSeconds = 60
	}
	cutoff := time.Now().Add(-time.Duration(withinSeconds) * time.Second)
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.activeModels))
	for m, a := range s.activeModels {
		if a.inflight > 0 || a.last.After(cutoff) {
			out = append(out, m)
		} else if a.inflight == 0 {
			delete(s.activeModels, m)
		}
	}
	return out
}

// GetAllLogs 返回内存中的全部日志（最多 maxLogs 条），供统计计算使用，
// 避免统计只基于最近 200 条导致 7 天数据不完整。返回顺序与 GetLogs 一致（最新在前）。
func (s *Server) GetAllLogs() []types.LogEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]types.LogEntry, len(s.logs))
	for i := range s.logs {
		out[i] = s.logs[len(s.logs)-1-i]
	}
	return out
}

func (s *Server) ClearLogs() {
	s.mu.Lock()
	s.logs = nil
	if s.saveTimer != nil {
		s.saveTimer.Stop()
	}
	s.mu.Unlock()
	s.saveLogs() // 立即落盘，防止重启后旧日志复活
}

func (s *Server) GetConfig() types.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func (s *Server) SaveConfig(cfg types.Config) types.SaveResult {
	// 串行化「落盘+换内存」：并发保存时若无此锁，两次落盘与两次内存替换可能
	// 交错成「磁盘是新配置、内存是旧配置」的乱序不一致。
	s.saveMu.Lock()
	defer s.saveMu.Unlock()

	// 先落盘、成功后再替换内存配置：避免磁盘写失败（磁盘满/权限拒绝）时
	// 内存已先行生效、重启后又回退旧配置的前后不一致。
	// 落盘放到锁外进行，避免磁盘 I/O 期间阻塞其他请求处理（含正在代理的 LLM 请求）；
	// 同时真实反映写入结果，失败时如实返回错误，不再谎报成功。
	if err := config.SaveConfig(cfg); err != nil {
		return types.SaveResult{Ok: false, Error: err.Error(), RestartNeeded: false}
	}

	s.mu.Lock()
	old := s.cfg
	s.cfg = cfg
	// 主端口 / host 的变更都需要重启才生效
	portChanged := cfg.Port != old.Port || cfg.Host != old.Host
	s.mu.Unlock()

	// 模型缓存失效：baseUrl/type 变化或已被删除的服务商不再使用旧缓存，
	// 变化的服务商异步重新拉取模型列表，避免路由继续按旧地址解析。
	changed := map[string]bool{}
	for _, np := range cfg.Providers {
		for _, op := range old.Providers {
			if np.ID == op.ID && (np.BaseURL != op.BaseURL || np.Type != op.Type) {
				changed[np.ID] = true
			}
		}
		// 此前关闭/不存在的服务商被重新启用且未内置模型时，一并后台拉取模型列表，
		// 否则这类服务商路由/模型列表会一直为空（原逻辑只在启动时或 URL/类型变化时拉取）。
		// 仅影响"新启用且无静态模型"的服务商，其它情况行为不变。
		if np.Enabled && len(np.Models) == 0 {
			wasEnabled := false
			hadModels := false
			for _, op := range old.Providers {
				if op.ID == np.ID {
					wasEnabled = op.Enabled
					hadModels = len(op.Models) > 0
					break
				}
			}
			// 新启用、或静态模型从非空被清空：均需后台重拉，否则该服务商
			// 模型列表变空（静态为空且缓存缺失时路由/模型列表解析不到）
			if !wasEnabled || hadModels {
				changed[np.ID] = true
			}
		}
	}
	for _, op := range old.Providers {
		removed := true
		for _, np := range cfg.Providers {
			if np.ID == op.ID {
				removed = false
				break
			}
		}
		if removed {
			router.ClearFetchedModel(op.ID)
		}
	}
	if len(changed) > 0 {
		ids := make([]string, 0, len(changed))
		for id := range changed {
			ids = append(ids, id)
		}
		go s.refetchChangedModels(ids)
	}
	return types.SaveResult{Ok: true, RestartNeeded: portChanged}
}

// refetchChangedModels 为配置发生变化的服务商清除旧缓存并异步重新拉取模型列表。
func (s *Server) refetchChangedModels(ids []string) {
	// 后台 goroutine 没有 HTTP 层的 recover 兕底，panic 会直接崩掉整个进程
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("[RECOVER] panic in refetchChangedModels: %v", rec)
		}
	}()
	cfg := s.GetConfig()
	for _, id := range ids {
		var p *types.Provider
		for i := range cfg.Providers {
			if cfg.Providers[i].ID == id {
				p = &cfg.Providers[i]
				break
			}
		}
		if p == nil || !p.Enabled {
			continue
		}
		router.ClearFetchedModel(id)
		if r := router.FetchModels(*p); r.Ok && len(r.Models) > 0 {
			router.UpdateFetchedModels(id, r.Models)
			log.Printf("[SERVER] 已重新拉取服务商「%s」的 %d 个模型", p.Name, len(r.Models))
		}
	}
}

func (s *Server) Port() int          { s.mu.RLock(); defer s.mu.RUnlock(); return s.cfg.Port }
func (s *Server) Host() string       { s.mu.RLock(); defer s.mu.RUnlock(); return s.cfg.Host }
func (s *Server) Running() bool      { s.mu.RLock(); defer s.mu.RUnlock(); return s.running }
func (s *Server) StartError() string { s.mu.RLock(); defer s.mu.RUnlock(); return s.startError }

func (s *Server) LocalBase() string {
	h := s.Host()
	if h == "0.0.0.0" || h == "" {
		h = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(h, strconv.Itoa(s.Port()))
}

func (s *Server) Start() error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	addr := net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.cfg.Port))

	var ln net.Listener
	for attempt := 0; attempt < 3; attempt++ {
		var err error
		ln, err = net.Listen("tcp", addr)
		if err == nil {
			break
		}
		if attempt < 2 {
			time.Sleep(400 * time.Millisecond)
			continue
		}
		s.startError = err.Error()
		s.mu.Unlock()
		return err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRequest)
	// ReadTimeout 覆盖整个 body 读取（Go 语义），10MB 大请求 + 慢网络会被
	// 30s 掐断（多模态 base64 图片很常见），客户端只看到莫名连接重置；
	// 改为只限制请求头读取，body 超时交由请求级 context（10 分钟）兜底。
	srv := &http.Server{Handler: mux, IdleTimeout: 30 * time.Second, ReadHeaderTimeout: 10 * time.Second}
	s.httpServer = srv
	s.running = true
	s.startError = ""
	stopCh := make(chan struct{})
	s.stopCh = stopCh
	s.mu.Unlock()

	// 捕获局部变量后启动，避免 goroutine 内延迟读取 s.httpServer / s.stopCh
	//（Stop 会置 nil）造成的 data race 与 nil.Serve 恐慌 / nil 通道阻塞泄漏。
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("[SERVER] %v", err)
		}
	}()

	log.Printf("路由服务已就绪: %s", s.LocalBase())
	go s.autoFetchModels()
	go s.periodicLogCleanup(stopCh)
	return nil
}

// periodicLogCleanup removes logs older than 7 days every hour.
// stopCh is captured at Start time instead of reading s.stopCh each
// iteration, avoiding a data race with Stop() replacing the field under lock.
func (s *Server) periodicLogCleanup(stopCh <-chan struct{}) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			s.mu.Lock()
			cutoff := time.Now().AddDate(0, 0, -7)
			filtered := make([]types.LogEntry, 0, len(s.logs))
			for _, e := range s.logs {
				t, err := time.Parse(time.RFC3339, e.Time)
				if err != nil || t.After(cutoff) {
					filtered = append(filtered, e)
				}
			}
			s.logs = filtered
			s.mu.Unlock()
		}
	}
}

func (s *Server) Stop() error {
	s.mu.Lock()
	if s.saveTimer != nil {
		s.saveTimer.Stop()
	}
	srv := s.httpServer
	s.httpServer = nil
	stopCh := s.stopCh
	s.stopCh = nil
	s.running = false
	s.mu.Unlock()

	// 优雅关闭：先等在途请求完成（限时兑底强断），再落盘日志。
	// 之前的 httpServer.Close() 会立即拍断所有在途请求，这些请求随后
	// AddLog 的条目只留在内存，导致退出/重启时丢日志。
	if srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = srv.Shutdown(ctx)
		cancel()
	}
	if stopCh != nil {
		close(stopCh)
	}

	// 排空期间在途请求的 AddLog 可能重新挂上 saveTimer，停掉它，
	// 由下面的 saveLogs() 做最终一次性落盘
	s.mu.Lock()
	if s.saveTimer != nil {
		s.saveTimer.Stop()
		s.saveTimer = nil
	}
	s.mu.Unlock()

	// 落盘在锁外进行，避免持锁做磁盘 I/O 阻塞其余请求处理
	s.saveLogs()
	return nil
}

func (s *Server) Restart() error {
	s.Stop()
	return s.Start()
}

func (s *Server) autoFetchModels() {
	// 后台 goroutine 没有 HTTP 层的 recover 兕底，panic 会直接崩掉整个进程
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("[RECOVER] panic in autoFetchModels: %v", rec)
		}
	}()
	cfg := s.GetConfig()
	// 为配置中未内置模型列表的服务商拉取一次模型，保持路由与 /v1/models 可用。
	// 仅拉取，不做可用性测试。
	for _, p := range cfg.Providers {
		if !p.Enabled || len(p.Models) > 0 {
			continue
		}
		result := router.FetchModels(p)
		if result.Ok && len(result.Models) > 0 {
			router.UpdateFetchedModels(p.ID, result.Models)
			log.Printf("已自动拉取「%s」的 %d 个模型", p.Name, len(result.Models))
		}
	}
}

func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	// 限制请求体大小
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)

	// panic 恢复，防止单个请求异常导致整个服务崩溃
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("[RECOVER] panic in handler: %v", rec)
			http.Error(w, "Internal Server Error", 500)
		}
	}()

	if r.Method == "OPTIONS" {
		// 预检与管理接口同策略：仅回环 Origin 回显，外部网站不下发 ACAO
		for k, v := range adminCORSHeaders(r) {
			w.Header().Set(k, v)
		}
		w.WriteHeader(204)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		s.handleAdminAPI(w, r)
		return
	}
	// 为 LLM 代理响应（含流式 SSE）统一设置受限 CORS 头：仅回环来源回显 Origin，
	// 外部网页既读不到响应也无法通过预检发送，防止借浏览器消耗用户上游 Key；
	// 管理接口在前面单独设置。
	for k, v := range adminCORSHeaders(r) {
		w.Header().Set(k, v)
	}
	cfg := s.GetConfig()
	resp := router.HandleLLM(w, r, &cfg, func(e types.LogEntry) { s.AddLog(e) }, s.markModelActive)
	if resp != nil {
		// CORS 头已由上面统一设置，复制上游头时跳过避免重复；
		// 其余头（如上游 Content-Type）透传给客户端。
		for k, vals := range resp.Header {
			if strings.HasPrefix(strings.ToLower(k), "access-control-") {
				continue
			}
			for _, v := range vals {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		if resp.Body != nil {
			io.Copy(w, resp.Body)
			resp.Body.Close()
		}
	}
}

func (s *Server) handleAdminAPI(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("[RECOVER] panic in admin API: %v", rec)
			http.Error(w, "Internal Server Error", 500)
		}
	}()

	// 管理接口只接受本机回环访问。第三方网页（无论是否带 token）都可能触发
	// 跨域读取日志/配置或执行清空/改配置等副作用，这里先按 transport 拦截，
	// 避免日志和请求体被任意网页读取；CORS 也只回显回环 Origin，不再下发 *。
	if !s.adminTransportAllowed(r) {
		writeJSON(w, 403, map[string]string{"error": "管理接口仅允许本机访问"})
		return
	}
	// 鉴权前设置 CORS：未通过鉴权返回 401 时浏览器也能读取错误信息（仅回环来源）
	for k, v := range adminCORSHeaders(r) {
		w.Header().Set(k, v)
	}
	if !s.adminAllowed(r) {
		writeJSON(w, 401, map[string]string{"error": "管理接口需要路由令牌"})
		return
	}
	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		s.handleAdminHTML(w, r)
		return
	}

	path := r.URL.Path
	switch {
	case path == "/api/config" && r.Method == "GET":
		// 管理接口属于浏览器可达通道：脱敏 API Key 与路由令牌，避免无鉴权模式下泄露密钥
		cfg := s.GetConfig()
		// GetConfig 返回的 Providers 切片与内部配置共享底层数组，直接改会污染内存中的
		// API Key；先复制一份再脱敏，避免后续桌面端保存时把 "****" 落盘。
		provs := make([]types.Provider, len(cfg.Providers))
		for i, p := range cfg.Providers {
			provs[i] = p
			if p.APIKey != "" {
				provs[i].APIKey = apiKeyMask
			}
		}
		cfg.Providers = provs
		// AuthToken 是值拷贝字段，直接改副本不污染内存配置
		if cfg.AuthToken != "" {
			cfg.AuthToken = apiKeyMask
		}
		writeJSON(w, 200, map[string]any{"config": cfg})
	case path == "/api/config" && r.Method == "POST":
		var data map[string]any
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			// 解析失败直接拒绝，绝不把空/默认配置落盘覆盖用户现有配置
			writeJSON(w, 400, map[string]string{"error": "无法解析请求体: " + err.Error()})
			return
		}
		if data == nil {
			writeJSON(w, 400, map[string]string{"error": "请求体缺少 config 字段"})
			return
		}
		raw, ok := data["config"].(map[string]any)
		if !ok || raw == nil {
			writeJSON(w, 400, map[string]string{"error": "请求体缺少 config 对象"})
			return
		}
		// 保存前校验：非法配置（端口越界、类型错误、BaseURL 格式不对）一律拦截，
		// 既防止脏数据落盘，也避免保存后才在运行时暴露问题。
		if errMsg := config.ValidateConfig(raw); errMsg != "" {
			writeJSON(w, 400, map[string]string{"error": errMsg})
			return
		}
		cfg := config.Normalize(raw)
		// 掩码回写保护：提交的 key 为掩码哨兵时保留服务端现有密钥
		preserveMaskedAPIKeys(&cfg, s.GetConfig())
		result := s.SaveConfig(cfg)
		code := 200
		if !result.Ok {
			code = 400
		}
		writeJSON(w, code, result)
	case path == "/api/logs" && r.Method == "GET":
		writeJSON(w, 200, s.GetLogs())
	case path == "/api/logs/clear" && r.Method == "POST":
		s.ClearLogs()
		writeJSON(w, 200, map[string]bool{"ok": true})
	case path == "/api/status" && r.Method == "GET":
		writeJSON(w, 200, types.StatusData{
			Running: s.Running(), Port: s.Port(), Host: s.Host(),
			LocalBase: s.LocalBase(), Providers: len(s.GetConfig().Providers),
		})
	case path == "/api/models" && r.Method == "GET":
		// 与 handleAdminHTML 中 /api/models 的跳转目标保持一致：
		// 返回聚合后的模型列表（含服务商归属），供管理端/脚本直接取用。
		cfg := s.GetConfig()
		writeJSON(w, 200, map[string]any{"data": router.AggregateModels(&cfg)})
	case path == "/api/stats" && r.Method == "GET":
		writeJSON(w, 200, ComputeStats(s.GetAllLogs()))
	default:
		writeJSON(w, 404, map[string]string{"error": "未知管理接口: " + path})
	}
}

// preserveMaskedAPIKeys 收到掩码哨兵 apiKeyMask 的字段（服务商 API Key 与
// 路由令牌 AuthToken）用服务端现有值回填，防止第三方脚本「GET 掩码配置 →
// POST 原样回写」时把真实密钥/令牌覆盖成 "****"。
func preserveMaskedAPIKeys(cfg *types.Config, cur types.Config) {
	if cfg.AuthToken == apiKeyMask {
		cfg.AuthToken = cur.AuthToken
	}
	for i := range cfg.Providers {
		if cfg.Providers[i].APIKey != apiKeyMask {
			continue
		}
		matched := false
		for _, op := range cur.Providers {
			if op.ID == cfg.Providers[i].ID {
				// 匹配到即回填服务器现有值（可能为空=清空），避免把哨兵 "****" 落成真实密钥
				cfg.Providers[i].APIKey = op.APIKey
				matched = true
				break
			}
		}
		// 未匹配到（如 provider 被改名/新增后仍带掩码）：置空而不是把
		// 字面量 "****" 当真实密钥写入 config.json，否则上游鉴权必然 401
		if !matched {
			cfg.Providers[i].APIKey = ""
		}
	}
}

func (s *Server) adminAllowed(r *http.Request) bool {
	if r.Method == "OPTIONS" {
		return true
	}
	token := s.GetConfig().AuthToken
	if token == "" {
		return true
	}
	// Authorization 的 scheme 按 HTTP 规范大小写不敏感（Bearer/bearer 均可），
	// 与 router.CheckAuth 的 LLM 代理鉴权保持一致；token 比较用常量时间算法，
	// 避免逐字节短路造成的时序侧信道。
	const bearerScheme = "Bearer "
	auth := r.Header.Get("Authorization")
	if len(auth) > len(bearerScheme) && strings.EqualFold(auth[:len(bearerScheme)], bearerScheme) &&
		subtle.ConstantTimeCompare([]byte(auth[len(bearerScheme):]), []byte(token)) == 1 {
		return true
	}
	return subtle.ConstantTimeCompare([]byte(r.Header.Get("x-api-key")), []byte(token)) == 1 ||
		subtle.ConstantTimeCompare([]byte(r.Header.Get("x-admin-token")), []byte(token)) == 1
}

// adminTransportAllowed 阻止第三方网页跨域读写管理接口。浏览器跨域请求会带
// Origin 头：仅放行回环来源。无 Origin 的普通导航或非浏览器客户端不会带 Origin，
// 此时要求 Host 是回环地址，防止 DNS rebinding 把 evil.com:3000 当作管理端访问。
func (s *Server) adminTransportAllowed(r *http.Request) bool {
	if o := r.Header.Get("Origin"); o != "" {
		return loopbackOrigin(o)
	}
	if strings.HasPrefix(r.URL.Path, "/api/") && r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	if !isLoopbackHost(r.Host) {
		return false
	}
	// 无 Origin 的请求（非浏览器直接 TCP 客户端）Host 头可任意伪造，单靠它不足以
	// 证明请求来自本机。追加校验对端 RemoteAddr 必须为回环：即便监听 0.0.0.0 且
	// 未设 authToken，局域网机器也无法靠伪造 Host 头访问管理接口。
	return isLoopbackAddr(r.RemoteAddr)
}

// isLoopbackAddr 判断请求对端地址是否为本机回环（127.0.0.1 / ::1 及 IPv4-mapped 形式）。
func isLoopbackAddr(remoteAddr string) bool {
	host := strings.TrimSpace(remoteAddr)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// adminCORSHeaders 生成受限 CORS 头（LLM 代理端点与管理接口共用）：仅当请求
// Origin 的主机是本机回环（localhost/127.0.0.1/[::1]，任意端口）时回显该 Origin，
// 外部网站 Origin 一律不返回 Access-Control-Allow-Origin。
func adminCORSHeaders(r *http.Request) map[string]string {
	h := map[string]string{
		"Access-Control-Allow-Methods": "GET,POST,OPTIONS",
		"Access-Control-Allow-Headers": "content-type, authorization, x-api-key, anthropic-version, x-goog-api-key, x-admin-token",
	}
	if o := r.Header.Get("Origin"); o != "" && loopbackOrigin(o) {
		h["Access-Control-Allow-Origin"] = o
		h["Vary"] = "Origin"
	}
	return h
}

func loopbackOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	return isLoopbackHost(u.Host)
}

func isLoopbackHost(hostport string) bool {
	host := strings.TrimSpace(hostport)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	lower := strings.ToLower(host)
	switch lower {
	case "", "localhost", "127.0.0.1", "::1", "0.0.0.0", "0:0:0:0:0:0:0:1":
		return true
	}
	if ip := net.ParseIP(lower); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// FetchModelsList 拉取模型列表并更新缓存（不测试可用性），
// 供桌面端“检测连接”“复制模型ID”等操作使用。
func (s *Server) FetchModelsList(p types.Provider) types.FetchModelsResult {
	result := router.FetchModels(p)
	if result.Ok && len(result.Models) > 0 {
		router.UpdateFetchedModels(p.ID, result.Models)
	}
	return result
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// handleAdminHTML 处理浏览器对管理接口的访问：统一跳转到首页对应面板。
// 表单 POST（清空日志）先执行动作再跳转；JSON 请求不受影响。
func (s *Server) handleAdminHTML(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// 禁止浏览器缓存，避免用户看到旧版本页面
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	// 浏览器表单触发的清空日志：清空后跳回首页日志面板
	if path == "/api/logs/clear" && r.Method == "POST" {
		s.ClearLogs()
		redirectToTab(w, "logs")
		return
	}

	// 其余管理接口：统一跳转到首页对应面板
	tab := "overview"
	switch path {
	case "/api/status":
		tab = "overview"
	case "/api/stats":
		tab = "stats"
	case "/api/logs":
		tab = "logs"
	case "/api/models":
		tab = "models"
	}
	redirectToTab(w, tab)
}

// redirectToTab 跳转到首页并打开指定面板
func redirectToTab(w http.ResponseWriter, tab string) {
	w.Header().Set("Location", "/?tab="+tab)
	w.WriteHeader(http.StatusFound)
}

func logsPath() string {
	return filepath.Join(filepath.Dir(config.ConfigPath()), "logs.json")
}

func (s *Server) loadLogs() {
	data, err := os.ReadFile(logsPath())
	if err != nil {
		return
	}
	var logs []types.LogEntry
	if json.Unmarshal(data, &logs) != nil {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -7)
	var recent []types.LogEntry
	for _, e := range logs {
		t, err := time.Parse(time.RFC3339, e.Time)
		if err != nil || t.After(cutoff) {
			recent = append(recent, e)
		}
	}
	// 文件按最新在前保存；内存按 oldest→newest 存储（AddLog 尾部追加），翻转对齐
	for i, j := 0, len(recent)-1; i < j; i, j = i+1, j-1 {
		recent[i], recent[j] = recent[j], recent[i]
	}
	s.logs = recent
}

// FlushLogs 显式强制把内存日志立即落盘（绕过防抖定时器），供进程退出路径调用。
func (s *Server) FlushLogs() { s.saveLogs() }

// saveLogs 在锁外把内存日志落盘：先快照再写，避免持 s.mu 做磁盘 I/O 阻塞请求处理。
// logsMu 串行化多次并发的落盘调用（AddLog 防抖 goroutine / ClearLogs / Stop /
// FlushLogs），防止两个 goroutine 同时写 logs.json 产生交错损坏的文件。
func (s *Server) saveLogs() {
	// saveLogs 会在 AfterFunc 定时器 goroutine 中运行（无 recover 兕底），
	// panic 会直接崩掉整个进程
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("[RECOVER] panic in saveLogs: %v", rec)
		}
	}()
	s.logsMu.Lock()
	defer s.logsMu.Unlock()

	s.mu.RLock()
	logs := make([]types.LogEntry, len(s.logs))
	copy(logs, s.logs)
	s.mu.RUnlock()

	cutoff := time.Now().AddDate(0, 0, -7)
	var recent []types.LogEntry
	for _, e := range logs {
		t, err := time.Parse(time.RFC3339, e.Time)
		if err != nil || t.After(cutoff) {
			recent = append(recent, e)
		}
	}
	// 内存为 oldest→newest，落盘时翻转为最新在前，保持与旧版文件格式一致
	out := make([]types.LogEntry, len(recent))
	for i := range recent {
		out[i] = recent[len(recent)-1-i]
	}
	data, err := json.Marshal(out)
	if err != nil {
		return
	}
	// 原子落盘（tmp+rename，与 config.json 写盘策略一致），避免进程写中途
	// 崩溃/断电留下半截 logs.json（loadLogs 会静默容忍，仅丢日志不崩程序）
	p := logsPath()
	tmp, err := os.CreateTemp(filepath.Dir(p), "."+filepath.Base(p)+".*.tmp")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	_, writeErr := tmp.Write(data)
	if writeErr == nil {
		_ = tmp.Sync()
		writeErr = tmp.Close()
	} else {
		_ = tmp.Close()
	}
	if writeErr == nil {
		if renameErr := os.Rename(tmpName, p); renameErr == nil {
			tmpName = "" // 已改名成功，不再清理
		}
	}
	if tmpName != "" {
		_ = os.Remove(tmpName)
	}
}
