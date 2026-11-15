package format

import (
	"strconv"
	"strings"
)

// ===========================================================================
// 推理参数映射表（唯一事实来源 / single source of truth）
//
// 一张表描述「统一档位 ↔ 各上游 API 格式的原生字段与取值」。全仓所有推理参数的
// 读（客户端原生 → 档位/展示值）、写（档位 → 原生字段）、兜底注入都只查这张表，
// 不再各处复制取值表。
//
//	档位    | OpenAI 兼容     | Responses     | Anthropic(旧 enabled) | Anthropic(新 adaptive) | Gemini 3 系 | Gemini 2.5 系    | Interactions
//	-------|----------------|---------------|-----------------------|------------------------|-------------|------------------|-------------
//	none   | 不写            | 不写           | 不启用                 | 不写                    | MINIMAL     | 0（pro 不写）     | minimal
//	minimal| low            | minimal       | 1024                  | low                    | MINIMAL     | 1024             | minimal
//	low    | low            | low           | 2048                  | low                    | LOW         | 1024             | low
//	medium | medium         | medium        | 8192                  | medium                 | MEDIUM      | 8192             | medium
//	high   | high           | high          | 24576                 | high                   | HIGH        | 24576            | high
//	xhigh  | high           | xhigh         | 32768                 | xhigh                  | HIGH        | 24576            | high
//	max    | high           | max           | 32768                 | max                    | HIGH        | 24576            | high
//
// 「不写」= 该档位下不落任何字段（OpenAI / Responses 的 none 表示不发送推理档位）。
// OpenAI 兼容列对 xhigh/max 封顶为 high（官方枚举没有更高档，写非法枚举会被严格
// 上游 400；XHigh 仅存在于各 CLI 的本地枚举）。
//
// 不携带任何推理参数的模型家族（encode 返回 nil）：
//   - Gemma 系：不支持思考配置，写 thinkingConfig / thinking_level 会被上游 400；
//   - Gemini 2.5-pro：思考不可关闭，none 档不写字段（其余档位照常写 budget）；
//   - gemini-openai（Google 的 OpenAI 兼容端点）：protobuf 严格校验，不认
//     reasoning_effort，出站前显式剥离（stripGeminiUnsupported）。
//
// 反向折算（原生值 → 统一档位）：
//   - 枚举：枚举名小写即档位（MINIMAL/LOW/MEDIUM/HIGH/XHIGH/MAX/…）；
//   - 数值预算：≤0 → none、≤2048 → low、<16384 → medium、≥16384 → high
//     （2048 取闭区间：它正是表内 low 的预算值，开区间会把自家 low 折成 medium，
//     导致「客户端 low → 换上游变成 8192」）；
//     负预算（-1）是 Gemini 官方「动态思考」标记（模型自决），不折算、保持未指定；
//   - 未收录的枚举字符串：OpenAI / Responses 原样透传（枚举开放）；Anthropic 不猜
//     （返回空档位，避免替客户端乱写预算）；Gemini / Interactions 按 high 处理
//     （与历史 default 分支一致）。
//
// 与「是否携带」判定（router.HasReasoningDepth）的分工：本表负责**取值映射**，
// 是否携带由 router 侧的保守谓词判定（它还兜住了 IR 扩展键与空对象等边界）。
// ===========================================================================

// 统一档位。none/low/medium/high/max 是设置页可选项；minimal/xhigh 是各客户端
// 自带枚举（如 cline 的 minimal、cc/kimi 的 xhigh），归一后落到表内行。
const (
	ReasoningNone    = "none"
	ReasoningMinimal = "minimal"
	ReasoningLow     = "low"
	ReasoningMedium  = "medium"
	ReasoningHigh    = "high"
	ReasoningXHigh   = "xhigh"
	ReasoningMax     = "max"
)

