package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/storage"
)

func integrationMeHandlers(t *testing.T, storageClient storage.Client) (*MeHandlers, *AuthHandlers, privy.Client, *postgres.TestIsolation) {
	t.Helper()
	authHandlers, privyClient, db, iso := integrationApp(t)
	store := postgres.NewStore(db)
	sessions := app.NewSessionService(store, privyClient)
	profilePhotos := app.NewProfilePhotoService(store, privyClient, storageClient)
	meHandlers := &MeHandlers{Sessions: sessions, ProfilePhoto: profilePhotos}
	return meHandlers, authHandlers, privyClient, iso
}

func TestMeHandler_returnsProfilePhotoUrlNullWhenUnset(t *testing.T) {
	authHandlers, privyClient, db, iso := integrationApp(t)
	store := postgres.NewStore(db)
	meHandlers := &MeHandlers{Sessions: app.NewSessionService(store, privyClient)}

	_, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "me-null-photo", "Photo Null")

	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()
	meHandlers.MeHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if _, ok := payload["profilePhotoUrl"]; !ok {
		t.Fatalf("profilePhotoUrl missing from response: %s", rec.Body.String())
	}
	if string(payload["profilePhotoUrl"]) != "null" {
		t.Fatalf("profilePhotoUrl = %s, want null", payload["profilePhotoUrl"])
	}
}

func TestUploadProfilePhotoHandler_storesPhotoAndReturnsURL(t *testing.T) {
	fakeStorage := storage.NewFakeClient("https://example.supabase.co")
	meHandlers, authHandlers, privyClient, iso := integrationMeHandlers(t, fakeStorage)

	session, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "me-upload", "Uploader")
	body, contentType := multipartPhotoBody(t, minimalPNG())

	req := httptest.NewRequest(http.MethodPost, "/v1/me/profile-photo", body)
	req.Header.Set("Authorization", "Bearer "+string(token))
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	meHandlers.UploadProfilePhotoHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("upload status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	var uploadPayload meResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &uploadPayload); err != nil {
		t.Fatalf("decode upload json: %v", err)
	}
	if uploadPayload.ProfilePhotoURL == nil || *uploadPayload.ProfilePhotoURL == "" {
		t.Fatalf("upload profilePhotoUrl missing: %s", rec.Body.String())
	}
	if !strings.Contains(*uploadPayload.ProfilePhotoURL, session.UserID) {
		t.Fatalf("profilePhotoUrl = %q, want path containing user id %q", *uploadPayload.ProfilePhotoURL, session.UserID)
	}
	if len(fakeStorage.Uploads) != 1 {
		t.Fatalf("upload count = %d, want 1", len(fakeStorage.Uploads))
	}

	getReq := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	getReq.Header.Set("Authorization", "Bearer "+string(token))
	getRec := httptest.NewRecorder()
	meHandlers.MeHandler(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get me status = %d, want 200", getRec.Code)
	}
	var getPayload meResponse
	if err := json.Unmarshal(getRec.Body.Bytes(), &getPayload); err != nil {
		t.Fatalf("decode get me json: %v", err)
	}
	if getPayload.ProfilePhotoURL == nil || *getPayload.ProfilePhotoURL != *uploadPayload.ProfilePhotoURL {
		t.Fatalf("persisted profilePhotoUrl = %v, want %q", getPayload.ProfilePhotoURL, *uploadPayload.ProfilePhotoURL)
	}
}

func TestUploadProfilePhotoHandler_rejectsInvalidImage(t *testing.T) {
	meHandlers, authHandlers, privyClient, iso := integrationMeHandlers(t, storage.NewFakeClient("https://example.supabase.co"))
	_, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "me-invalid", "Invalid")

	body, contentType := multipartPhotoBody(t, []byte("not-an-image"))
	req := httptest.NewRequest(http.MethodPost, "/v1/me/profile-photo", body)
	req.Header.Set("Authorization", "Bearer "+string(token))
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	meHandlers.UploadProfilePhotoHandler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
}

func multipartPhotoBody(t *testing.T, photo []byte) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("photo", "avatar.png")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := io.Copy(part, bytes.NewReader(photo)); err != nil {
		t.Fatalf("write photo: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return &body, writer.FormDataContentType()
}

func minimalPNG() []byte {
	return []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
		0x89, 0x00, 0x00, 0x00, 0x0A, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
		0x42, 0x60, 0x82,
	}
}
