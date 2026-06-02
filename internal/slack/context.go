package slack

import "context"

func workflowContext(ctx context.Context) context.Context {
	return context.WithoutCancel(ctx)
}