// 映射表的家族键（= 上游 API 格式 / 接入格式的归一化键）。
const (
	ReasoningFamilyOpenAI       = "openai"
	ReasoningFamilyGeminiOpenAI = "gemini-openai"
	ReasoningFamilyResponses    = "responses"
	ReasoningFamilyAnthropic    = "anthropic"
	ReasoningFamilyGemini       = "gemini"
	ReasoningFamilyInteractions = "gemini-interactions"
)

// reasoningTierRow 映射表的一行：某个统一档位在各家族里的取值。
// 空串 / 0 表示该家族在此档位下不写该字段（是否整体落字段由 encode 决定）。
type reasoningTierRow struct {
	openaiEffort      string // OpenAI 兼容：reasoning_effort
	responsesEffort   string // Responses：reasoning.effort
	anthropicBudget   int    // Anthropic 旧家族：thinking.budget_tokens（0 = 不启用）
	anthropicEffort   string // Anthropic 新家族：output_config.effort
	geminiLevel       string // Gemini 3 系：thinkingConfig.thinkingLevel
	gemini25Budget    int    // Gemini 2.5 系：thinkingConfig.thinkingBudget
	interactionsLevel string // Interactions：generation_config.thinking_level
}

// reasoningTierTable 映射表本体。新增档位/家族只改这里（表即文档，见文件头）。
var reasoningTierTable = map[string]reasoningTierRow{
	ReasoningNone:    {"", "", 0, "", "MINIMAL", 0, "minimal"},
	ReasoningMinimal: {"low", "minimal", 1024, "low", "MINIMAL", 1024, "minimal"},
	ReasoningLow:     {"low", "low", 2048, "low", "LOW", 1024, "low"},
	ReasoningMedium:  {"medium", "medium", 8192, "medium", "MEDIUM", 8192, "medium"},
	ReasoningHigh:    {"high", "high", 24576, "high", "HIGH", 24576, "high"},
	ReasoningXHigh:   {"high", "xhigh", 32768, "xhigh", "HIGH", 24576, "high"},
	ReasoningMax:     {"high", "max", 32768, "max", "HIGH", 24576, "high"},
}

// tierRow 查表；未收录（含空串）返回第二值为 false。
func tierRow(tier string) (reasoningTierRow, bool) {
	r, ok := reasoningTierTable[strings.ToLower(strings.TrimSpace(tier))]
	return r, ok
}

// NormalizeReasoningTier 把任意输入收敛为表内档位：大小写/空白无关，空或未收录
// 一律视为 high（与 router 的默认档位策略一致）。
func NormalizeReasoningTier(tier string) string {
	if _, ok := tierRow(tier); ok {
		return strings.ToLower(strings.TrimSpace(tier))
	}
	return ReasoningHigh
}

// reasoningFamily 把 API 格式（上游 provider.Type 或接入格式）归一为家族键。
// 同时接受两种拼写：responses / response（ProviderResponse）。
func reasoningFamily(apiFormat string) string {
	switch strings.ToLower(strings.TrimSpace(apiFormat)) {
	case ReasoningFamilyOpenAI:
		return ReasoningFamilyOpenAI
	case ReasoningFamilyGeminiOpenAI:
		return ReasoningFamilyGeminiOpenAI
	case ReasoningFamilyResponses, "response":
		return ReasoningFamilyResponses
	case ReasoningFamilyAnthropic:
		return ReasoningFamilyAnthropic
	case ReasoningFamilyGemini:
		return ReasoningFamilyGemini
	case ReasoningFamilyInteractions:
		return ReasoningFamilyInteractions
	}
	return ""
}

// ---------------------------------------------------------------------------
// 档位 → 某家族取值（转换/注入都从这里取，禁止在别处复制数值）
// ---------------------------------------------------------------------------

// OpenAIEffortOf 档位 → OpenAI 兼容 reasoning_effort。未收录的枚举原样透传
// （上游可能是自定义枚举，如 auto），因此调用方需自行判空。
func OpenAIEffortOf(tier string) string {
	if r, ok := tierRow(tier); ok {
		return r.openaiEffort
	}
	return strings.ToLower(strings.TrimSpace(tier))
}

