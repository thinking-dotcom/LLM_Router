package transrules

// exactOrNormalize 是同型直通的请求转换规则：当客户端实际请求体格式与
// 服务商 API 格式完全一致（SameType）时，调用 engine.BuildExactOutbound
// 原样转发（保留 thinking / safetySettings / fileData 等原生字段）；否则
// 降级走 defaultRequest 的 OpenAI 归一化转换。
func exactOrNormalize(cx RequestContext) (OutboundRequest, error) {
	if cx.SameType {
		if engine == nil {
			return OutboundRequest{}, errNoEngine
		}
		return engine.BuildExactOutbound(cx.Provider, cx.RawBody, cx.OutModel, cx.Stream), nil
	}
	return defaultRequest(cx)
}
