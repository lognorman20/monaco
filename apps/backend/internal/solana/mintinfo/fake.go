package mintinfo

import "context"

// FakeReader returns fixed Info values from a map.
type FakeReader struct {
	byMint map[string]Info
}

// NewFakeReader builds a Reader backed by the given mint map.
func NewFakeReader(byMint map[string]Info) *FakeReader {
	cp := make(map[string]Info, len(byMint))
	for k, v := range byMint {
		cp[k] = v
	}
	return &FakeReader{byMint: cp}
}

// Info implements Reader.
func (f *FakeReader) Info(_ context.Context, mint string) (Info, error) {
	info, ok := f.byMint[mint]
	if !ok {
		return Info{}, ErrUnknownMint
	}
	return info, nil
}
