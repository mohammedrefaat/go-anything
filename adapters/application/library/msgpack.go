package library

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"sort"
)

// Msgpack format constants.
const (
	mpNil     byte = 0xc0
	mpFalse   byte = 0xc2
	mpTrue    byte = 0xc3
	mpBin8    byte = 0xc4
	mpBin16   byte = 0xc5
	mpBin32   byte = 0xc6
	mpFloat32 byte = 0xca
	mpFloat64 byte = 0xcb
	mpUint8   byte = 0xcc
	mpUint16  byte = 0xcd
	mpUint32  byte = 0xce
	mpUint64  byte = 0xcf
	mpInt8    byte = 0xd0
	mpInt16   byte = 0xd1
	mpInt32   byte = 0xd2
	mpInt64   byte = 0xd3
	mpStr8    byte = 0xd9
	mpStr16   byte = 0xda
	mpStr32   byte = 0xdb
	mpArray16 byte = 0xdc
	mpArray32 byte = 0xdd
	mpMap16   byte = 0xde
	mpMap32   byte = 0xdf
)

// EncodeMsgpack encodes a value into standard MessagePack binary representation.
func EncodeMsgpack(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := encodeValue(&buf, reflect.ValueOf(v)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encodeValue(buf *bytes.Buffer, val reflect.Value) error {
	if !val.IsValid() {
		buf.WriteByte(mpNil)
		return nil
	}

	for val.Kind() == reflect.Pointer || val.Kind() == reflect.Interface {
		if val.IsNil() {
			buf.WriteByte(mpNil)
			return nil
		}
		val = val.Elem()
	}

	switch val.Kind() {
	case reflect.Bool:
		if val.Bool() {
			buf.WriteByte(mpTrue)
		} else {
			buf.WriteByte(mpFalse)
		}
		return nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return encodeInt(buf, val.Int())

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return encodeUint(buf, val.Uint())

	case reflect.Float32:
		buf.WriteByte(mpFloat32)
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], math.Float32bits(float32(val.Float())))
		buf.Write(b[:])
		return nil

	case reflect.Float64:
		buf.WriteByte(mpFloat64)
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], math.Float64bits(val.Float()))
		buf.Write(b[:])
		return nil

	case reflect.String:
		return encodeString(buf, val.String())

	case reflect.Slice:
		if val.Type().Elem().Kind() == reflect.Uint8 {
			return encodeBytes(buf, val.Bytes())
		}
		fallthrough
	case reflect.Array:
		n := val.Len()
		if n <= 15 {
			buf.WriteByte(0x90 | byte(n))
		} else if n <= math.MaxUint16 {
			buf.WriteByte(mpArray16)
			var b [2]byte
			binary.BigEndian.PutUint16(b[:], uint16(n))
			buf.Write(b[:])
		} else {
			buf.WriteByte(mpArray32)
			var b [4]byte
			binary.BigEndian.PutUint32(b[:], uint32(n))
			buf.Write(b[:])
		}
		for i := 0; i < n; i++ {
			if err := encodeValue(buf, val.Index(i)); err != nil {
				return err
			}
		}
		return nil

	case reflect.Map:
		keys := val.MapKeys()
		// Sort keys for deterministic output (Rule 2)
		sort.Slice(keys, func(i, j int) bool {
			return fmt.Sprint(keys[i].Interface()) < fmt.Sprint(keys[j].Interface())
		})
		n := len(keys)
		if n <= 15 {
			buf.WriteByte(0x80 | byte(n))
		} else if n <= math.MaxUint16 {
			buf.WriteByte(mpMap16)
			var b [2]byte
			binary.BigEndian.PutUint16(b[:], uint16(n))
			buf.Write(b[:])
		} else {
			buf.WriteByte(mpMap32)
			var b [4]byte
			binary.BigEndian.PutUint32(b[:], uint32(n))
			buf.Write(b[:])
		}
		for _, k := range keys {
			if err := encodeValue(buf, k); err != nil {
				return err
			}
			if err := encodeValue(buf, val.MapIndex(k)); err != nil {
				return err
			}
		}
		return nil

	case reflect.Struct:
		// Convert struct to map of fields
		typ := val.Type()
		fields := make(map[string]reflect.Value)
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			if f.PkgPath != "" { // unexported
				continue
			}
			name := f.Name
			if tag := f.Tag.Get("json"); tag != "" && tag != "-" {
				name = tag
			}
			fields[name] = val.Field(i)
		}
		var fieldNames []string
		for name := range fields {
			fieldNames = append(fieldNames, name)
		}
		sort.Strings(fieldNames)
		n := len(fieldNames)
		if n <= 15 {
			buf.WriteByte(0x80 | byte(n))
		} else if n <= math.MaxUint16 {
			buf.WriteByte(mpMap16)
			var b [2]byte
			binary.BigEndian.PutUint16(b[:], uint16(n))
			buf.Write(b[:])
		} else {
			buf.WriteByte(mpMap32)
			var b [4]byte
			binary.BigEndian.PutUint32(b[:], uint32(n))
			buf.Write(b[:])
		}
		for _, name := range fieldNames {
			if err := encodeString(buf, name); err != nil {
				return err
			}
			if err := encodeValue(buf, fields[name]); err != nil {
				return err
			}
		}
		return nil

	default:
		return fmt.Errorf("msgpack: unsupported type %s", val.Type())
	}
}

