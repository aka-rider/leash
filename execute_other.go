//go:build !darwin

package leash

import (
	"context"
	"errors"
)

// ErrUnsupported is returned by Execute on non-macOS platforms.
var ErrUnsupported = errors.New("leash: sandboxing is only supported on macOS")

func Execute(_ context.Context, _ Leash) error {
	return ErrUnsupported
}
