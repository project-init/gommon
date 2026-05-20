package grpc

import (
	"context"
	"path"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

const fakeFullMethod = "/test.v1.Service/Method"

// invokeStubbed runs req through Stubbed() and returns the resulting response/error
// pair. It uses a no-op handler since the middleware never invokes it on the
// stubbed path.
func invokeStubbed(t *testing.T, m *Middleware, req proto.Message) (interface{}, error) {
	t.Helper()
	interceptor := m.Stubbed()
	info := &grpc.UnaryServerInfo{FullMethod: fakeFullMethod}
	handler := func(ctx context.Context, r interface{}) (interface{}, error) {
		t.Fatalf("handler should not be called on the stubbed path")
		return nil, nil
	}
	return interceptor(context.Background(), req, info, handler)
}

func newTestMiddleware(t *testing.T, errorProtos map[string][]func() proto.Message) (*Middleware, string) {
	t.Helper()
	tempDir := t.TempDir()
	methodsToProtos := map[string]func() proto.Message{
		fakeFullMethod: func() proto.Message { return &emptypb.Empty{} },
	}
	if errorProtos == nil {
		return NewMiddleware(methodsToProtos, tempDir), tempDir
	}
	return NewMiddlewareWithErrorProtos(methodsToProtos, errorProtos, tempDir), tempDir
}

// fixtureDir returns the directory layout the middleware expects for the fake
// method, and ensures it exists for WriteStubFile.
func fixtureDir(t *testing.T, baseDir string) string {
	t.Helper()
	// info.FullMethod "/test.v1.Service/Method" -> "/test/v1/Service/Method"
	rel := "/test/v1/Service/Method"
	return path.Join(baseDir, rel)
}

func Test_Stubbed_ErrorDetails_Honored_When_Registered(t *testing.T) {
	errorProtos := map[string][]func() proto.Message{
		fakeFullMethod: {
			func() proto.Message { return &errdetails.BadRequest{} },
		},
	}
	m, baseDir := newTestMiddleware(t, errorProtos)

	st, detailErr := status.New(codes.InvalidArgument, "invalid argument").
		WithDetails(&errdetails.BadRequest{
			FieldViolations: []*errdetails.BadRequest_FieldViolation{
				{Field: "userId", Description: "must be positive"},
			},
		})
	require.NoError(t, detailErr)

	require.NoError(t, WriteStubFile(&emptypb.Empty{}, nil, st, fixtureDir(t, baseDir)))

	resp, err := invokeStubbed(t, m, &emptypb.Empty{})
	require.Nil(t, resp)
	require.Error(t, err)

	gotStatus, ok := status.FromError(err)
	require.True(t, ok, "returned error must be a gRPC status")
	assert.Equal(t, codes.InvalidArgument, gotStatus.Code())
	assert.Equal(t, "invalid argument", gotStatus.Message())

	details := gotStatus.Details()
	require.Len(t, details, 1)
	br, ok := details[0].(*errdetails.BadRequest)
	require.True(t, ok, "expected first detail to be *errdetails.BadRequest, got %T", details[0])
	require.Len(t, br.FieldViolations, 1)
	assert.Equal(t, "userId", br.FieldViolations[0].Field)
	assert.Equal(t, "must be positive", br.FieldViolations[0].Description)
}

func Test_Stubbed_ErrorDetails_Dropped_When_Method_Not_Registered(t *testing.T) {
	// Use NewMiddleware (no error proto registry) — details on disk should be ignored.
	m, baseDir := newTestMiddleware(t, nil)

	st, detailErr := status.New(codes.InvalidArgument, "invalid argument").
		WithDetails(&errdetails.BadRequest{
			FieldViolations: []*errdetails.BadRequest_FieldViolation{
				{Field: "userId", Description: "must be positive"},
			},
		})
	require.NoError(t, detailErr)

	require.NoError(t, WriteStubFile(&emptypb.Empty{}, nil, st, fixtureDir(t, baseDir)))

	resp, err := invokeStubbed(t, m, &emptypb.Empty{})
	require.Nil(t, resp)
	require.Error(t, err)

	gotStatus, ok := status.FromError(err)
	require.True(t, ok)
	// Code and message still propagate.
	assert.Equal(t, codes.InvalidArgument, gotStatus.Code())
	assert.Equal(t, "invalid argument", gotStatus.Message())
	// Details silently dropped.
	assert.Empty(t, gotStatus.Details())
}

func Test_Stubbed_BasicError_NoDetails_Still_Works(t *testing.T) {
	// Regression guard: a fixture without errorDetails must still produce the
	// correct code+message.
	m, baseDir := newTestMiddleware(t, nil)

	st := status.New(codes.NotFound, "user not found")
	require.NoError(t, WriteStubFile(&emptypb.Empty{}, nil, st, fixtureDir(t, baseDir)))

	resp, err := invokeStubbed(t, m, &emptypb.Empty{})
	require.Nil(t, resp)
	require.Error(t, err)

	gotStatus, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, gotStatus.Code())
	assert.Equal(t, "user not found", gotStatus.Message())
	assert.Empty(t, gotStatus.Details())
}

