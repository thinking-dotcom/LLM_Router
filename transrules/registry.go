// Package transrules 是 LLM Router 的「格式转换规则引擎」，一个可插拔的独立模块。
//
// 它把「将客户端请求数据转换成目标服务商 API 数据格式」以及「把上游响应
// 逆向转换回客户端格式」的规则统一登记成一张 (源格式 → 目标格式) 矩阵，
// 由路由器按 src→dst 查表调用，而不再散落在各处理分支里硬编码。
//
// 规则按「目标 API 格式」拆分到本目录的独立文件中（openai_rules.go、
// anthropic_rules.go、gemini_rules.go、response_rules.go），
// 每个文件负责登记能够产出该格式的转换规则。
//
// 本包遵循依赖倒置原则：**不依赖任何具体转换实现**。全部转换能力抽象为
// Engine 接口（见 Engine），由外部实现（本项目中是 format 包）经 SetEngine
// 注入；注入时自动按格式矩阵装配默认注册表。公共 API（OutboundRequest /
// RequestContext / RequestFunc / NonStreamFunc / StreamFunc / ChunkFunc 以及
// RegisterRequest / RegisterNonStream / RegisterStream / RegisterChunk / Lookup*）
// 均定义在本包，不依赖具体实现。外部（新增格式、用户自定义转换）可通过公开的
// Register* 自由登记或覆盖规则，从而让规则具备可插拔性；
// router 仅依赖本包一个入口完成全部格式转换。
package transrules

import (
	"fmt"
	"io"

	"llm-router-go/types"
)

// OutboundRequest 描述一次转换后的上游调用（最终 URL、请求头与请求体），
// 复用于 types.OutboundRequest（format 与 transrules 共享，互不依赖）。
type OutboundRequest = types.OutboundRequest

// RequestContext 携带将客户端请求转换到目标上游格式所需的全部信息。
// 当前管线会把所有接入格式归一化为 OpenAI 形状（OpenAIReq），
// 再由此转换成目标服务商的原生格式。
type RequestContext struct {
	Provider  types.Provider // 匹配到的服务商（其 Type 即目标 API 格式）
	OpenAIReq map[string]any // 归一化后的 OpenAI 形状请求体
	RawBody   map[string]any // 客户端原始请求体（同型直通用）
	OutModel  string         // 已解析别名后的出站模型名
	Stream    bool           // 是否流式请求
	SameType  bool           // 是否同型直通（不转换、原样转发原生字段）
}

// RequestFunc 把归一化的客户端请求转换成目标上游的 OutboundRequest
// （含最终 URL、请求头与请求体）。
type RequestFunc func(cx RequestContext) (OutboundRequest, error)

// NonStreamFunc 将上游非流式响应（已归一化为 OpenAI 形状 openaiResp）
// 逆向转换回客户端声明的格式。
type NonStreamFunc func(openaiResp map[string]any, outModel string) map[string]any

// StreamFunc 将上游 SSE 分片增量写出到客户端声明的格式。
type StreamFunc func(w io.Writer, chunks <-chan map[string]any, outModel string) (*types.Usage, error)

// ChunkFunc 将上游流式响应体按上游 API 格式解析为统一的 chunk 事件流，
// 是「上游响应 → 客户端格式」之逆过程的第一步（上游原生 SSE → 归一化 chunk）。
type ChunkFunc func(body io.ReadCloser) <-chan map[string]any

// Engine 抽象了格式转换所需的全部能力，是「实现」与「规则引擎」之间的契约。
// 格式实现层（项目中的 format 包）提供 Engine 的实现并注入 SetEngine，从而
// 让 transrules 不依赖任何具体实现，满足依赖倒置。
type Engine interface {
	// BuildOutbound 按服务商 API 格式把归一化的 OpenAI 请求装配成上游调用
	//（含 URL/头/请求体），内部按 provider.Type 分发到对应目标格式转换。
	BuildOutbound(provider types.Provider, openaiReq map[string]any, model string) OutboundRequest
	// BuildExactOutbound 用于同型直通：不转换、原样转发客户端原生字段。
	BuildExactOutbound(provider types.Provider, rawBody map[string]any, model string, stream bool) OutboundRequest
	// ToClient 把已归一化为 OpenAI 形状的响应还原成目标客户端格式 dst 的响应。
	ToClient(dst string, openaiResp map[string]any, model string) map[string]any
	// WriteChunks 按客户端格式 dst 把归一化 chunk 增量写出为客户端 SSE。
	WriteChunks(dst string, w io.Writer, chunks <-chan map[string]any, model string) (*types.Usage, error)
	// ParseChunks 按上游 API 格式 src 把上游流式响应体解析为归一化 chunk 流。
	ParseChunks(src string, body io.ReadCloser) <-chan map[string]any
}

// 请求规则表：requestRules[srcFormat][dstFormat]
var requestRules = map[string]map[string]RequestFunc{}

// 响应非流式规则表：nonStreamRules[upstreamFormat][clientFormat]
var nonStreamRules = map[string]map[string]NonStreamFunc{}

// 响应流式解析规则表：chunkRules[upstreamFormat]
var chunkRules = map[string]ChunkFunc{}

// 响应流式写出规则表：streamRules[clientFormat]
var streamRules = map[string]StreamFunc{}

// engine 是当前注入的格式转换实现（由 SetEngine 设置）。规则引擎只面向该
// 接口工作，完成对具体实现的完全解耦。
var engine Engine

// errNoEngine 表示规则引擎尚未注入任何转换实现。
var errNoEngine = fmt.Errorf("transrules: no engine injected, call transrules.SetEngine")

