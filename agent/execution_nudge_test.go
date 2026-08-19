package main

import "testing"

func TestShouldNudgeToExecuteDetectsTutorialAnswer(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "你能修改我们这个项目前端的侧边栏，让他能收起折叠吗？"},
		{Role: "assistant", Content: "<report>要实现前端侧边栏的可折叠功能，通常需要在前端代码中进行一些修改。假设前提是使用 React 和 Ant Design...</report>"},
	}
	if !shouldNudgeToExecute(messages, messages[1].Content) {
		t.Fatal("expected actionable tutorial to trigger nudge")
	}
}

func TestShouldNudgeToExecuteSkipsExplanationRequests(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "解释一下如何实现侧边栏折叠"},
		{Role: "assistant", Content: "通常需要给 Sidebar 加一个 collapsed 状态。"},
	}
	if shouldNudgeToExecute(messages, messages[1].Content) {
		t.Fatal("explanation request should not trigger nudge")
	}
}
