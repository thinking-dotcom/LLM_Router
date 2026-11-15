package router

import (
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"

	"llm-router-go/format"
	"llm-router-go/types"
)

// ---------------------------------------------------------------------------
// 推理深度参数（reasoning depth）业务规则
//
// 业务流程：
//   1. 校验客户端 Payload 中是否包含「推理深度参数」；
//   2. 若不包含 → 按客户端数据格式从路由预设的三种推理参数方案中追加相应参数，
//      并强制进入【数据格式转换】节点，由转换节点把追加的参数映射成上游格式；
//   3. 若已包含 → 维持既有判定：接入格式与上游 API 格式一致时同型直通透传，
//      不一致时进入【数据格式转换】节点。
//
// 档位（config.json 的 reasoningDepth，设置页全局配置）：
//   none / low / medium / high / max（默认 high）。各格式映射：
//   - OpenAI 格式：    reasoning_effort 档位（low/medium/high；max 封顶为 high，
//                      OpenAI 枚举没有更高档）
//   - Anthropic 格式： thinking.budget_tokens 思考预算（low=2048 / medium=8192 /
//                      high=24576 / max=32768，与 format.anthropicThinkingBudget
//                      保持一致；受 max_tokens > budget_tokens 协议约束收敛）
//   - Gemini 格式：    generationConfig.thinkingConfig.thinkingLevel 档位
//                      （low=LOW / medium=MEDIUM / high、max=HIGH）
//
// 注入采用「双写」：
//   - 原生形状写入 bodyMap（保持客户端格式语义，供原生路径与日志追溯）；
//   - 统一档位写入归一化 openaiReq 的 reasoning_effort（跨型转换节点只读该
//     归一化形状，format 包各出站实现会把它映射为上游格式的推理参数：
//     anthropic→thinking、gemini→thinkingConfig、openai 系→原样保留）。
//     归一化取值用 OpenAI 安全枚举（max 折算为 high），避免严格校验的上游
//     拒绝未知档位枚举。
// ---------------------------------------------------------------------------

// 五档推理深度（与 config 包 reasoningDepths 一致）。
const (
	ReasoningNone   = "none"
	ReasoningLow    = "low"
	ReasoningMedium = "medium"
	ReasoningHigh   = "high"
	ReasoningMax    = "max"
)

// defaultReasoningTier 配置缺省/非法时使用的默认档位。
const defaultReasoningTier = ReasoningHigh

// normalizeTier 把任意输入折算为合法档位（空/非法 → 默认 high）。
func normalizeTier(s string) string {
	switch s {
	case ReasoningNone, ReasoningLow, ReasoningMedium, ReasoningHigh, ReasoningMax:
		return s
	}
	return defaultReasoningTier
}

// 以下「档位 → 某格式取值」助手委托映射表（format/reasoning_table.go），路由器
// 不再自带取值表：新增/调整档位或家族只改那张表（本文件保留函数名是为了让注入
// 逻辑读起来仍是「路由器自己的档位策略」）。

// openaiEffortOf 档位 → OpenAI reasoning_effort（max/xhigh 封顶 high）。
func openaiEffortOf(tier string) string { return format.OpenAIEffortOf(tier) }

// anthropicBudgetOf 档位 → Anthropic 思考预算（token）。
func anthropicBudgetOf(tier string) int { return format.AnthropicBudgetOf(tier) }

// geminiLevelOf 档位 → Gemini thinkingLevel 枚举。
func geminiLevelOf(tier string) string { return format.GeminiLevelOf(tier) }

