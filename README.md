# LLM Router

![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)
![Wails](https://img.shields.io/badge/Wails-v2-DF0000)
![Vue](https://img.shields.io/badge/Vue-3.5-42B883?logo=vue.js&logoColor=white)
![Platform](https://img.shields.io/badge/Windows-WebView2-0078D4?logo=windows&logoColor=white)

> **本地 LLM 网关 + 桌面管理台。** 把 OpenAI Chat、Anthropic Messages、Google Gemini（generateContent / Interactions）、OpenAI Responses 五种入站格式统一到 `/router`，按模型名路由到不同上游，并完成请求/响应/流式/工具调用的跨格式互转。

---

## 目录

- [亮点](#亮点)
- [快速开始](#快速开始)
- [版本与自动更新](#版本与自动更新)
- [客户端接入](#客户端接入)
- [上游服务商](#上游服务商)
- [路由与推理](#路由与推理)
- [管理 API](#管理-api)
- [桌面 GUI](#桌面-gui)
- [配置与环境变量](#配置与环境变量)
- [目录结构](#目录结构)
- [安全](#安全)

---

## 亮点

| 能力 | 说明 |
|---|---|
| 🔀 五种格式互转 | Chat / Messages / generateContent / Interactions / Responses 全矩阵转换，同型直通保留原生字段 |
| 🧠 推理深度注入 | `none/low/medium/high/max` 五档，按上游格式自动补齐推理参数 |
| 🧩 模型路由 | 模型列表、`*` 通配、别名、`model@provider` 定向、默认/唯一服务商兜底 |
| 🛠 Agent 管理 | Skills / MCP 集中管理，一键注入或撤销到 12 家客户端 |
| 🖥 原生桌面 | Wails v2 + Vue 3，纯黑暗色 / 浅色主题，中英双语，托盘常驻 |
| 🔄 自动更新 | 启动检查 GitHub Release，发现新版自动弹出「关于」，支持下载安装并重启 |

---

## 快速开始

**环境要求：** Windows + WebView2、Go 1.25+、Wails CLI

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
wails build --clean -o llm-router.exe
```

开发模式：

```bash
wails dev    # 前端 dev server: http://localhost:5173
```

> 修改代码后需自行重新编译，不会自动编译。

> ⚠️ `format/`、`transrules/` 是格式互译与规则引擎核心，已被 `.gitignore` 排除、不随仓库分发；`router/` 直接 import 二者，clone 后需本地补齐才能编译。

---

## 版本与自动更新

版本号通过构建参数注入（默认 `1.0.0`）：

```bash
wails build --clean -o llm-router.exe -ldflags "-X main.appVersion=1.2.3"
```

程序启动时自动查询 GitHub Release（[`sala-coding/LLM_Router`](https://github.com/sala-coding/LLM_Router)）最新版本：

- 发现新版本 → 自动弹出「路由设置 → 关于」并展开新版本信息栏
- **下载并安装**：显示下载进度，完成后自动替换当前 exe 并重启
- **打开页面**：跳转 Release 页面手动下载

自动安装支持 Release 资源为 `.exe` 或 `.zip`（自动解包）；程序目录不可写时会提示手动下载。

---

## 客户端接入

默认监听 `http://127.0.0.1:3000`，SDK 的 `base_url` 填 `http://host:port/router`，路由器按路径自动识别格式：

| 格式 | 请求路径 |
|---|---|
| OpenAI Chat | `/router/chat/completions` |
| Anthropic Messages | `/router/v1/messages` |
| Google Gemini generateContent | `/router/v1beta/models/{model}:generateContent` |
| Google Gemini Interactions | `/router/v1beta/interactions` |
| OpenAI Responses | `/router/v1/responses` |

`/oai`、`/aai`、`/gai` 是 `/router` 的等价前缀；不带前缀的标准路径同样可用。`GET /`、`GET /v1` 返回管理面板。

配置 `authToken` 后需携带 `Authorization: Bearer <token>`、`x-api-key`、`x-goog-api-key` 或 `?key=`。管理接口（`/api/*`）另接受 `x-admin-token`，仅允许本机回环访问。

---

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

---

## 路由与推理

模型名解析顺序：

```
启用服务商模型列表（支持 * 通配）
  → 别名
  → model@provider 定向
  → 默认服务商
  → 唯一服务商
```

`model@provider` 转发前剥掉后缀，别名替换为真实模型名；`/v1/models` 聚合各服务商模型与别名，重名追加 `@服务商`。启动时后台拉取上游模型并缓存，GUI 可检测连接、拉取、复制模型 ID。

推理深度预设 `none/low/medium/high/max`（默认 `high`）：客户端未带推理参数时按格式注入（OpenAI `reasoning_effort`、Anthropic `thinking.budget_tokens`、Gemini `thinkingConfig` 等），已携带则不覆盖。

---

## 管理 API

`/api/*` 需管理员鉴权且仅限本机回环：

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` / `POST` | `/api/config` | 读取 / 保存配置（密钥与令牌脱敏回写，掩码值保留原密钥） |
| `GET` | `/api/status` | 服务状态 |
| `GET` / `POST` | `/api/logs`、`/api/logs/clear` | 日志查询 / 清空 |
| `GET` | `/api/stats` | 近 7 天用量 |
| `GET` | `/api/models` | 聚合模型列表 |

日志内存限 5000 条、保留 7 天，延时落盘到 `logs.json`；请求体上限 10MB，上游超时 10 分钟。

---

## 桌面 GUI

Wails v2 + Vue 3.5 / Vite 5 + WebView2。左「概览」+ 右页签，支持纯黑暗色 / 浅色主题与中英双语。

| 页面 | 功能 |
|---|---|
| **概览** | 启停服务、运行状态、接入地址一键复制、今日请求 / Token 统计 |
| **日志 / 服务商** | 日志检索、服务商增删改、连接检测、模型列表、别名 |
| **智能体 / 技能 / MCP** | 管理 `%APPDATA%\LLMRouter\data\skills` 与 `data\mcp`，一键注入 / 撤销到 12 家客户端，支持映射批量注入 |
| **同步** | 直读直写各客户端真实配置文件（首次以 `agent/Json_modles/` 模板为初始值），改前自动备份 |
| **对话终端** | ConPTY 拉起客户端 CLI，前端自研迷你终端渲染 |
| **会话管理** | 浏览 / 批量删除各客户端历史会话，终端恢复 |
| **设置** | 端口 / host、兜底服务商、推理档位、路由令牌、开机自启、托盘常驻、检查更新 |

**已适配客户端（12 家）：** Claude Code、OpenCode、pi、Grok Build、Codex、Command Code、Kimi Code、Copilot CLI、MimoCode、Antigravity CLI、CodeBuddy CLI、Deep Code CLI。

**桌面能力：** 托盘常驻（关闭窗口隐藏、服务保持运行）、单例保护（端口占用时激活已有窗口）。

---

## 配置与环境变量

`config.json` 关键字段：

| 字段 | 说明 |
|---|---|
| `port` / `host` | 监听地址 |
| `authToken` | 客户端访问令牌 |
| `defaultProvider` | 默认服务商 |
| `reasoningDepth` | 推理深度预设 |
| `providers[]` | `id/name/type/baseUrl/apiKey/models/aliases/enabled` |

原子写，损坏时备份为 `.bak` 重建；缺 `id` 时按 `name|baseUrl|type` 派生稳定 ID。

**环境变量：**

| 变量 | 作用 |
|---|---|
| `LLMROUTER_CONFIG` | 配置文件路径 |
| `LLMROUTER_DATA` | skills / mcp 数据目录（默认 `%APPDATA%\LLMRouter\data`） |
| `LLMROUTER_JSON_MODLES_DIR` | Agent 参考模板目录 |

---

## 目录结构

```text
main.go / *_windows.go / *_other.go   # 入口、托盘、自启、终端、工作区
update.go                             # 版本检查、下载安装与自动重启
config/                               # 配置读写与校验
router/                               # 路由解析、模型列表、请求管线、鉴权、直通
core/                                 # HTTP 服务、管理 API、统计
format/                               # 格式互译（.gitignore 排除，构建必需）
transrules/                           # 转换规则引擎（.gitignore 排除，构建必需）
types/                                # 共享数据结构与中间表示
agent/
  core/                               # 路径、skills/mcp CRUD、适配器注册、同步、会话
  clients/                            # 12 家客户端适配器
  Json_modles/                        # 客户端配置参考模板
frontend/                             # Vue 3 前端源码（dist/ 为构建产物）
vendor/                               # Go 依赖（离线可构建）
```

---

## 安全

- 🔐 API Key 明文存于 `config.json` 与 `data/mcp`，勿提交仓库；`GET /api/config` 已脱敏
- 🛡 管理接口仅本机回环，防 DNS rebinding；局域网开放务必设置 `authToken`
- ⏱ 令牌比较使用常数时间算法；`redactURL` 脱敏上游错误中的密钥
- 🌐 `OpenURL` 仅允许 http(s)，拒绝 shell 元字符
- 📦 自动更新仅从 GitHub 官方域名下载，并校验下载文件为有效 Windows 程序

