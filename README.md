# LLM Router · Go + Wails 桌面版

一个带图形界面的本地 LLM 网关——把多种 API 格式统一挂到本地一个端口上，按模型名路由到不同云端上游服务商，并做请求/响应/流式/工具调用的格式互转。新增多目标 Agent 适配：集中管理 `skills` 与 `mcp`，一键注入到各 Agent 客户端。

- **Go 后端**（`net/http` + Wails v2.14 桌面框架，Go 1.25）
- **Vue 3.5 + Vite 5 GUI**（WebView2，`1200×633` 启动最小化到任务栏（托盘常驻时从托盘恢复），`Min 960×633`；启动后按内容自然高度自适应调整窗口高度，420–1100 裁剪；左概览 + 右页签仪表盘布局，深色/浅色主题，简体中文 / English 双语）
- **多客户端接入**：OpenAI Chat Completions、Anthropic Messages、Gemini generateContent（legacy）、Gemini Interactions、OpenAI Responses 五种入站格式共用统一接入地址 `/router`，SDK 把 `base_url` 指到 `http://host:port/router` 即可，路由器按请求路径自动识别格式（`/v1/messages` → Anthropic、`/v1/responses` → Responses、`/v1beta/*:generateContent` → Gemini 旧、`/v1beta/interactions` → Gemini Interactions 新、其余 → OpenAI）。`/oai`、`/aai`、`/gai` 是 `/router` 的等价短前缀，同样可用。接入格式与上游 API 格式可自由跨型组合（全矩阵转换，见下方「格式转换」），不再做格式亲和拦截
- **多上游服务商**：`openai`、`gemini-openai`、`response`、`anthropic`、`gemini`（共 5 种 `ProviderType`）
- **请求 / 响应 / 流式（SSE）/ 工具调用** 全链路格式互转，`transrules` 规则引擎按 `(接入格式 → 上游格式)` 查表分发，同型直通保留原生字段
- **路由规则**：按模型名匹配（支持 `*` 通配），可设默认兜底服务商，支持 `model@provider` 定向
- **模型别名**：手动映射 + 自动 `模型@服务商` 定向别名（大小写不敏感）
- **模型列表**：启动时后台自动拉取上游模型列表并缓存供路由与 `/v1/models` 使用；GUI 支持连接检测 / 拉取 / 查看 / 复制模型 ID
- **鉴权**：可选路由令牌，支持 `Authorization: Bearer`、`x-api-key`、`x-goog-api-key`、`?key=` 多种携带方式
- **推理深度预设**：客户端请求未携带推理参数时，按接入格式与全局档位自动注入（设置页可配 无 / 低 / 中 / 高 / 最大，默认 `high`）：OpenAI → `reasoning_effort`、OpenAI Responses → `reasoning.effort`、Anthropic → `thinking.budget_tokens`（受 `max_tokens` 协议约束收敛；Claude 4.7+/5 系新模型出站自动折算为 `thinking.type=adaptive` + `output_config.effort`）、Gemini generateContent → `thinkingConfig.thinkingLevel`、Gemini Interactions → `generation_config.thinking_level`；请求已携带推理参数时不覆盖
- **桌面控制台**：概览页可直接启动 / 停止路由服务，端口 / host 等变更保存后自动重启；单例保护（端口被占用时提示并激活已有窗口）
- **系统托盘**：可选托盘常驻——关闭窗口时隐藏到托盘而非退出，路由服务保持运行；单击/双击恢复窗口，右键菜单 `打开面板 / 退出`
- **开机自启**：登录 Windows 后自动启动（写 `HKCU\...\CurrentVersion\Run` 的 `LLM Router` 值）；配合 `--hidden` 直接隐藏到托盘启动
- **管理接口安全**：管理 API 仅允许本机回环访问（`127.0.0.1`/`localhost`/`::1` 等，防 DNS rebinding），CORS 仅回显回环来源
- **用量可视**：请求日志 + 今日 Token 用量饼图 + `正在使用` 光波动画（基于在途请求计数，`ActiveModels(withinSeconds)`）；近 7 天按天/模型聚合统计（`ComputeStats` 仅统计 2xx 成功请求，排除 `/`、`/v1/models` 等非业务路径）；日志每小时清理 7 天外记录
- **Skills / MCP / 同步**：可执行文件同目录 `data/skills` 与 `data/mcp` 集中管理（`data/mcp` 下每个 MCP 服务一个独立 JSON 配置文件），一键注入/撤销到各目标客户端——skills 注入支持 Claude Code、OpenCode、Kimi Code、Kilo、Antigravity CLI，全部 13 个已接入客户端（Claude Code、OpenCode、Gemini CLI、pi、Grok Build、Codex、Cline、Hermes、Command Code、Kimi Code、Copilot CLI、Kilo、Antigravity CLI）均支持 MCP 注入，并可按「客户端 ↔ MCP 服务」映射批量注入；全部客户端均支持可视化编辑，配置项与源码直接读写客户端真实配置文件（双向同步；`agent/Json_modles/` 参考模板仅作真实文件缺失时的首次初始值），Codex 保存时还会把路由令牌自动写入其 `env_key` 指定的用户级环境变量

## 快速开始

```bash
# 1. 安装 Go 1.25+
#    https://go.dev/dl/

# 2. 下载依赖（仓库已附带 vendor，离线可 go build -mod=vendor）
go mod tidy

# 3. 编译桌面版（需要 Windows + WebView2，先安装 Wails CLI）
go install github.com/wailsapp/wails/v2/cmd/wails@latest
wails build --clean -o llm-router.exe

# 4. 开发模式（热重载，前端 dev server http://localhost:5173）
wails dev
```

> 修改代码后请自行重新编译，编译命令见上。不要依赖自动编译。

## 客户端接入方式

服务在本地监听一个端口（默认 `http://127.0.0.1:3000`）。五种客户端格式共用统一接入地址 `/router`，路由器根据请求路径自动识别报文格式；SDK 把 `base_url` 指到 `/router` 即可。

| 客户端格式 | SDK `base_url` | 完整请求路径示例 |
|---|---|---|
| OpenAI Chat | `http://host:port/router` | `POST /router/chat/completions` |
| Anthropic Messages | `http://host:port/router` | `POST /router/v1/messages` |
| Gemini generateContent（legacy） | `http://host:port/router` | `POST /router/v1beta/models/{model}:generateContent` |
| Gemini Interactions（2026 GA 标准） | `http://host:port/router` | `POST /router/v1beta/interactions` |
| OpenAI Responses | `http://host:port/router` | `POST /router/v1/responses` |