func encodeInt(buf *bytes.Buffer, v int64) error {
	if v >= 0 && v <= 127 {
		buf.WriteByte(byte(v))
		return nil
	}
	if v >= -32 && v < 0 {
		buf.WriteByte(byte(v))
		return nil
	}
	if v >= math.MinInt8 && v <= math.MaxInt8 {
		buf.WriteByte(mpInt8)
		buf.WriteByte(byte(int8(v)))
		return nil
	}
	if v >= math.MinInt16 && v <= math.MaxInt16 {
		buf.WriteByte(mpInt16)
		var b [2]byte
		binary.BigEndian.PutUint16(b[:], uint16(v))
		buf.Write(b[:])
		return nil
	}
	if v >= math.MinInt32 && v <= math.MaxInt32 {
		buf.WriteByte(mpInt32)
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], uint32(v))
		buf.Write(b[:])
		return nil
	}
	buf.WriteByte(mpInt64)
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(v))
	buf.Write(b[:])
	return nil
}

func encodeUint(buf *bytes.Buffer, v uint64) error {
	if v <= 127 {
		buf.WriteByte(byte(v))
		return nil
	}
	if v <= math.MaxUint8 {
		buf.WriteByte(mpUint8)
		buf.WriteByte(byte(v))
		return nil
	}
	if v <= math.MaxUint16 {
		buf.WriteByte(mpUint16)
		var b [2]byte
		binary.BigEndian.PutUint16(b[:], uint16(v))
		buf.Write(b[:])
		return nil
	}
	if v <= math.MaxUint32 {
		buf.WriteByte(mpUint32)
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], uint32(v))
		buf.Write(b[:])
		return nil
	}
	buf.WriteByte(mpUint64)
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)
	buf.Write(b[:])
	return nil
}

func encodeString(buf *bytes.Buffer, s string) error {
	b := []byte(s)
	n := len(b)
	if n <= 31 {
		buf.WriteByte(0xa0 | byte(n))
	} else if n <= math.MaxUint8 {
		buf.WriteByte(mpStr8)
		buf.WriteByte(byte(n))
	} else if n <= math.MaxUint16 {
		buf.WriteByte(mpStr16)
		var lenBytes [2]byte
		binary.BigEndian.PutUint16(lenBytes[:], uint16(n))
		buf.Write(lenBytes[:])
	} else {
		buf.WriteByte(mpStr32)
		var lenBytes [4]byte
		binary.BigEndian.PutUint32(lenBytes[:], uint32(n))
		buf.Write(lenBytes[:])
	}
	buf.Write(b)
	return nil
}

