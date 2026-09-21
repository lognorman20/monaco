package httpapi

import (
	"bytes"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/ratelimit"
	"github.com/monaco/monaco/apps/backend/internal/storage"
)

// pictureFixture is a multi-user app plus the cabal picture handlers, so a test
// can create a club over HTTP and then act on its picture as any member.
type pictureFixture struct {
	*multiUserApp
	Pictures *GroupPictureHandlers
	Storage  *storage.FakeClient
}

func newPictureFixture(t *testing.T) *pictureFixture {
	t.Helper()
	return newPictureFixtureWithLimiter(t, nil)
}

func newPictureFixtureWithLimiter(t *testing.T, limiter *ratelimit.Limiter) *pictureFixture {
	t.Helper()
	multi := newMultiUserApp(t)
	fakeStorage := storage.NewFakeClient("https://example.supabase.co")
	service := app.NewGroupPictureService(multi.Store, multi.Auth.Verifier, fakeStorage)
	if limiter != nil {
		service = service.WithWriteLimiter(limiter)
	}
	return &pictureFixture{
		multiUserApp: multi,
		Pictures:     &GroupPictureHandlers{Pictures: service},
		Storage:      fakeStorage,
	}
}

// upload posts data as the cabal picture for club c, as member.
func (f *pictureFixture) upload(t *testing.T, member clubMember, groupID string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	body, contentType := multipartPictureBody(t, data)
	req := httptest.NewRequest(http.MethodPost, "/v1/groups/"+groupID+"/picture", body)
	req.Header.Set("Content-Type", contentType)
	if member.Token != "" {
		req.Header.Set("Authorization", "Bearer "+string(member.Token))
	}
	req.SetPathValue("id", groupID)
	rec := httptest.NewRecorder()
	f.Pictures.UploadGroupPictureHandler(rec, req)
	return rec
}

func (f *pictureFixture) remove(t *testing.T, member clubMember, groupID string) *httptest.ResponseRecorder {
	t.Helper()
	return f.call(t, f.Pictures.RemoveGroupPictureHandler, http.MethodDelete,
		"/v1/groups/"+groupID+"/picture", member.Token, "", "id", groupID)
}

func multipartPictureBody(t *testing.T, picture []byte) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("picture", "cabal.png")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := io.Copy(part, bytes.NewReader(picture)); err != nil {
		t.Fatalf("write picture: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return &body, writer.FormDataContentType()
}

// samplePicture returns a decodable, opaque PNG of the given size.
func samplePicture(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 0x80, A: 0xFF})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// groupPictureURL reads the persisted column, so a test can tell an echoed
// response apart from a value that actually landed in the database.
func groupPictureURL(t *testing.T, store *postgres.Store, groupID string) string {
	t.Helper()
	group, found, err := store.GetGroupByID(t.Context(), groupID)
	if err != nil {
		t.Fatalf("GetGroupByID: %v", err)
	}
	if !found {
		t.Fatalf("group %s not found", groupID)
	}
	if !group.PictureURL.Valid {
		return ""
	}
	return group.PictureURL.String
}

func TestUploadGroupPicture_creatorStoresPictureAndItPersists(t *testing.T) {
	f := newPictureFixture(t)
	owner := f.signIn(t, "pic-owner", "Picture Owner")
	c := f.createClub(t, owner, "Picture Club")

	rec := f.upload(t, owner, c.ID, samplePicture(t, 80, 80))
	requireStatus(t, rec, http.StatusOK, "creator POST /v1/groups/{id}/picture")

	payload := decodeBody[groupPictureResponse](t, rec)
	if payload.GroupID != c.ID {
		t.Fatalf("groupId = %q, want %q", payload.GroupID, c.ID)
	}
	if payload.PictureURL == nil || *payload.PictureURL == "" {
		t.Fatalf("pictureUrl missing: %s", rec.Body.String())
	}
	// The key is namespaced by group so a group id can never collide with a user id.
	if !strings.Contains(*payload.PictureURL, "groups/"+c.ID+"/") {
		t.Fatalf("pictureUrl = %q, want a groups/<id>/ key", *payload.PictureURL)
	}
	if len(f.Storage.Uploads) != 1 {
		t.Fatalf("storage upload count = %d, want 1", len(f.Storage.Uploads))
	}
	if persisted := groupPictureURL(t, f.Store, c.ID); persisted != *payload.PictureURL {
		t.Fatalf("persisted picture_url = %q, want %q", persisted, *payload.PictureURL)
	}
}