`/router` 前缀会被 `stripAccessPrefix` 剥掉还原成标准路径再按原格式处理；`/oai`、`/aai`、`/gai` 是等价短前缀（同样剥掉，格式仍按剥前缀后的标准路径判定）；不带前缀的标准路径（`/v1/chat/completions`、`/chat/completions`、`/v1/messages`、`/v1/responses`、`/responses`、`/v1beta/models/...`、`/v1beta/interactions`、`/v1alpha/interactions`）同样可用，Gemini 路径前缀 `v1beta` 与 `v1alpha` 均支持（含 `/v1beta/v1beta/`、`/v1alpha/v1alpha/` 重复段容错）。若客户端把完整端点当 `base_url` 填写，拼出的重复路径段（如 `/v1/chat/completions/chat/completions`）会被 `normalizeDoubledPath` 自动折叠还原。

### Cursor CLI（已移除）

> Cursor CLI 接入已不再支持，相关代码（`agent/cursor_config.go` / `agent/cursor_env.go`）与环境变量写入面板已移除。

> **格式转换全矩阵**：接入格式按标准路径判定（`/v1/messages` → Anthropic、`/v1/responses` → Responses、`/v1beta/*:generateContent` → Gemini legacy、`/v1beta/interactions` → Gemini Interactions、其余 → OpenAI）。同型直通保留原生字段（`anthropic→anthropic`、`gemini→gemini`、`gemini-interactions→gemini(interactions base)`、`responses→response` 原样转发）；跨型组合经 OpenAI 归一化形状转换——字段无法承载时丢弃并记录 `logDroppedRequestFields`。仅当接入格式到目标格式无对应转换规则时才返回 400 并提示「暂无转换规则」。

`GET /` 与 `GET /v1`（浏览器访问，`Accept: text/html`）返回管理面板落地页；模型列表 `GET /v1/models`、`/models`、`/v1beta/models`（含 `/v1beta/v1beta/models` 容错）按客户端格式返回聚合后的模型列表（OpenAI / Anthropic / Gemini 三种形状），浏览器直开会 `302` 跳转到面板对应 Tab。管理接口（`GET /api/status`、`GET /api/logs` 等）在浏览器直开时同样重定向到对应面板（`handleAdminHTML`）。

### 鉴权

配置 `authToken` 后，所有 LLM 代理请求需通过任一方式携带令牌：`Authorization: Bearer <token>`（scheme 大小写不敏感）、`x-api-key`、`x-goog-api-key` 头，或 `?key=` 查询参数（Gemini 风格）。管理接口（`/api/*`）接受 `Authorization: Bearer <token>`、`x-api-key` 或 `x-admin-token`。未配置令牌则无需鉴权；`OPTIONS` 预检始终放行。

### 管理接口安全

管理 API 仅允许本机回环访问（`127.0.0.1`、`localhost`、`::1` 等，`adminTransportAllowed` 会校验 `Origin` 与 `Host`，拦截 `Sec-Fetch-Site: cross-site`），防止第三方网页通过 DNS rebinding 或跨域请求读取配置/日志。CORS 仅回显回环来源的 `Origin`（`adminCORSHeaders`），非回环请求返回 `403`。

LLM 代理端点的 CORS 与管理接口同策略（`adminCORSHeaders`）：仅当请求 Origin 的主机为本机回环时回显该 `Origin`，外部网站 Origin 不下发 `Access-Control-Allow-Origin`——第三方网页既读不到代理响应，也无法通过预检跨域调用，防止白嫖本机上游 Key。

## 上游服务商类型

`providers[].type` 决定入站请求如何转换成上游格式以及调用哪个上游端点（`transrules` + `format` 协作）：

| type | 说明 | 上游端点 |
|---|---|---|
| `openai` | OpenAI 兼容 | `{base}/v1/chat/completions` |
| `gemini-openai` | Gemini 的 OpenAI 兼容层 | `{base}/v1beta/openai/chat/completions` |
| `response` | OpenAI Responses | `{base}/v1/responses` |
| `anthropic` | Anthropic Messages | `{base}/v1/messages` |
| `gemini` | Gemini generateContent | `{base}/v1beta/models/{model}:generateContent`（流式自动换成 `:streamGenerateContent`）。若 `baseUrl` 指向 `.../v1beta/interactions`，则改用 Gemini 官方推荐的新标准 Interactions API（端点即 base，模型在 body 的 `model` 字段，流式用 `stream:true` + `alt=sse`） |

流式请求默认注入 `stream_options.include_usage`（仅 `openai` 和 `gemini-openai` 类型），以便统计页面的 Token 用量不为 0；`openai→openai` 流式响应按字节直通（仅当客户端已自带 `stream_options.include_usage=true` 时，否则走重建分支补注入），其余组合逐事件转换并即时 flush（`flushWriter`）。Gemini 类型会自动剥离 OpenAI 特有字段（如 `prompt_cache_key`、`additionalProperties`），避免上游 protobuf 校验失败。非流式上游响应体上限 50MB，请求体上限 10MB；HTTP 上游超时 10 分钟。

## 路由规则

模型名按 `router/routing.go` 中 `ResolveProviderWithStrip` 顺序解析到某个启用服务商：

1. 模型名命中某启用服务商的模型列表（`GetProviderModels` 优先静态 `models`，否则取 `fetchedModels` 缓存，支持 `*` 通配 via `MatchModel`）。
2. 模型名命中某启用服务商的别名 `aliases`（大小写不敏感）。
3. `model@provider` 后缀定向：`@` 后的名字必须是某个服务商的 `id` 或 `name`（大小写不敏感），前缀再在该服务商内匹配模型/别名；若 hint 明确指向某服务商但未命中模型/别名，则直接返回 `nil`（不静默落入默认服务商，避免误导性 `model not found`）。
4. 命中 `defaultProvider`（按 `id` 或 `name` 匹配启用服务商）。
5. 仅有一个启用服务商时直接采用。

所有端点均要求携带 `model`。若解析走 `model@服务商` 分支，转发上游前会剥掉 `@` 后缀；按别名路由时则替换为别名映射的上游真实模型名。

