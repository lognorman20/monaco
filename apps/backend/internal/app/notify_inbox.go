package app

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

// Inbox paging and write limits.
const (
	NotificationsDefaultPageLimit = 30
	NotificationsMaxPageLimit     = 100
	// MaxNotificationIDsPerRead bounds one "mark these read" request.
	MaxNotificationIDsPerRead = 200
)

var (
	// ErrInvalidNotificationCursor means the cursor could not be decoded.
	ErrInvalidNotificationCursor = errors.New("invalid cursor")
	// ErrInvalidNotificationLimit means the page limit is outside 1..NotificationsMaxPageLimit.
	ErrInvalidNotificationLimit = errors.New("invalid limit")
	// ErrInvalidNotificationRead means the body named neither ids nor all, or too many ids, or a bad id.
	ErrInvalidNotificationRead = errors.New("send ids or all")
	// ErrInvalidDeviceToken means the APNs token is not hex of a plausible length.
	ErrInvalidDeviceToken = errors.New("invalid device token")
	// ErrInvalidDevicePlatform means a platform other than ios.
	ErrInvalidDevicePlatform = errors.New("platform must be ios")
	// ErrInvalidDeviceAppEnv means appEnv is neither debug nor production.
	ErrInvalidDeviceAppEnv = errors.New("appEnv must be debug or production")
)

// NotificationItem is one inbox row as the member sees it.
type NotificationItem struct {
	ID              string
	Kind            string
	Category        string
	Title           string
	Body            string
	GroupID         string
	GroupName       string
	GroupPictureURL string
	ProposalID      string
	TransactionID   string
	Symbol          string
	ReadAt          *time.Time
	CreatedAt       time.Time
}

// NotificationsPage is one newest-first page. NextCursor is empty on the last page.
type NotificationsPage struct {
	Items       []NotificationItem
	UnreadCount int
	NextCursor  string
}

// NotificationService serves the signed-in member's inbox and push devices.
type NotificationService struct {
	store *postgres.Store
	privy privy.Client
	now   func() time.Time
}

// NewNotificationService wires the inbox routes.
func NewNotificationService(store *postgres.Store, privyClient privy.Client) *NotificationService {
	return &NotificationService{store: store, privy: privyClient, now: time.Now}
}

// SetClock overrides time.Now. For tests.
func (s *NotificationService) SetClock(now func() time.Time) {
	if now == nil {
		now = time.Now
	}
	s.now = now
}

