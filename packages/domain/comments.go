package domain

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// MaxProposalCommentRunes caps a proposal comment body in Unicode code points.
// Mirrors the proposal_comments.body CHECK constraint.
const MaxProposalCommentRunes = 1000

var (
	// ErrCommentBodyEmpty is returned when a comment has no visible text.
	ErrCommentBodyEmpty = errors.New("comment body is required")
	// ErrCommentBodyTooLong is returned when a comment exceeds MaxProposalCommentRunes.
	ErrCommentBodyTooLong = errors.New("comment body is too long")
	// ErrCommentBodyInvalid is returned for text Postgres cannot store (NUL, invalid UTF-8).
	ErrCommentBodyInvalid = errors.New("comment body contains invalid characters")
)

// NormalizeProposalCommentBody trims surrounding whitespace and validates length and encoding.
// Inner newlines are kept so members can write short paragraphs.
func NormalizeProposalCommentBody(raw string) (string, error) {
	if !utf8.ValidString(raw) || strings.ContainsRune(raw, 0) {
		return "", ErrCommentBodyInvalid
	}
	body := strings.TrimSpace(raw)
	if body == "" {
		return "", ErrCommentBodyEmpty
	}
	if utf8.RuneCountInString(body) > MaxProposalCommentRunes {
		return "", ErrCommentBodyTooLong
	}
	return body, nil
}