func TestUploadGroupPicture_replacingWritesANewKeySoCachesCannotServeTheOldOne(t *testing.T) {
	f := newPictureFixture(t)
	owner := f.signIn(t, "pic-replace", "Replacer")
	c := f.createClub(t, owner, "Replace Club")

	first := decodeBody[groupPictureResponse](t, f.upload(t, owner, c.ID, samplePicture(t, 40, 40)))
	second := decodeBody[groupPictureResponse](t, f.upload(t, owner, c.ID, samplePicture(t, 60, 60)))

	if first.PictureURL == nil || second.PictureURL == nil {
		t.Fatalf("expected both uploads to return a url")
	}
	if *first.PictureURL == *second.PictureURL {
		t.Fatalf("replacement reused the object key %q", *first.PictureURL)
	}
	if persisted := groupPictureURL(t, f.Store, c.ID); persisted != *second.PictureURL {
		t.Fatalf("persisted picture_url = %q, want the replacement %q", persisted, *second.PictureURL)
	}
}

func TestRemoveGroupPicture_creatorClearsItBackToNull(t *testing.T) {
	f := newPictureFixture(t)
	owner := f.signIn(t, "pic-remove", "Remover")
	c := f.createClub(t, owner, "Remove Club")

	requireStatus(t, f.upload(t, owner, c.ID, samplePicture(t, 48, 48)), http.StatusOK, "upload")

	rec := f.remove(t, owner, c.ID)
	requireStatus(t, rec, http.StatusOK, "creator DELETE /v1/groups/{id}/picture")

	payload := decodeBody[groupPictureResponse](t, rec)
	if payload.PictureURL != nil {
		t.Fatalf("pictureUrl = %v, want null after removal", *payload.PictureURL)
	}
	if persisted := groupPictureURL(t, f.Store, c.ID); persisted != "" {
		t.Fatalf("persisted picture_url = %q, want empty after removal", persisted)
	}
}

