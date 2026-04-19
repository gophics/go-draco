package draco

import (
	"bytes"
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type blockingReader struct {
	first   []byte
	started chan struct{}
	ctx     context.Context
	read    int
}

type cancelAfterChecksContext struct {
	parent      context.Context
	cancelAfter int32
	done        chan struct{}
	checks      atomic.Int32
	canceled    atomic.Bool
}

func newCancelAfterChecksContext(parent context.Context, cancelAfter int32) *cancelAfterChecksContext {
	return &cancelAfterChecksContext{
		parent:      parent,
		cancelAfter: cancelAfter,
		done:        make(chan struct{}),
	}
}

func (c *cancelAfterChecksContext) Deadline() (time.Time, bool) {
	return c.parent.Deadline()
}

func (c *cancelAfterChecksContext) Done() <-chan struct{} {
	return c.done
}

func (c *cancelAfterChecksContext) Err() error {
	if err := c.parent.Err(); err != nil {
		c.cancel()
		return err
	}

	if c.canceled.Load() {
		return context.Canceled
	}

	if c.checks.Add(1) >= c.cancelAfter {
		c.cancel()
		return context.Canceled
	}

	return nil
}

func (c *cancelAfterChecksContext) Value(key any) any {
	return c.parent.Value(key)
}

func (c *cancelAfterChecksContext) CheckCount() int32 {
	return c.checks.Load()
}

func (c *cancelAfterChecksContext) cancel() {
	if c.canceled.CompareAndSwap(false, true) {
		close(c.done)
	}
}

func (r *blockingReader) Read(p []byte) (int, error) {
	switch r.read {
	case 0:
		r.read++
		n := copy(p, r.first)
		close(r.started)
		return n, nil
	default:
		<-r.ctx.Done()
		return 0, r.ctx.Err()
	}
}

func TestContextAwareDecodeEntryPointsCancelEarly(t *testing.T) {
	data := encodeMesh(t, newMeshFromData(t,
		[][]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}},
		[]Face{{0, 1, 2}},
	), WithMeshMethod(MeshSequentialEncoding))

	tests := []struct {
		name string
		run  func(context.Context) error
	}{
		{
			name: "decode",
			run: func(ctx context.Context) error {
				_, err := Decode(ctx, data)
				return err
			},
		},
		{
			name: "decode-with-stats",
			run: func(ctx context.Context) error {
				_, err := DecodeWithStats(ctx, data)
				return err
			},
		},
		{
			name: "inspect",
			run: func(ctx context.Context) error {
				_, err := Inspect(ctx, data)
				return err
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(testContext(t))
			cancel()
			err := tc.run(ctx)
			require.ErrorIs(t, err, context.Canceled)
		})
	}
}

func TestContextAwareEncodeEntryPointsCancelEarly(t *testing.T) {
	mesh := newMeshFromData(t,
		[][]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}},
		[]Face{{0, 1, 2}},
	)

	tests := []struct {
		name string
		run  func(context.Context) error
	}{
		{
			name: "encode",
			run: func(ctx context.Context) error {
				_, err := Encode(ctx, mesh)
				return err
			},
		},
		{
			name: "encode-with-stats",
			run: func(ctx context.Context) error {
				_, err := EncodeWithStats(ctx, mesh)
				return err
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(testContext(t))
			cancel()
			err := tc.run(ctx)
			require.ErrorIs(t, err, context.Canceled)
		})
	}
}

func TestDecodeFromCancelsDuringRead(t *testing.T) {
	ctx, cancel := context.WithCancel(testContext(t))
	reader := &blockingReader{
		first:   []byte{0x44, 0x52, 0x41, 0x43, 0x4f},
		started: make(chan struct{}),
		ctx:     ctx,
	}

	done := make(chan error, 1)
	go func() {
		_, err := DecodeFrom(ctx, reader)
		done <- err
	}()

	select {
	case <-reader.started:
	case <-time.After(2 * time.Second):
		t.Fatal("DecodeFrom did not start reading")
	}

	cancel()

	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("DecodeFrom did not return after cancellation")
	}
}

func TestBoundedReaderDecodeEntryPoints(t *testing.T) {
	data := encodeMesh(t, newMeshFromData(t,
		[][]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}},
		[]Face{{0, 1, 2}},
	), WithMeshMethod(MeshSequentialEncoding))

	for _, tc := range []struct {
		name string
		run  func() error
	}{
		{
			name: "decode-with-exact-limit",
			run: func() error {
				_, err := DecodeFrom(testContext(t), bytes.NewReader(data), WithInputLimit(int64(len(data))))
				return err
			},
		},
		{
			name: "decode-with-stats",
			run: func() error {
				result, err := DecodeWithStatsFrom(testContext(t), bytes.NewReader(data), WithInputLimit(int64(len(data))))
				if err != nil {
					return err
				}

				require.Equal(t, len(data), result.Stats.BytesRead)
				return nil
			},
		},
		{
			name: "inspect",
			run: func() error {
				info, err := InspectFrom(testContext(t), bytes.NewReader(data), WithInputLimit(int64(len(data))))
				if err != nil {
					return err
				}

				want, err := Inspect(testContext(t), data)
				if err != nil {
					return err
				}

				require.Equal(t, want, info)
				return nil
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, tc.run())
		})
	}

	t.Run("rejects-too-large", func(t *testing.T) {
		_, err := DecodeFrom(testContext(t), bytes.NewReader(data), WithInputLimit(int64(len(data)-1)))
		require.Error(t, err)
		require.Contains(t, err.Error(), "input exceeds")
	})

	t.Run("rejects-invalid-limit", func(t *testing.T) {
		_, err := DecodeFrom(testContext(t), bytes.NewReader(data), WithInputLimit(-1))
		require.Error(t, err)
		require.ErrorIs(t, err, ErrInvalidArgument)
	})
}

