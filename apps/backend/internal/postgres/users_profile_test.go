package postgres

import (
	"context"
	"testing"
)

func TestUpdateUserDisplayName_updatesRowAndReportsMissingUser(t *testing.T) {
	t.Parallel()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	ctx := context.Background()

	user, err := store.UpsertUser(ctx, iso.UniqueDynamicID("rename"), "Before")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	iso.TrackUser(user.ID)

	updated, found, err := store.UpdateUserDisplayName(ctx, user.ID, "After")
	if err != nil || !found {
		t.Fatalf("UpdateUserDisplayName found=%v err=%v, want found", found, err)
	}
	if updated.DisplayName.String != "After" || updated.ID != user.ID || !updated.CreatedAt.Equal(user.CreatedAt) {
		t.Fatalf("updated = %+v, want same user renamed to After", updated)
	}

	_, found, err = store.UpdateUserDisplayName(ctx, "00000000-0000-0000-0000-000000000000", "Nobody")
	if err != nil {
		t.Fatalf("UpdateUserDisplayName unknown user err = %v, want nil", err)
	}
	if found {
		t.Fatal("found = true for unknown user id")
	}
}

func TestListUserProfilesByIDs_returnsNamesAndPhotosInOneMap(t *testing.T) {
	t.Parallel()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	ctx := context.Background()

	withPhoto, err := store.UpsertUser(ctx, iso.UniqueDynamicID("profiles-photo"), "Has Photo")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	iso.TrackUser(withPhoto.ID)
	if _, err := store.UpdateUserProfilePhotoURL(ctx, withPhoto.ID, "https://example.test/a.png"); err != nil {
		t.Fatalf("UpdateUserProfilePhotoURL: %v", err)
	}
	unnamed, err := store.UpsertUser(ctx, iso.UniqueDynamicID("profiles-unnamed"), "")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	iso.TrackUser(unnamed.ID)

	profiles, err := store.ListUserProfilesByIDs(ctx, []string{withPhoto.ID, unnamed.ID})
	if err != nil {
		t.Fatalf("ListUserProfilesByIDs: %v", err)
	}

	if got := profiles[withPhoto.ID]; got.DisplayName != "Has Photo" || got.ProfilePhotoURL != "https://example.test/a.png" {
		t.Fatalf("withPhoto profile = %+v", got)
	}
	got, ok := profiles[unnamed.ID]
	if !ok {
		t.Fatal("unnamed user missing from map")
	}
	if got.DisplayName != "" || got.ProfilePhotoURL != "" {
		t.Fatalf("unnamed profile = %+v, want empty strings", got)
	}

	empty, err := store.ListUserProfilesByIDs(ctx, nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty ids = %v, %v; want empty map", empty, err)
	}
}
