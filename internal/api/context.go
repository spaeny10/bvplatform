package api

import "context"

// contextWithValue is a thin wrapper to avoid a lint warning about using
// built-in string keys as context keys.
func contextWithValue(ctx context.Context, key, val any) context.Context {
	return context.WithValue(ctx, key, val)
}
