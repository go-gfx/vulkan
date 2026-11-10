//go:build !(linux && (amd64 || arm64))

// Package native opens the system Vulkan loader. Only linux/amd64 and
// linux/arm64 are supported; elsewhere Open reports errors.ErrUnsupported.
package native

import (
	"errors"

	"github.com/go-gfx/vulkan/internal/vk"
)

// Open reports that this platform has no binding.
func Open() (vk.Loader, error) {
	return nil, errors.ErrUnsupported
}
