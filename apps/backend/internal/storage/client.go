package storage

import "context"

// Client uploads avatar objects and returns a public display URL.
type Client interface {
	Upload(ctx context.Context, objectKey, contentType string, data []byte) (publicURL string, err error)
}
