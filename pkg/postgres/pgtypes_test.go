package postgres

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_UuidStringAsPGUuid(t *testing.T) {
	_, err := UuidStringAsPGUuid("")
	assert.Error(t, err)

	pgUuid, err := UuidStringAsPGUuid("00000000-0000-0000-0000-000000000000")
	assert.NoError(t, err)
	assert.Equal(t, "00000000-0000-0000-0000-000000000000", pgUuid.String())
}
