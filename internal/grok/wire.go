package grok

import (
	"encoding/binary"
	"errors"
	"math"
)

const maxWireDepth = 16

type wireField struct {
	number uint64
	wire   byte
	varint uint64
	bytes  []byte
}

func scanMessage(data []byte, depth int) ([]wireField, error) {
	if depth > maxWireDepth {
		return nil, errors.New("protobuf nesting is too deep")
	}
	fields := make([]wireField, 0)
	for offset := 0; offset < len(data); {
		key, next, err := readVarint(data, offset)
		if err != nil {
			return nil, err
		}
		offset = next
		number := key >> 3
		wire := byte(key & 7)
		if number == 0 || number > 0x1fffffff {
			return nil, errors.New("invalid protobuf field")
		}
		field := wireField{number: number, wire: wire}
		switch wire {
		case 0:
			field.varint, offset, err = readVarint(data, offset)
		case 1:
			field.bytes, offset, err = takeBytes(data, offset, 8)
		case 2:
			var length uint64
			length, offset, err = readVarint(data, offset)
			if err == nil {
				if length > maxResponseBytes {
					return nil, errors.New("protobuf field is too large")
				}
				field.bytes, offset, err = takeBytes(data, offset, int(length))
			}
		case 3:
			offset, err = skipGroup(data, offset, number, depth+1)
		case 4:
			return nil, errors.New("unexpected protobuf end group")
		case 5:
			field.bytes, offset, err = takeBytes(data, offset, 4)
		default:
			return nil, errors.New("unsupported protobuf wire type")
		}
		if err != nil {
			return nil, err
		}
		fields = append(fields, field)
	}
	return fields, nil
}

func skipGroup(data []byte, offset int, number uint64, depth int) (int, error) {
	if depth > maxWireDepth {
		return 0, errors.New("protobuf nesting is too deep")
	}
	for offset < len(data) {
		key, next, err := readVarint(data, offset)
		if err != nil {
			return 0, err
		}
		offset = next
		fieldNumber := key >> 3
		wire := byte(key & 7)
		if fieldNumber == 0 || fieldNumber > 0x1fffffff {
			return 0, errors.New("invalid protobuf field")
		}
		if wire == 4 {
			if fieldNumber != number {
				return 0, errors.New("mismatched protobuf end group")
			}
			return offset, nil
		}
		switch wire {
		case 0:
			_, offset, err = readVarint(data, offset)
		case 1:
			_, offset, err = takeBytes(data, offset, 8)
		case 2:
			var length uint64
			length, offset, err = readVarint(data, offset)
			if err == nil {
				if length > maxResponseBytes {
					return 0, errors.New("protobuf field is too large")
				}
				_, offset, err = takeBytes(data, offset, int(length))
			}
		case 3:
			offset, err = skipGroup(data, offset, fieldNumber, depth+1)
		case 5:
			_, offset, err = takeBytes(data, offset, 4)
		default:
			return 0, errors.New("unsupported protobuf wire type")
		}
		if err != nil {
			return 0, err
		}
	}
	return 0, errors.New("unterminated protobuf group")
}

func readVarint(data []byte, offset int) (uint64, int, error) {
	var value uint64
	for count := 0; count < 10 && offset < len(data); count++ {
		current := data[offset]
		offset++
		if count == 9 && current > 1 {
			return 0, 0, errors.New("protobuf varint overflows")
		}
		value |= uint64(current&0x7f) << (7 * count)
		if current&0x80 == 0 {
			return value, offset, nil
		}
	}
	return 0, 0, errors.New("protobuf varint is truncated")
}

func takeBytes(data []byte, offset, length int) ([]byte, int, error) {
	if length < 0 || offset < 0 || offset > len(data) || length > len(data)-offset {
		return nil, 0, errors.New("protobuf field is truncated")
	}
	end := offset + length
	return data[offset:end], end, nil
}

func float32Value(field wireField) (float64, error) {
	if field.wire != 5 || len(field.bytes) != 4 {
		return 0, errors.New("protobuf field is not float32")
	}
	value := float64(math.Float32frombits(binary.LittleEndian.Uint32(field.bytes)))
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, errors.New("protobuf float is not finite")
	}
	return value, nil
}