// A faker demo club (#153) is a read-only spectator fixture: even the user
// recorded as its creator cannot change its picture, and a refused write never
// reaches storage or the column.
func TestGroupPicture_fakerDemoClubRefusesEveryWrite(t *testing.T) {
	f := newPictureFixture(t)
	owner := f.signIn(t, "pic-faker", "Faker Owner")
	c := f.createClub(t, owner, "Faker Club")
	tx, err := f.Store.BeginTx(t.Context())
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := tx.ExecContext(t.Context(),
		`UPDATE groups SET is_faker = true, faker_key = $2 WHERE id = $1`, c.ID, "test:picture:"+c.ID); err != nil {
		_ = tx.Rollback()
		t.Fatalf("flag club as faker: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	upload := f.upload(t, owner, c.ID, samplePicture(t, 40, 40))
	requireStatus(t, upload, http.StatusForbidden, "creator POST picture on a faker club")
	if !strings.Contains(upload.Body.String(), "demo club is read-only") {
		t.Fatalf("upload body = %s, want the read-only demo club error", upload.Body.String())
	}

	remove := f.remove(t, owner, c.ID)
	requireStatus(t, remove, http.StatusForbidden, "creator DELETE picture on a faker club")
	if !strings.Contains(remove.Body.String(), "demo club is read-only") {
		t.Fatalf("remove body = %s, want the read-only demo club error", remove.Body.String())
	}

	if len(f.Storage.Uploads) != 0 {
		t.Fatalf("storage upload count = %d, want 0: a faker club must not reach storage", len(f.Storage.Uploads))
	}
	if persisted := groupPictureURL(t, f.Store, c.ID); persisted != "" {
		t.Fatalf("persisted picture_url = %q, want empty", persisted)
	}
}

// The permission check: a member who did not create the cabal is refused, and
// told why, because they can already see the cabal exists.
func TestUploadGroupPicture_memberWhoIsNotTheCreatorIsForbidden(t *testing.T) {
	f := newPictureFixture(t)
	owner := f.signIn(t, "pic-owner2", "Owner Two")
	member := f.signIn(t, "pic-member", "Plain Member")
	c := f.createClub(t, owner, "Member Club")
	f.join(t, member, c)

	rec := f.upload(t, member, c.ID, samplePicture(t, 40, 40))
	requireStatus(t, rec, http.StatusForbidden, "member POST /v1/groups/{id}/picture")

	if len(f.Storage.Uploads) != 0 {
		t.Fatalf("storage upload count = %d, want 0: a refused caller must not reach storage", len(f.Storage.Uploads))
	}
	if persisted := groupPictureURL(t, f.Store, c.ID); persisted != "" {
		t.Fatalf("persisted picture_url = %q, want empty", persisted)
	}
}

func TestRemoveGroupPicture_memberWhoIsNotTheCreatorIsForbidden(t *testing.T) {
	f := newPictureFixture(t)
	owner := f.signIn(t, "pic-owner3", "Owner Three")
	member := f.signIn(t, "pic-member2", "Plain Member Two")
	c := f.createClub(t, owner, "Remove Guard Club")
	f.join(t, member, c)
	requireStatus(t, f.upload(t, owner, c.ID, samplePicture(t, 40, 40)), http.StatusOK, "owner upload")

	rec := f.remove(t, member, c.ID)
	requireStatus(t, rec, http.StatusForbidden, "member DELETE /v1/groups/{id}/picture")

	if persisted := groupPictureURL(t, f.Store, c.ID); persisted == "" {
		t.Fatalf("a refused removal cleared the picture anyway")
	}
}

// A non-member is told the cabal does not exist: they should learn nothing about
// a club they cannot see, not even that their guess at an id was right.
func TestUploadGroupPicture_nonMemberGetsNotFound(t *testing.T) {
	f := newPictureFixture(t)
	owner := f.signIn(t, "pic-owner4", "Owner Four")
	stranger := f.signIn(t, "pic-stranger", "Stranger")
	c := f.createClub(t, owner, "Private Club")

	rec := f.upload(t, stranger, c.ID, samplePicture(t, 40, 40))
	requireStatus(t, rec, http.StatusNotFound, "stranger POST /v1/groups/{id}/picture")

	if len(f.Storage.Uploads) != 0 {
		t.Fatalf("storage upload count = %d, want 0", len(f.Storage.Uploads))
	}
}

func TestUploadGroupPicture_unknownGroupGetsNotFound(t *testing.T) {
	f := newPictureFixture(t)
	owner := f.signIn(t, "pic-owner5", "Owner Five")

	rec := f.upload(t, owner, "11111111-1111-4111-8111-111111111111", samplePicture(t, 40, 40))
	requireStatus(t, rec, http.StatusNotFound, "unknown group POST /v1/groups/{id}/picture")
}

func TestUploadGroupPicture_withoutATokenIsUnauthorized(t *testing.T) {
	f := newPictureFixture(t)
	owner := f.signIn(t, "pic-owner6", "Owner Six")
	c := f.createClub(t, owner, "Auth Club")

	rec := f.upload(t, clubMember{}, c.ID, samplePicture(t, 40, 40))
	requireStatus(t, rec, http.StatusUnauthorized, "anonymous POST /v1/groups/{id}/picture")
}

// Validation failures. Each one is the client's mistake and must not reach storage.
func TestUploadGroupPicture_validationFailures(t *testing.T) {
	cases := []struct {
		name       string
		picture    []byte
		wantStatus int
	}{
		{
			name:       "wrong type",
			picture:    []byte("#!/bin/sh\necho not an image\n"),
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "malformed image behind a real png header",
			// The sniff passes on the signature; the decode must still fail.
			picture:    append([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, bytes.Repeat([]byte{0x41}, 256)...),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty file",
			picture:    nil,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "gif is not an accepted format",
			picture:    []byte("GIF89a" + strings.Repeat("\x00", 64)),
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newPictureFixture(t)
			owner := f.signIn(t, "pic-invalid", "Invalid Uploader")
			c := f.createClub(t, owner, "Invalid Club")

			rec := f.upload(t, owner, c.ID, tc.picture)
			requireStatus(t, rec, tc.wantStatus, tc.name)

			if len(f.Storage.Uploads) != 0 {
				t.Fatalf("storage upload count = %d, want 0", len(f.Storage.Uploads))
			}
			if persisted := groupPictureURL(t, f.Store, c.ID); persisted != "" {
				t.Fatalf("persisted picture_url = %q, want empty", persisted)
			}
		})
	}
}

func TestUploadGroupPicture_oversizedBodyIsRejected(t *testing.T) {
	f := newPictureFixture(t)
	owner := f.signIn(t, "pic-big", "Big Uploader")
	c := f.createClub(t, owner, "Big Club")

	// Over the 2MB cap, with a valid PNG header so the rejection is about size.
	oversized := append(samplePicture(t, 8, 8), bytes.Repeat([]byte{0x7F}, 3<<20)...)

	// One status for "too big", shared with the profile photo upload: the app
	// maps 413 to its own copy, so a 400 here would read as "not an image".
	rec := f.upload(t, owner, c.ID, oversized)
	requireStatus(t, rec, http.StatusRequestEntityTooLarge, "oversized picture")
	if len(f.Storage.Uploads) != 0 {
		t.Fatalf("storage upload count = %d, want 0", len(f.Storage.Uploads))
	}
}

// A small file that decodes to an enormous bitmap is refused on its header,
// before the pixels are ever allocated.
func TestUploadGroupPicture_decompressionBombIsRejectedOnItsHeader(t *testing.T) {
	f := newPictureFixture(t)
	owner := f.signIn(t, "pic-bomb", "Bomb Uploader")
	c := f.createClub(t, owner, "Bomb Club")

	bomb := hugeDimensionPNG(t, 20000, 20000)
	if len(bomb) > 2<<20 {
		t.Fatalf("fixture is %d bytes; it must be small enough that only the dimension cap can reject it", len(bomb))
	}

	rec := f.upload(t, owner, c.ID, bomb)
	requireStatus(t, rec, http.StatusBadRequest, "decompression bomb")
	if len(f.Storage.Uploads) != 0 {
		t.Fatalf("storage upload count = %d, want 0", len(f.Storage.Uploads))
	}
}

// hugeDimensionPNG builds a tiny PNG whose IHDR claims an enormous size. The
// image data is never read, because the dimension check runs first.
func hugeDimensionPNG(t *testing.T, width, height uint32) []byte {
	t.Helper()
	var out bytes.Buffer
	out.Write([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A})

	var ihdr bytes.Buffer
	ihdr.WriteString("IHDR")
	writeUint32(&ihdr, width)
	writeUint32(&ihdr, height)
	// 8-bit RGBA, no interlace.
	ihdr.Write([]byte{8, 6, 0, 0, 0})
	writeChunk(&out, ihdr.Bytes())

	return out.Bytes()
}

func writeUint32(buf *bytes.Buffer, v uint32) {
	buf.Write([]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
}

func writeChunk(out *bytes.Buffer, typeAndData []byte) {
	writeUint32(out, uint32(len(typeAndData)-4))
	out.Write(typeAndData)
	writeUint32(out, crc32.ChecksumIEEE(typeAndData))
}

func TestUploadGroupPicture_notConfiguredWithoutStorage(t *testing.T) {
	multi := newMultiUserApp(t)
	handlers := &GroupPictureHandlers{Pictures: app.NewGroupPictureService(multi.Store, multi.Auth.Verifier, nil)}
	f := &pictureFixture{multiUserApp: multi, Pictures: handlers, Storage: storage.NewFakeClient("https://unused")}

	owner := f.signIn(t, "pic-nostore", "No Storage")
	c := f.createClub(t, owner, "No Storage Club")

	rec := f.upload(t, owner, c.ID, samplePicture(t, 32, 32))
	requireStatus(t, rec, http.StatusServiceUnavailable, "no storage configured")
}

func TestUploadGroupPicture_rateLimitedReturns429WithRetryAfter(t *testing.T) {
	// One request allowed, then a long wait: the second upload must be refused.
	f := newPictureFixtureWithLimiter(t, ratelimit.New(1, time.Minute))
	owner := f.signIn(t, "pic-limit", "Limited Uploader")
	c := f.createClub(t, owner, "Limited Club")

	requireStatus(t, f.upload(t, owner, c.ID, samplePicture(t, 32, 32)), http.StatusOK, "first upload")

	rec := f.upload(t, owner, c.ID, samplePicture(t, 32, 32))
	requireStatus(t, rec, http.StatusTooManyRequests, "second upload")
	if retryAfter := rec.Header().Get("Retry-After"); retryAfter == "" {
		t.Fatalf("429 without a Retry-After header; body = %s", rec.Body.String())
	}
	if len(f.Storage.Uploads) != 1 {
		t.Fatalf("storage upload count = %d, want 1: the refused upload must not reach storage", len(f.Storage.Uploads))
	}
}

// The group view is what the app reads, so the picture and the creator flag have
// to arrive there, not just in the upload response.
func TestGroupView_carriesPictureUrlAndIsCreator(t *testing.T) {
	f := newPictureFixture(t)
	owner := f.signIn(t, "pic-view-owner", "View Owner")
	member := f.signIn(t, "pic-view-member", "View Member")
	c := f.createClub(t, owner, "View Club")
	f.join(t, member, c)

	// Before any upload: null picture, and only the creator is told they are one.
	ownerView := f.groupView(t, owner, c)
	if ownerView.PictureURL != nil {
		t.Fatalf("pictureUrl = %v, want null before any upload", *ownerView.PictureURL)
	}
	if !ownerView.IsCreator {
		t.Fatalf("isCreator = false for the cabal's creator")
	}
	if memberView := f.groupView(t, member, c); memberView.IsCreator {
		t.Fatalf("isCreator = true for a member who did not create the cabal")
	}

	uploaded := decodeBody[groupPictureResponse](t, f.upload(t, owner, c.ID, samplePicture(t, 64, 64)))

	for _, viewer := range []clubMember{owner, member} {
		view := f.groupView(t, viewer, c)
		if view.PictureURL == nil || *view.PictureURL != *uploaded.PictureURL {
			t.Fatalf("%s sees pictureUrl = %v, want %q", viewer.Name, view.PictureURL, *uploaded.PictureURL)
		}
	}

	requireStatus(t, f.remove(t, owner, c.ID), http.StatusOK, "remove")
	if after := f.groupView(t, member, c); after.PictureURL != nil {
		t.Fatalf("pictureUrl = %v after removal, want null", *after.PictureURL)
	}
}

// A client built against the payload before this change must keep decoding it.
func TestGroupView_staysDecodableByAClientThatDoesNotKnowThePictureFields(t *testing.T) {
	f := newPictureFixture(t)
	owner := f.signIn(t, "pic-compat", "Compat Owner")
	c := f.createClub(t, owner, "Compat Club")
	requireStatus(t, f.upload(t, owner, c.ID, samplePicture(t, 32, 32)), http.StatusOK, "upload")

	rec := f.call(t, f.Groups.GetGroupViewHandler, http.MethodGet, "/v1/groups/"+c.ID+"/view", owner.Token, "", "id", c.ID)
	requireStatus(t, rec, http.StatusOK, "GET /v1/groups/{id}/view")

	// The shape as it was before pictureUrl and isCreator existed.
	var legacy struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		TreasuryAddress string `json:"treasuryAddress"`
		PotTotalUsd     string `json:"potTotalUsd"`
		Pot             []any  `json:"pot"`
		Members         []any  `json:"members"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &legacy); err != nil {
		t.Fatalf("a client that predates the picture fields can no longer decode the view: %v", err)
	}
	if legacy.ID != c.ID || legacy.Name != c.Name {
		t.Fatalf("legacy decode lost fields: %+v", legacy)
	}
}

// Every board that draws a cabal's mark reads the picture from its own payload,
// so each one has to carry it: the home board, the dashboard's "your cabals"
// rows, search, the leaderboard, GET /v1/groups/{id} and a member's shared
// cabals (GET /v1/users/{id}/groups).
func TestGroupPicture_reachesEveryPayloadThatDrawsTheMark(t *testing.T) {
	f := newPictureFixture(t)
	owner := f.signIn(t, "pic-boards", "Boards Owner")
	member := f.signIn(t, "pic-boards-member", "Boards Member")
	c := f.createClub(t, owner, "Boards Club")
	f.deposit(t, owner, c, 5_000_000)

	uploaded := decodeBody[groupPictureResponse](t, f.upload(t, owner, c.ID, samplePicture(t, 32, 32)))
	if uploaded.PictureURL == nil {
		t.Fatalf("upload returned no pictureUrl")
	}
	want := *uploaded.PictureURL

	homeRow, found := homeGroupRow(f.home(t, owner).Groups, c.ID)
	if !found {
		t.Fatalf("club %s missing from GET /v1/home groups", c.ID)
	}
	if got := derefOrNil(homeRow.PictureURL); got != want {
		t.Fatalf("home board pictureUrl = %q, want %q", got, want)
	}

	myGroup := findMyGroup(t, f.dashboard(t, owner).MyGroups, c)
	if got := derefOrNil(myGroup.PictureURL); got != want {
		t.Fatalf("dashboard myGroups pictureUrl = %q, want %q", got, want)
	}

	tab := &GroupsTabHandlers{GroupsTab: app.NewGroupsTabService(f.Groups.Home, f.Store)}
	search := f.call(t, tab.SearchGroupsHandler, http.MethodGet,
		"/v1/groups/search?q="+url.QueryEscape(c.Name), owner.Token, "")
	requireStatus(t, search, http.StatusOK, "GET /v1/groups/search")
	var searchRow *groupDiscoveryRowResponse
	for _, row := range decodeBody[groupSearchResponse](t, search).Groups {
		if row.GroupID == c.ID {
			searchRow = &row
		}
	}
	if searchRow == nil {
		t.Fatalf("club %s missing from search; body = %s", c.ID, search.Body.String())
	}
	if got := derefOrNil(searchRow.PictureURL); got != want {
		t.Fatalf("search pictureUrl = %q, want %q", got, want)
	}

	board := f.call(t, tab.GroupLeaderboardHandler, http.MethodGet,
		"/v1/groups/leaderboard?limit="+strconv.Itoa(app.GroupsTabMaxLimit), owner.Token, "")
	requireStatus(t, board, http.StatusOK, "GET /v1/groups/leaderboard")
	var boardRow *groupLeaderboardRowResponse
	for _, row := range decodeBody[groupLeaderboardResponse](t, board).Groups {
		if row.GroupID == c.ID {
			boardRow = &row
		}
	}
	if boardRow == nil {
		t.Fatalf("funded club %s missing from the leaderboard; body = %s", c.ID, board.Body.String())
	}
	if got := derefOrNil(boardRow.PictureURL); got != want {
		t.Fatalf("leaderboard pictureUrl = %q, want %q", got, want)
	}

	group := f.call(t, f.Groups.GetGroupHandler, http.MethodGet, "/v1/groups/"+c.ID, owner.Token, "", "id", c.ID)
	requireStatus(t, group, http.StatusOK, "GET /v1/groups/{id}")
	if got := derefOrNil(decodeBody[getGroupResponse](t, group).PictureURL); got != want {
		t.Fatalf("GET /v1/groups/{id} pictureUrl = %q, want %q", got, want)
	}

	// Another member looking at the owner's profile sees the cabals they share.
	f.join(t, member, c)
	shared := f.call(t, f.Home.UserSharedGroupsHandler, http.MethodGet,
		"/v1/users/"+owner.UserID+"/groups", member.Token, "", "id", owner.UserID)
	requireStatus(t, shared, http.StatusOK, "GET /v1/users/{id}/groups")
	sharedRow, found := homeGroupRow(decodeBody[map[string][]homeGroupBoardRowResponse](t, shared)["groups"], c.ID)
	if !found {
		t.Fatalf("shared club %s missing from GET /v1/users/{id}/groups; body = %s", c.ID, shared.Body.String())
	}
	if got := derefOrNil(sharedRow.PictureURL); got != want {
		t.Fatalf("shared groups pictureUrl = %q, want %q", got, want)
	}

	// A cabal with no picture sends an explicit null, not a blank string.
	requireStatus(t, f.remove(t, owner, c.ID), http.StatusOK, "remove")
	cleared, _ := homeGroupRow(f.home(t, owner).Groups, c.ID)
	if cleared.PictureURL != nil {
		t.Fatalf("home board pictureUrl = %q after removal, want null", *cleared.PictureURL)
	}
	clearedGroup := f.call(t, f.Groups.GetGroupHandler, http.MethodGet, "/v1/groups/"+c.ID, owner.Token, "", "id", c.ID)
	if !strings.Contains(clearedGroup.Body.String(), `"pictureUrl":null`) {
		t.Fatalf("GET /v1/groups/{id} after removal = %s, want an explicit null pictureUrl", clearedGroup.Body.String())
	}
}

func homeGroupRow(rows []homeGroupBoardRowResponse, groupID string) (homeGroupBoardRowResponse, bool) {
	for _, row := range rows {
		if row.GroupID == groupID {
			return row, true
		}
	}
	return homeGroupBoardRowResponse{}, false
}
