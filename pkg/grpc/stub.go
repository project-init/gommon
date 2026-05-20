package grpc

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func GetStubHash(jsonBytes []byte) (string, error) {
	data := map[string]any{}
	if err := json.Unmarshal(jsonBytes, &data); err != nil {
		return "", fmt.Errorf("%w: failed to unmarshal stub", err)
	}

	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("%w: failed to marshal sorted json", err)
	}

	hashBytes := md5.Sum(jsonBytes)
	return hex.EncodeToString(hashBytes[:]), nil
}

// Deprecated: Prefer WriteStubFile, which accepts a *status.Status (carrying code, message, and rich error details)
// and writes the canonical on-disk format consumed by Middleware.Stubbed().
//
// RequestAndResponseToFile has several known limitations:
//   - It always writes a `response` field, even when errorCode is set. The runtime middleware ignores `response`
//     whenever errorCode is present, so that data is dead bytes on disk.
//   - It does not support errorDetails (the gRPC richer error model).
//   - It builds JSON via string concatenation and does not safely escape the errorMessage value, which can produce
//     invalid JSON for messages containing quotes, backslashes, or newlines.
//
// This function is retained for backward compatibility.
func RequestAndResponseToFile(requestMessage proto.Message, responseMessage proto.Message, errorCode *int, errorMessage *string, requestDirectory string) error {
	requestBytes, err := protojson.MarshalOptions{EmitUnpopulated: true}.Marshal(requestMessage)
	if err != nil {
		return fmt.Errorf("%w: failed to marshal request", err)
	}

	responseBytes, err := protojson.MarshalOptions{EmitUnpopulated: true}.Marshal(responseMessage)
	if err != nil {
		return fmt.Errorf("%w: failed to marshal response", err)
	}

	hash, err := GetStubHash(requestBytes)
	if err != nil {
		return fmt.Errorf("%w: failed to get stub hash", err)
	}

	jsonStrings := []string{
		fmt.Sprintf("\"request\": %s", string(requestBytes)),
		fmt.Sprintf("\"response\": %s", string(responseBytes)),
	}

	if errorCode != nil {
		jsonStrings = append(jsonStrings, fmt.Sprintf("\"errorCode\": %s", strconv.Itoa(*errorCode)))
	}

	if errorMessage != nil {
		jsonStrings = append(jsonStrings, fmt.Sprintf("\"errorMessage\": \"%s\"", *errorMessage))
	}

	// This allows us to pretty format the json
	jsonBytes := fmt.Sprintf("{%s}", strings.Join(jsonStrings, ","))
	var out bytes.Buffer
	err = json.Indent(&out, []byte(jsonBytes), "", "  ")
	if err != nil {
		return fmt.Errorf("%w: failed to indent json", err)
	}

	if err = os.MkdirAll(requestDirectory, os.ModePerm); err != nil {
		return fmt.Errorf("%w: failed to create directory", err)
	}

	outputFilePath := fmt.Sprintf("%s/%s.json", requestDirectory, hash)
	return WriteToFileAndSync(outputFilePath, out.Bytes(), 0644)
}

// WriteStubFile writes a stub fixture for the given request to <requestDirectory>/<md5(canonicalized request)>.json.
//
// The fixture format depends on st:
//
//   - If st is nil or st.Code() == codes.OK, a success fixture is written containing `request` and `response`.
//     responseMessage must be non-nil.
//
//   - If st is non-nil and st.Code() != codes.OK, an error fixture is written containing `request`, `errorCode`,
//     `errorMessage`, and (if any are present on the status) `errorDetails`. The responseMessage parameter is
//     IGNORED in this case, matching the runtime behavior of Middleware.Stubbed(): when errorCode is set on disk,
//     the response field is never read.
//
// The on-disk format is the canonical format consumed by Middleware.Stubbed().
func WriteStubFile(
	requestMessage proto.Message,
	responseMessage proto.Message,
	st *status.Status,
	requestDirectory string,
) error {
	if requestMessage == nil {
		return fmt.Errorf("requestMessage must not be nil")
	}

	requestBytes, err := protojson.MarshalOptions{EmitUnpopulated: true}.Marshal(requestMessage)
	if err != nil {
		return fmt.Errorf("%w: failed to marshal request", err)
	}

	hash, err := GetStubHash(requestBytes)
	if err != nil {
		return fmt.Errorf("%w: failed to get stub hash", err)
	}

	isError := st != nil && st.Code() != codes.OK

	var out []byte
	if isError {
		var details []json.RawMessage
		for _, d := range st.Details() {
			pm, ok := d.(proto.Message)
			if !ok {
				return fmt.Errorf("error detail is not a proto.Message: %T", d)
			}
			detailBytes, err := protojson.MarshalOptions{EmitUnpopulated: true}.Marshal(pm)
			if err != nil {
				return fmt.Errorf("%w: failed to marshal error detail", err)
			}
			details = append(details, detailBytes)
		}

		payload := struct {
			Request      json.RawMessage   `json:"request"`
			ErrorCode    codes.Code        `json:"errorCode"`
			ErrorMessage string            `json:"errorMessage"`
			ErrorDetails []json.RawMessage `json:"errorDetails,omitempty"`
		}{
			Request:      requestBytes,
			ErrorCode:    st.Code(),
			ErrorMessage: st.Message(),
			ErrorDetails: details,
		}

		out, err = json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return fmt.Errorf("%w: failed to marshal error fixture", err)
		}
	} else {
		if responseMessage == nil {
			return fmt.Errorf("responseMessage must not be nil for success fixtures")
		}

		responseBytes, err := protojson.MarshalOptions{EmitUnpopulated: true}.Marshal(responseMessage)
		if err != nil {
			return fmt.Errorf("%w: failed to marshal response", err)
		}

		payload := struct {
			Request  json.RawMessage `json:"request"`
			Response json.RawMessage `json:"response"`
		}{
			Request:  requestBytes,
			Response: responseBytes,
		}

		out, err = json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return fmt.Errorf("%w: failed to marshal success fixture", err)
		}
	}

	if err := os.MkdirAll(requestDirectory, os.ModePerm); err != nil {
		return fmt.Errorf("%w: failed to create directory", err)
	}

	outputFilePath := fmt.Sprintf("%s/%s.json", requestDirectory, hash)
	return WriteToFileAndSync(outputFilePath, out, 0644)
}

func WriteToFileAndSync(filePath string, jsonBytes []byte, syncFileMode os.FileMode) error {
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, syncFileMode)
	if err != nil {
		return fmt.Errorf("%w: failed to open file %s", err, filePath)
	}

	defer func(file *os.File) {
		_ = file.Close()
	}(file)

	_, err = file.Write(jsonBytes)
	if err != nil {
		return fmt.Errorf("%w: failed to write to file to %s", err, filePath)
	}

	if err = file.Sync(); err != nil {
		return fmt.Errorf("%w: failed to sync file to %s", err, filePath)
	}

	return nil
}
