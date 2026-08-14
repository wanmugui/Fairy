package tool

import (
	"agentloop/agent/internal/dtypes"
	"context"
	"sync"
)

type Dispatcher struct {
	Registry       *Registry
	MaxConcurrency int
}

func (d *Dispatcher) Execute(ctx context.Context, invocations []dtypes.ToolInvocation) []dtypes.ToolInvocationResult {
	results := make([]dtypes.ToolInvocationResult, len(invocations))
	maxConcurrency := d.MaxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = 1
	}
	semaphore := make(chan struct{}, maxConcurrency)

	if len(invocations) == 0 {
		return results
	}
	var waitGroup sync.WaitGroup
	waitGroup.Add(len(invocations))
	for index, invocation := range invocations {
		results[index] = dtypes.ToolInvocationResult{
			Index:  invocation.Index,
			CallID: invocation.CallID,
			Name:   invocation.Name,
		}
		go func(index int, invocation dtypes.ToolInvocation) {
			defer waitGroup.Done()

			if err := ctx.Err(); err != nil {
				results[index].Err = err
				return
			}
			select {
			case <-ctx.Done():
				results[index].Err = ctx.Err()
				return
			case semaphore <- struct{}{}:
			}
			defer func() { <-semaphore }()

			if err := ctx.Err(); err != nil {
				results[index].Err = err
				return
			}
			if d.Registry == nil {
				results[index].Result = dtypes.ToolResult{
					Value:   map[string]any{"error": "tool registry is unavailable", "tool": invocation.Name},
					IsError: true,
				}
				return
			}
			tool, ok := d.Registry.Get(invocation.Name)
			if !ok {
				results[index].Result = dtypes.ToolResult{
					Value:   map[string]any{"error": "no tool registered", "tool": invocation.Name},
					IsError: true,
				}
				return
			}
			results[index].Result, results[index].Err = tool.Execute(ctx, invocation)
		}(index, invocation)
	}

	waitGroup.Wait()
	return results
}