func encodeBytes(buf *bytes.Buffer, b []byte) error {
	n := len(b)
	if n <= math.MaxUint8 {
		buf.WriteByte(mpBin8)
		buf.WriteByte(byte(n))
	} else if n <= math.MaxUint16 {
		buf.WriteByte(mpBin16)
		var lenBytes [2]byte
		binary.BigEndian.PutUint16(lenBytes[:], uint16(n))
		buf.Write(lenBytes[:])
	} else {
		buf.WriteByte(mpBin32)
		var lenBytes [4]byte
		binary.BigEndian.PutUint32(lenBytes[:], uint32(n))
		buf.Write(lenBytes[:])
	}
	buf.Write(b)
	return nil
}

// DecodeMsgpack decodes MessagePack data into a target pointer or returns any.
func DecodeMsgpack(data []byte, target any) error {
	r := bytes.NewReader(data)
	decoded, err := decodeValue(r)
	if err != nil {
		return err
	}

	if target == nil {
		return nil
	}

	tgtVal := reflect.ValueOf(target)
	if tgtVal.Kind() != reflect.Pointer {
		return errors.New("msgpack: target must be a non-nil pointer")
	}

	assignValue(tgtVal.Elem(), reflect.ValueOf(decoded))
	return nil
}

func decodeValue(r *bytes.Reader) (any, error) {
	tag, err := r.ReadByte()
	if err != nil {
		return nil, err
	}

	// Positive fixint: 0x00 - 0x7f
	if tag <= 0x7f {
		return int64(tag), nil
	}

	// Fixmap: 0x80 - 0x8f
	if tag >= 0x80 && tag <= 0x8f {
		return decodeMap(r, int(tag&0x0f))
	}

	// Fixarray: 0x90 - 0x9f
	if tag >= 0x90 && tag <= 0x9f {
		return decodeArray(r, int(tag&0x0f))
	}

	// Fixstr: 0xa0 - 0xbf
	if tag >= 0xa0 && tag <= 0xbf {
		return decodeStringBytes(r, int(tag&0x1f))
	}

	// Negative fixint: 0xe0 - 0xff
	if tag >= 0xe0 {
		return int64(int8(tag)), nil
	}

	switch tag {
	case mpNil:
		return nil, nil
	case mpFalse:
		return false, nil
	case mpTrue:
		return true, nil

	case mpBin8:
		l, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		return readBytes(r, int(l))

	case mpBin16:
		var b [2]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return nil, err
		}
		return readBytes(r, int(binary.BigEndian.Uint16(b[:])))

	case mpBin32:
		var b [4]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return nil, err
		}
		return readBytes(r, int(binary.BigEndian.Uint32(b[:])))

	case mpFloat32:
		var b [4]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return nil, err
		}
		return float64(math.Float32frombits(binary.BigEndian.Uint32(b[:]))), nil

	case mpFloat64:
		var b [8]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return nil, err
		}
		return math.Float64frombits(binary.BigEndian.Uint64(b[:])), nil

	case mpUint8:
		b, err := r.ReadByte()
		return uint64(b), err

	case mpUint16:
		var b [2]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return nil, err
		}
		return uint64(binary.BigEndian.Uint16(b[:])), nil

	case mpUint32:
		var b [4]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return nil, err
		}
		return uint64(binary.BigEndian.Uint32(b[:])), nil

	case mpUint64:
		var b [8]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return nil, err
		}
		return binary.BigEndian.Uint64(b[:]), nil

	case mpInt8:
		b, err := r.ReadByte()
		return int64(int8(b)), err

	case mpInt16:
		var b [2]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return nil, err
		}
		return int64(int16(binary.BigEndian.Uint16(b[:]))), nil

	case mpInt32:
		var b [4]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return nil, err
		}
		return int64(int32(binary.BigEndian.Uint32(b[:]))), nil

	case mpInt64:
		var b [8]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return nil, err
		}
		return int64(binary.BigEndian.Uint64(b[:])), nil

	case mpStr8:
		l, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		return decodeStringBytes(r, int(l))

	case mpStr16:
		var b [2]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return nil, err
		}
		return decodeStringBytes(r, int(binary.BigEndian.Uint16(b[:])))

	case mpStr32:
		var b [4]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return nil, err
		}
		return decodeStringBytes(r, int(binary.BigEndian.Uint32(b[:])))

	case mpArray16:
		var b [2]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return nil, err
		}
		return decodeArray(r, int(binary.BigEndian.Uint16(b[:])))

	case mpArray32:
		var b [4]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return nil, err
		}
		return decodeArray(r, int(binary.BigEndian.Uint32(b[:])))

	case mpMap16:
		var b [2]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return nil, err
		}
		return decodeMap(r, int(binary.BigEndian.Uint16(b[:])))

	case mpMap32:
		var b [4]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return nil, err
		}
		return decodeMap(r, int(binary.BigEndian.Uint32(b[:])))

	default:
		return nil, fmt.Errorf("msgpack: unknown tag 0x%02x", tag)
	}
}

