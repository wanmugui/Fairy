package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
)

func NewLocalTodoCreateTool(schema ToolDef) Tool {
	return newLocalStructuredTool("todolist_create", schema, func(ctx context.Context, invocation ToolInvocation) (ToolResult, error) {
		return executeLocalTodoCreate(ctx, invocation)
	})
}

func NewLocalTodoAppendTool(schema ToolDef) Tool {
	return newLocalStructuredTool("todolist_append", schema, func(ctx context.Context, invocation ToolInvocation) (ToolResult, error) {
		return executeLocalTodoAppend(ctx, invocation)
	})
}

func NewLocalTodoUpdateTool(schema ToolDef) Tool {
	return newLocalStructuredTool("todolist_update", schema, func(ctx context.Context, invocation ToolInvocation) (ToolResult, error) {
		return executeLocalTodoUpdate(ctx, invocation)
	})
}

func NewLocalListTodosTool(schema ToolDef) Tool {
	return newLocalStructuredTool("list_todos", schema, func(ctx context.Context, invocation ToolInvocation) (ToolResult, error) {
		return executeLocalListTodos(ctx, invocation)
	})
}

func executeLocalTodoCreate(ctx context.Context, invocation ToolInvocation) (ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return ToolResult{}, err
	}
	args, err := decodeLocalToolArgs(invocation)
	if err != nil {
		return localErrorResult("todolist_create", err), nil
	}
	inputs, err := parseLocalTodoInputs(args["todo_list"])
	if err != nil {
		return localErrorResult("todolist_create", err), nil
	}
	store, err := localTodoStoreForInvocation(invocation)
	if err != nil {
		return localErrorResult("todolist_create", err), nil
	}
	data, err := store.Create(localStringArg(args, "date"), inputs)
	if err != nil {
		return localErrorResult("todolist_create", err), nil
	}
	return ToolResult{Value: map[string]any{
		"ok":            true,
		"path":          store.path(data.Date),
		"date":          data.Date,
		"items_created": len(data.Items),
	}}, nil
}

func executeLocalTodoAppend(ctx context.Context, invocation ToolInvocation) (ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return ToolResult{}, err
	}
	args, err := decodeLocalToolArgs(invocation)
	if err != nil {
		return localErrorResult("todolist_append", err), nil
	}
	inputs, err := parseLocalTodoInputs(args["todo_list"])
	if err != nil {
		return localErrorResult("todolist_append", err), nil
	}
	store, err := localTodoStoreForInvocation(invocation)
	if err != nil {
		return localErrorResult("todolist_append", err), nil
	}
	data, err := store.Append(localStringArg(args, "date"), inputs)
	if err != nil {
		return localErrorResult("todolist_append", err), nil
	}
	return ToolResult{Value: map[string]any{
		"ok":            true,
		"path":          store.path(data.Date),
		"items_created": len(inputs),
		"total_items":   len(data.Items),
	}}, nil
}

func executeLocalTodoUpdate(ctx context.Context, invocation ToolInvocation) (ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return ToolResult{}, err
	}
	args, err := decodeLocalToolArgs(invocation)
	if err != nil {
		return localErrorResult("todolist_update", err), nil
	}
	updates, err := parseLocalTodoUpdates(args["updates"])
	if err != nil {
		return localErrorResult("todolist_update", err), nil
	}
	store, err := localTodoStoreForInvocation(invocation)
	if err != nil {
		return localErrorResult("todolist_update", err), nil
	}
	data, err := store.Update(localStringArg(args, "date"), updates)
	if err != nil {
		return localErrorResult("todolist_update", err), nil
	}
	return ToolResult{Value: map[string]any{
		"ok":              true,
		"path":            store.path(data.Date),
		"updates_applied": len(updates),
		"total_items":     len(data.Items),
	}}, nil
}

func executeLocalListTodos(ctx context.Context, invocation ToolInvocation) (ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return ToolResult{}, err
	}
	args, err := decodeLocalToolArgs(invocation)
	if err != nil {
		return localErrorResult("list_todos", err), nil
	}
	store, err := localTodoStoreForInvocation(invocation)
	if err != nil {
		return localErrorResult("list_todos", err), nil
	}
	data, err := store.Read(localStringArg(args, "date"))
	if err != nil {
		if os.IsNotExist(err) {
			return localErrorResult("list_todos", fmt.Errorf("no todos found for %s at %s", store.normalizedDate(localStringArg(args, "date")), store.path(store.normalizedDate(localStringArg(args, "date"))))), nil
		}
		return localErrorResult("list_todos", err), nil
	}
	openItems := make([]map[string]any, 0)
	for _, item := range data.Items {
		if item.Done {
			continue
		}
		openItems = append(openItems, map[string]any{
			"id":       item.ID,
			"title":    item.Title,
			"priority": item.Priority,
			"done":     item.Done,
		})
	}
	return ToolResult{Value: map[string]any{
		"date":    data.Date,
		"message": fmt.Sprintf("%d open / %d total", len(openItems), len(data.Items)),
		"total":   len(openItems),
		"items":   openItems,
	}}, nil
}

func localTodoStoreForInvocation(invocation ToolInvocation) (*LocalTodoStore, error) {
	localContext, err := localToolContext(invocation)
	if err != nil {
		return nil, err
	}
	return NewLocalTodoStore(localContext.Workspace, nil)
}

func parseLocalTodoInputs(raw any) ([]LocalTodoInput, error) {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return nil, fmt.Errorf("todo_list is empty")
	}
	inputs := make([]LocalTodoInput, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		inputs = append(inputs, LocalTodoInput{
			ID:          localTodoID(object["task_id"]),
			Description: localStringArg(object, "description", "title"),
		})
	}
	if len(inputs) == 0 {
		return nil, fmt.Errorf("todo_list is empty")
	}
	return inputs, nil
}

func parseLocalTodoUpdates(raw any) ([]LocalTodoUpdate, error) {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return nil, fmt.Errorf("updates is empty")
	}
	updates := make([]LocalTodoUpdate, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		result, _ := object["result"].(string)
		updates = append(updates, LocalTodoUpdate{
			ID:          localTodoID(object["task_id"]),
			Action:      localStringArg(object, "action"),
			Description: localStringArg(object, "description", "title"),
			Result:      result,
		})
	}
	if len(updates) == 0 {
		return nil, fmt.Errorf("updates is empty")
	}
	return updates, nil
}

func localTodoID(value any) string {
	switch value := value.(type) {
	case string:
		return value
	case float64:
		return strconv.Itoa(int(value))
	case json.Number:
		return value.String()
	case int:
		return strconv.Itoa(value)
	default:
		return ""
	}
}
