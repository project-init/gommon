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