// ResponsesEffortOf 档位 → Responses API reasoning.effort。none 返回空串表示
// 不写该字段；未收录枚举原样透传。
func ResponsesEffortOf(tier string) string {
	if r, ok := tierRow(tier); ok {
		return r.responsesEffort
	}
	return strings.ToLower(strings.TrimSpace(tier))
}

// AnthropicBudgetOf 档位 → Anthropic thinking.budget_tokens（0 = 不启用）。
// 未收录的枚举不猜预算，返回 0。
func AnthropicBudgetOf(tier string) int {
	if r, ok := tierRow(tier); ok {
		return r.anthropicBudget
	}
	return 0
}

// AnthropicEffortOf 档位 → Anthropic output_config.effort（空 = 不写）。
func AnthropicEffortOf(tier string) string {
	if r, ok := tierRow(tier); ok {
		return r.anthropicEffort
	}
	return ""
}

// GeminiLevelOf 档位 → Gemini thinkingLevel 枚举。未收录枚举按 high 处理
// （历史 default 分支行为）。
func GeminiLevelOf(tier string) string {
	if r, ok := tierRow(tier); ok {
		return r.geminiLevel
	}
	return "HIGH"
}

// Gemini25BudgetOf 档位 → Gemini 2.5 系 thinkingBudget。未收录枚举按 high。
func Gemini25BudgetOf(tier string) int {
	if r, ok := tierRow(tier); ok {
		return r.gemini25Budget
	}
	return 24576
}

// InteractionsLevelOf 档位 → Interactions thinking_level。未收录枚举按 high。
func InteractionsLevelOf(tier string) string {
	if r, ok := tierRow(tier); ok {
		return r.interactionsLevel
	}
	return "high"
}

// ---------------------------------------------------------------------------
// 统一档位 → 原生字段（写出方向）
// ---------------------------------------------------------------------------

// NativeReasoning 描述原生推理参数在请求体里的落点：先按 Path 逐层建/取父对象，
// 再把 Value 合并进去。Value 为空表示该模型/档位不应携带推理参数。
type NativeReasoning struct {
	Path  []string       // 顶层字段路径，如 ["generationConfig","thinkingConfig"]
	Value map[string]any // 落到 Path 上的字段/值
}

// SupportsReasoningInjection 该接入格式是否支持兜底注入预设推理参数。
func SupportsReasoningInjection(apiFormat string) bool {
	switch reasoningFamily(apiFormat) {
	case "", ReasoningFamilyGeminiOpenAI:
		return false
	}
	return true
}