`/v1/models` 列表由 `AggregateModels` 把每个服务商的模型与别名聚合去重；跨服务商重名的模型会在 id 后追加 `@服务商名`（`ClientModelID` / `ModelOccurrences`）以便客户端区分。通配符模式（含 `*`）不会出现在模型列表中。

模型列表拉取（`router.FetchModels`）会依次尝试多个候选地址：服务商类型的主端点、同一 base 下的 `/models` 和 `/v1/models`（OpenAI 风格兜底）；Anthropic 兼容层（baseURL 以 `/anthropic` 结尾）还会回退到去掉后缀的根地址。首个 2xx 成功即返回并经 `UpdateFetchedModels` 缓存。

## 格式转换

- `format/` 负责入站识别（`ParseInbound` 区分 openai/anthropic/gemini/gemini-interactions/responses）、出站装配（`BuildOutbound`/`BuildExactOutbound`）、非流式/流式互译、SSE 解析与错误翻译。
- `transrules/` 是可插拔规则引擎，登记 `(src → dst)` 请求规则、`(upstream → client)` 非流式逆向规则、按上游格式的 `Chunk` 解析与按客户端格式的 `Stream` 写出。`Engine` 接口由 `format` 实现并经 `SetEngine` 注入，`router` 仅依赖 `transrules` 查表调用。
- 直通优化：`openai→openai` 非流式/流式按字节直通（`passthroughOpenAIStream` 逐行透传并扫描 `usage`，缺 `id` 时补 `chatcmpl-*`）；`anthropic→anthropic`、`gemini→gemini`、`responses→response` 同型直通原样转发原生字段（`sameTypePassthrough` + `isBodyCompatibleWithFormat` 校验，跨型则经 `transrules` 白名单重建并记录 `logDroppedRequestFields`）。Anthropic / Gemini / Responses 同型直通仅信任官方上游域名（`allowSameTypePassthrough`），第三方兼容层必须走归一化转换剥离未实现字段；Gemini `baseUrl` 指向 Interactions API（含 `/interactions`）时禁用同型直通，统一经归一化装配 Interactions 请求体。
- 2026-09 最新参数对齐：Anthropic 请求侧支持 `output_config`（effort/format）、`inference_geo`、`service_tier`、`thinking.type=adaptive`（Claude 4.7+/5 系新模型按 `reasoning_effort` 档位自动折算 adaptive + `output_config.effort`，旧模型仍走 `enabled`+`budget_tokens`），响应侧映射 `pause_turn`/`model_context_window_exceeded` 等新 `stop_reason`、`stop_details`→`message.refusal`、`output_tokens_details.thinking_tokens`→`reasoning_tokens`、流式 `citations_delta` 往返保真；Chat⇄Responses 互通新增 `store` / `service_tier` / `metadata` / `safety_identifier` / `prompt_cache_key` / `parallel_tool_calls` / `max_tool_calls` / `verbosity` 透传；Gemini 原生出站支持 `seed`。

## 管理 API

`/api/*` 由桌面 GUI 与浏览器管理面板使用，均需管理员鉴权且仅允许本机回环访问（所有 `/api/*` 路径一律按管理接口处理，未知路径返回 404「未知管理接口」）：

- `GET /api/config` — 读取配置（API Key 与路由令牌 `authToken` 均脱敏为 `****` 哨兵；返回前复制切片避免污染内存）
- `POST /api/config` — 写配置（请求体 `{"config": {...}}`；写入前 `ValidateConfig` 校验端口范围、host、服务商必填项与 Base URL 格式，非法一律 400；掩码回写时 `preserveMaskedAPIKeys` 保留服务端现有密钥与令牌）
- `GET /api/status` — 运行状态（`StatusData`：监听端口 / host / 本地地址 / 服务商数量 / 运行态）
- `GET /api/logs`、`POST /api/logs/clear` — 请求日志（最近 200 条，最新在前，`GetLogs`）；清空立即落盘
- `GET /api/stats` — 近 7 天按天 / 模型聚合、今日按服务商聚合的用量统计（`ComputeStats`，只统计 2xx 成功请求，排除 `/`、`/v1/models` 等非业务路径，`perProvider` 含 `avg_ms`）
- `GET /api/models` — 聚合后的模型列表（含服务商归属，`AggregateModels`）

日志内存限 5000 条、保留 7 天（每小时 `periodicLogCleanup` 清理过期条目），延时落盘（5 秒防抖 `logSaveDebounceMs`）到配置文件同目录的 `logs.json`（`logsPath`）；请求体上限 10MB；HTTP 上游请求超时 10 分钟。端口 / host 变更保存后需重启服务才生效（保存响应带 `restartNeeded`，设置页会自动 `Restart`）。

模型缓存：`fetchedModels`（`sync.Map`）保存后台拉取的模型列表；保存配置后，BaseURL / type 变化或已删除的服务商缓存会被 `ClearFetchedModel` 清除，变化的启用服务商（含新启用且无静态模型的服务商）在后台 `refetchChangedModels` 异步重新拉取。

## 桌面 GUI

七块仪表盘布局（左区概览大块常驻显示，右区顶部页签切换：日志 / 服务商 / 技能 / MCP / 同步 / 设置，另有 Token 用量图表页签；`App.vue` 的 `panels` 与 `rightTabs`，状态/统计/在用模型分别 15s/2s/1s 轮询；启动后 `fitWindowHeight` 按内容自然高度调整窗口高度一次；`frontend/dist` 为构建产物）：

