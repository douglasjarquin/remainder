package grok

import (
	"errors"
	"math"
	"time"
)

func periodAt(config []wireField) (period, bool, error) {
	raw, present, err := bytesAt(config, 8)
	if err != nil || !present {
		return period{}, false, err
	}
	fields, err := scanMessage(raw, 2)
	if err != nil {
		return period{}, false, err
	}
	kind, hasKind, err := varintAt(fields, 1)
	if err != nil || !hasKind || kind != 1 && kind != 2 {
		return period{}, false, errors.New("Grok quota period is unsupported")
	}
	startRaw, hasStart, err := bytesAt(fields, 2)
	if err != nil || !hasStart {
		return period{}, false, errors.New("Grok quota period has no start")
	}
	resetRaw, hasReset, err := bytesAt(fields, 3)
	if err != nil || !hasReset {
		return period{}, false, errors.New("Grok quota period has no reset")
	}
	start, err := decodeTimestamp(startRaw, 3)
	if err != nil {
		return period{}, false, err
	}
	reset, err := decodeTimestamp(resetRaw, 3)
	if err != nil || !reset.After(start) {
		return period{}, false, errors.New("Grok quota period timestamps are invalid")
	}
	return period{start: start, reset: reset}, true, nil
}

func decodeTimestamp(raw []byte, depth int) (time.Time, error) {
	fields, err := scanMessage(raw, depth)
	if err != nil {
		return time.Time{}, err
	}
	seconds, present, err := varintAt(fields, 1)
	if err != nil || !present || seconds > math.MaxInt64 {
		return time.Time{}, errors.New("Grok quota timestamp is invalid")
	}
	nanos, hasNanos, err := varintAt(fields, 2)
	if err != nil || hasNanos && nanos > 999_999_999 {
		return time.Time{}, errors.New("Grok quota timestamp is invalid")
	}
	value := time.Unix(int64(seconds), int64(nanos)).UTC()
	if value.Year() < 1 || value.Year() > 9999 {
		return time.Time{}, errors.New("Grok quota timestamp is invalid")
	}
	return value, nil
}

func varintAt(fields []wireField, number uint64) (uint64, bool, error) {
	var value uint64
	found := false
	for _, field := range fields {
		if field.number != number {
			continue
		}
		if field.wire != 0 || found {
			return 0, false, errors.New("Grok quota protobuf field has wrong type or is duplicated")
		}
		value, found = field.varint, true
	}
	return value, found, nil
}

func floatAt(fields []wireField, number uint64) (float64, bool, error) {
	var value float64
	found := false
	for _, field := range fields {
		if field.number != number {
			continue
		}
		if found {
			return 0, false, errors.New("Grok quota protobuf field is duplicated")
		}
		var err error
		value, err = float32Value(field)
		if err != nil {
			return 0, false, err
		}
		found = true
	}
	return value, found, nil
}

func bytesAt(fields []wireField, number uint64) ([]byte, bool, error) {
	var value []byte
	found := false
	for _, field := range fields {
		if field.number != number {
			continue
		}
		if field.wire != 2 || found {
			return nil, false, errors.New("Grok quota protobuf field has wrong type or is duplicated")
		}
		value, found = field.bytes, true
	}
	return value, found, nil
}

func allBytesAt(fields []wireField, number uint64) ([][]byte, error) {
	var result [][]byte
	for _, field := range fields {
		if field.number != number {
			continue
		}
		if field.wire != 2 {
			return nil, errors.New("Grok quota protobuf field has wrong type")
		}
		result = append(result, field.bytes)
	}
	return result, nil
}