func TestTypedReaderDecodeEntryPoints(t *testing.T) {
	meshData := encodeMesh(t, newMeshFromData(t,
		[][]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}},
		[]Face{{0, 1, 2}},
	), WithMeshMethod(MeshSequentialEncoding))
	meshLimit := WithInputLimit(int64(len(meshData)))

	mesh, err := DecodeMeshFrom(testContext(t), bytes.NewReader(meshData), meshLimit)
	require.NoError(t, err)
	require.Equal(t, 1, mesh.FaceCount())

	_, err = DecodePointCloudFrom(testContext(t), bytes.NewReader(meshData), meshLimit)
	require.ErrorIs(t, err, ErrInvalidGeometry)

	decoder, err := NewDecoder(meshLimit)
	require.NoError(t, err)
	mesh, err = decoder.DecodeMeshFrom(testContext(t), bytes.NewReader(meshData))
	require.NoError(t, err)
	require.Equal(t, 1, mesh.FaceCount())

	pc := mustNewPointCloud(2)
	position := mustNewFloat32Attribute(AttributePosition, 3, 2)
	setFloat32Value(t, position, 0, 0, 0, 0)
	setFloat32Value(t, position, 1, 1, 0, 0)
	addPointCloudAttribute(t, pc, position)
	pointCloudData := encodePointCloud(t, pc)
	pointCloudLimit := WithInputLimit(int64(len(pointCloudData)))

	decodedPC, err := DecodePointCloudFrom(testContext(t), bytes.NewReader(pointCloudData), pointCloudLimit)
	require.NoError(t, err)
	require.Equal(t, 2, decodedPC.PointCount())

	decoder, err = NewDecoder(pointCloudLimit)
	require.NoError(t, err)
	decodedPC, err = decoder.DecodePointCloudFrom(testContext(t), bytes.NewReader(pointCloudData))
	require.NoError(t, err)
	require.Equal(t, 2, decodedPC.PointCount())
}

func TestDecodeCancelsDuringInMemoryDecode(t *testing.T) {
	const pointCount = 250000
	const cancelAfterChecks = 12

	values := make([]int32, pointCount*3)
	for i := range values {
		values[i] = int32(i % 1024)
	}

	attr, err := NewInt32Attribute(AttributeGeneric, 3, values)
	require.NoError(t, err)
	pc := mustNewPointCloud(pointCount)
	addPointCloudAttribute(t, pc, attr)
	data := encodePointCloud(t, pc)

	ctx := newCancelAfterChecksContext(testContext(t), cancelAfterChecks)
	_, err = Decode(ctx, data)
	require.ErrorIs(t, err, context.Canceled)
	require.GreaterOrEqual(t, ctx.CheckCount(), int32(cancelAfterChecks))
}

func TestEncodeCancelsDuringInMemoryEncode(t *testing.T) {
	const pointCount = contextCheckInterval * 64
	const cancelAfterChecks = 12

	values := make([]int32, pointCount*3)
	for i := range values {
		values[i] = int32(i % 2048)
	}

	attr, err := NewInt32Attribute(AttributeGeneric, 3, values)
	require.NoError(t, err)
	pc := mustNewPointCloud(pointCount)
	addPointCloudAttribute(t, pc, attr)

	ctx := newCancelAfterChecksContext(testContext(t), cancelAfterChecks)
	_, err = Encode(ctx, pc)
	require.ErrorIs(t, err, context.Canceled)
	require.GreaterOrEqual(t, ctx.CheckCount(), int32(cancelAfterChecks))
}

func TestReusableDecoderHonorsConfiguredInputLimit(t *testing.T) {
	data := encodeMesh(t, newMeshFromData(t,
		[][]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}},
		[]Face{{0, 1, 2}},
	), WithMeshMethod(MeshSequentialEncoding))

	decoder, err := NewDecoder(WithInputLimit(int64(len(data))))
	require.NoError(t, err)

	geom, err := decoder.DecodeFrom(testContext(t), bytes.NewReader(data))
	require.NoError(t, err)
	_, ok := geom.(*Mesh)
	require.True(t, ok)

	info, err := decoder.Inspect(testContext(t), data)
	require.NoError(t, err)
	require.Equal(t, MeshGeometry, info.GeometryType)

	decoder, err = NewDecoder(WithInputLimit(int64(len(data) - 1)))
	require.NoError(t, err)

	_, err = decoder.DecodeFrom(testContext(t), bytes.NewReader(data))
	require.Error(t, err)
	require.Contains(t, err.Error(), "input exceeds")
}
