package check

import "context"

// Lookup returns what get finds under id, or missing, the error naming
// nothing, when get finds nothing there.
func Lookup[T any](ctx context.Context, id int64, get func(context.Context, int64) (T, bool, error), missing error) (T, error) {
	v, found, err := get(ctx, id)
	if err != nil {
		var zero T
		return zero, err
	}
	if !found {
		var zero T
		return zero, missing
	}
	return v, nil
}