// EncodeReasoningNative 按「上游 API 格式 + 目标模型」把统一档位写成该格式的原生
// 推理参数。返回 nil 表示不应携带（Gemma 系、gemini-openai 端点、Anthropic none、
// 未知格式）。
//
// 注意 OpenAI / Responses 两族对 xhigh/max 封顶为 high：本函数同时服务「兜底注入」，
// 注入值要保证官方枚举合法（OpenAI 枚举只有 minimal/low/medium/high），因此这里
// 用安全枚举；客户端显式带来的自定义枚举不在本函数范围内（走透传）。
func EncodeReasoningNative(apiFormat, model, tier string) *NativeReasoning {
	tier = strings.ToLower(strings.TrimSpace(tier))
	if tier == "" {
		return nil
	}
	switch reasoningFamily(apiFormat) {
	case ReasoningFamilyOpenAI:
		if eff := OpenAIEffortOf(tier); eff != "" {
			return &NativeReasoning{Value: map[string]any{"reasoning_effort": eff}}
		}
	case ReasoningFamilyResponses:
		if eff := OpenAIEffortOf(tier); eff != "" {
			return &NativeReasoning{Value: map[string]any{"reasoning": map[string]any{"effort": eff}}}
		}
	case ReasoningFamilyAnthropic:
		if isAnthropicAdaptiveModel(model) {
			// Claude 4.7+/5、Mythos、Fable 5：官方推荐 adaptive + output_config.effort
			if tier == ReasoningNone {
				return nil
			}
			p := &NativeReasoning{Value: map[string]any{"thinking": map[string]any{"type": "adaptive"}}}
			if eff := AnthropicEffortOf(tier); eff != "" {
				p.Value["output_config"] = map[string]any{"effort": eff}
			}
			return p
		}
		if budget := AnthropicBudgetOf(tier); budget > 0 {
			return &NativeReasoning{Value: map[string]any{
				"thinking": map[string]any{"type": "enabled", "budget_tokens": budget},
			}}
		}
	case ReasoningFamilyGemini:
		// 模型家族择形（Gemma 不支持、2.5 系只认 budget、3 系用 level）
		if isGemmaModel(model) {
			return nil
		}
		var tc map[string]any
		if isGemini25Model(model) {
			if tier == ReasoningNone {
				// 2.5-pro 思考不可关闭，none 档不落字段；其余 2.5 用 budget=0 关闭
				if strings.Contains(strings.ToLower(model), "pro") {
					return nil
				}
				tc = map[string]any{"thinkingBudget": 0}
			} else {
				tc = map[string]any{"thinkingBudget": Gemini25BudgetOf(tier)}
			}
		} else {
			tc = map[string]any{"thinkingLevel": GeminiLevelOf(tier)}
		}
		return &NativeReasoning{
			Path:  []string{"generationConfig"},
			Value: map[string]any{"thinkingConfig": tc},
		}
	case ReasoningFamilyInteractions:
		if isGemmaModel(model) {
			return nil
		}
		return &NativeReasoning{
			Path:  []string{"generation_config"},
			Value: map[string]any{"thinking_level": InteractionsLevelOf(tier)},
		}
	}
	return nil
}

// ApplyReasoningNative 按映射表把档位写成 body 里的原生推理参数（就地修改），
// 返回是否写入。父对象不存在时按 Path 创建（如 generationConfig / generation_config）。
func ApplyReasoningNative(body map[string]any, apiFormat, model, tier string) bool {
	p := EncodeReasoningNative(apiFormat, model, tier)
	if p == nil || body == nil || len(p.Value) == 0 {
		return false
	}
	target := body
	for _, k := range p.Path {
		next, _ := target[k].(map[string]any)
		if next == nil {
			next = map[string]any{}
			target[k] = next
		}
		target = next
	}
	for k, v := range p.Value {
		target[k] = v
	}
	return true
}

// ---------------------------------------------------------------------------
// 原生字段 → 统一档位 / 展示值（读取方向）
// ---------------------------------------------------------------------------

// nativeReasoning 原生推理参数的读取结果：档位、展示值、命中的字段与原始文本。
type nativeReasoning struct {
	tier    string // 统一档位（"" = 不折算，如 Gemini 动态思考、Anthropic 只给了 thinking.type）
	label   string // 展示值（原文；Gemini 的 -1 标注为「动态」）
	field   string // 命中的原生字段路径，如 thinkingConfig.thinkingBudget
	value   string // 该字段的原始值文本，如 -1 / HIGH / 8192
	present bool   // 该格式的原生字段是否存在
}

// desc 返回「字段=值」描述（日志「上游推理」列用）；未携带时为空串。
func (r nativeReasoning) desc() string {
	if !r.present || r.field == "" {
		return ""
	}
	return r.field + "=" + r.value
}

// DecodeReasoningNative 从请求体（客户端原生形状或上游原生形状）读出推理参数，返回：
//   - tier：统一档位（"" = 不折算，如 Gemini 动态思考、Anthropic 只给了 thinking.type）
//   - label：展示值原文（数值型预算或档位枚举原文；Gemini 的 -1 标注为「动态」）
//   - present：该格式的原生字段是否存在
func DecodeReasoningNative(inFormat string, body map[string]any) (tier, label string, present bool) {
	r := decodeReasoningNative(inFormat, body)
	return r.tier, r.label, r.present
}

