package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// #153: HTTP mapping for faker scale clubs — spectator view is 200, join is 403 read-only.
func TestFakerScaleClub_viewIs200AndJoinIs403(t *testing.T) {
	homeHandlers, authHandlers, groupHandlers, privyClient, store, iso := integrationHomeApp(t)
	groupHandlers.Home = homeHandlers.Home
	_, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "spectator", "Spectator")
	ctx := context.Background()

	sfx := iso.Suffix()
	fakerUser, err := store.UpsertUser(ctx, "faker:user:test-"+sfx+"-creator", "Rowan Ellis")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	iso.TrackUser(fakerUser.ID)
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	var groupID string
	if _, err := tx.ExecContext(ctx, `UPDATE users SET is_faker = true WHERE id = $1`, fakerUser.ID); err != nil {
		t.Fatalf("flag faker user: %v", err)
	}
	if err := tx.QueryRowContext(ctx, `INSERT INTO groups (name, creator_user_id, is_faker, faker_key) VALUES ($1, $2, true, $3) RETURNING id`, "Scale "+sfx, fakerUser.ID, "test:"+sfx).Scan(&groupID); err != nil {
		t.Fatalf("insert faker group: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO treasuries (group_id, privy_wallet_id, solana_address) VALUES ($1, $2, $3)`, groupID, "faker:treasury:"+sfx, "faker-treasury-"+sfx); err != nil {
		t.Fatalf("insert faker treasury: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)`, groupID, fakerUser.ID); err != nil {
		t.Fatalf("insert faker member: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	iso.TrackGroup(groupID)

	viewReq := httptest.NewRequest(http.MethodGet, "/v1/groups/"+groupID+"/view", nil)
	viewReq.SetPathValue("id", groupID)
	viewReq.Header.Set("Authorization", "Bearer "+string(token))
	viewRec := httptest.NewRecorder()
	groupHandlers.GetGroupViewHandler(viewRec, viewReq)
	if viewRec.Code != http.StatusOK {
		t.Fatalf("view status = %d, want 200; body = %s", viewRec.Code, viewRec.Body.String())
	}

	joinReq := httptest.NewRequest(http.MethodPost, "/v1/groups/"+groupID+"/join", nil)
	joinReq.SetPathValue("id", groupID)
	joinReq.Header.Set("Authorization", "Bearer "+string(token))
	joinRec := httptest.NewRecorder()
	groupHandlers.JoinGroupHandler(joinRec, joinReq)
	if joinRec.Code != http.StatusForbidden {
		t.Fatalf("join status = %d, want 403; body = %s", joinRec.Code, joinRec.Body.String())
	}
}