func Test_Stubbed_ErrorDetails_FirstParsableCandidateWins(t *testing.T) {
	// BadRequest is registered before ErrorInfo. A BadRequest-shaped detail
	// should match BadRequest; an ErrorInfo-shaped detail should fall through
	// to ErrorInfo.
	errorProtos := map[string][]func() proto.Message{
		fakeFullMethod: {
			func() proto.Message { return &errdetails.BadRequest{} },
			func() proto.Message { return &errdetails.ErrorInfo{} },
		},
	}
	m, baseDir := newTestMiddleware(t, errorProtos)

	st, detailErr := status.New(codes.InvalidArgument, "invalid").
		WithDetails(
			&errdetails.BadRequest{
				FieldViolations: []*errdetails.BadRequest_FieldViolation{
					{Field: "x"},
				},
			},
			&errdetails.ErrorInfo{
				Reason: "USER_DELETED",
				Domain: "users.v1",
			},
		)
	require.NoError(t, detailErr)

	require.NoError(t, WriteStubFile(&emptypb.Empty{}, nil, st, fixtureDir(t, baseDir)))

	_, err := invokeStubbed(t, m, &emptypb.Empty{})
	require.Error(t, err)

	gotStatus, _ := status.FromError(err)
	details := gotStatus.Details()
	require.Len(t, details, 2)
	_, ok := details[0].(*errdetails.BadRequest)
	assert.True(t, ok, "first detail should be *errdetails.BadRequest, got %T", details[0])
	ei, ok := details[1].(*errdetails.ErrorInfo)
	require.True(t, ok, "second detail should be *errdetails.ErrorInfo, got %T", details[1])
	assert.Equal(t, "USER_DELETED", ei.Reason)
	assert.Equal(t, "users.v1", ei.Domain)
}

func Test_Stubbed_ErrorDetails_UnmatchedDetailIsDropped(t *testing.T) {
	// Only BadRequest is registered. An ErrorInfo-shaped detail will not
	// strict-unmarshal against BadRequest (unknown fields "reason"/"domain") and
	// should be silently dropped, while a BadRequest-shaped detail still comes
	// through.
	errorProtos := map[string][]func() proto.Message{
		fakeFullMethod: {
			func() proto.Message { return &errdetails.BadRequest{} },
		},
	}
	m, baseDir := newTestMiddleware(t, errorProtos)

	st, detailErr := status.New(codes.InvalidArgument, "invalid").
		WithDetails(
			&errdetails.ErrorInfo{Reason: "X", Domain: "y"},
			&errdetails.BadRequest{
				FieldViolations: []*errdetails.BadRequest_FieldViolation{{Field: "x"}},
			},
		)
	require.NoError(t, detailErr)

	require.NoError(t, WriteStubFile(&emptypb.Empty{}, nil, st, fixtureDir(t, baseDir)))

	_, err := invokeStubbed(t, m, &emptypb.Empty{})
	require.Error(t, err)

	gotStatus, _ := status.FromError(err)
	details := gotStatus.Details()
	require.Len(t, details, 1, "expected unmatched ErrorInfo to be dropped, only BadRequest kept")
	_, ok := details[0].(*errdetails.BadRequest)
	assert.True(t, ok)
}

func Test_Stubbed_SuccessResponseStillWorks(t *testing.T) {
	// Regression guard: a success fixture (no errorCode/errorMessage) must
	// still unmarshal the response.
	m, baseDir := newTestMiddleware(t, nil)

	require.NoError(t, WriteStubFile(&emptypb.Empty{}, &emptypb.Empty{}, nil, fixtureDir(t, baseDir)))

	resp, err := invokeStubbed(t, m, &emptypb.Empty{})
	require.NoError(t, err)
	_, ok := resp.(*emptypb.Empty)
	assert.True(t, ok, "expected *emptypb.Empty response, got %T", resp)
}
