package core

// placeholder 表示一个尚未接入的目标客户端（占位）。占位客户端在界面中会
// 显示为「待接入」，注入/撤销会返回提示而不会向真实配置目录写入任何内容，
// 避免因配置格式未知而破坏用户文件。
type placeholder struct {
	id     string
	name   string
	format string
}

func (p *placeholder) Info() TargetInfo {
	return TargetInfo{ID: p.id, Name: p.name, APIFormat: p.format, Placeholder: true}
}
func (p *placeholder) InjectSkills(string, []Skill) error  { return nil }
func (p *placeholder) UndoSkills() error                   { return nil }
func (p *placeholder) InjectMcp(string, []MCPServer) error { return nil }

// 历史上 Codex 曾以占位形式注册于此；
// 现已全部正式接入（见 targets_known.go），占位注册清空，类型保留供测试构造使用。
