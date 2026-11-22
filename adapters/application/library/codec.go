package library

import (
	"encoding/json"
	"errors"
	"fmt"
)

// SerializationFormat identifies the payload serialization format.
type SerializationFormat string

const (
	// FormatJSON formats invocations as standard JSON.
	FormatJSON SerializationFormat = "json"

	// FormatMsgpack formats invocations as binary MessagePack.
	FormatMsgpack SerializationFormat = "msgpack"

	// FormatBinary passes raw binary bytes directly without transformation.
	FormatBinary SerializationFormat = "binary"
)

// Codec serializes and deserializes payloads for foreign library invocations.
type Codec interface {
	Format() SerializationFormat
	ContentType() string
	Encode(v any) ([]byte, error)
	Decode(data []byte, target any) error
}

// JSONCodec implements Codec for JSON.
type JSONCodec struct{}

func (c JSONCodec) Format() SerializationFormat { return FormatJSON }
func (c JSONCodec) ContentType() string         { return "application/json" }

func (c JSONCodec) Encode(v any) ([]byte, error) {
	if v == nil {
		return []byte("null"), nil
	}
	switch val := v.(type) {
	case []byte:
		return val, nil
	case string:
		return []byte(val), nil
	default:
		return json.Marshal(v)
	}
}

func (c JSONCodec) Decode(data []byte, target any) error {
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, target)
}

// MsgpackCodec implements Codec for MessagePack.
type MsgpackCodec struct{}

func (c MsgpackCodec) Format() SerializationFormat { return FormatMsgpack }
func (c MsgpackCodec) ContentType() string         { return "application/msgpack" }

func (c MsgpackCodec) Encode(v any) ([]byte, error) {
	return EncodeMsgpack(v)
}

func (c MsgpackCodec) Decode(data []byte, target any) error {
	if len(data) == 0 {
		return nil
	}
	return DecodeMsgpack(data, target)
}

// BinaryCodec implements Codec for raw binary payloads.
type BinaryCodec struct{}

func (c BinaryCodec) Format() SerializationFormat { return FormatBinary }
func (c BinaryCodec) ContentType() string         { return "application/octet-stream" }

func (c BinaryCodec) Encode(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	switch val := v.(type) {
	case []byte:
		return val, nil
	case string:
		return []byte(val), nil
	default:
		return nil, errors.New("binary codec: input must be []byte or string")
	}
}

func (c BinaryCodec) Decode(data []byte, target any) error {
	if target == nil {
		return nil
	}
	switch tgt := target.(type) {
	case *[]byte:
		*tgt = append([]byte(nil), data...)
		return nil
	case *string:
		*tgt = string(data)
		return nil
	case *any:
		*tgt = data
		return nil
	default:
		return errors.New("binary codec: target must be *[]byte, *string, or *any")
	}
}

// GetCodec returns the Codec implementation for the given format.
func GetCodec(format SerializationFormat) (Codec, error) {
	switch format {
	case FormatJSON, "":
		return JSONCodec{}, nil
	case FormatMsgpack:
		return MsgpackCodec{}, nil
	case FormatBinary:
		return BinaryCodec{}, nil
	default:
		return nil, fmt.Errorf("library: unsupported serialization format %q", format)
	}
}