// HasReasoningDepth 判断请求是否携带了「推理深度参数」。同时检查两层载体：
//   - 归一化 OpenAI 形状 norm（跨型管线的统一载体，ParseInbound 已把各格式的
//     推理参数折算进这里的统一标记键）；
//   - 客户端原生 body 形状（兜底，防止归一化规则变化导致漏判）。
//
// 任一载体命中即视为「已包含」，路由器不再追加预设参数。
func HasReasoningDepth(inFormat string, body, norm map[string]any) bool {
	// 1. 归一化标记（覆盖所有接入格式的显式推理配置）
	if norm != nil {
		if s, _ := norm["reasoning_effort"].(string); s != "" {
			return true
		}
		if _, ok := types.IRGet(norm, types.ExtAnthropicThinking); ok { // Anthropic extended thinking（IR 扩展键）
			return true
		}
		if _, ok := norm["thinking"]; ok { // OpenAI 形状直接携带 thinking 的客户端（网关直连场景）
			return true
		}
		if b, ok := types.IRGet(norm, types.ExtClientAnthropicBudget); ok && numAny(b) > 0 {
			return true
		}
		if _, ok := types.IRGet(norm, types.ExtGeminiThinkingConfig); ok {
			return true
		}
		if s, ok := types.IRGet(norm, types.ExtClientReasoningEffort); ok {
			if ss, _ := s.(string); ss != "" {
				return true
			}
		}
		if rs, ok := norm["reasoning"].(map[string]any); ok {
			if s, _ := rs["effort"].(string); s != "" { // Responses API 风格
				return true
			}
		}
	}
	// 2. 原生 body 形状兜底：字段路径与映射表同源（含 openai 形状的顶层
	//    thinking:{type:enabled|disabled}、interactions 的 thinking_budget 数值写法）。
	//    只看「是否携带」，不看取值——thinking.type=disabled 这类显式关闭同样算已
	//    携带：客户端已表态，路由器不再用全局档位覆盖它的意图。
	if format.HasReasoningField(inFormat, body) {
		return true
	}
	return false
}

// ClientReasoningLabel 提取客户端显式携带的推理参数展示值（供日志「推理」列）。
//
// 原生形状的字段路径与取值全部来自映射表（format/reasoning_table.go 的读取方向，
// 与出站「档位 → 原生字段」用的是同一张表）：
//   - anthropic：output_config.effort > thinking.budget_tokens > thinking.type
//   - gemini：generationConfig.thinkingConfig（thinkingLevel > thinkingBudget；
//     budget 为 -1 是官方「动态思考」标记，列里显示「动态」）
//   - gemini-interactions：generation_config（thinking_level / thinking_budget）
//   - responses：reasoning.effort
//   - openai：reasoning_effort > reasoning.effort
//
// 原生形状取不到时回退归一化形状（ParseInbound 已把各家族参数折算进 IR 扩展键
// 与统一档位 reasoning_effort）。客户端完全未携带时返回空串（日志列显示 —）。
func ClientReasoningLabel(inFormat string, body, norm map[string]any) string {
	if _, label, ok := format.DecodeReasoningNative(inFormat, body); ok && label != "" {
		return label
	}
	if norm == nil {
		return ""
	}
	// 归一化回退：显式预算 / 显式档位 / Gemini thinkingConfig 原始值 → 展示值
	if b, ok := types.IRGet(norm, types.ExtClientAnthropicBudget); ok {
		if s := numLabelOf(b); s != "" {
			return s
		}
	}
	if s, ok := types.IRGet(norm, types.ExtClientReasoningEffort); ok {
		if ss, _ := s.(string); ss != "" {
			return ss
		}
	}
	if tc, ok := types.IRGet(norm, types.ExtGeminiThinkingConfig); ok {
		if m, ok := tc.(map[string]any); ok {
			if s := format.GeminiThinkingLabel(m); s != "" {
				return s
			}
		}
	}
	if s, _ := norm["reasoning_effort"].(string); s != "" {
		return s
	}
	return ""
}

// numLabelOf 把数值型参数转成展示值（如思考预算 8192、0）；非数值类型返回空串
// （字符串形式的档位由调用方单独处理，不能用 numAny 兜成 "0"）。
// （字符串形式的档位由调用方单独处理，不能用 numAny 兜成 "0"）。
func numLabelOf(v any) string {
	switch n := v.(type) {
	case float64:
		return strconv.Itoa(int(n))
	case int:
		return strconv.Itoa(n)
	case int64:
		return strconv.Itoa(int(n))
	}
	return ""
}

