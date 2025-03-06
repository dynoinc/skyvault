package middleware

import (
	"context"
	"log/slog"

	"connectrpc.com/connect"
)

// LogErrors returns a connect.Interceptor that logs any RPC errors to stderr.
func LogErrors() connect.UnaryInterceptorFunc {
	interceptor := func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			resp, err := next(ctx, req)
			if err != nil {
				// Get information about the failed request
				procedure := req.Spec().Procedure
				connectErr, ok := err.(*connect.Error)

				// Log the error with relevant details
				if ok {
					slog.ErrorContext(ctx, "RPC failed",
						"procedure", procedure,
						"code", connectErr.Code(),
						"error", connectErr.Message(),
					)
				} else {
					slog.ErrorContext(ctx, "RPC failed with non-connect error",
						"procedure", procedure,
						"error", err.Error(),
					)
				}
			}

			return resp, err
		}
	}
	return interceptor
}
