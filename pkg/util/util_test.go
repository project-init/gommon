package util

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func Test_Must(t *testing.T) {
	t.Run("should return value", func(t *testing.T) {
		id := "c6e20e5d-3d1b-43f5-a25f-797fa6867bda"
		result := Must(uuid.Parse(id))
		assert.Equal(t, id, result.String())
	})

	t.Run("should panic on err", func(t *testing.T) {
		assert.Panics(t, func() {
			Must(uuid.Parse("12121212121"))
		})
	})
}

func Test_AsPtr(t *testing.T) {
	t.Run("should return pointer to int64", func(t *testing.T) {
		v := int64(123)
		ptr := AsPtr(v)
		assert.Equal(t, v, *ptr)
	})
}

func Test_SafeDeref(t *testing.T) {
	t.Run("should deref non-nil pointer", func(t *testing.T) {
		v := int64(123)
		ptr := &v
		result := SafeDeref(ptr)
		assert.Equal(t, v, result)
	})

	t.Run("should return zero value for nil pointer", func(t *testing.T) {
		var ptr *int64 = nil
		result := SafeDeref(ptr)
		assert.Equal(t, int64(0), result)
	})
}
