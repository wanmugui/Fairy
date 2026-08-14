package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

func fixedTodoClock() time.Time {
	return time.Date(2026, time.August, 11, 9, 0, 0, 0, time.FixedZone("CST", 8*60*60))
}

func newTodoTestStore(t *testing.T) *LocalTodoStore {
	t.Helper()
	store, err := NewLocalTodoStore(t.TempDir(), func() time.Time { return fixedTodoClock() })
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestTodoStoreCreatesAndListsItems(t *testing.T) {
	store := newTodoTestStore(t)
	data, err := store.Create("2026-08-11", []LocalTodoInput{
		{ID: "1", Description: "first"},
		{ID: "2", Description: "second"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Items) != 2 || data.Items[0].Title != "first" || data.Items[1].ID != "2" {
		t.Fatalf("unexpected created data: %#v", data)
	}
	read, err := store.Read("2026-08-11")
	if err != nil {
		t.Fatal(err)
	}
	if len(read.Items) != 2 || read.Date != "2026-08-11" {
		t.Fatalf("unexpected read data: %#v", read)
	}
}

func TestTodoStoreAppendsWithoutDroppingExistingItems(t *testing.T) {
	store := newTodoTestStore(t)
	if _, err := store.Create("2026-08-11", []LocalTodoInput{{ID: "1", Description: "first"}}); err != nil {
		t.Fatal(err)
	}
	data, err := store.Append("2026-08-11", []LocalTodoInput{{ID: "2", Description: "second"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Items) != 2 || data.Items[0].ID != "1" || data.Items[1].ID != "2" {
		t.Fatalf("append dropped existing item: %#v", data)
	}
}

func TestTodoStoreUpdatesMatchingIDs(t *testing.T) {
	store := newTodoTestStore(t)
	if _, err := store.Create("2026-08-11", []LocalTodoInput{{ID: "1", Description: "first"}}); err != nil {
		t.Fatal(err)
	}
	data, err := store.Update("2026-08-11", []LocalTodoUpdate{{ID: "1", Action: "finish", Result: "done"}})
	if err != nil {
		t.Fatal(err)
	}
	if !data.Items[0].Done || data.Items[0].Result != "done" {
		t.Fatalf("unexpected update data: %#v", data)
	}
}

func TestTodoStoreUsesAtomicWrite(t *testing.T) {
	workspace := t.TempDir()
	store, err := NewLocalTodoStore(workspace, func() time.Time { return fixedTodoClock() })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create("2026-08-11", []LocalTodoInput{{ID: "1", Description: "first"}}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(workspace, "todos"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "2026-08-11.json" {
		t.Fatalf("temporary files were left behind: %#v", entries)
	}
	info, err := os.Stat(filepath.Join(workspace, "todos", "2026-08-11.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("unexpected todo file permissions: %o", info.Mode().Perm())
	}
}

func TestTodoStoreSerializesConcurrentUpdates(t *testing.T) {
	store := newTodoTestStore(t)
	if _, err := store.Create("2026-08-11", nil); err != nil {
		// Create intentionally rejects an empty checklist; initialize through a
		// single item so the following append operations have a real file.
		if _, err := store.Create("2026-08-11", []LocalTodoInput{{ID: "0", Description: "seed"}}); err != nil {
			t.Fatal(err)
		}
	}

	const workers = 20
	var wait sync.WaitGroup
	wait.Add(workers)
	for index := 1; index <= workers; index++ {
		index := index
		go func() {
			defer wait.Done()
			if _, err := store.Append("2026-08-11", []LocalTodoInput{{ID: strconv.Itoa(index), Description: "task" + strconv.Itoa(index)}}); err != nil {
				t.Errorf("append %d: %v", index, err)
			}
		}()
	}
	wait.Wait()
	data, err := store.Read("2026-08-11")
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Items) != workers+1 {
		t.Fatalf("concurrent append lost items: got %d want %d (%#v)", len(data.Items), workers+1, data.Items)
	}
}

func TestTodoToolsRejectMissingRequiredArguments(t *testing.T) {
	workspace := t.TempDir()
	tool := NewLocalTodoCreateTool(localFileTestSchema("todolist_create"))
	result, err := tool.Execute(context.Background(), ToolInvocation{
		Name:      "todolist_create",
		Workspace: workspace,
		Args:      json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || result.Value["error"] == nil {
		t.Fatalf("expected required-argument error, got %#v", result)
	}
}