// List returns up to limit notifications older than cursor (newest first when cursor is empty),
// with the member's unread count.
func (s *NotificationService) List(ctx context.Context, accessToken, cursor string, limit int) (NotificationsPage, error) {
	if limit < 1 || limit > NotificationsMaxPageLimit {
		return NotificationsPage{}, ErrInvalidNotificationLimit
	}
	var before *postgres.NotificationCursor
	if strings.TrimSpace(cursor) != "" {
		decoded, err := decodeNotificationCursor(cursor)
		if err != nil {
			return NotificationsPage{}, err
		}
		before = &decoded
	}
	userID, err := s.userID(ctx, accessToken)
	if err != nil {
		return NotificationsPage{}, err
	}
	rows, err := s.store.ListNotifications(ctx, userID, before, limit+1)
	if err != nil {
		return NotificationsPage{}, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	unread, err := s.store.CountUnreadNotifications(ctx, userID)
	if err != nil {
		return NotificationsPage{}, err
	}
	page := NotificationsPage{Items: make([]NotificationItem, 0, len(rows)), UnreadCount: unread}
	for _, row := range rows {
		page.Items = append(page.Items, notificationItemFromRow(row))
	}
	if hasMore {
		last := rows[len(rows)-1]
		page.NextCursor = encodeNotificationCursor(postgres.NotificationCursor{CreatedAt: last.CreatedAt, ID: last.ID})
	}
	return page, nil
}

// MarkRead marks ids (or every row, with all) read and returns the unread count left.
func (s *NotificationService) MarkRead(ctx context.Context, accessToken string, ids []string, all bool) (int, error) {
	if all == (len(ids) > 0) || len(ids) > MaxNotificationIDsPerRead {
		// Exactly one of the two: ids to mark, or all.
		return 0, ErrInvalidNotificationRead
	}
	for _, id := range ids {
		if !isUUID(id) {
			return 0, ErrInvalidNotificationRead
		}
	}
	userID, err := s.userID(ctx, accessToken)
	if err != nil {
		return 0, err
	}
	now := s.now()
	if all {
		_, err = s.store.MarkAllNotificationsRead(ctx, userID, now)
	} else {
		_, err = s.store.MarkNotificationsRead(ctx, userID, ids, now)
	}
	if err != nil {
		return 0, err
	}
	return s.store.CountUnreadNotifications(ctx, userID)
}

// RegisterDevice records the install's APNs token for the member (and moves it off anyone else).
func (s *NotificationService) RegisterDevice(ctx context.Context, accessToken, deviceToken, platform, appEnv string) error {
	token, err := NormalizeDeviceToken(deviceToken)
	if err != nil {
		return err
	}
	if strings.TrimSpace(platform) != "ios" {
		return ErrInvalidDevicePlatform
	}
	appEnv = strings.TrimSpace(appEnv)
	if appEnv != "debug" && appEnv != "production" {
		return ErrInvalidDeviceAppEnv
	}
	userID, err := s.userID(ctx, accessToken)
	if err != nil {
		return err
	}
	return s.store.UpsertDeviceToken(ctx, userID, token, "ios", appEnv, s.now())
}

// UnregisterDevice forgets the member's install. A token that is not theirs, or unknown, is a no-op.
func (s *NotificationService) UnregisterDevice(ctx context.Context, accessToken, deviceToken string) error {
	token, err := NormalizeDeviceToken(deviceToken)
	if err != nil {
		return err
	}
	userID, err := s.userID(ctx, accessToken)
	if err != nil {
		return err
	}
	_, err = s.store.DeleteDeviceTokenForUser(ctx, userID, token)
	return err
}

// NormalizeDeviceToken lowercases an APNs token and checks it is hex of a plausible length.
// Apple's tokens are 32 bytes today (64 hex characters) and documented as variable length.
func NormalizeDeviceToken(raw string) (string, error) {
	token := strings.ToLower(strings.TrimSpace(raw))
	if len(token) < 32 || len(token) > 256 {
		return "", ErrInvalidDeviceToken
	}
	for _, r := range token {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return "", ErrInvalidDeviceToken
		}
	}
	return token, nil
}

func (s *NotificationService) userID(ctx context.Context, accessToken string) (string, error) {
	identity, err := s.privy.VerifySession(ctx, privy.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			return "", privy.ErrInvalidToken
		}
		return "", fmt.Errorf("verify session: %w", err)
	}
	user, found, err := s.store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
	if err != nil {
		return "", err
	}
	if !found {
		return "", ErrUserNotFound
	}
	return user.ID, nil
}

func notificationItemFromRow(row postgres.NotificationRow) NotificationItem {
	item := NotificationItem{
		ID:              row.ID,
		Kind:            row.Kind,
		Category:        NotificationCategory(row.Kind),
		Title:           row.Title,
		Body:            row.Body,
		GroupID:         row.GroupID.String,
		GroupName:       row.GroupName.String,
		GroupPictureURL: row.GroupPictureURL.String,
		ProposalID:      row.ProposalID.String,
		TransactionID:   row.TransactionID.String,
		Symbol:          row.Symbol.String,
		CreatedAt:       row.CreatedAt.UTC(),
	}
	if row.ReadAt.Valid {
		at := row.ReadAt.Time.UTC()
		item.ReadAt = &at
	}
	return item
}

// Cursor format: base64url("<RFC3339Nano UTC>|<notification uuid>"). Opaque to clients.
func encodeNotificationCursor(c postgres.NotificationCursor) string {
	raw := c.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + c.ID
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeNotificationCursor(cursor string) (postgres.NotificationCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(cursor))
	if err != nil {
		return postgres.NotificationCursor{}, ErrInvalidNotificationCursor
	}
	ts, id, ok := strings.Cut(string(raw), "|")
	if !ok || !isUUID(id) {
		return postgres.NotificationCursor{}, ErrInvalidNotificationCursor
	}
	createdAt, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return postgres.NotificationCursor{}, ErrInvalidNotificationCursor
	}
	return postgres.NotificationCursor{CreatedAt: createdAt.UTC(), ID: id}, nil
}
