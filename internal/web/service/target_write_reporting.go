package service

import "context"

type targetWriteStartCallbackContextKey struct{}

func withTargetWriteStartCallback(ctx context.Context, callback func(string) error) context.Context {
	if callback == nil {
		return ctx
	}
	return context.WithValue(ctx, targetWriteStartCallbackContextKey{}, callback)
}

func reportTargetWriteStarting(ctx context.Context, assetName string) error {
	if ctx == nil {
		return nil
	}
	callback, _ := ctx.Value(targetWriteStartCallbackContextKey{}).(func(string) error)
	if callback == nil {
		return nil
	}
	return callback(assetName)
}
