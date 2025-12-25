package uuid

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func Test_GenerateUuidFromPrevious(t *testing.T) {
	previousUuid := uuid.MustParse("e438d488-10e1-70ba-e875-c7a0fda3436c")
	assert.Equal(t, "a9f08b41-a5fa-5d3d-a83b-f9b1506d628c", GenerateUuidFromPrevious(previousUuid, "123456789").String())

	previousUuid = uuid.MustParse("f49844e8-b0c1-706a-beac-1e0fbe6e7af3")
	assert.Equal(t, "5a806abb-276e-5898-8db8-8894993f2ac4", GenerateUuidFromPrevious(previousUuid, "someString").String())
}