- **概览**（左区常驻，`Overview.vue`）：路由服务启动 / 停止与实时状态（状态光环 + 运行态雨滴光波动画）；统计条：今日请求 / 总输出 / 总输入 / 在线模型（来自今日统计）；接入地址卡片——三种格式（OpenAI 兼容 / Anthropic / Gemini）的 SDK `base_url` 与完整请求路径（一键复制）+「打开管理后台」入口。
- **日志**（右页签，`Logs.vue`）：最近请求列表（时间 / 模型 / 服务商 / 输入 / 输出 / 耗时 / 状态列）与 Token 明细；新条目带 2 秒闪烁（`row-flash`：成功绿、失败红），耗时分档配色（≥2s 黄、≥5s 红）；清空日志需确认（`ClearLogsDialog.vue`）。
- **服务商**（右页签，`Providers.vue` + `ProviderDialog.vue`）：增删改服务商；连接检测（`FetchModels`）、后台拉取模型列表、复制模型 ID、分组查看模型；未保存的服务商也能先测连接（`stableProviderID` 派生确定性 id 保证缓存复用）；删除确认（`DeleteDialog.vue`）、模型列表弹窗（`ModelsDialog.vue`）。
- **技能**（`Skills.vue`）：`data/skills` 列表、目录展示、刷新、新建/编辑/删除（`SKILL.md`），支持子目录 `SKILL.md` 与根目录散放 `.md` 两种存放方式。
- **MCP**（右页签，`Mcps.vue`）：`data/mcp` 下每个 MCP 服务一个独立 JSON 配置文件（`sse`/`stdio`/`http`，`command`/`args`/`url`），`mcps.json` 仅保存路由器接入地址 `baseUrl`（旧版内嵌 `servers[]` 读取时自动迁移拆分）；列表编辑后 `SaveMcps` 落盘、单文件原样编辑走 `SaveMcpFile`；删除服务需确认，改名保存会同步清理旧名配置文件；「映射注入」弹窗（`McpMappingDialog.vue`）按「客户端 ↔ MCP 服务」映射经 `SyncMcpMapping` 批量注入。
- **同步**（右页签，`Clients.vue`）：目标客户端列表（`ListTargets`）与路由器地址（`GetRouterBase`），按目标执行 `inject` / `undo`（`SyncTargets`）；点击卡片进入各客户端的可视化配置编辑——配置项与源码**直接读写客户端真实配置文件，双向同步、无模板中转**（通用主配置走 `GetTargetConfig`/`SaveTargetConfig`；`agent/Json_modles/` 参考模板仅当真实文件不存在时作为首次编辑初始值 `presetFile`，不再参与保存与同步；所有真实配置保存前自动备份：首次 `.bak`），另有各客户端专属编辑器（同样直读直写真实文件）：Claude Code（settings.json 表单——「模型配置」首卡三列同行：接入地址只读 + 令牌环境变量 `ANTHROPIC_AUTH_TOKEN` + 默认模型（顶层 `model`）；`env` 模型映射行（固定槽位 `ANTHROPIC_MODEL` / `ANTHROPIC_DEFAULT_SONNET_MODEL` / `ANTHROPIC_DEFAULT_OPUS_MODEL` / `ANTHROPIC_DEFAULT_HAIKU_MODEL` / `ANTHROPIC_SMALL_FAST_MODEL` + 每行上下文窗口，追加自动编号 `ANTHROPIC_MODEL_n`）；「权限配置」一行两组——「权限模式」写 `permissions.defaultMode`（询问 / 自动接受编辑 / 不询问 / 只读规划）+「执行命令」定点维护 `Bash` 工具 allow/ask/deny 规则（允许 / 询问 / 拒绝）；其后按官方字段五组表单编辑（「模板顶层字段」theme/editorMode/autoCompactEnabled/maxContextTokens/cleanupPeriodDays/alwaysThinkingEnabled/includeGitInstructions 7 项固定单行不换行；「交互与代理」agent/effortLevel/language；「上下文与记忆」autoCompactWindow/autoContinueAtUsageLimit；「钩子与 MCP 管控」钩子开关 + allowed/deniedMcpServers；「其它」通知/更新通道等）；另有 `~/.claude/CLAUDE.md` 全局指令源码直编）、pi（settings.json / mcp.json / AGENTS.md）、Grok Build（config.toml——[models]/[model."X"] 结构化块 + 权限/UI/CLI 等段表单 + AGENTS.md 源码直编）、Codex（config.toml——model / model_providers / permissions 三段结构化表单 + AGENTS.md，保存时经 `SetUserEnv` 把路由令牌写入 `env_key` 指定的用户环境变量）、Gemini CLI（settings.json 表单 + .env / GEMINI.md 源码）、OpenCode（AGENTS.md）、Kimi Code（config.toml——default_model / providers / models 三段结构化表单直读直写真实 `~/.kimi-code/config.toml`，其余配置段原样保留，AGENTS.md 源码直编并入同一编辑页）、Cline（models.json / providers.json / globalState.json / AGENTS.md——前三者各为可展开面板并随主配置合并保存，models/providers/globalstate 均支持表单与源码页签双向联动；providers.json 为空或不存在时，编辑连接凭据面板会自动创建默认服务商条目 `openai-compatible`）、Command Code（config.json / settings.json / providers.json——BYOK 模型端点、模型级推理深度映射与权限审批规则，指向本地服务商时保存自动写入启动免登录凭据（`COMMAND_CODE_API_KEY` 用户环境变量 + `~/.commandcode/auth.json`））、Hermes（config.yaml——顶级 `model:` 块结构化表单，其余内容原样保留）、Kilo（kilo.jsonc——provider.* 块结构化表单（@ai-sdk/openai-compatible、baseURL 指向路由器、apiKey、models 条目含推理与视觉 modalities）+ 顶层默认模型三键 model / small_model / subagent_model（「服务商键/模型」格式点选）+ permission 工具级 allow/ask/deny 胶囊点按循环 + mcp 只读展示 + AGENTS.md 源码直编；MCP 注入写入 kilo.jsonc 顶层 `mcp` 键，JSONC 容忍，local 用 command 数组 / remote 用 url）、Antigravity CLI（settings.json 表单，面板序：「模型映射」整合面板（连接 env 与映射同卡——上半行只读路由地址 / API Key / 写入·清除 env 按钮，下半部内置模型 ID 逐行映射目标模型）→ parameters → 权限配置（两个带组名的淡绿胶囊分组：工具权限五档单选框 不写/严格/请求审查/始终放行/沙箱内放行 写 `permissions.toolPermission` + 「执行所有命令」三档 默认/允许/拒绝——仅定点维护 `command(*)` 通配规则写入 allow/deny，其余 allow/deny 规则原样保留可在 JSON 源码页编辑）→ 安全区四开关（终端沙箱 / 非工作区访问 / G1 额度 / 遥测）→ 风格配置（主题/配色/编辑器等下拉，5 项固定单行）→ 子代理字段，另有 mcp_config.json 源码页签；下拉框枚举选项显示文本本地化，写盘枚举值不变；接入走官方契约：模型请求经环境变量 `GOOGLE_GEMINI_BASE_URL` 指向路由器、凭据经 `GEMINI_API_KEY`（保存主配置时 modelProvider=gemini 自动写入这两个用户级环境变量，另有「写入/清除环境变量」按钮即时生效，新开终端生效）；「模型映射」面板把 agy 发出的内置模型 ID（选择名 → 实际发送名不完全一致，实测如 `gemini-3.1-flash-lite-preview` / `gemini-3.7-flash`）定点映射到目标服务商 aliases：目标模型下拉按字母分组定位 + 可选 `@服务商` 后缀，随配置窗口「保存」一并落盘（`applyAgyAliases` 先解析并回写路由器服务商 aliases，失败则中止整次保存；不提供单独的保存映射按钮）、按后缀/目标模型精确回写对应服务商、剥去后缀存干净模型名；映射文本整体为空时清空全部已启用服务商的 aliases，编辑/保存会原样保留 aliases 中非 agy 内置 ID 的行（其它客户端别名、`gemini-*` 通配别名等，防止一次编辑误清整台路由器的别名）；建议给目标服务商配置 `gemini-*` 通配别名一劳永逸覆盖全部 agy 模型 ID（路由器 ResolveProvider / 别名改写均支持 `*` 通配）；MCP 注入写入全局 `~/.gemini/config/mcp_config.json` 顶层 mcpServers 键，stdio 用 command/args、远程用 serverUrl）、Copilot CLI（settings.json 通用 JSON 编辑 + 接入配置面板 + 审批权限页签——官方 BYOK：端点三个环境变量 `COPILOT_PROVIDER_BASE_URL` / `COPILOT_PROVIDER_TYPE` / `COPILOT_PROVIDER_API_KEY` 经 `SetUserEnv` / `DelUserEnv` 写入/清除用户环境变量；模型、推理深度与用户级权限写入 `~/.copilot/settings.json`（`GetCopilotSettings`/`SaveCopilotSettings` 一键批量读写：`model` 等价持久化 `COPILOT_MODEL`、`effortLevel` 推理深度、`allowedUrls`/`deniedUrls` URL 白黑名单、`permissions.disableBypassPermissionsMode` 提权限制、沙箱 `sandbox.enabled`/`sandbox.allowBypass`/`sandbox.userPolicy.network.allowLocalNetwork`；JSONC 宽容读取、严格 JSON 定点落盘、空值移除对应键），面板内选模型（下拉来自路由器已启用模型，默认首个）与 API Key（留空自动用路由令牌）；「审批权限」页签源码直编 `~/.copilot/permissions-config.json`（按项目位置的工具审批 `tool_approvals` + `allowed_directories`，严格 JSON 校验 + 备份），重开终端后 Copilot CLI 生效）。MCP 注入合并只改动目标配置的 `mcpServers`/模型相关键；若目标主配置存在但解析失败（如写到一半的活文件），会拒绝写入并返回错误，避免破坏用户现有配置。
- **设置**（右页签，`Settings.vue`）：监听端口 / IP、默认兜底服务商（自定义下拉，已禁用服务商不可选）、推理深度档位（无 / 低 / 中 / 高 / 最大五档按钮组）、路由令牌（明文/密码切换）；开机自启与托盘常驻开关（切换即时生效，不依赖保存按钮）；深色 / 浅色主题与中英文切换（`i18n.js`，桌面面板生效；浏览器管理面板固定深色）；「关于」分组显示程序名与当前版本（`GetAppInfo`，更新检查与下载入口已预留，暂未接入）。
- **Token 用量**（右页签 `nav.chart`，`TokenChart.vue`）：SVG 饼图扇形 + 悬浮明细行，按 `GetStats` 渲染今日各模型 Token 占比；在用模型扇形按 `GetActiveModels(withinSeconds)` 标记亮起（基于在途计数，`markModelActive` 请求开始 +1、结束 -1，`withinSeconds` 缓冲避免闪烁）。

