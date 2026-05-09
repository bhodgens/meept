package services

import (
	"errors"
	"testing"
)

func TestStandardErrorsDefined(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"ErrNotFound", ErrNotFound},
		{"ErrAlreadyExists", ErrAlreadyExists},
		{"ErrInvalidInput", ErrInvalidInput},
		{"ErrUnauthorized", ErrUnauthorized},
		{"ErrInternal", ErrInternal},
		{"ErrTimeout", ErrTimeout},
		{"ErrUnavailable", ErrUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err == nil {
				t.Errorf("expected %s to be defined", tt.name)
			}
		})
	}
}

func TestServiceError(t *testing.T) {
	baseErr := errors.New("base error")

	tests := []struct {
		name         string
		serviceError *ServiceError
		wantContains string
	}{
		{
			name: "chat service",
			serviceError: &ServiceError{
				Service: "chat",
				Op:      "Chat",
				Err:     baseErr,
			},
			wantContains: "chat.Chat: base error",
		},
		{
			name: "memory service",
			serviceError: &ServiceError{
				Service: "memory",
				Op:      "Query",
				Err:     baseErr,
			},
			wantContains: "memory.Query: base error",
		},
		{
			name: "task service",
			serviceError: &ServiceError{
				Service: "task",
				Op:      "Get",
				Err:     baseErr,
			},
			wantContains: "task.Get: base error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.serviceError.Error()
			if got != tt.wantContains {
				t.Errorf("ServiceError.Error() = %q, want %q", got, tt.wantContains)
			}
		})
	}
}

func TestServiceError_Unwrap(t *testing.T) {
	baseErr := errors.New("wrapped error")
	serviceErr := &ServiceError{
		Service: "test",
		Op:      "Test",
		Err:     baseErr,
	}

	unwrapped := serviceErr.Unwrap()
	if unwrapped != baseErr {
		t.Errorf("Unwrap() returned %v, want %v", unwrapped, baseErr)
	}
}

func TestServiceError_Is(t *testing.T) {
	tests := []struct {
		name    string
		wrapped error
		target  error
		want    bool
	}{
		{
			name:    "matches ErrNotFound",
			wrapped: &ServiceError{Service: "test", Op: "Test", Err: ErrNotFound},
			target:  ErrNotFound,
			want:    true,
		},
		{
			name:    "matches ErrInvalidInput",
			wrapped: &ServiceError{Service: "test", Op: "Test", Err: ErrInvalidInput},
			target:  ErrInvalidInput,
			want:    true,
		},
		{
			name:    "does not match different error",
			wrapped: &ServiceError{Service: "test", Op: "Test", Err: ErrNotFound},
			target:  ErrInvalidInput,
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := errors.Is(tt.wrapped, tt.target)
			if got != tt.want {
				t.Errorf("errors.Is() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWrapError(t *testing.T) {
	tests := []struct {
		name    string
		service string
		op      string
		err     error
		wantNil bool
	}{
		{
			name:    "nil error returns nil",
			service: "test",
			op:      "Test",
			err:     nil,
			wantNil: true,
		},
		{
			name:    "wraps error with context",
			service: "chat",
			op:      "Chat",
			err:     errors.New("something went wrong"),
			wantNil: false,
		},
		{
			name:    "wraps standard error",
			service: "memory",
			op:      "Query",
			err:     ErrNotFound,
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wrapError(tt.service, tt.op, tt.err)
			if (got == nil) != tt.wantNil {
				t.Errorf("wrapError() = %v, want nil = %v", got, tt.wantNil)
			}

			if !tt.wantNil {
				serviceErr, ok := got.(*ServiceError)
				if !ok {
					t.Fatalf("wrapError() returned %T, want *ServiceError", got)
				}
				if serviceErr.Service != tt.service {
					t.Errorf("Service = %q, want %q", serviceErr.Service, tt.service)
				}
				if serviceErr.Op != tt.op {
					t.Errorf("Op = %q, want %q", serviceErr.Op, tt.op)
				}
			}
		})
	}
}

func TestServiceError_ErrorFormat(t *testing.T) {
	err := &ServiceError{
		Service: "queue",
		Op:      "Enqueue",
		Err:     errors.New("queue is full"),
	}

	want := "queue.Enqueue: queue is full"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
