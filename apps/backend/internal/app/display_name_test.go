package app

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeDisplayName_accepts(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"plain ascii", "Logan", "Logan"},
		{"trims outer whitespace", "  Logan \n", "Logan"},
		{"collapses interior spaces", "Logan    Norman", "Logan Norman"},
		{"folds unicode spaces to ascii", "Logan\u00a0\u3000Norman", "Logan Norman"},
		{"single character", "L", "L"},
		{"length counted after collapsing", "a" + strings.Repeat(" ", 40) + "b", "a b"},
		{"digits only", "2049", "2049"},
		{"punctuation around letters", "O'Brien-Smith Jr.", "O'Brien-Smith Jr."},
		{"accented precomposed", "José", "José"},
		{"decomposed accent is NFC-normalized", "Jose\u0301", "José"},
		{"cjk", "山田太郎", "山田太郎"},
		{"emoji with letters", "Ana 🚀", "Ana 🚀"},
		{"emoji with variation selector", "Ana ❤\ufe0f", "Ana ❤\ufe0f"},
		{"flag emoji", "Kai 🇫🇷", "Kai 🇫🇷"},
		{"exactly 32 multibyte runes", strings.Repeat("é", 32), strings.Repeat("é", 32)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeDisplayName(tc.raw)
			if err != nil {
				t.Fatalf("NormalizeDisplayName(%q) error = %v, want %q", tc.raw, err, tc.want)
			}
			if got != tc.want {
				t.Fatalf("NormalizeDisplayName(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestNormalizeDisplayName_rejects(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		raw    string
		reason DisplayNameReason
	}{
		{"empty", "", DisplayNameRequired},
		{"whitespace only", " \t\n ", DisplayNameRequired},
		{"unicode spaces only", "\u00a0\u3000", DisplayNameRequired},
		{"33 ascii runes", strings.Repeat("a", 33), DisplayNameTooLong},
		{"33 multibyte runes", strings.Repeat("é", 33), DisplayNameTooLong},
		{"long name with spaces", strings.Repeat("ab ", 11) + "c", DisplayNameTooLong},
		{"interior newline", "Logan\nNorman", DisplayNameInvalidCharacters},
		{"interior tab", "Logan\tNorman", DisplayNameInvalidCharacters},
		{"bell control", "Lo\u0007gan", DisplayNameInvalidCharacters},
		{"nul byte", "Lo\x00gan", DisplayNameInvalidCharacters},
		{"zero width space", "Lo\u200bgan", DisplayNameInvalidCharacters},
		{"zero width joiner", "Lo\u200dgan", DisplayNameInvalidCharacters},
		{"zero width non-joiner", "Lo\u200cgan", DisplayNameInvalidCharacters},
		{"byte order mark", "\ufeffLogan", DisplayNameInvalidCharacters},
		{"right to left override", "\u202eLogan", DisplayNameInvalidCharacters},
		{"line separator", "Logan\u2028Norman", DisplayNameInvalidCharacters},
		{"private use", "Logan\ue000", DisplayNameInvalidCharacters},
		{"hangul filler", "\u3164", DisplayNameInvalidCharacters},
		{"braille blank", "Lo\u2800gan", DisplayNameInvalidCharacters},
		{"invalid utf8", "Lo\xffgan", DisplayNameInvalidCharacters},
		{"leading combining mark", "\u0301Logan", DisplayNameInvalidCharacters},
		{"stacked combining marks", "Lox\u0301\u0302\u0303gan", DisplayNameInvalidCharacters},
		{"punctuation only", "...", DisplayNameNeedsLetterOrNum},
		{"emoji only", "🚀🚀", DisplayNameNeedsLetterOrNum},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeDisplayName(tc.raw)
			if err == nil {
				t.Fatalf("NormalizeDisplayName(%q) = %q, want %s error", tc.raw, got, tc.reason)
			}
			if !errors.Is(err, ErrInvalidDisplayName) {
				t.Fatalf("error %v does not match ErrInvalidDisplayName", err)
			}
			var nameErr *DisplayNameError
			if !errors.As(err, &nameErr) {
				t.Fatalf("error %T is not *DisplayNameError", err)
			}
			if nameErr.Reason != tc.reason {
				t.Fatalf("reason = %s, want %s", nameErr.Reason, tc.reason)
			}
			if nameErr.Message() == "" {
				t.Fatal("expected user-facing message")
			}
		})
	}
}
