package core

import (
	"time"

	"llm-router-go/types"
)

func localDateKey(d time.Time) string {
	return d.Format("2006-01-02")
}

func ComputeStats(logs []types.LogEntry) types.StatsData {
	today := time.Now()
	todayKey := localDateKey(today)

	days := make(map[string]types.DayStat)
	for i := 6; i >= 0; i-- {
		d := localDateKey(today.AddDate(0, 0, -i))
		days[d] = types.DayStat{}
	}

	perModel := make(map[string]types.PerModelStat)
	type ppInternal struct {
		types.PerProviderStat
		ms []int
	}
	perProvider := make(map[string]*ppInternal)

	for _, l := range logs {
		// 排除非业务路径：模型列表端点，以及根路径的管理面板落地页
		// （桌面端「后台」按钮 / 浏览器打开 http://host:port/ 会触发，model/provider
		// 均为 "-"，并非真实 LLM 调用，计入会虚增「今日请求」与「在用模型」）。
		if l.Path == "/" || l.Path == "/v1" || l.Path == "/v1/models" || l.Path == "/models" || l.Path == "/v1beta/models" || l.Path == "/v1beta/v1beta/models" {
			continue
		}
		// 只统计成功请求（2xx）：失败请求（401/400/404/502 等）不应计入
		// 「今日请求」与 Token 用量，否则面板数字会被虚增。
		if l.Status < 200 || l.Status >= 300 {
			continue
		}
		d := localDateKey(timeNow(l.Time))
		if _, ok := days[d]; !ok {
			continue
		}
		u := l.Usage
		prompt := 0
		completion := 0
		if u != nil {
			if u.PromptTokens != nil {
				prompt = *u.PromptTokens
			}
			if u.CompletionTokens != nil {
				completion = *u.CompletionTokens
			}
		}
		ds := days[d]
		ds.Count++
		ds.Prompt += prompt
		ds.Completion += completion
		days[d] = ds

		m := l.Model
		if m == "" {
			m = "-"
		}
		pm, ok := perModel[m]
		if !ok {
			pm = types.PerModelStat{Days: make(map[string]types.DayStat)}
		}
		pd := pm.Days[d]
		pd.Count++
		pd.Prompt += prompt
		pd.Completion += completion
		pm.Days[d] = pd
		perModel[m] = pm

		if d == todayKey {
			p := l.Provider
			if p == "" {
				p = "-"
			}
			pp, ok := perProvider[p]
			if !ok {
				pp = &ppInternal{ms: []int{}}
			}
			pp.Count++
			pp.Prompt += prompt
			pp.Completion += completion
			if l.Ms > 0 {
				pp.ms = append(pp.ms, l.Ms)
			}
			perProvider[p] = pp
		}
	}

	// Fill missing days for perModel
	for _, pm := range perModel {
		for d := range days {
			if _, ok := pm.Days[d]; !ok {
				pm.Days[d] = types.DayStat{}
			}
		}
	}

	// Compute avg_ms for perProvider
	ppOut := make(map[string]types.PerProviderStat)
	for k, pp := range perProvider {
		avgMs := 0
		if len(pp.ms) > 0 {
			sum := 0
			for _, ms := range pp.ms {
				sum += ms
			}
			avgMs = sum / len(pp.ms)
		}
		pps := pp.PerProviderStat
		pps.AvgMs = &avgMs
		ppOut[k] = pps
	}

	return types.StatsData{Days: days, PerModel: perModel, PerProvider: ppOut}
}

func timeNow(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		// 解析失败返回零值：localDateKey 会得到 "0001-01-01"，不在近 7 天的
		// days 集合中，对应日志条目会被跳过，避免污染"今日"统计。
		return time.Time{}
	}
	return t
}
