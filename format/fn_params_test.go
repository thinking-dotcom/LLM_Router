package format

import (
	"encoding/json"
	"reflect"
	"testing"
)

// 工具声明 parameters 缺省归一：Gemini 协议允许无参函数省略 parameters，
// 原样透传为 null 时严格 OpenAI 兼容上游（serde）按 missing field `parameters`
// 报 400（Gemini CLI 实测日志）。
func TestNormFnParamsMissing(t *testing.T) {
	body := map[string]any{
		"model": "m",
		"tools": []any{map[string]any{
			"functionDeclarations": []any{
				map[string]any{"name": "no_args", "description": "d"},
			},
		}},
	}
	out := OpenaiFromGemini(body)
	tools, _ := out["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(tools))
	}
	fn := tools[0].(map[string]any)["function"].(map[string]any)
	want := map[string]any{"type": "object", "properties": map[string]any{}}
	if !reflect.DeepEqual(fn["parameters"], want) {
		t.Fatalf("parameters = %v, want %v", fn["parameters"], want)
	}
	// 序列化后必须是对象而非 null
	b, _ := json.Marshal(fn["parameters"])
	if string(b) == "null" {
		t.Fatalf("parameters serialized as null")
	}
}

func TestNormFnParamsJsonSchemaFallback(t *testing.T) {
	body := map[string]any{
		"model": "m",
		"tools": []any{map[string]any{
			"functionDeclarations": []any{
				map[string]any{
					"name":                 "f",
					"parametersJsonSchema": map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "string"}}},
				},
			},
		}},
	}
	out := OpenaiFromGemini(body)
	fn := out["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)
	p := fn["parameters"].(map[string]any)
	if _, ok := p["properties"].(map[string]any)["a"]; !ok {
		t.Fatalf("parametersJsonSchema not used: %v", p)
	}
}

func TestNormFnParamsFillTypeProps(t *testing.T) {
	// 空 parameters {} 也要补 type/properties；且不改动调用方原始 map
	schema := map[string]any{}
	body := map[string]any{
		"model": "m",
		"tools": []any{map[string]any{
			"functionDeclarations": []any{
				map[string]any{"name": "f", "parameters": schema},
			},
		}},
	}
	out := OpenaiFromGemini(body)
	fn := out["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)
	p := fn["parameters"].(map[string]any)
	if p["type"] != "object" {
		t.Fatalf("type = %v, want object", p["type"])
	}
	if _, ok := p["properties"].(map[string]any); !ok {
		t.Fatalf("properties missing: %v", p)
	}
	if len(schema) != 0 {
		t.Fatalf("input schema mutated: %v", schema)
	}
	// 完整 schema 原样保留（同一引用，不克隆不多补）
	full := map[string]any{"type": "object", "properties": map[string]any{}}
	out2 := OpenaiFromGemini(map[string]any{
		"model": "m",
		"tools": []any{map[string]any{
			"functionDeclarations": []any{map[string]any{"name": "f", "parameters": full}},
		}},
	})
	got := out2["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)["parameters"]
	if !reflect.DeepEqual(got, full) {
		t.Fatalf("full schema altered: %v", got)
	}
}

// Interactions 入站的 function 工具同样缺省归一
func TestInteractionsFnParamsMissing(t *testing.T) {
	body := map[string]any{
		"model": "m",
		"tools": []any{
			map[string]any{"type": "function", "name": "no_args", "description": "d"},
			map[string]any{"type": "google_search"},
		},
	}
	out := OpenaiFromGeminiInteractions(body)
	tools, _ := out["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools len = %d, want 1 (仅 function 工具)", len(tools))
	}
	fn := tools[0].(map[string]any)["function"].(map[string]any)
	want := map[string]any{"type": "object", "properties": map[string]any{}}
	if !reflect.DeepEqual(fn["parameters"], want) {
		t.Fatalf("parameters = %v, want %v", fn["parameters"], want)
	}
}
