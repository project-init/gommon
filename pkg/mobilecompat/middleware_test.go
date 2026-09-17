//go:build unit_test

package mobilecompat

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockPolicy struct {
	exempt   bool
	global   *Requirement
	endpoint *Requirement
}

func (m *mockPolicy) IsExempt(procedure string) bool {
	return m.exempt
}
func (m *mockPolicy) Global(platform Platform) *Requirement {
	return m.global
}
func (m *mockPolicy) Endpoint(platform Platform, procedure string) *Requirement {
	return m.endpoint
}

func TestInterceptor(t *testing.T) {
	policy := &mockPolicy{}
	interceptor := NewInterceptor(policy)
	unaryFunc := interceptor.WrapUnary(func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		return nil, nil // Represents successful handler execution
	})

	t.Run("MissingHeaders", func(t *testing.T) {
		req := connect.NewRequest(&struct{}{})
		_, err := unaryFunc(context.Background(), req)
		// Usually if optional these pass, but ParseHeaders errors if partially incomplete, or returns nil if absent.
		// Wait, ParseHeaders returns (nil, nil) if completely absent!
		require.NoError(t, err) // No headers at all means pass gracefully!
	})

	t.Run("IncompleteHeaders", func(t *testing.T) {
		req := connect.NewRequest(&struct{}{})
		req.Header().Set(HeaderPlatform, "ios") // Missing version/build
		_, err := unaryFunc(context.Background(), req)
		require.Error(t, err)
		assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	})

	t.Run("ExemptProcedure", func(t *testing.T) {
		policy.exempt = true
		req := connect.NewRequest(&struct{}{})
		req.Header().Set(HeaderPlatform, "ios")
		req.Header().Set(HeaderVersion, "1.0.0")
		req.Header().Set(HeaderBuild, "10")

		_, err := unaryFunc(context.Background(), req)
		require.NoError(t, err) // Block normally if policy was strict, but it's exempt
		policy.exempt = false
	})

	t.Run("FailsGlobalRequirement", func(t *testing.T) {
		policy.global = &Requirement{
			MinimumVersion: "2.0.0",
			MinimumBuild:   50,
			StoreURL:       "https://apple.com/app",
		}

		req := connect.NewRequest(&struct{}{})
		req.Header().Set(HeaderPlatform, "ios")
		req.Header().Set(HeaderVersion, "1.0.0") // 1.0.0 < 2.0.0
		req.Header().Set(HeaderBuild, "10")

		_, err := unaryFunc(context.Background(), req)
		require.Error(t, err)

		var connectErr *connect.Error
		require.True(t, errors.As(err, &connectErr))
		assert.Equal(t, connect.CodeFailedPrecondition, connectErr.Code())
		assert.Equal(t, "global", connectErr.Meta().Get(HeaderUpdateScope))
		assert.Equal(t, "2.0.0", connectErr.Meta().Get(HeaderMinimumVersion))
		assert.Equal(t, "50", connectErr.Meta().Get(HeaderMinimumBuild))
		assert.Equal(t, "https://apple.com/app", connectErr.Meta().Get(HeaderStoreURL))
	})

	t.Run("PassesGlobalFailsEndpoint", func(t *testing.T) {
		policy.global = &Requirement{
			MinimumVersion: "1.0.0",
			MinimumBuild:   10,
		}
		policy.endpoint = &Requirement{
			MinimumVersion: "2.0.0",
			MinimumBuild:   50,
			StoreURL:       "https://google.com/app", // Custom endpoint URL override
		}

		req := connect.NewRequest(&struct{}{})
		req.Header().Set(HeaderPlatform, "android")
		req.Header().Set(HeaderVersion, "1.5.0")
		req.Header().Set(HeaderBuild, "15") // 15 < 50

		_, err := unaryFunc(context.Background(), req)
		require.Error(t, err)

		var connectErr *connect.Error
		require.True(t, errors.As(err, &connectErr))
		assert.Equal(t, "endpoint", connectErr.Meta().Get(HeaderUpdateScope))
		assert.Equal(t, "50", connectErr.Meta().Get(HeaderMinimumBuild))
	})

	t.Run("PassesAllRequirements", func(t *testing.T) {
		req := connect.NewRequest(&struct{}{})
		req.Header().Set(HeaderPlatform, "ios")
		req.Header().Set(HeaderVersion, "3.0.0")
		req.Header().Set(HeaderBuild, "300")

		_, err := unaryFunc(context.Background(), req)
		require.NoError(t, err)
	})
}
