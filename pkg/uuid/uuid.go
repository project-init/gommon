package uuid

import (
	"github.com/google/uuid"
)

func GenerateUuidFromPrevious(previousUuid uuid.UUID, data string) uuid.UUID {
	return uuid.NewSHA1(previousUuid, []byte(data))
}
