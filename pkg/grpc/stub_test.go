package grpc

import (
	"encoding/json"
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

func Test_WriteStubFile(t *testing.T) {
	// MD5 of the canonicalized JSON for an Empty proto ({}).
	const expectedHashFileName = "99914b932bd37a50b983c5e7c90ae93b.json"
	request := &emptypb.Empty{}
	response := &emptypb.Empty{}

	t.Run("success fixture: nil status writes request and response, no error fields", func(t *testing.T) {
		tempDir := t.TempDir()

		err := WriteStubFile(request, response, nil, tempDir)
		require.NoError(t, err)

		filePath := path.Join(tempDir, expectedHashFileName)
		b, err := os.ReadFile(filePath)
		require.NoError(t, err, "expected stub file to exist")

		assert.True(t, gjson.GetBytes(b, "request").Exists())
		assert.True(t, gjson.GetBytes(b, "response").Exists())
		assert.False(t, gjson.GetBytes(b, "errorCode").Exists())
		assert.False(t, gjson.GetBytes(b, "errorMessage").Exists())
		assert.False(t, gjson.GetBytes(b, "errorDetails").Exists())
	})

	t.Run("success fixture: codes.OK status is treated as success", func(t *testing.T) {
		tempDir := t.TempDir()

		err := WriteStubFile(request, response, status.New(codes.OK, ""), tempDir)
		require.NoError(t, err)

		filePath := path.Join(tempDir, expectedHashFileName)
		b, err := os.ReadFile(filePath)
		require.NoError(t, err)

		assert.True(t, gjson.GetBytes(b, "response").Exists())
		assert.False(t, gjson.GetBytes(b, "errorCode").Exists())
	})

	t.Run("success fixture: nil response returns error", func(t *testing.T) {
		tempDir := t.TempDir()

		err := WriteStubFile(request, nil, nil, tempDir)
		assert.Error(t, err)
	})

	t.Run("error fixture: writes errorCode and errorMessage, no response", func(t *testing.T) {
		tempDir := t.TempDir()
		st := status.New(codes.NotFound, "user not found")

		err := WriteStubFile(request, response, st, tempDir)
		require.NoError(t, err)

		filePath := path.Join(tempDir, expectedHashFileName)
		b, err := os.ReadFile(filePath)
		require.NoError(t, err)

		errorCode := gjson.GetBytes(b, "errorCode")
		errorMessage := gjson.GetBytes(b, "errorMessage")
		assert.True(t, errorCode.Exists())
		assert.True(t, errorMessage.Exists())
		assert.Equal(t, int64(codes.NotFound), errorCode.Int())
		assert.Equal(t, "user not found", errorMessage.String())

		// response is silently ignored when status is a non-OK error.
		assert.False(t, gjson.GetBytes(b, "response").Exists())
		// errorDetails is omitted when there are none (omitempty).
		assert.False(t, gjson.GetBytes(b, "errorDetails").Exists())
	})

	t.Run("error fixture: errorDetails are written when status carries them", func(t *testing.T) {
		tempDir := t.TempDir()

		st, detailErr := status.New(codes.InvalidArgument, "invalid argument").
			WithDetails(&errdetails.BadRequest{
				FieldViolations: []*errdetails.BadRequest_FieldViolation{
					{Field: "userId", Description: "must be positive"},
				},
			})
		require.NoError(t, detailErr)

		err := WriteStubFile(request, nil, st, tempDir)
		require.NoError(t, err)

		filePath := path.Join(tempDir, expectedHashFileName)
		b, err := os.ReadFile(filePath)
		require.NoError(t, err)

		details := gjson.GetBytes(b, "errorDetails")
		require.True(t, details.Exists())
		require.True(t, details.IsArray())
		require.Equal(t, 1, len(details.Array()))

		assert.Equal(t, "userId", details.Array()[0].Get("fieldViolations.0.field").String())
		assert.Equal(t, "must be positive", details.Array()[0].Get("fieldViolations.0.description").String())
	})

	t.Run("error fixture: errorMessage with quotes and newlines produces valid JSON", func(t *testing.T) {
		tempDir := t.TempDir()
		// Regression test against the unsafe string-concat behavior in the deprecated RequestAndResponseToFile.
		nasty := "broke it: \"quote\", backslash \\ and\nnewline"
		st := status.New(codes.Internal, nasty)

		err := WriteStubFile(request, nil, st, tempDir)
		require.NoError(t, err)

		filePath := path.Join(tempDir, expectedHashFileName)
		b, err := os.ReadFile(filePath)
		require.NoError(t, err)

		// File must parse as valid JSON.
		var generic map[string]any
		require.NoError(t, json.Unmarshal(b, &generic))

		// And the message round-trips exactly.
		assert.Equal(t, nasty, gjson.GetBytes(b, "errorMessage").String())
	})

	t.Run("nil request returns error", func(t *testing.T) {
		tempDir := t.TempDir()

		err := WriteStubFile(nil, response, nil, tempDir)
		assert.Error(t, err)
	})
}
