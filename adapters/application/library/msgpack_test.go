package library

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMsgpack_RoundtripPrimitives(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected any
	}{
		{"nil", nil, nil},
		{"bool_true", true, true},
		{"bool_false", false, false},
		{"pos_fixint", int64(42), int64(42)},
		{"zero", int64(0), int64(0)},
		{"neg_fixint", int64(-15), int64(-15)},
		{"int8", int64(-100), int64(-100)},
		{"int16", int64(-25000), int64(-25000)},
		{"int32", int64(1000000), int64(1000000)},
		{"int64", int64(5000000000), int64(5000000000)},
		{"uint8", uint64(200), uint64(200)},
		{"uint16", uint64(60000), uint64(60000)},
		{"uint32", uint64(3000000000), uint64(3000000000)},
		{"float32", float32(3.14), float64(float32(3.14))},
		{"float64", float64(2.718281828), float64(2.718281828)},
		{"short_string", "hello world", "hello world"},
		{"empty_string", "", ""},
		{"bytes", []byte("raw binary bytes"), []byte("raw binary bytes")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := EncodeMsgpack(tc.input)
			require.NoError(t, err)
			require.NotEmpty(t, data)

			var out any
			err = DecodeMsgpack(data, &out)
			require.NoError(t, err)

			if tc.expected == nil {
				assert.Nil(t, out)
			} else {
				assert.Equal(t, tc.expected, out)
			}
		})
	}
}

func TestMsgpack_RoundtripComplex(t *testing.T) {
	t.Run("array", func(t *testing.T) {
		arr := []any{"apple", int64(123), true, nil}
		data, err := EncodeMsgpack(arr)
		require.NoError(t, err)

		var out any
		err = DecodeMsgpack(data, &out)
		require.NoError(t, err)

		slice, ok := out.([]any)
		require.True(t, ok)
		assert.Len(t, slice, 4)
		assert.Equal(t, "apple", slice[0])
		assert.Equal(t, int64(123), slice[1])
		assert.Equal(t, true, slice[2])
		assert.Nil(t, slice[3])
	})

	t.Run("map", func(t *testing.T) {
		m := map[string]any{
			"key1": "value1",
			"key2": int64(999),
			"key3": []any{"sub1", "sub2"},
		}
		data, err := EncodeMsgpack(m)
		require.NoError(t, err)

		var out any
		err = DecodeMsgpack(data, &out)
		require.NoError(t, err)

		resMap, ok := out.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "value1", resMap["key1"])
		assert.Equal(t, int64(999), resMap["key2"])
	})

	t.Run("struct_encode", func(t *testing.T) {
		type Person struct {
			Name string `json:"name"`
			Age  int    `json:"age"`
		}
		p := Person{Name: "Alice", Age: 30}
		data, err := EncodeMsgpack(p)
		require.NoError(t, err)

		var out any
		err = DecodeMsgpack(data, &out)
		require.NoError(t, err)

		resMap, ok := out.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "Alice", resMap["name"])
		assert.Equal(t, int64(30), resMap["age"])
	})
}
