package ai

import "context"

// ExecuteRequest and ExecuteResult use the same contract as Run.
type ExecuteRequest = RunRequest
type ExecuteResult = RunResult

// Execute is a compatibility entry point; Run owns the only execution loop.
func (g *Gateway) Execute(ctx context.Context, who Caller, in ExecuteRequest, executor ToolExecutor) (*ExecuteResult, error) {
	return g.Run(ctx, who, in, executor)
}
