# LLM Router · Go + Wails 桌面版

本地 LLM 网关 + GUI：将 OpenAI Chat、Anthropic Messages、Gemini generateContent、Gemini Interactions、OpenAI Responses 五种入站格式统一到 `/router`，按模型名路由到不同上游并完成请求/响应/流式/工具调用的格式互转；另含多目标 Agent 的 Skills/MCP 集中管理与配置注入。

## 快速开始

Windows + WebView2，Go 1.25+，Wails CLI：

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
wails build --clean -o llm-router.exe
```

开发模式 `wails dev`（前端 dev server `http://localhost:5173`）。修改代码后需自行重新编译，不会自动编译。

> `format/`、`transrules/` 是格式互译与规则引擎核心，已由 `.gitignore` 排除、不随仓库分发；`router/` 直接 import 二者，clone 后需本地补齐才能编译。

## 客户端接入

服务默认监听 `http://127.0.0.1:3000`，SDK 的 `base_url` 填 `http://host:port/router`，路由器按路径自动识别格式：

| 格式 | 请求路径 |
|---|---|
| OpenAI Chat | `/router/chat/completions` |
| Anthropic Messages | `/router/v1/messages` |
| Gemini generateContent | `/router/v1beta/models/{model}:generateContent` |
| Gemini Interactions | `/router/v1beta/interactions` |
| OpenAI Responses | `/router/v1/responses` |

`/oai`、`/aai`、`/gai` 是 `/router` 的等价前缀；不带前缀的标准路径同样可用。`GET /`、`GET /v1` 返回管理面板。

配置 `authToken` 后需携带 `Authorization: Bearer <token>`、`x-api-key`、`x-goog-api-key` 或 `?key=`。管理接口（`/api/*`）另接受 `x-admin-token`，仅允许本机回环访问。

## 上游服务商

`providers[].type` 共 5 种：

| type | 上游端点 |
|---|---|
| `openai` | `{base}/v1/chat/completions` |
| `gemini-openai` | `{base}/v1beta/openai/chat/completions` |
| `response` | `{base}/v1/responses` |
| `anthropic` | `{base}/v1/messages` |
| `gemini` | `{base}/v1beta/models/{model}:generateContent`（流式 `:streamGenerateContent`；base 指向 `/v1beta/interactions` 时走 Interactions API） |

全矩阵跨格式转换，同型直通保留原生字段；流式默认注入 usage 统计。

## 路由与推理

模型名解析顺序：命中启用服务商的模型列表（支持 `*` 通配）→ 别名 → `model@provider` 定向 → 默认服务商 → 唯一服务商。`model@provider` 转发前剥掉后缀，别名替换为真实模型名；`/v1/models` 聚合各服务商模型与别名，重名追加 `@服务商`。启动时后台拉取上游模型并缓存，GUI 可检测连接、拉取、复制模型 ID。

推理深度预设 `none/low/medium/high/max`（默认 `high`）：客户端未带推理参数时按格式注入（OpenAI `reasoning_effort`、Anthropic `thinking.budget_tokens`、Gemini `thinkingConfig` 等），已携带则不覆盖。

## 管理 API

`/api/*` 需管理员鉴权且仅限本机回环：

- `GET /api/config`、`POST /api/config`（密钥与令牌脱敏回写，掩码值保留原密钥）
- `GET /api/status`、`GET /api/logs`、`POST /api/logs/clear`
- `GET /api/stats`（近 7 天用量）、`GET /api/models`

日志内存限 5000 条、保留 7 天，延时落盘到 `logs.json`；请求体上限 10MB，上游超时 10 分钟。

## 桌面 GUI

Wails v2 + Vue 3.5 / Vite 5，WebView2，深色/浅色双主题、中英双语。左概览 + 右页签：

- **概览**：启停服务、接入地址一键复制、今日请求/Token 统计
- **日志 / 服务商**：日志检索、服务商增删改、连接检测、模型列表、别名
- **技能 / MCP**：管理 `%APPDATA%\LLMRouter\data\skills` 与 `data\mcp`，一键注入/撤销到 12 个 Agent 客户端，支持映射批量注入
- **同步**：直读直写各客户端真实配置文件（首次编辑以 `agent/Json_modles/` 模板为初始值），改前自动备份
- **对话终端**：ConPTY 拉起客户端 CLI，前端自研迷你终端渲染
- **会话管理**：浏览/批量删除各客户端历史会话，终端恢复
- **设置**：端口/host、兜底服务商、推理档位、路由令牌、开机自启、托盘常驻

客户端适配器 12 家：Claude Code、OpenCode、pi、Grok Build、Codex、Command Code、Kimi Code、Copilot CLI、MimoCode、Antigravity CLI、CodeBuddy CLI、Deep Code CLI。托盘常驻：关闭窗口隐藏到托盘、服务保持运行；单例保护：端口占用时激活已有窗口。

## 配置与环境变量

`config.json`：`port`、`host`、`authToken`、`defaultProvider`、`reasoningDepth`、`providers[]`（`id/name/type/baseUrl/apiKey/models/aliases/enabled`）。原子写，损坏时备份为 `.bak` 重建；缺 `id` 时按 `name|baseUrl|type` 派生稳定 ID。

- `LLMROUTER_CONFIG`：配置文件路径
- `LLMROUTER_DATA`：skills/mcp 数据目录（默认 `%APPDATA%\LLMRouter\data`）
- `LLMROUTER_JSON_MODLES_DIR`：Agent 参考模板目录

## 目录结构

```
main.go / *_windows.go / *_other.go   # 入口、托盘、自启、终端、工作区
config/       # 配置读写与校验
router/       # 路由解析、模型列表、请求管线、鉴权、直通
core/         # HTTP 服务、管理 API、统计
format/       # 格式互译（.gitignore 排除，构建必需）
transrules/   # 转换规则引擎（.gitignore 排除，构建必需）
types/        # 共享数据结构与中间表示
agent/
  core/       # 路径、skills/mcp CRUD、适配器注册、同步、会话
  clients/    # 12 家客户端适配器
  Json_modles/# 客户端配置参考模板
frontend/     # Vue 3 前端源码（dist/ 为构建产物）
vendor/       # Go 依赖（离线可构建）
```

## 安全

- API Key 明文存于 `config.json` 与 `data/mcp`，勿提交仓库；`GET /api/config` 已脱敏
- 管理接口仅本机回环，防 DNS rebinding；局域网开放务必设 `authToken`
- 令牌比较用常数时间算法；`redactURL` 脱敏上游错误中的密钥
- `OpenURL` 仅允许 http(s)，拒绝 shell 元字符
