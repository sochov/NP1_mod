package modem

import "context"

// A detector error or a strict CM-reader error is final for this sample. Never
// bypass an unverifiable owner by falling back to the exclusive getter.
func readByStockCMOwnership[T any](ctx context.Context, detect func(context.Context) (bool, error), alongCM, exclusive func(context.Context) (T, error)) (T, error) {
	present, err := detect(ctx)
	if err != nil {
		var empty T
		return empty, err
	}
	if present {
		return alongCM(ctx)
	}
	return exclusive(ctx)
}
