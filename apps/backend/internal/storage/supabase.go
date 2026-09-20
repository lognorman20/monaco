package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/telemetry"
)

const avatarsBucket = "avatars"

// SupabaseClient uploads objects via the Supabase Storage REST API (service role).
type SupabaseClient struct {
	baseURL    string
	serviceKey string
	httpClient *http.Client
}

// NewSupabaseClient returns a client for the given project URL and service role key.
func NewSupabaseClient(baseURL, serviceRoleKey string) *SupabaseClient {
	return &SupabaseClient{
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		serviceKey: strings.TrimSpace(serviceRoleKey),
		httpClient: telemetry.InstrumentClient(telemetry.UpstreamSupabase, &http.Client{Timeout: 30 * time.Second}),
	}
}

// Upload puts an object in the avatars bucket and returns its public URL.
func (c *SupabaseClient) Upload(ctx context.Context, objectKey, contentType string, data []byte) (string, error) {
	if c.baseURL == "" || c.serviceKey == "" {
		return "", fmt.Errorf("supabase storage is not configured")
	}
	if objectKey == "" {
		return "", fmt.Errorf("object key is required")
	}
	if contentType == "" {
		return "", fmt.Errorf("content type is required")
	}

	url := fmt.Sprintf("%s/storage/v1/object/%s/%s", c.baseURL, avatarsBucket, objectKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("build upload request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.serviceKey)
	req.Header.Set("apikey", c.serviceKey)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("x-upsert", "true")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload object: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("upload object: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return fmt.Sprintf("%s/storage/v1/object/public/%s/%s", c.baseURL, avatarsBucket, objectKey), nil
}
