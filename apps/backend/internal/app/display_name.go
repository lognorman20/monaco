package app

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Display name bounds match the onboarding username contract (1–32 characters,
// counted as Unicode code points after trimming and normalization).
const (
	DisplayNameMinRunes = 1
	DisplayNameMaxRunes = 32

	// maxConsecutiveMarks caps stacked combining marks ("zalgo" text) while still
	// allowing accented letters in decomposed scripts.
	maxConsecutiveMarks = 2
)

// ErrInvalidDisplayName means displayName failed validation. Wrapped by *DisplayNameError.
var ErrInvalidDisplayName = errors.New("invalid display name")

// DisplayNameReason is a stable machine-readable validation failure.
type DisplayNameReason string

const (
	DisplayNameRequired          DisplayNameReason = "required"
	DisplayNameTooLong           DisplayNameReason = "too_long"
	DisplayNameInvalidCharacters DisplayNameReason = "invalid_characters"
	DisplayNameNeedsLetterOrNum  DisplayNameReason = "needs_letter_or_number"
)

// DisplayNameError explains why a display name was rejected.
type DisplayNameError struct {
	Reason DisplayNameReason
}

func (e *DisplayNameError) Error() string {
	return "invalid display name: " + string(e.Reason)
}

// Is lets callers match with errors.Is(err, ErrInvalidDisplayName).
func (e *DisplayNameError) Is(target error) bool {
	return target == ErrInvalidDisplayName
}

// Message is user-facing copy for the rejection.
func (e *DisplayNameError) Message() string {
	switch e.Reason {
	case DisplayNameRequired:
		return "Display name is required."
	case DisplayNameTooLong:
		return "Display name must be 32 characters or fewer."
	case DisplayNameNeedsLetterOrNum:
		return "Display name must include a letter or number."
	default:
		return "Display name can only use letters, numbers, spaces, punctuation, and emoji."
	}
}

// blankLookingRunes are assigned letters or symbols that render as empty space and
// are used to fake blank or duplicate-looking names.
var blankLookingRunes = map[rune]struct{}{
	'\u115F': {}, // HANGUL CHOSEONG FILLER
	'\u1160': {}, // HANGUL JUNGSEONG FILLER
	'\u3164': {}, // HANGUL FILLER
	'\uFFA0': {}, // HALFWIDTH HANGUL FILLER
	'\u2800': {}, // BRAILLE PATTERN BLANK
	'\u034F': {}, // COMBINING GRAPHEME JOINER
	'\u17B4': {}, // KHMER VOWEL INHERENT AQ
	'\u17B5': {}, // KHMER VOWEL INHERENT AA
}

// NormalizeDisplayName returns the canonical stored form of raw or a *DisplayNameError.
//
// Rules: valid UTF-8; NFC-normalized; leading/trailing whitespace trimmed; runs of
// interior spaces (including non-breaking and other Unicode spaces) collapsed to one
// ASCII space; control, format (zero-width, bidi override, joiners), private-use, and
// unassigned code points rejected; at most two stacked combining marks; at least one
// letter or digit; 1–32 code points.
func NormalizeDisplayName(raw string) (string, error) {
	if !utf8.ValidString(raw) {
		return "", &DisplayNameError{Reason: DisplayNameInvalidCharacters}
	}

	normalized := strings.TrimSpace(norm.NFC.String(raw))

	var b strings.Builder
	b.Grow(len(normalized))
	pendingSpace := false
	consecutiveMarks := 0
	hasLetterOrNumber := false
	for _, r := range normalized {
		if isDisplayNameSpace(r) {
			pendingSpace = b.Len() > 0
			consecutiveMarks = 0
			continue
		}
		if !isAllowedDisplayNameRune(r) {
			return "", &DisplayNameError{Reason: DisplayNameInvalidCharacters}
		}
		if unicode.IsMark(r) {
			consecutiveMarks++
			if consecutiveMarks > maxConsecutiveMarks || b.Len() == 0 {
				return "", &DisplayNameError{Reason: DisplayNameInvalidCharacters}
			}
		} else {
			consecutiveMarks = 0
		}
		if pendingSpace {
			b.WriteByte(' ')
			pendingSpace = false
		}
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			hasLetterOrNumber = true
		}
		b.WriteRune(r)
	}

	name := b.String()
	count := utf8.RuneCountInString(name)
	switch {
	case count < DisplayNameMinRunes:
		return "", &DisplayNameError{Reason: DisplayNameRequired}
	case count > DisplayNameMaxRunes:
		return "", &DisplayNameError{Reason: DisplayNameTooLong}
	case !hasLetterOrNumber:
		return "", &DisplayNameError{Reason: DisplayNameNeedsLetterOrNum}
	}
	return name, nil
}

// isDisplayNameSpace reports visible word separators. Line/paragraph separators and
// tabs are not spaces here; they are rejected as invalid characters.
func isDisplayNameSpace(r rune) bool {
	return r == ' ' || unicode.Is(unicode.Zs, r)
}

func isAllowedDisplayNameRune(r rune) bool {
	if _, blank := blankLookingRunes[r]; blank {
		return false
	}
	return unicode.IsLetter(r) ||
		unicode.IsMark(r) ||
		unicode.IsNumber(r) ||
		unicode.IsPunct(r) ||
		unicode.IsSymbol(r)
}