func decodeStringBytes(r *bytes.Reader, length int) (string, error) {
	b, err := readBytes(r, length)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func readBytes(r *bytes.Reader, length int) ([]byte, error) {
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func decodeArray(r *bytes.Reader, length int) ([]any, error) {
	arr := make([]any, length)
	for i := 0; i < length; i++ {
		elem, err := decodeValue(r)
		if err != nil {
			return nil, err
		}
		arr[i] = elem
	}
	return arr, nil
}

func decodeMap(r *bytes.Reader, length int) (map[string]any, error) {
	m := make(map[string]any, length)
	for i := 0; i < length; i++ {
		key, err := decodeValue(r)
		if err != nil {
			return nil, err
		}
		val, err := decodeValue(r)
		if err != nil {
			return nil, err
		}
		keyStr := fmt.Sprint(key)
		m[keyStr] = val
	}
	return m, nil
}

func assignValue(target reflect.Value, source reflect.Value) {
	if !target.CanSet() || !source.IsValid() {
		return
	}

	if source.Kind() == reflect.Interface {
		source = source.Elem()
	}

	if !source.IsValid() {
		return
	}

	// Direct assignment if assignable
	if source.Type().AssignableTo(target.Type()) {
		target.Set(source)
		return
	}

	// Conversion between numeric types
	if isNumeric(target.Kind()) && isNumeric(source.Kind()) {
		switch target.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if source.Kind() == reflect.Float32 || source.Kind() == reflect.Float64 {
				target.SetInt(int64(source.Float()))
			} else if isUint(source.Kind()) {
				target.SetInt(int64(source.Uint()))
			} else {
				target.SetInt(source.Int())
			}
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			if source.Kind() == reflect.Float32 || source.Kind() == reflect.Float64 {
				target.SetUint(uint64(source.Float()))
			} else if isUint(source.Kind()) {
				target.SetUint(source.Uint())
			} else {
				target.SetUint(uint64(source.Int()))
			}
		case reflect.Float32, reflect.Float64:
			if isUint(source.Kind()) {
				target.SetFloat(float64(source.Uint()))
			} else if isInt(source.Kind()) {
				target.SetFloat(float64(source.Int()))
			} else {
				target.SetFloat(source.Float())
			}
		}
		return
	}

	// Map to Struct or Map to Map
	if source.Kind() == reflect.Map && target.Kind() == reflect.Struct {
		for _, key := range source.MapKeys() {
			kStr := fmt.Sprint(key.Interface())
			f := target.FieldByName(kStr)
			if f.IsValid() && f.CanSet() {
				assignValue(f, source.MapIndex(key))
			}
		}
		return
	}

	if target.Kind() == reflect.Interface {
		target.Set(source)
	}
}

func isNumeric(k reflect.Kind) bool {
	return isInt(k) || isUint(k) || k == reflect.Float32 || k == reflect.Float64
}

func isInt(k reflect.Kind) bool {
	return k >= reflect.Int && k <= reflect.Int64
}

func isUint(k reflect.Kind) bool {
	return k >= reflect.Uint && k <= reflect.Uint64
}
