package mobilecompat

import (
	"context"
	"errors"
	"log/slog"
	"strconv"

	"connectrpc.com/connect"
)

const (
	UpdateScopeGlobal   = "global"
	UpdateScopeEndpoint = "endpoint"
)

type Requirement struct {
	Scope          string
	MinimumVersion string
	MinimumBuild   int64
	StoreURL       string
}

type Policy interface {
	IsExempt(procedure string) bool
	Global(platform Platform) *Requirement
	Endpoint(platform Platform, procedure string) *Requirement
}

func NewInterceptor(policy Policy) connect.Interceptor {
	return connect.UnaryInterceptorFunc(
		func(next connect.UnaryFunc) connect.UnaryFunc {
			return func(ctx context.Context, request connect.AnyRequest) (connect.AnyResponse, error) {
				client, err := ParseHeaders(request.Header())
				if err != nil {
					return nil, connect.NewError(connect.CodeInvalidArgument, err)
				}

				if client == nil || policy.IsExempt(request.Spec().Procedure) {
					return next(ctx, request)
				}

				req, err := evaluateRequirement(policy, client, request.Spec().Procedure)
				if err != nil {
					// "Unexpected evaluation errors are logged and requests continue."
					slog.Error("mobilecompat: unexpected error evaluating client update requirement", "err", err)
					return next(ctx, request)
				}

				if req != nil {
					return nil, requiredUpdateError(req)
				}

				return next(ctx, request)
			}
		},
	)
}

func evaluateRequirement(
	policy Policy,
	client *Client,
	procedure string,
) (*Requirement, error) {
	global := policy.Global(client.Platform)
	if global == nil {
		return nil, nil // No global rules
	}

	belowGlobal, err := belowMinimum(client, global.MinimumVersion, global.MinimumBuild)
	if err != nil {
		return nil, err
	}
	if belowGlobal {
		global.Scope = UpdateScopeGlobal
		return global, nil
	}

	endpoint := policy.Endpoint(client.Platform, procedure)
	if endpoint == nil {
		return nil, nil
	}

	belowEndpoint, err := belowMinimum(client, endpoint.MinimumVersion, endpoint.MinimumBuild)
	if err != nil {
		return nil, err
	}
	if belowEndpoint {
		endpoint.Scope = UpdateScopeEndpoint
		return endpoint, nil
	}

	return nil, nil
}

func belowMinimum(client *Client, minimumVersion string, minimumBuild int64) (bool, error) {
	switch client.Platform {
	case PlatformIOS:
		return IOSBelowMinimum(client.Version, client.Build, minimumVersion, minimumBuild)
	case PlatformAndroid:
		return AndroidBelowMinimum(client.Build, minimumBuild), nil
	default:
		return false, nil
	}
}

func requiredUpdateError(req *Requirement) error {
	connectError := connect.NewError(
		connect.CodeFailedPrecondition,
		errors.New("mobile app update required"),
	)
	connectError.Meta().Set(HeaderUpdateScope, req.Scope)
	connectError.Meta().Set(HeaderMinimumVersion, req.MinimumVersion)
	connectError.Meta().Set(HeaderMinimumBuild, strconv.FormatInt(req.MinimumBuild, 10))
	connectError.Meta().Set(HeaderStoreURL, req.StoreURL)
	return connectError
}
