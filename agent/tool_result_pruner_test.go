package main

import (
	"strings"
	"testing"
)

func TestPruneToolResultsKeepsHeadMarkerTail(t *testing.T) {
	cfg := &Config{}
	cfg.ToolResultPruner.ThresholdChars = 20
	cfg.ToolResultPruner.HeadChars = 5
	cfg.ToolResultPruner.TailChars = 4
	msgs := []Message{
		{Role: "user", Content: "keep"},
		{Role: "tool", Content: "0123456789abcdefghijklmnopqrstuvwxyz"},
		{Role: "tool", Content: "short"},
	}

	pruned := pruneToolResults(msgs, cfg)
	if !pruned {
		t.Fatal("expected pruning to happen")
	}
	got := msgs[1].Content
	if !strings.HasPrefix(got, "01234") || !strings.HasSuffix(got, "wxyz") {
		t.Fatalf("pruned content lost head/tail: %q", got)
	}
	if !strings.Contains(got, "[... tool result middle pruned ...]") {
		t.Fatalf("pruned content missing marker: %q", got)
	}
	if msgs[2].Content != "short" {
		t.Fatalf("under-budget tool result was modified: %q", msgs[2].Content)
	}
	if msgs[0].Content != "keep" {
		t.Fatal("non-tool message was modified")
	}
}

func TestPruneToolResultsSkipsSmallResults(t *testing.T) {
	cfg := &Config{}
	cfg.ToolResultPruner.ThresholdChars = 20
	cfg.ToolResultPruner.HeadChars = 5
	cfg.ToolResultPruner.TailChars = 4
	msgs := []Message{{Role: "tool", Content: "short"}}

	if pruned := pruneToolResults(msgs, cfg); pruned {
		t.Fatal("expected no pruning for under-budget result")
	}
	if msgs[0].Content != "short" {
		t.Fatalf("content changed unexpectedly: %q", msgs[0].Content)
	}
}