托盘常驻（`tray_windows.go`，`Shell_NotifyIcon`）开启后，关闭窗口隐藏到托盘（`OnBeforeClose` 拦截，`forceQuit` 绕过），路由服务保持运行；托盘图标单击 / 双击恢复主窗口（`WindowHide`/`Show`），右键菜单 `打开面板 / 退出`。开机自启（`autostart_windows.go`，`HKCU\...\CurrentVersion\Run`）配合 `--hidden` 启动时直接隐藏到托盘。

单例保护：`main.go` 启动时若端口已被占用（`isPortInUseError`），弹窗提示并 `activateExistingWindow`（`FindWindowW` + `ShowWindow`/`SetForegroundWindow`）后退出。窗口启动为最小化（`WindowStartState: Minimised`），首次显示时水平垂直居中于工作区（避开任务栏，`centerOnceWhenVisible`），之后由看门狗（`startWorkAreaWatchdog`，`workarea_windows.go`）周期校正防止被任务栏遮挡（窗口最大化时跳过校正，避免破坏最大化状态）；窗口支持最大化。

Wails 绑定（`frontend/wailsjs/go/main/App.d.ts` 自动生成，勿手改）：
- 状态 / 配置 / 服务：`GetStatus`/`GetAppInfo`/`GetConfig`/`SaveConfig`/`StartServer`/`StopServer`/`RestartServer`
- 模型：`FetchModels`/`FetchModelsList`/`GetProviderModels`/`GetModelsByFormat`
- 日志 / 统计：`GetLogs`/`ClearLogs`（清空并立即落盘）/`GetStats`/`GetActiveModels`
- 桌面偏好：`GetAutoStartEnabled`/`SetAutoStartEnabled`/`GetTrayEnabled`/`SetTrayEnabled`/`OpenURL`（仅 `http(s)` 且拒绝 shell 元字符）
- Skills / MCP / 同步：`GetSkillsList`/`GetSkillsDir`/`OpenSkillsDir`/`GetAgentSkills`/`GetSkillContent`/`SaveSkill`/`DeleteSkill`/`GetMcps`/`SaveMcps`/`SaveMcpFile`（单文件编辑）/`ListTargets`/`SyncTargets`/`SyncMcpMapping`（按「客户端 ↔ MCP 服务」映射批量注入）/`GetRouterBase`
- 真实配置定位：`GetClientRealPaths`（各客户端真实配置路径清单，来源 `client-paths.json`）/`OpenFileLocation`（在文件管理器中打开所在目录并选中）
- Agent 配置编辑：`GetTargetConfig`/`SaveTargetConfig`（直接读写真实配置文件，模板仅作首次初始值）、Claude Code（`GetClaudeMarkdown`/`SaveClaudeMarkdown`——直编 `~/.claude/CLAUDE.md`）、Gemini CLI（`GetGeminiAgents`/`SaveGeminiAgents`、`GetGeminiDotEnv`/`SaveGeminiDotEnv`）、Grok Build（`GetGrokAgents`/`SaveGrokAgents`）、pi（`GetPiAgents`/`SavePiAgents`、`GetPiSettings`/`SavePiSettings`、`GetPiMcp`/`SavePiMcp`）、OpenCode（`GetOpencodeAgents`/`SaveOpencodeAgents`）、Kilo（`GetKiloAgents`/`SaveKiloAgents`——直编 `~/.config/kilo/AGENTS.md`）、Codex（`GetCodexAgents`/`SaveCodexAgents`）、Kimi Code（`GetKimiAgents`/`SaveKimiAgents`）、Cline（`GetClineModels`/`SaveClineModels`、`GetClineProviders`/`SaveClineProviders`、`GetClineAgents`/`SaveClineAgents`、`GetClineGlobalState`/`SaveClineGlobalState`）、Command Code（`GetCommandcodeSettings`/`SaveCommandcodeSettings`、`GetCommandcodeProviders`/`SaveCommandcodeProviders`、`GetCommandcodeNoLogin`/`SetCommandcodeNoLogin`）、Copilot CLI（`GetCopilotSettings`/`SaveCopilotSettings`——批量读写 `~/.copilot/settings.json` 的 `model` / `effortLevel` / `allowedUrls` / `deniedUrls` / `permissions.disableBypassPermissionsMode` / 沙箱 `sandbox.enabled` / `sandbox.allowBypass` / `sandbox.userPolicy.network.allowLocalNetwork`，空值或空数组=移除对应键，嵌套父对象变空自动清理；`Get/CopilotModel`、`SaveCopilotModel` 为单键兼容入口；`GetCopilotPermissions`/`SaveCopilotPermissions`——校验并读写 `~/.copilot/permissions-config.json` 按项目位置的工具审批 `tool_approvals` + `allowed_directories`）、Antigravity CLI（`GetAgyMcp`/`SaveAgyMcp`——直编 `~/.gemini/config/mcp_config.json`）
- 服务商别名：`GetProviderAliases`/`SaveProviderAliases`（按服务商 `id`/`name` 读取或整体回写其 `aliases` 表）
- 环境变量：`SetUserEnv`（写当前用户级环境变量并广播 `WM_SETTINGCHANGE`；非 Windows 返回不支持错误）、`GetUserEnv`（Windows 读当前用户级环境变量，其它平台仅读进程环境变量、可用于展示；返回 exists/value）、`DelUserEnv`（删除用户级环境变量，不存在时幂等成功；非 Windows 返回不支持错误）