// DescribeReasoningNative 返回请求体里实际携带的推理参数「字段=值」（日志「原始推理/
// 上游推理」两列用），如 reasoning_effort=high、thinkingConfig.thinkingBudget=24576。
// 空串表示该格式下没有可识别的推理字段——上游视角即「不写」。
func DescribeReasoningNative(inFormat string, body map[string]any) string {
	return decodeReasoningNative(inFormat, body).desc()
}

// HasReasoningField 报告请求体里是否「出现」了可识别的推理字段——只看是否携带，
// 不看取值：thinking.type=disabled（显式关闭）、reasoning_effort=none、
// thinkingBudget:-1（动态思考）都算已携带。客户端已经就此表态，路由器不应再用
// 全局档位覆盖它。
//
// 字段路径与 DecodeReasoningNative 同源（同一个 decodeReasoningNative），因此
// 「日志能显示出来的字段」一定判为已携带——杜绝「原始推理列显示了字段、却仍被
// 自动注入」这类判据不一致。
func HasReasoningField(inFormat string, body map[string]any) bool {
	return decodeReasoningNative(inFormat, body).present
}

// decodeReasoningNative 各家族原生推理参数的唯一读取实现：展示值与「字段=值」描述
// 共用它，避免两套优先级各自漂移。
func decodeReasoningNative(inFormat string, body map[string]any) nativeReasoning {
	if body == nil {
		return nativeReasoning{}
	}
	switch reasoningFamily(inFormat) {
	case ReasoningFamilyOpenAI:
		if s, _ := body["reasoning_effort"].(string); s != "" {
			return levelNative("reasoning_effort", s)
		}
		if rs, ok := body["reasoning"].(map[string]any); ok {
			if s, _ := rs["effort"].(string); s != "" {
				return levelNative("reasoning.effort", s)
			}
		}
		// OpenAI 形状直接携带 thinking 的客户端（如 Deep Code CLI 发的
		// thinking:{type:"enabled"|"disabled"}）：只如实描述字段，**不折算档位**
		// ——enabled/disabled 不含档位信息，硬折算会误改客户端意图。
		if th, ok := body["thinking"].(map[string]any); ok {
			if s, _ := th["type"].(string); s != "" {
				return nativeReasoning{label: s, field: "thinking.type", value: s, present: true}
			}
		}
	case ReasoningFamilyResponses:
		if rs, ok := body["reasoning"].(map[string]any); ok {
			if s, _ := rs["effort"].(string); s != "" {
				return levelNative("reasoning.effort", s)
			}
		}
		if s, _ := body["reasoning_effort"].(string); s != "" {
			return levelNative("reasoning_effort", s)
		}
	case ReasoningFamilyAnthropic:
		return decodeAnthropicThinking(body)
	case ReasoningFamilyGemini:
		if gc, ok := body["generationConfig"].(map[string]any); ok {
			if tc, ok := gc["thinkingConfig"].(map[string]any); ok {
				return decodeGeminiThinking(tc)
			}
		}
	case ReasoningFamilyInteractions:
		if gc, ok := body["generation_config"].(map[string]any); ok {
			if r := decodeInteractionsLevel(gc["thinking_level"]); r.present {
				return r
			}
			// thinking_budget（数值预算写法）：与 Gemini 同语义（-1 动态、0 关闭）
			if b, ok := numOf(gc["thinking_budget"]); ok {
				return budgetNative("thinking_budget", b)
			}
		}
	}
	return nativeReasoning{}
}

// levelNative 枚举档位字段的读取结果。
func levelNative(field, v string) nativeReasoning {
	return nativeReasoning{tier: levelTier(v), label: v, field: field, value: v, present: true}
}

