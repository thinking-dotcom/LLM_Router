package core

import (
	"testing"
	"time"
)

func TestActiveModelsInflight(t *testing.T) {
	// 裸 Server：不调用 NewServer（避免触发 config.LoadConfig 的全局路径缓存，
	// 干扰其它用真实配置文件做端到端测试的用例）。
	s := &Server{activeModels: make(map[string]*activeModel)}

	// 两个请求进入在途
	stop := s.markModelActive("model-a")
	stop2 := s.markModelActive("model-a")

	// 在途期间始终返回（即使窗口很小）
	active := s.ActiveModels(1)
	if len(active) != 1 || active[0] != "model-a" {
		t.Fatalf("inflight: got %v, want [model-a]", active)
	}

	// 第一个请求结束，仍有 1 个在途 → 仍应可见
	stop()
	active = s.ActiveModels(1)
	if len(active) != 1 || active[0] != "model-a" {
		t.Fatalf("one remaining inflight: got %v, want [model-a]", active)
	}

	// 全部结束：缓冲窗口（1s）内仍可见，避免连续请求间隙动画闪烁
	stop2()
	active = s.ActiveModels(1)
	if len(active) != 1 || active[0] != "model-a" {
		t.Fatalf("grace after stop: got %v, want [model-a]", active)
	}

	// 超过缓冲后不再返回，动画随之熄灭
	time.Sleep(1100 * time.Millisecond)
	active = s.ActiveModels(1)
	if len(active) != 0 {
		t.Fatalf("expired: got %v, want []", active)
	}
}

func TestActiveModelsEmptyModel(t *testing.T) {
	s := &Server{activeModels: make(map[string]*activeModel)}
	// 空模型名不记录，也不产生条目
	s.markModelActive("")()
	stop := s.markModelActive("")
	defer stop()
	if n := len(s.ActiveModels(5)); n != 0 {
		t.Fatalf("empty model should not be tracked, got %d", n)
	}
}
