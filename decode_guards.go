package draco

import (
	"context"
	"fmt"
	"io"
	"math"
	"unsafe"
)

const (
	defaultReaderLimitBytes       int64 = 256 << 20
	maxDecodeAllocationBytes      int64 = 512 << 20
	maxPooledScratchRetainedBytes       = 8 << 20
	contextCheckInterval                = 1024
)

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("%w: context is nil", ErrInvalidArgument)
	}

	return ctx.Err()
}

func checkContextEvery(ctx context.Context, index int) error {
	if index&(contextCheckInterval-1) != 0 {
		return nil
	}

	return checkContext(ctx)
}

func guardNonNilWriter(w io.Writer) error {
	if w == nil {
		return fmt.Errorf("%w: writer is nil", ErrInvalidArgument)
	}

	return nil
}

func guardEncodeUint32Value(value int, what string) error {
	if value < 0 {
		return fmt.Errorf("%w: %s %d is negative", ErrInvalidGeometry, what, value)
	}

	if uint64(value) > math.MaxUint32 {
		return fmt.Errorf("%w: %s %d exceeds uint32 range", ErrInvalidGeometry, what, value)
	}

	return nil
}

func guardEncodeInt32Value(value int, what string) error {
	if value < 0 {
		return fmt.Errorf("%w: %s %d is negative", ErrInvalidGeometry, what, value)
	}

	if value > math.MaxInt32 {
		return fmt.Errorf("%w: %s %d exceeds int32 range", ErrInvalidGeometry, what, value)
	}

	return nil
}

func guardEncodeUint8Value(value int, what string) error {
	if value < 0 {
		return fmt.Errorf("%w: %s %d is negative", ErrInvalidGeometry, what, value)
	}

	if value > math.MaxUint8 {
		return fmt.Errorf("%w: %s %d exceeds uint8 range", ErrInvalidGeometry, what, value)
	}

	return nil
}

func guardEncodeInt8Value(value int, what string) error {
	if value < math.MinInt8 || value > math.MaxInt8 {
		return fmt.Errorf("%w: %s %d exceeds int8 range", ErrInvalidGeometry, what, value)
	}

	return nil
}

func guardAllocationBytes(bytes int64, what string) error {
	if bytes < 0 {
		return fmt.Errorf("%w: %s allocation underflow", ErrInvalidGeometry, what)
	}

	if bytes > maxDecodeAllocationBytes {
		return fmt.Errorf("%w: %s allocation %d bytes exceeds %d-byte limit", ErrInvalidGeometry, what, bytes, maxDecodeAllocationBytes)
	}

	return nil
}

func guardSliceAllocation(count int, elemSize uintptr, what string) error {
	if count < 0 {
		return fmt.Errorf("%w: %s count %d is negative", ErrInvalidGeometry, what, count)
	}

	bytes := int64(count) * int64(elemSize)
	if count > 0 && bytes/int64(count) != int64(elemSize) {
		return fmt.Errorf("%w: %s allocation overflow", ErrInvalidGeometry, what)
	}

	return guardAllocationBytes(bytes, what)
}

func guardUint32SliceAllocation(count uint32, elemSize uintptr, what string) (int, error) {
	if uint64(count) > uint64(math.MaxInt) {
		return 0, fmt.Errorf("%w: %s count %d exceeds int range", ErrInvalidGeometry, what, count)
	}

	n := int(count)
	if err := guardSliceAllocation(n, elemSize, what); err != nil {
		return 0, err
	}

	return n, nil
}

func guardIntProductAllocation(left, right int, elemSize uintptr, what string) (int, error) {
	if left < 0 || right < 0 {
		return 0, fmt.Errorf("%w: %s count is negative", ErrInvalidGeometry, what)
	}

	total := int64(left) * int64(right)
	if left > 0 && total/int64(left) != int64(right) {
		return 0, fmt.Errorf("%w: %s count overflow", ErrInvalidGeometry, what)
	}

	if total > int64(math.MaxInt) {
		return 0, fmt.Errorf("%w: %s count %d exceeds int range", ErrInvalidGeometry, what, total)
	}

	if err := guardSliceAllocation(int(total), elemSize, what); err != nil {
		return 0, err
	}

	return int(total), nil
}

func resetScratchSlice[T any](buf []T) []T {
	var zero T
	if uint64(cap(buf))*uint64(unsafe.Sizeof(zero)) > maxPooledScratchRetainedBytes {
		return nil
	}

	return buf[:0]
}

func resetScratchSliceClear[T any](buf []T) []T {
	clear(buf)
	return resetScratchSlice(buf)
}