## 配置文件

- 字段：`port`、`host`、`authToken`、`defaultProvider`、`reasoningDepth`（全局推理深度档位：`none` / `low` / `medium` / `high` / `max`，缺省或非法值按 `high` 处理）、`providers[]`（`id` / `name` / `type` / `baseUrl` / `apiKey` / `models` / `aliases` / `enabled`）、`tray`（托盘常驻，桌面端偏好，`omitempty` 可选）。开机自启状态不写入配置文件，存于注册表。
- 缺 `id` 的服务商按 `name|baseUrl|type` 的 SHA-256 哈希派生确定性 `p-` 前缀 id（`config/stableProviderID` 与 `main/stableProviderID` 一致），同一服务商反复保存 ID 不变。
- 落盘为原子写（临时文件 + rename，`saveConfig`）；配置文件损坏时自动备份为 `.bak` 后重建默认配置（`LoadConfig`）。
- 保存配置后，BaseURL / type 变化或已删除的服务商缓存会被清除，变化的启用服务商（含新启用且无静态模型的服务商）在后台异步重新拉取模型列表。
- `GET /api/config` 返回的 API Key 与 `authToken` 为脱敏值 `****`；POST 回写时若检测到该哨兵值，自动保留服务端现有密钥/令牌，防止掩码覆盖真实值（`apiKeyMask` + `preserveMaskedAPIKeys`）。
- 各客户端真实配置路径集中管理在 `agent/Json_modles/client-paths.json`（键为客户端 id，值为「用途 → 路径」，支持 `~` 主目录缩写）；文件缺失或损坏时回退到 `agent/client_paths.go` 内置默认。后端路径函数、目标注册、技能扫描与前端「打开所在文件夹」按钮均以此清单为准。

## 目录结构