// InjectDefaultReasoning 按客户端数据格式与配置档位把路由预设的推理参数方案
// 追加进请求。tier 为五档之一（空/非法按默认 high 处理，none 表示不注入、
// 直接返回 false）。返回是否成功追加。
//
// 仅应在 HasReasoningDepth 为 false 时调用；调用后请求必须走归一化格式转换
// 节点（由调用方禁用同型直通保证），否则追加的参数不会被映射到上游格式。
func InjectDefaultReasoning(inFormat, tier string, body, norm map[string]any) bool {
	if body == nil || norm == nil {
		return false
	}
	tier = normalizeTier(tier)
	if tier == ReasoningNone {
		return false // 「无」档位：不注入任何预设参数
	}
	if !format.SupportsReasoningInjection(inFormat) {
		return false
	}
	// 原生形状按映射表写入（format/reasoning_table.go）：字段名、取值、模型家族
	// 择形（gemini 2.5 系写 thinkingBudget / 3 系写 thinkingLevel / Gemma 不写、
	// Anthropic 新老模型 adaptive 与 enabled）都不再在本文件硬编码。
	// 模型名取归一化请求里的客户端原始模型名：注入发生在别名改写之前
	// （handle.go 的模型解析段在其后），家族择形以客户端模型为准；跨型路径真正
	// 上送的原生参数由出站层按改写后的模型名重新查表编码，与这里的原生写入无关
	//（这里的原生写入只服务同型直通与日志追溯）。
	model, _ := norm["model"].(string)
	format.ApplyReasoningNative(body, inFormat, model, tier)
	if inFormat == "anthropic" {
		// Anthropic 协议要求 max_tokens > budget_tokens：把注入的预算收敛到协议
		// 允许的范围。
		clampInjectedAnthropicBudget(body)
	}
	// 双写：统一档位落入归一化形状，供数据格式转换节点映射为上游格式参数。
	// 取 OpenAI 安全枚举（max 折算为 high），避免严格校验的上游拒绝未知枚举；
	// anthropic/gemini 的出站映射对 high 已是各自格式的高档位。
	norm["reasoning_effort"] = openaiEffortOf(tier)
	// 记下客户端实际的顶层字段名，便于事后核对「为何判为未携带」（只记字段名，不记值）。
	log.Printf("[reasoning] payload 未携带推理参数（顶层字段: %s），已按映射表追加 %s 格式预设方案 (tier=%s)",
		topLevelKeys(body), inFormat, tier)
	return true
}

// clampAnthropicBudget 计算可安全注入的 Anthropic 思考预算：
// 目标预算 target 受 max_tokens 约束（协议要求 max_tokens > budget_tokens）。
// 未声明 max_tokens 时直接用目标值；剩余空间不足 1024 时返回 0 表示放弃。
func clampAnthropicBudget(body map[string]any, target int) int {
	maxTokens := 0
	for _, k := range []string{"max_tokens", "max_completion_tokens"} {
		if n := numAny(body[k]); int(n) > maxTokens {
			maxTokens = int(n)
		}
	}
	if maxTokens <= 0 {
		return target
	}
	headroom := maxTokens - 1024 // 预留 1024 token 给最终回答
	if headroom < 1024 {
		return 0
	}
	if headroom < target {
		return headroom
	}
	return target
}

// clampInjectedAnthropicBudget 对映射表注入的 Anthropic 思考预算做协议约束收敛
// （协议要求 max_tokens > budget_tokens）。预算连 1024 都放不下时移除 body 里的
// thinking——归一化档位仍保留，出站层会把 max_tokens 抬到 budget+1024，因此不会
// 像更早的实现那样静默丢掉全局档位。
func clampInjectedAnthropicBudget(body map[string]any) {
	th, _ := body["thinking"].(map[string]any)
	if th == nil {
		return
	}
	target := int(numAny(th["budget_tokens"]))
	if target <= 0 {
		return
	}
	safe := clampAnthropicBudget(body, target)
	if safe <= 0 {
		delete(body, "thinking")
		return
	}
	th["budget_tokens"] = safe
}

// numAny 把 any 数值（json 反序列化只会得到 float64，内部代码可能写 int）
// 安全转为 float64，非数值返回 0。
func numAny(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0
}

// topLevelKeys 返回请求体顶层字段名（排序、最多 12 个，超出以 … 结尾；IR 扩展键
// 不计入）；数组型字段附元素个数（如 messages(1)、tools(14)），便于区分「主对话」
// 与「客户端辅助子调用」。只记字段名与个数，不记值。
func topLevelKeys(body map[string]any) string {
	names := make([]string, 0, len(body))
	counts := make(map[string]int, len(body))
	for k, v := range body {
		if types.IRIsExtKey(k) {
			continue
		}
		names = append(names, k)
		if arr, ok := v.([]any); ok {
			counts[k] = len(arr)
		}
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, k := range names {
		if n, ok := counts[k]; ok {
			parts = append(parts, fmt.Sprintf("%s(%d)", k, n))
			continue
		}
		parts = append(parts, k)
	}
	if len(parts) > 12 {
		return strings.Join(parts[:12], ",") + ",…"
	}
	return strings.Join(parts, ",")
}