// budgetNative 数值预算字段的读取结果：负值（-1）是 Gemini 官方「动态思考」
// （模型自决），不折算档位，但字段与原始值照实描述。
func budgetNative(field string, b float64) nativeReasoning {
	value := strconv.Itoa(int(b))
	if b < 0 {
		return nativeReasoning{label: "动态", field: field, value: value, present: true}
	}
	return nativeReasoning{tier: tierFromBudget(b), label: value, field: field, value: value, present: true}
}

// GeminiThinkingLabel 从 thinkingConfig 取展示值（归一化形状里的 IR 扩展键回退用，
// 与 DecodeReasoningNative 共用同一套取值规则）。
func GeminiThinkingLabel(tc map[string]any) string {
	return decodeGeminiThinking(tc).label
}

// decodeAnthropicThinking 读 Anthropic 原生思考配置。优先级与原日志列一致：
// output_config.effort > thinking.budget_tokens > thinking.type。
func decodeAnthropicThinking(body map[string]any) nativeReasoning {
	if oc, ok := body["output_config"].(map[string]any); ok {
		if s, _ := oc["effort"].(string); s != "" {
			return levelNative("output_config.effort", s)
		}
	}
	th, ok := body["thinking"].(map[string]any)
	if !ok {
		return nativeReasoning{}
	}
	if b, ok := numOf(th["budget_tokens"]); ok {
		return budgetNative("thinking.budget_tokens", b)
	}
	if s, _ := th["type"].(string); s != "" {
		// type=enabled/adaptive 只说明「开了思考」，没给档位 → 不折算档位
		return nativeReasoning{label: s, field: "thinking.type", value: s, present: true}
	}
	return nativeReasoning{}
}

// decodeGeminiThinking 读 Gemini thinkingConfig（档位优先，其次数值预算）。
func decodeGeminiThinking(tc map[string]any) nativeReasoning {
	if tc == nil {
		return nativeReasoning{}
	}
	if lv, ok := tc["thinkingLevel"].(string); ok && lv != "" {
		return levelNative("thinkingConfig.thinkingLevel", lv)
	}
	if b, ok := numOf(tc["thinkingBudget"]); ok {
		return budgetNative("thinkingConfig.thinkingBudget", b)
	}
	return nativeReasoning{}
}

// decodeInteractionsLevel 读 Interactions thinking_level。
func decodeInteractionsLevel(v any) nativeReasoning {
	s, _ := v.(string)
	if s == "" {
		return nativeReasoning{}
	}
	switch strings.ToLower(s) {
	case "none", "minimal":
		// Interactions 的 minimal 等价于「最低思考」；保留 minimal 档位，避免被
		// 当作 none（禁用）传给 OpenAI 系上游。
		return nativeReasoning{tier: ReasoningMinimal, label: s, field: "thinking_level", value: s, present: true}
	}
	return levelNative("thinking_level", s)
}

// levelTier 枚举字符串 → 统一档位（大小写无关）。未收录枚举原样小写返回，
// 交由各出站按表内 fallback 规则处理。
func levelTier(v string) string {
	s := strings.ToLower(strings.TrimSpace(v))
	switch s {
	case ReasoningNone, ReasoningMinimal, ReasoningLow, ReasoningMedium,
		ReasoningHigh, ReasoningXHigh, ReasoningMax:
		return s
	}
	return s
}

// numOf 把 any 数值安全转成 float64 并报告「确实是数值」（0 与字段缺失要区分）。
// JSON 反序列化得到 float64，内部构造/测试可能给 int / int64，三种都要认。
func numOf(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

// tierFromBudget 数值预算 → 统一档位（阈值见文件头「反向折算」）。
// 2048 取闭区间，保证表内 low 的预算值（Anthropic 2048）能原档折回。
func tierFromBudget(b float64) string {
	switch {
	case b <= 0:
		return ReasoningNone
	case b <= 2048:
		return ReasoningLow
	case b < 16384:
		return ReasoningMedium
	default:
		return ReasoningHigh
	}
}