// registerEngineDefaults 统一装配默认的格式转换规则矩阵，全部基于注入的
// 实现 e 完成，按「目标 API 格式」分文件组织（见各 register*Rules 函数）。
func registerEngineDefaults(e Engine) {
	registerOpenAIRules(e)
	registerAnthropicRules(e)
	registerGeminiRules(e)
	registerResponseRules(e)
}

// SetEngine 注入格式转换实现并据此装配默认注册表。注入后，规则引擎通过
// engine 完成所有实际的格式转换。已在外部注入后再次调用会覆盖此前装配的
// 注册表（以最后一次注入的实现为准）。
func SetEngine(e Engine) {
	if e == nil {
		return
	}
	engine = e
	// 清空并重建默认注册表，统一基于注入的实现填写整张格式矩阵。
	requestRules = map[string]map[string]RequestFunc{}
	nonStreamRules = map[string]map[string]NonStreamFunc{}
	chunkRules = map[string]ChunkFunc{}
	streamRules = map[string]StreamFunc{}
	registerEngineDefaults(e)
}

// engineAvailable 返回是否已注入转换实现。
func engineAvailable() bool { return engine != nil }

// defaultRequest 是基于注入实现的标准请求转换：无论来源接入方式为何，都从
// OpenAI 归一化形状经 engine.BuildOutbound 装配出站请求（内部按目标类型分发）。
// 同型直通（anthropic→anthropic / gemini→gemini）由 exactOrNormalize 覆盖。
func defaultRequest(cx RequestContext) (OutboundRequest, error) {
	if engine == nil {
		return OutboundRequest{}, errNoEngine
	}
	return engine.BuildOutbound(cx.Provider, cx.OpenAIReq, cx.OutModel), nil
}

// defaultNonStream 是默认的非流式响应转换：客户端为 openai 接入时响应即
// OpenAI 形状，无需再转换。
func defaultNonStream(openaiResp map[string]any, _ string) map[string]any {
	return openaiResp
}

// RegisterRequest 登记一条「源格式 Src → 目标格式 Dst」的请求转换规则。
// 路由器按声明接入方式(src) 与服务商 API 格式(dst) 查表调用。
func RegisterRequest(src, dst string, fn RequestFunc) {
	if requestRules[src] == nil {
		requestRules[src] = map[string]RequestFunc{}
	}
	requestRules[src][dst] = fn
}

// RegisterNonStream 登记一条「上游格式 src → 客户端格式 dst」的非流式响应
// 逆向转换规则。
func RegisterNonStream(src, dst string, fn NonStreamFunc) {
	if nonStreamRules[src] == nil {
		nonStreamRules[src] = map[string]NonStreamFunc{}
	}
	nonStreamRules[src][dst] = fn
}

// RegisterStream 登记一条产出「客户端格式 dst」流式事件的规则。
func RegisterStream(dst string, fn StreamFunc) {
	streamRules[dst] = fn
}

// RegisterChunk 登记一条按「上游 API 格式 src」解析上游 SSE 分片的规则。
func RegisterChunk(src string, fn ChunkFunc) {
	chunkRules[src] = fn
}

// LookupChunk 按上游解析 API 格式查表取得分片解析规则；未知格式统一回退为
// OpenAI 分片解析（经注入实现，覆盖未登记的上游格式兜底）。
func LookupChunk(src string) ChunkFunc {
	if fn := chunkRules[src]; fn != nil {
		return fn
	}
	return engineParseChunks("openai")
}

// LookupRequest 按客户端接入方式(src) 与服务商 API 格式(dst) 查表取得请求
// 转换规则；无对应规则时报错（路由器据此判定接入方式与目标格式不亲和）。
func LookupRequest(src, dst string) (RequestFunc, error) {
	if m := requestRules[src]; m != nil {
		if fn := m[dst]; fn != nil {
			return fn, nil
		}
	}
	return nil, fmt.Errorf("no request conversion rule: %s → %s", src, dst)
}

// LookupNonStream 按上游格式(src) 与客户端格式(dst) 查表取得非流式响应
// 逆向转换规则；查询不到客户端维度的规则时回退为默认（OpenAI 形状直出）。
func LookupNonStream(src, dst string) NonStreamFunc {
	if m := nonStreamRules[src]; m != nil {
		if fn := m[dst]; fn != nil {
			return fn
		}
	}
	return defaultNonStream
}

// LookupStream 按客户端格式查表取得流式写出规则；无规则时统一回退为
// OpenAI 格式写出（覆盖 access 未注册的兜底，经注入实现）。
func LookupStream(dst string) StreamFunc {
	if fn := streamRules[dst]; fn != nil {
		return fn
	}
	return engineWriteChunks("openai")
}

// engineParseChunks 返回基于注入实现对指定上游格式的分片解析函数；未注入时
// 返回一个即时返回空流的函数，避免 nil 恐慌。
func engineParseChunks(src string) ChunkFunc {
	return func(body io.ReadCloser) <-chan map[string]any {
		if engine == nil {
			ch := make(chan map[string]any)
			close(ch)
			return ch
		}
		return engine.ParseChunks(src, body)
	}
}

// engineWriteChunks 返回基于注入实现对指定客户端格式的流式写出函数；未注入时
// 返回即时报错的函数，避免 nil 恐慌。
func engineWriteChunks(dst string) StreamFunc {
	return func(w io.Writer, chunks <-chan map[string]any, model string) (*types.Usage, error) {
		if engine == nil {
			return nil, errNoEngine
		}
		return engine.WriteChunks(dst, w, chunks, model)
	}
}
