package storage

import (
	"context"
	"fmt"
	"sync"
)

// FakeClient records uploads in memory for tests.
type FakeClient struct {
	mu      sync.Mutex
	Uploads map[string][]byte
	BaseURL string
}

// NewFakeClient returns a fake storage client with the given public base URL.
func NewFakeClient(baseURL string) *FakeClient {
	return &FakeClient{
		Uploads: make(map[string][]byte),
		BaseURL: baseURL,
	}
}

// Upload stores data and returns a deterministic public URL.
func (f *FakeClient) Upload(ctx context.Context, objectKey, contentType string, data []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if objectKey == "" {
		return "", fmt.Errorf("object key is required")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	copied := make([]byte, len(data))
	copy(copied, data)
	f.Uploads[objectKey] = copied
	return fmt.Sprintf("%s/storage/v1/object/public/%s", f.BaseURL, objectKey), nil
}
