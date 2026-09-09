package grok

import (
	"errors"
	"math"
	"testing"
)

func TestWireBoundaries(t *testing.T) {
	t.Run("unknown_wire_types_are_skipped", func(t *testing.T) {
		message := append(varintField(20, 1), fixed32Field(21, 2)...)
		message = append(message, append(varint(22<<3|1), make([]byte, 8)...)...)
		message = append(message, bytesField(23, []byte("opaque"))...)
		message = append(message, varint(24<<3|3)...)
		message = append(message, varintField(1, 7)...)
		message = append(message, varint(24<<3|4)...)
		fields, err := scanMessage(message, 0)
		if err != nil || len(fields) != 4 {
			t.Fatalf("fields = %+v, error = %v", fields, err)
		}
	})

	t.Run("unknown_group_depth_is_bounded", func(t *testing.T) {
		var message []byte
		for depth := 0; depth <= maxWireDepth; depth++ {
			message = append(message, varint(uint64(depth+1)<<3|3)...)
		}
		for depth := maxWireDepth; depth >= 0; depth-- {
			message = append(message, varint(uint64(depth+1)<<3|4)...)
		}
		if _, err := scanMessage(message, 0); err == nil {
			t.Fatal("deep protobuf group was accepted")
		}
	})

	t.Run("varint_overflow", func(t *testing.T) {
		overflow := []byte{0x08, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x02}
		if _, err := scanMessage(overflow, 0); err == nil {
			t.Fatal("overflowing protobuf varint was accepted")
		}
	})

	t.Run("length_overflow", func(t *testing.T) {
		message := append(varint(1<<3|2), varint(maxResponseBytes+1)...)
		if _, err := scanMessage(message, 0); err == nil {
			t.Fatal("oversized protobuf field was accepted")
		}
	})

	t.Run("finite_float", func(t *testing.T) {
		field := wireField{wire: 5, bytes: fixed32Field(1, math.Float32bits(float32(math.NaN())))[1:]}
		if _, err := float32Value(field); err == nil {
			t.Fatal("non-finite float was accepted")
		}
	})
}

func TestGRPCWebBoundaries(t *testing.T) {
	payload := []byte{0x0a, 0}
	t.Run("one_data_then_trailers", func(t *testing.T) {
		got, err := decodeGRPCWeb(grpcResponse(payload))
		if err != nil || string(got) != string(payload) {
			t.Fatalf("payload = %v, error = %v", got, err)
		}
	})
	t.Run("compressed_frame", func(t *testing.T) {
		if _, err := decodeGRPCWeb(frame(1, payload)); err == nil {
			t.Fatal("compressed frame was accepted")
		}
	})
	t.Run("multiple_data_frames", func(t *testing.T) {
		if _, err := decodeGRPCWeb(append(frame(0, payload), frame(0, payload)...)); err == nil {
			t.Fatal("multiple data frames were accepted")
		}
	})
	t.Run("data_after_trailers", func(t *testing.T) {
		body := append(frame(0x80, []byte("grpc-status: 0\r\n")), frame(0, payload)...)
		if _, err := decodeGRPCWeb(body); err == nil {
			t.Fatal("data after trailers was accepted")
		}
	})
	t.Run("truncated_frame", func(t *testing.T) {
		if _, err := decodeGRPCWeb([]byte{0, 0, 0, 0, 3, 1}); err == nil {
			t.Fatal("truncated frame was accepted")
		}
	})
	t.Run("auth_status", func(t *testing.T) {
		body := append(frame(0, payload), frame(0x80, []byte("grpc-status: 16\r\n"))...)
		if _, err := decodeGRPCWeb(body); !errors.Is(err, ErrAuthorizationRejected) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("permission_status_is_not_assumed_revoked", func(t *testing.T) {
		body := append(frame(0, payload), frame(0x80, []byte("grpc-status: 7\r\ngrpc-message: quota%20permission\r\n"))...)
		if _, err := decodeGRPCWeb(body); !errors.Is(err, ErrTransient) || errors.Is(err, ErrAuthorizationRejected) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("permission_auth_message_is_revoked", func(t *testing.T) {
		for _, message := range []string{"token%20expired", "access%20token%20could%20not%20be%20validated"} {
			body := append(frame(0, payload), frame(0x80, []byte("grpc-status: 7\r\ngrpc-message: "+message+"\r\n"))...)
			if _, err := decodeGRPCWeb(body); !errors.Is(err, ErrAuthorizationRejected) {
				t.Fatalf("message = %q, error = %v", message, err)
			}
		}
	})
	t.Run("rate_status", func(t *testing.T) {
		body := append(frame(0, payload), frame(0x80, []byte("grpc-status: 8\r\n"))...)
		if _, err := decodeGRPCWeb(body); !errors.Is(err, ErrTransient) {
			t.Fatalf("error = %v", err)
		}
	})
}
