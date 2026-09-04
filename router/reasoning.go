package router

import (
	"log"
	"strings"

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

// openaiEffortOf 把档位折算为 OpenAI reasoning_effort 枚举（max 封顶为 high，
// none 不应走到这里——注入前已短路返回）。
func openaiEffortOf(tier string) string {
	if tier == ReasoningMax {
		return ReasoningHigh
	}
	return tier
}

// anthropicBudgetOf 把档位映射为 Anthropic 思考预算（token）。
// 与 format.anthropicThinkingBudget 保持一致。
func anthropicBudgetOf(tier string) int {
	switch tier {
	case ReasoningLow:
		return 2048
	case ReasoningMedium:
		return 8192
	case ReasoningHigh:
		return 24576
	case ReasoningMax:
		return 32768
	}
	return 0
}

// geminiLevelOf 把档位折算为 Gemini thinkingLevel 枚举。
func geminiLevelOf(tier string) string {
	switch tier {
	case ReasoningLow:
		return "LOW"
	case ReasoningMedium:
		return "MEDIUM"
	default: // high / max：Gemini 上限为 HIGH
		return "HIGH"
	}
}

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
	// 2. 原生 body 形状兜底
	switch inFormat {
	case "anthropic":
		if _, ok := body["thinking"]; ok {
			return true
		}
	case "gemini":
		if gc, ok := body["generationConfig"].(map[string]any); ok {
			if tc, ok := gc["thinkingConfig"].(map[string]any); ok && len(tc) > 0 {
				return true
			}
		}
	case "gemini-interactions":
		if gc, ok := body["generation_config"].(map[string]any); ok {
			if lv, ok := gc["thinking_level"].(string); ok && lv != "" {
				return true
			}
		}
	default: // openai
		if s, _ := body["reasoning_effort"].(string); s != "" {
			return true
		}
		if rs, ok := body["reasoning"].(map[string]any); ok {
			if s, _ := rs["effort"].(string); s != "" {
				return true
			}
		}
	}
	return false
}

// InjectDefaultReasoning 按客户端数据格式与配置档位把路由预设的推理参数方案
// 追加进请求。tier 为五档之一（空/非法按默认 high 处理，none 表示不注入、
// 直接返回 false）。返回是否成功追加（Anthropic 预算受 max_tokens 约束放不下
// 时不追加并返回 false）。
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
	switch inFormat {
	case "openai":
		// 方案一：OpenAI 兼容档位参数（max 封顶为 high）
		body["reasoning_effort"] = openaiEffortOf(tier)

	case "anthropic":
		// 方案二：Anthropic extended thinking 思考预算。
		// 协议要求 max_tokens > budget_tokens；放不下有效预算则放弃注入。
		budget := clampAnthropicBudget(body, anthropicBudgetOf(tier))
		if budget <= 0 {
			return false
		}
		body["thinking"] = map[string]any{"type": "enabled", "budget_tokens": budget}

	case "gemini":
		// 方案三：Gemini thinkingConfig 思考档位（2.5 系按预算、3 系按档位，
		// 这里与 format 出站映射一致使用 thinkingLevel）。
		gc, _ := body["generationConfig"].(map[string]any)
		if gc == nil {
			gc = map[string]any{}
			body["generationConfig"] = gc
		}
		tc, _ := gc["thinkingConfig"].(map[string]any)
		if tc == nil {
			tc = map[string]any{}
			gc["thinkingConfig"] = tc
		}
		tc["thinkingLevel"] = geminiLevelOf(tier)

	case "gemini-interactions":
		// Interactions API 客户端（/v1beta/interactions）：GenerationConfig 内
		// thinking_level 承载档位（snake_case，Gemini 3 系新标准）。
		gc, _ := body["generation_config"].(map[string]any)
		if gc == nil {
			gc = map[string]any{}
			body["generation_config"] = gc
		}
		gc["thinking_level"] = strings.ToLower(geminiLevelOf(tier))

	case "responses":
		// Responses 协议接入（如新版 Codex）：请求体用 reasoning.effort 承载档位。
		// 注入后 forceConvert 会禁用同型直通，出站由归一化形状（下方 norm 双写）
		// 统一映射，这里写入 body 仅保证 Responses 语义完整。
		body["reasoning"] = map[string]any{"effort": openaiEffortOf(tier)}

	default:
		return false
	}
	// 双写：统一档位落入归一化形状，供数据格式转换节点映射为上游格式参数。
	// 取 OpenAI 安全枚举（max 折算为 high），避免严格校验的上游拒绝未知枚举；
	// anthropic/gemini 的出站映射对 high 已是各自格式的高档位。
	norm["reasoning_effort"] = openaiEffortOf(tier)
	log.Printf("[reasoning] payload 未携带推理深度参数，已追加 %s 格式预设方案 (tier=%s)", inFormat, tier)
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
