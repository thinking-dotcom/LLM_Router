package router

import (
	"encoding/json"
	"log"
	"sort"
	"strings"

	"llm-router-go/types"
)

// structurallyFolded 归一化请求中会被转换器结构性折叠/重命名的顶层字段：
// 它们在出站 body 中以其它形态保留（如 max_completion_tokens→max_tokens、
// stop→stop_sequences / stopSequences、top_k→topK），不算被丢弃。
// 随转换器演进需同步维护。
var structurallyFolded = map[string]bool{
	"model":                 true,
	"stream":                true,
	"stream_options":        true,
	"messages":              true,
	"temperature":           true,
	"top_p":                 true,
	"top_k":                 true,
	"max_tokens":            true,
	"max_completion_tokens": true,
	"stop":                  true,
	"tools":                 true,
	"tool_choice":           true,
	// 统一推理档位会被出站实现折叠为目标格式的思考参数
	// （anthropic→thinking、gemini→thinkingConfig、responses→reasoning.effort），
	// 故不算被丢弃。
	"reasoning_effort": true,
	"reasoning":        true,
}

// logDroppedRequestFields 对比归一化请求（clientReq）与转换后出站 body 的顶层
// 字段，把「客户端显式给出、但目标格式白名单重建丢弃」的字段记入日志，让跨型
// 转换的降级显式可查，而非静默丢失。客户端未给出的字段不参与；同型直通
// （body 为原样拷贝）与 openai→openai（body 为全量克隆）天然不会有丢弃。
func logDroppedRequestFields(clientReq map[string]any, outBody any) {
	if dropped := droppedRequestFields(clientReq, outBody); len(dropped) > 0 {
		log.Printf("[ROUTER] 跨型转换丢弃的请求字段(目标格式不识别): %s", strings.Join(dropped, ", "))
	}
}

// droppedRequestFields 返回被丢弃的顶层字段名（已排序、去重由 map 键保证）。
// 拆分为独立函数便于单元测试。
func droppedRequestFields(clientReq map[string]any, outBody any) []string {
	out := bodyAsMap(outBody)
	var dropped []string
	for k := range clientReq {
		if structurallyFolded[k] {
			continue
		}
		if types.IRIsExtKey(k) {
			// IR 扩展键：出站时要么被目标格式还原（如 _ext_anthropic_thinking →
			// thinking）、要么按目标能力剥离，不视为静默丢弃。
			continue
		}
		if _, ok := out[k]; !ok {
			dropped = append(dropped, k)
		}
	}
	sort.Strings(dropped)
	return dropped
}

// bodyAsMap 把出站 body 规整为 map[string]any，兼容 map 与非 map（JSON 反序列化兜底）。
func bodyAsMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	var m map[string]any
	if b, err := json.Marshal(v); err == nil {
		json.Unmarshal(b, &m)
	}
	return m
}
