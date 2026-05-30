package llm

import "context"

type ProviderInfo struct {
	Name  string `json:"name"`
	Model string `json:"model"`
}

type providerCallbackKey struct{}

func WithProviderCallback(ctx context.Context, callback func(ProviderInfo)) context.Context {
	if callback == nil {
		return ctx
	}
	return context.WithValue(ctx, providerCallbackKey{}, callback)
}

func NotifyProvider(ctx context.Context, info ProviderInfo) {
	callback, ok := ctx.Value(providerCallbackKey{}).(func(ProviderInfo))
	if !ok || callback == nil {
		return
	}
	callback(info)
}
