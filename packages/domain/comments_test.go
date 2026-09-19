package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeProposalCommentBody_trimsSurroundingWhitespaceKeepsInnerNewlines(t *testing.T) {
	// Arrange
	raw := "  \n Buying Apple before earnings.\n\nSmall position only.  \t"

	// Act
	body, err := NormalizeProposalCommentBody(raw)

	// Assert
	if err != nil {
		t.Fatalf("NormalizeProposalCommentBody: %v", err)
	}
	if body != "Buying Apple before earnings.\n\nSmall position only." {
		t.Fatalf("body = %q", body)
	}
}

func TestNormalizeProposalCommentBody_whitespaceOnly_returnsEmpty(t *testing.T) {
	// Arrange
	raw := " \n\t "

	// Act
	_, err := NormalizeProposalCommentBody(raw)

	// Assert
	if !errors.Is(err, ErrCommentBodyEmpty) {
		t.Fatalf("err = %v, want ErrCommentBodyEmpty", err)
	}
}

func TestNormalizeProposalCommentBody_exactlyMaxRunes_isAccepted(t *testing.T) {
	// Arrange: multi-byte runes prove the limit counts code points, not bytes.
	raw := strings.Repeat("é", MaxProposalCommentRunes)

	// Act
	body, err := NormalizeProposalCommentBody(raw)

	// Assert
	if err != nil {
		t.Fatalf("NormalizeProposalCommentBody: %v", err)
	}
	if body != raw {
		t.Fatal("body changed for max-length input")
	}
}

func TestNormalizeProposalCommentBody_overMaxRunes_returnsTooLong(t *testing.T) {
	// Arrange
	raw := strings.Repeat("a", MaxProposalCommentRunes+1)

	// Act
	_, err := NormalizeProposalCommentBody(raw)

	// Assert
	if !errors.Is(err, ErrCommentBodyTooLong) {
		t.Fatalf("err = %v, want ErrCommentBodyTooLong", err)
	}
}

func TestNormalizeProposalCommentBody_nulByte_returnsInvalid(t *testing.T) {
	// Arrange
	raw := "looks fine\x00but is not"

	// Act
	_, err := NormalizeProposalCommentBody(raw)

	// Assert
	if !errors.Is(err, ErrCommentBodyInvalid) {
		t.Fatalf("err = %v, want ErrCommentBodyInvalid", err)
	}
}

func TestNormalizeProposalCommentBody_invalidUTF8_returnsInvalid(t *testing.T) {
	// Arrange
	raw := "bad \xff bytes"

	// Act
	_, err := NormalizeProposalCommentBody(raw)

	// Assert
	if !errors.Is(err, ErrCommentBodyInvalid) {
		t.Fatalf("err = %v, want ErrCommentBodyInvalid", err)
	}
}
