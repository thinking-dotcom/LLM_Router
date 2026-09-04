package router

import (
	"llm-router-go/format"
	"llm-router-go/transrules"
)

// init 是格式转换规则的装配点（wiring 层）：把 format 实现的转换引擎注入
// 规则引擎 transrules。router 同时依赖二者，故在此完成依赖注入，使 transrules
// 无需感知具体实现，满足依赖倒置。本 init 随 router 包被导入（core、main）
// 自动执行，保证任何 Lookup* 调用前注册表已装配完毕。
func init() {
	transrules.SetEngine(format.NewEngine())
}
