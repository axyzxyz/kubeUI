package errcode

import (
	"errors"
	"fmt"
	"testing"
)

func TestHTTPStatusOf(t *testing.T) {
	tests := []struct {
		name string
		code int
		want int
	}{
		{name: "param invalid", code: ParamInvalid, want: 400},
		{name: "unauthorized", code: Unauthorized, want: 401},
		{name: "forbidden", code: Forbidden, want: 403},
		{name: "cluster not found", code: ClusterNotFound, want: 404},
		{name: "internal error", code: InternalError, want: 500},
		{name: "unknown falls back to 500", code: 12345, want: 500},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HTTPStatusOf(tt.code); got != tt.want {
				t.Fatalf("HTTPStatusOf(%d) = %d, want %d", tt.code, got, tt.want)
			}
		})
	}
}

func TestErrorWrapAndUnwrap(t *testing.T) {
	base := errors.New("dial timeout")
	wrapped := New(ClusterUnreachable, "cluster unreachable").WithCause(fmt.Errorf("probe: %w", base))

	if wrapped.Error() != "cluster unreachable" {
		t.Fatalf("message = %q, want stable message", wrapped.Error())
	}
	if !errors.Is(wrapped, base) {
		t.Fatal("errors.Is should unwrap through Cause")
	}
	var ec *Error
	if !errors.As(fmt.Errorf("outer: %w", wrapped), &ec) || ec.Code != ClusterUnreachable {
		t.Fatal("errors.As should extract *Error with code")
	}
	if From(wrapped) != wrapped {
		t.Fatal("From should return the *Error")
	}
	if From(errors.New("plain")) != nil {
		t.Fatal("From should return nil for non-errcode errors")
	}
}

func TestWithCauseDoesNotMutateOriginal(t *testing.T) {
	orig := New(NotFound, "not found")
	_ = orig.WithCause(errors.New("x"))
	if orig.Cause != nil {
		t.Fatal("WithCause must not mutate the original error")
	}
}