```
llm-router-go/
├── main.go                    # 入口：Wails 桌面应用 + HTTP 服务（含单例、托盘、Skills/MCP 绑定）
├── tray_windows.go            # 系统托盘（Win32 Shell_NotifyIcon + 隐藏窗口）
├── autostart_windows.go       # 开机自启（注册表 Run 键）
├── go.mod / wails.json        # 项目配置（wails 前端构建：npm install / vite build / dev server 5173）
├── types/types.go             # 共享数据结构（ProviderType×5、Provider、Config、LogEntry、StatsData 等）
├── types/ir.go                # 跨格式互转的中立 IR 中间表示（OpenAI 形状 + _ext_* 扩展键命名空间，旧版 _client_*/_gemini_* 键自动兼容映射）
├── config/config.go           # 配置读写/校验/原子落盘（ConfigPath、Normalize、ValidateConfig）
├── agent/                     # 多目标 Agent 适配（独立于 config/format/router/core）
│   ├── agent.go               # DataDir/SkillsDir/McpDir/HomeDir（LLMROUTER_DATA 优先，懒创建）
│   ├── skills.go              # data/skills 扫描与 CRUD（SKILL.md / 散放 .md）
│   ├── mcp.go                 # data/mcp 读写（每个 MCP 服务一个独立 JSON 文件；mcps.json 仅存 baseUrl，旧版内嵌 servers 自动迁移）
│   ├── targets.go             # TargetAdapter 抽象与注册表（RegisterTarget/ListTargets）
│   ├── targets_known.go       # 已接入目标注册（Claude Code、OpenCode、Gemini CLI、pi、Grok Build、Codex、Cline、Hermes、Command Code、Kimi Code、Copilot CLI、Kilo、Antigravity CLI，共 13 家）
│   ├── targets_placeholder.go # 占位目标类型定义（占位注册已清空：Codex / Cline / Hermes 均已正式接入）
│   ├── targets_sync.go        # SyncTargets 调度（inject/undo/状态）+ SyncMcpMapping（客户端 ↔ 服务映射批量注入）
│   ├── target_config.go       # 目标配置读取/保存（GetTargetConfig/SaveTargetConfig，直接读写真实文件，模板仅作首次初始值）
│   ├── validate.go            # 非文本配置保存前格式校验（ValidateToml / YAML，校验失败拒绝覆写）
│   ├── json_patch.go          # JSON 字节级定点替换（upsertJSONObjectKey：只改目标键，键序/缩进/行尾原样保留）
│   ├── json_mcp.go            # Gemini CLI MCP 合并（settings.json 顶层 mcpServers 键，定点替换 + 首次备份）
│   ├── toml_mcp.go            # Grok Build / Codex MCP 合并（config.toml 的 [mcp_servers.*] 段文本级段落合并，保留注释）
│   ├── yaml_mcp.go            # Hermes MCP 合并（config.yaml 顶层 mcp_servers 块文本级段落合并，保留注释）
│   ├── gemini_agents.go / gemini_config.go / gemini_templates.go  # Gemini CLI（settings.json 真实读取 / .env / GEMINI.md，模板仅作首次初始值）
│   ├── grok_agents.go / grok_templates.go   # Grok Build（config.toml / AGENTS.md）
│   ├── codex_templates.go     # Codex（config.toml / AGENTS.md 真实路径，模板仅作首次初始值）
│   ├── cline_config.go        # Cline（cline_mcp_settings.json / models.json / providers.json / globalState.json / AGENTS.md）
│   ├── commandcode_config.go  # Command Code（config.json / settings.json / providers.json，BYOK 模型端点与免登录凭据）
│   ├── opencode_template.go / opencode_agents.go  # OpenCode（opencode.json / AGENTS.md）
│   ├── pi_templates.go / pi_agents.go / pi_settings.go / pi_mcp.go  # pi（models.json / settings.json / mcp.json / AGENTS.md）
│   ├── claude_templates.go    # Claude Code（settings.json 直读直写 + CLAUDE.md 源码直编；Json_modles/claude/ 仅作首次初始值）
│   ├── hermes_config.go       # Hermes（config.yaml 原文直存，YAML 校验）
│   ├── kimi_agents.go / kimi_mcp.go / kimi_templates.go  # Kimi Code（config.toml 直读直写 / mcp.json 定点合并 / AGENTS.md 直编）
│   ├── copilot.go             # Copilot CLI（BYOK 端点环境变量常量 + settings.json model 键 + permissions-config.json 读写）
│   ├── kilo.go                # Kilo（kilo.jsonc JSONC 容忍读写 + 顶层 mcp 键 MCP 注入 + Provider/默认模型/权限受管表单 + AGENTS.md 直编）
│   ├── env_windows.go / env_other.go # 用户级环境变量读写（GetUserEnv/DelUserEnv/SetUserEnv，HKCU\Environment + WM_SETTINGCHANGE）
│   ├── client_paths.go        # 客户端真实路径集中加载（client-paths.json 键级合并，~ 自动展开）
│   ├── agy_templates.go       # Antigravity CLI（settings.json / mcp_config.json / skills 路径）
│   ├── agy_mcp.go             # Antigravity CLI MCP 合并（~/.gemini/config/mcp_config.json 顶层 mcpServers 键）
│   ├── sync.go                # 备份/恢复与文件操作辅助（writeFileAtomic 临时文件+rename 原子落盘）
│   └── Json_modles/           # 各客户端参考模板目录（agy/claude/cline/codex/commandcode/gemini/grok/hermes/junie/kimi/kilo/opencode/pi；仅作真实文件缺失时的首次初始值）+ client-paths.json 真实路径清单
├── format/                    # 多格式互译（入站/出站/流式/非流式，Engine 实现）
│   ├── engine.go              # Engine 实现（BuildOutbound/BuildExactOutbound/ToClient/WriteChunks/ParseChunks）
│   ├── inbound.go             # 入站格式识别（openai/anthropic/gemini/gemini-interactions/responses，ParseInbound）
│   ├── anthropic_inbound.go   # Anthropic → OpenAI 归一化
│   ├── gemini_inbound.go      # Gemini generateContent → OpenAI 归一化
│   ├── inbound_responses.go   # OpenAI Responses → OpenAI 归一化
│   ├── gemini_interactions.go # Gemini Interactions API（新 GA 标准）入站归一化 + 出站装配 + 非流式/流式互译（input/steps 时间线）
│   ├── outbound.go            # BuildOutboundRequest + 端点解析
│   ├── outbound_chat.go       # OpenAI Chat → Responses
│   ├── outbound_anthropic.go  # OpenAI → Anthropic
│   ├── outbound_gemini.go     # OpenAI → Gemini generateContent
│   ├── nonstream.go           # 非流式响应翻译（云端）
│   ├── sse.go                 # SSE 流解析
│   ├── stream.go              # 流式转换（云端）
│   ├── utils.go               # 通用辅助（JSON 序列化、随机 ID、DataURL 解析等）
│   ├── errors.go              # 上游错误翻译（UpstreamErrorToOpenai）
│   └── *_test.go
├── transrules/                # 格式转换规则引擎（可插拔，不依赖具体实现）
│   ├── registry.go            # 注册表与 Engine 注入（SetEngine、Lookup*）
│   ├── formats.go             # 格式常量与辅助
│   ├── openai_rules.go        # 产出 openai 目标的规则
│   ├── anthropic_rules.go     # 产出 anthropic 目标的规则
│   ├── gemini_rules.go        # 产出 gemini 目标的规则
│   ├── response_rules.go      # 产出 response 目标的规则
│   └── exact.go               # 同型直通规则（exactOrNormalize）
├── router/
│   ├── routing.go             # 路由解析 + 模型列表聚合（ResolveProviderWithStrip、AggregateModels、fetchedModels）
│   ├── match.go               # 模型名通配匹配（MatchModel）
│   ├── models.go              # 拉取上游模型列表（FetchModels）
│   ├── handle.go              # LLM 主处理管线 + 模型列表响应 + 流式/非流式分发（含落地页与鉴权）
│   ├── reasoning.go           # 推理深度参数注入（无推理参数时按格式与 reasoningDepth 档位追加预设）
│   ├── passthrough.go         # 同型直通判定与流式 usage 扫描（sameTypePassthrough、isBodyCompatibleWithFormat）
│   ├── droplog.go             # 跨型转换丢弃字段记录（logDroppedRequestFields）
│   ├── engine.go              # 依赖注入装配点（transrules.SetEngine(format.NewEngine())）
│   ├── errors.go              # 错误响应与鉴权（CheckAuth、ErrorResponse）
│   └── *_test.go
├── core/
│   ├── server.go              # HTTP 服务 + 管理 API + 日志/统计/模型缓存维护（NewServer、Start/Stop/Restart）
│   ├── stats.go               # 统计聚合（ComputeStats，近7天/今日/按模型/按服务商）
│   └── *_test.go
└── frontend/
    ├── index.html             # GUI 骨架
    ├── vite.config.js         # Vite 构建配置
    ├── package.json           # 前端依赖（vue 3.5、vite 5、@vitejs/plugin-vue）
    ├── wailsjs/               # Wails 自动生成的 Go↔JS 绑定（勿手改）
    ├── public/
    │   ├── style.css          # 主题样式（深色/浅色）
    │   ├── fonts/             # Blex Mono Nerd Font
    │   └── logos/             # 各客户端 logo 图标（agy/claude/cline/codex/commandcode/copilot/gemini/grok/hermes/junie/kimi/kilo/opencode/pi）
    └── src/
        ├── main.js            # Vue 入口
        ├── App.vue            # 根组件（左概览 + 右页签七块布局 + 弹层，15s/2s/1s 轮询）
        ├── store.js           # 全局状态（refresh/refreshStats/refreshActive）
        ├── api.js             # Wails 绑定封装
        ├── i18n.js            # 中英双语字典
        ├── utils.js           # 通用工具
        ├── toast.js           # Toast 提示
        └── components/
            ├── Overview.vue   # 概览（状态/统计/接入地址/Token 用量/最近请求）
            ├── Logs.vue       # 日志
            ├── Providers.vue  # 服务商管理
            ├── Settings.vue   # 设置
            ├── Skills.vue     # 技能
            ├── Mcps.vue       # MCP
            ├── Clients.vue    # 客户端（同步/配置注入编辑）
            ├── sync/          # 各客户端可视化编辑控制器（controller.js 统一调度 + 每客户端一个 js：agy/claude/cline/codex/copilot/gemini/grok/hermes/kimi/kilo/opencode/pi/cc + shared.js/store.js/model-select.js/optmap.js（下拉枚举值→i18n 显示文本映射表，写盘枚举不变）+ PermList.vue 权限规则胶囊组件（单胶囊点按循环 未设→允许→询问→拒绝，× 删除））
            ├── TokenChart.vue # Token 图表
            ├── CodeArea.vue   # 带行号栏的源码编辑框（可复用）
            ├── ProviderDialog.vue
            ├── DeleteDialog.vue
            ├── ModelsDialog.vue
            ├── ClearLogsDialog.vue # 清空日志确认弹窗
            ├── McpMappingDialog.vue # MCP 映射注入弹窗（客户端 ↔ 服务映射批量注入）
            └── Toast.vue
```

