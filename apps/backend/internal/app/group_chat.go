package app

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

// Cabal chat limits.
const (
	GroupMessageMaxChars          = 2000
	GroupMessagesDefaultPageLimit = 30
	GroupMessagesMaxPageLimit     = 100

	// A member may post a burst of messages, then one per refill interval.
	groupMessageBurst  = 10
	groupMessageRefill = time.Second
)

var (
	// ErrMessageEmpty means the message body is blank after trimming whitespace.
	ErrMessageEmpty = errors.New("message is empty")
	// ErrMessageTooLong means the message body exceeds GroupMessageMaxChars.
	ErrMessageTooLong = errors.New("message is too long")
	// ErrInvalidMessageCursor means the before cursor could not be decoded.
	ErrInvalidMessageCursor = errors.New("invalid cursor")
	// ErrInvalidMessageLimit means the page limit is outside 1..GroupMessagesMaxPageLimit.
	ErrInvalidMessageLimit = errors.New("invalid limit")
)

// RateLimitedError means the caller must wait RetryAfter before trying again.
type RateLimitedError struct {
	RetryAfter time.Duration
}

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("rate limited; retry after %s", e.RetryAfter)
}

// GroupMessage is one chat message as seen by the viewer.
type GroupMessage struct {
	ID         string
	GroupID    string
	AuthorID   string
	AuthorName string
	Body       string
	CreatedAt  time.Time
	Mine       bool
}

// GroupMessagesPage is one newest-first page of chat messages.
// NextCursor is empty when there are no older messages.
type GroupMessagesPage struct {
	Messages   []GroupMessage
	NextCursor string
}

// GroupChatService reads and posts cabal chat messages for group members.
type GroupChatService struct {
	store   *postgres.Store
	privy   privy.Client
	limiter *KeyedRateLimiter
}

// NewGroupChatService wires chat with the default per-user posting limit.
func NewGroupChatService(store *postgres.Store, privyClient privy.Client) *GroupChatService {
	return NewGroupChatServiceWithLimiter(store, privyClient, NewKeyedRateLimiter(groupMessageBurst, groupMessageRefill, time.Now))
}

// NewGroupChatServiceWithLimiter wires chat with an explicit posting limiter (tests inject clocks).
func NewGroupChatServiceWithLimiter(store *postgres.Store, privyClient privy.Client, limiter *KeyedRateLimiter) *GroupChatService {
	return &GroupChatService{store: store, privy: privyClient, limiter: limiter}
}

// ListMessages returns up to limit messages older than cursor (or the newest when cursor is empty).
func (s *GroupChatService) ListMessages(ctx context.Context, accessToken, groupID, cursor string, limit int) (GroupMessagesPage, error) {
	if limit < 1 || limit > GroupMessagesMaxPageLimit {
		return GroupMessagesPage{}, ErrInvalidMessageLimit
	}
	var before *postgres.GroupMessageCursor
	if strings.TrimSpace(cursor) != "" {
		decoded, err := decodeGroupMessageCursor(cursor)
		if err != nil {
			return GroupMessagesPage{}, err
		}
		before = &decoded
	}

	userID, err := s.authorizeMember(ctx, accessToken, groupID)
	if err != nil {
		return GroupMessagesPage{}, err
	}

	// Fetch one extra row to learn whether an older page exists.
	rows, err := s.store.ListGroupMessages(ctx, groupID, before, limit+1)
	if err != nil {
		return GroupMessagesPage{}, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	page := GroupMessagesPage{Messages: make([]GroupMessage, 0, len(rows))}
	for _, row := range rows {
		page.Messages = append(page.Messages, groupMessageFromRow(row, userID))
	}
	if hasMore {
		last := rows[len(rows)-1]
		page.NextCursor = encodeGroupMessageCursor(postgres.GroupMessageCursor{CreatedAt: last.CreatedAt, ID: last.ID})
	}
	return page, nil
}

// PostMessage validates and stores a chat message from the caller.
func (s *GroupChatService) PostMessage(ctx context.Context, accessToken, groupID, body string) (GroupMessage, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return GroupMessage{}, ErrMessageEmpty
	}
	if utf8.RuneCountInString(body) > GroupMessageMaxChars {
		return GroupMessage{}, ErrMessageTooLong
	}

	userID, err := s.authorizeMember(ctx, accessToken, groupID)
	if err != nil {
		return GroupMessage{}, err
	}

	if ok, wait := s.limiter.Allow(userID); !ok {
		return GroupMessage{}, &RateLimitedError{RetryAfter: wait}
	}

	row, err := s.store.InsertGroupMessage(ctx, groupID, userID, body)
	if err != nil {
		return GroupMessage{}, err
	}
	return groupMessageFromRow(row, userID), nil
}

// authorizeMember resolves the caller and requires membership.
// Unknown or malformed group ids are ErrGroupNotFound; existing groups the caller has not joined are ErrNotGroupMember.
func (s *GroupChatService) authorizeMember(ctx context.Context, accessToken, groupID string) (string, error) {
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

	if !isUUID(groupID) {
		return "", ErrGroupNotFound
	}
	_, found, err = s.store.GetGroupByID(ctx, groupID)
	if err != nil {
		return "", err
	}
	if !found {
		return "", ErrGroupNotFound
	}
	member, err := s.store.IsGroupMember(ctx, groupID, user.ID)
	if err != nil {
		return "", err
	}
	if !member {
		return "", ErrNotGroupMember
	}
	return user.ID, nil
}

func groupMessageFromRow(row postgres.GroupMessageRow, viewerID string) GroupMessage {
	name := strings.TrimSpace(row.AuthorDisplayName)
	if name == "" {
		name = "Member"
	}
	return GroupMessage{
		ID:         row.ID,
		GroupID:    row.GroupID,
		AuthorID:   row.AuthorID,
		AuthorName: name,
		Body:       row.Body,
		CreatedAt:  row.CreatedAt.UTC(),
		Mine:       row.AuthorID == viewerID,
	}
}

// Cursor format: base64url("<RFC3339Nano UTC>|<message uuid>"). Opaque to clients.
func encodeGroupMessageCursor(c postgres.GroupMessageCursor) string {
	raw := c.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + c.ID
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeGroupMessageCursor(cursor string) (postgres.GroupMessageCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(cursor))
	if err != nil {
		return postgres.GroupMessageCursor{}, ErrInvalidMessageCursor
	}
	ts, id, ok := strings.Cut(string(raw), "|")
	if !ok || !isUUID(id) {
		return postgres.GroupMessageCursor{}, ErrInvalidMessageCursor
	}
	createdAt, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return postgres.GroupMessageCursor{}, ErrInvalidMessageCursor
	}
	return postgres.GroupMessageCursor{CreatedAt: createdAt.UTC(), ID: id}, nil
}
