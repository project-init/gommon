package grpc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	gerror "github.com/project-init/gommon/pkg/errors"

	"github.com/tidwall/gjson"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/protoadapt"
)

type Middleware struct {
	methodsToProtos     map[string]func() proto.Message
	methodToErrorProtos map[string][]func() proto.Message
	dataDir             string
}

func NewMiddleware(methodsToProtos map[string]func() proto.Message, dataDir string) *Middleware {
	return &Middleware{
		methodsToProtos: methodsToProtos,
		dataDir:         dataDir,
	}
}

// NewMiddlewareWithErrorProtos creates a Middleware that additionally honors errorDetails entries in stub fixtures.
//
// methodToErrorProtos maps a full gRPC method path (e.g., "/users.v1.UsersService/GetUser") to a list of zero-arg
// constructors for the proto types that method may return as rich error details. At read time, each errorDetails
// JSON entry is unmarshaled (strict, DiscardUnknown: false) against the registered candidates in order; the first
// successful match is attached to the returned status via WithDetails. Methods absent from the map have their
// errorDetails silently dropped.
//
// See https://grpc.io/docs/guides/error/#richer-error-model.
func NewMiddlewareWithErrorProtos(methodsToProtos map[string]func() proto.Message, methodToErrorProtos map[string][]func() proto.Message, dataDir string) *Middleware {
	return &Middleware{
		methodsToProtos:     methodsToProtos,
		methodToErrorProtos: methodToErrorProtos,
		dataDir:             dataDir,
	}
}

func (m *Middleware) Stubbed() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		jsonReq, err := protojson.MarshalOptions{EmitUnpopulated: true}.Marshal(req.(proto.Message))
		if err != nil {
			return nil, status.Error(codes.Internal, "failed to marshal json request")
		}

		hash, err := GetStubHash(jsonReq)
		if err != nil {
			return nil, status.Error(codes.Internal, "unable to hash json request")
		}

		methodPath := strings.ReplaceAll(info.FullMethod, ".", "/")
		protoFunc, ok := m.methodsToProtos[info.FullMethod]
		if !ok {
			return nil, status.Error(codes.Unimplemented, fmt.Sprintf("method not found for %s", info.FullMethod))
		}

		fullMethodPath := path.Join(m.dataDir, methodPath)
		hashPath := path.Join(fullMethodPath, fmt.Sprintf("%s.json", hash))
		protoResponse, protoError, err := m.responseFromFile(hashPath, info.FullMethod, protoFunc())
		if err != nil {
			return nil, status.Error(
				gerror.GrpcFromError(err),
				fmt.Sprintf("failed to call method %s: %s", info.FullMethod, err),
			)
		}

		return protoResponse, protoError
	}
}

func (m *Middleware) responseFromFile(hashPath string, fullMethod string, protoFunc proto.Message) (proto.Message, error, error) {
	_, err := os.Stat(hashPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, fmt.Errorf("%w: file not found at %s", gerror.ErrNotFound, hashPath)
	}

	bytes, err := os.ReadFile(hashPath)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: failed to read file at %s", err, hashPath)
	}

	errorCode, errorMessage := gjson.GetBytes(bytes, "errorCode"), gjson.GetBytes(bytes, "errorMessage")
	if errorCode.Exists() && errorMessage.Exists() {
		st := status.New(codes.Code(int32(errorCode.Int())), errorMessage.String())
		errorDetails := gjson.GetBytes(bytes, "errorDetails")

		if errorDetails.Exists() && errorDetails.IsArray() && len(errorDetails.Array()) > 0 {
			st, _ = st.WithDetails(m.extractErrorDetails(fullMethod, errorDetails)...)
		}
		return nil, st.Err(), nil
	}

	resp := gjson.Get(string(bytes), "response")
	if err = protojson.Unmarshal([]byte(resp.Raw), protoFunc); err != nil {
		return nil, nil, fmt.Errorf("%w: failed to unmarshal response at %s", err, hashPath)
	}
	return protoFunc, nil, nil
}

// extractErrorDetails attempts to unmarshal each errorDetails JSON entry against the proto candidates registered
// for fullMethod. The first candidate that strict-unmarshals (DiscardUnknown: false) wins. Entries with no matching
// candidate, or methods absent from the registry, are silently dropped.
func (m *Middleware) extractErrorDetails(fullMethod string, errorDetails gjson.Result) []protoadapt.MessageV1 {
	var details []protoadapt.MessageV1

	candidates, ok := m.methodToErrorProtos[fullMethod]
	if !ok {
		return details
	}

	for _, detailJSON := range errorDetails.Array() {
		for _, newMsg := range candidates {
			msg := newMsg()
			unmarshalErr := protojson.UnmarshalOptions{DiscardUnknown: false}.Unmarshal([]byte(detailJSON.Raw), msg)
			if unmarshalErr == nil {
				details = append(details, protoadapt.MessageV1Of(msg))
				break
			}
		}
	}

	return details
}