## 与原 Bun/Electrobun 版的差异

| 项目 | Bun 版 | Go 版 |
|---|---|---|
| 运行时 | 需要 Bun | 单文件 exe，无依赖 |
| 桌面框架 | Electrobun | Wails v2.14 |
| 语言 | TypeScript | Go |
| 分发 | 需要安装 Bun | 双击 exe 即用 |
| 配置 | 共用 config.json | 共用 config.json（新增 `tray`、`data/skills`、`data/mcp`） |
| 编译 | Bun 打包 | `wails build`（静态编译，release 含 vendor） |
| 布局 | 窄面板 | 左概览 + 右页签七块仪表盘，启动最小化到任务栏 |
| 扩展 | — | Skills/MCP 多目标注入 |

## 环境变量

- `LLMROUTER_CONFIG`：指定配置文件路径；未设置时从当前目录向上逐级寻找含 `package.json`（`name == "llm-router"`）的目录，取其下 `config.json`，找不到则用 `./config.json`（`config.ConfigPath` 带缓存与锁）。
- `LLMROUTER_DATA`：指定 `data` 根目录；未设置时取可执行文件同目录的 `data/`，不存在则自动创建 `data/skills` 与 `data/mcp`（`agent.DataDir` 懒加载）。
- `LLMROUTER_DATA` 仅影响 skills/mcp 集中存储位置，不影响 `config.json` 位置。
- `LLMROUTER_JSON_MODLES_DIR`：指定 Agent 参考模板根目录；未设置时按 当前工作目录逐级向上查找 > 可执行文件同目录 的顺序定位 `agent/Json_modles/`（`agent.jsonModelsDir`）。
- `CLINE_MCP_SETTINGS`：覆盖 Cline 真实 MCP 配置文件路径（如 VSCode 扩展版的 globalStorage 路径）；未设置时用独立版桌面端默认路径 `~/.cline/data/settings/cline_mcp_settings.json`。

## 安全提示

- API Key 明文存储在 `config.json` 与 `data/mcp`，勿提交仓库；`GET /api/config` 返回时已脱敏；POST 回写掩码值时自动保留现有密钥
- 管理接口仅允许本机回环访问，防止第三方网页跨域读写
- 局域网开放时务必设置路由令牌 `authToken`
- `OpenURL` 仅允许 `http(s)` 且拒绝含 `&|<>\"'`$;^%()` 等 shell 元字符的地址
- Gemini API Key 通过 `?key=` 传递，上游报错时 URL 中的密钥会被 `redactURL` 脱敏为 `REDACTED`；模型列表拉取（`FetchModels`）返回的错误信息同样经脱敏，不会把带密钥的 URL 透传到前端或日志
- 鉴权令牌比较使用常数时间算法（`crypto/subtle.ConstantTimeCompare`），防计时侧信道
- 建议仅在可信网络中使用
