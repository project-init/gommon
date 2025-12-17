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
)

type Middleware struct {
	methodsToProtos map[string]func() proto.Message
	dataDir         string
}

func NewMiddleware(methodsToProtos map[string]func() proto.Message, dataDir string) *Middleware {
	return &Middleware{
		methodsToProtos: methodsToProtos,
		dataDir:         dataDir,
	}
}

func (m *Middleware) Stubbed() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
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
		protoResponse, protoError, err := responseFromFile(hashPath, protoFunc())
		if err != nil {
			return nil, status.Error(gerror.GrpcFromError(err), fmt.Sprintf("failed to call method %s: %s", info.FullMethod, err))
		}

		return protoResponse, protoError
	}
}

func responseFromFile(hashPath string, protoFunc proto.Message) (proto.Message, error, error) {
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
		return nil, status.Error(codes.Code(int32(errorCode.Int())), errorMessage.String()), nil
	}

	resp := gjson.Get(string(bytes), "response")
	if err = protojson.Unmarshal([]byte(resp.Raw), protoFunc); err != nil {
		return nil, nil, fmt.Errorf("%w: failed to unmarshal response at %s", err, hashPath)
	}
	return protoFunc, nil, nil
}
