package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// Notification categories a member can switch off in Settings. The keys are a contract with
// the app and with whatever sends pushes: users.preferences stores exactly
// {"notifications": {"proposals": bool, "results": bool, "chat": bool, "money": bool}}.
const (
	// NotifyProposals: a proposal to vote on, and votes on the member's own proposals.
	NotifyProposals = "proposals"
	// NotifyResults: a vote passing or failing, and the trade filling.
	NotifyResults = "results"
	// NotifyChat: messages in the member's cabals.
	NotifyChat = "chat"
	// NotifyMoney: money landing in or leaving the member's account and cabals.
	NotifyMoney = "money"
)

// preferencesNotificationsKey is the one top-level key a preferences document has today.
const preferencesNotificationsKey = "notifications"

var notificationKeys = []string{NotifyProposals, NotifyResults, NotifyChat, NotifyMoney}

// NotificationPreferences says which notification categories are on. Every category is on
// until the member turns it off.
type NotificationPreferences struct {
	Proposals bool `json:"proposals"`
	Results   bool `json:"results"`
	Chat      bool `json:"chat"`
	Money     bool `json:"money"`
}

// Allows reports whether category is switched on. An unknown category is allowed: a new
// kind of push must not be silenced by a document written before it existed.
func (n NotificationPreferences) Allows(category string) bool {
	switch category {
	case NotifyProposals:
		return n.Proposals
	case NotifyResults:
		return n.Results
	case NotifyChat:
		return n.Chat
	case NotifyMoney:
		return n.Money
	default:
		return true
	}
}

func (n *NotificationPreferences) set(key string, on bool) {
	switch key {
	case NotifyProposals:
		n.Proposals = on
	case NotifyResults:
		n.Results = on
	case NotifyChat:
		n.Chat = on
	case NotifyMoney:
		n.Money = on
	}
}

// Preferences is a member's settings as the API returns them: every key present, stored
// values over defaults.
type Preferences struct {
	Notifications NotificationPreferences `json:"notifications"`
}

// DefaultPreferences is a member who has changed nothing.
func DefaultPreferences() Preferences {
	return Preferences{Notifications: NotificationPreferences{
		Proposals: true,
		Results:   true,
		Chat:      true,
		Money:     true,
	}}
}

// PreferencesFromStored reads users.preferences over the defaults. A key that is missing,
// or holds anything but a boolean, keeps its default, so a damaged row can only leave a
// switch on, never quietly turn one off.
func PreferencesFromStored(raw []byte) Preferences {
	prefs := DefaultPreferences()
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return prefs
	}
	var notifications map[string]json.RawMessage
	if err := json.Unmarshal(doc[preferencesNotificationsKey], &notifications); err != nil {
		return prefs
	}
	for _, key := range notificationKeys {
		if on, ok := jsonBool(notifications[key]); ok {
			prefs.Notifications.set(key, on)
		}
	}
	return prefs
}

// ErrInvalidPreferences means a preferences patch was refused. Wrapped by *PreferencesError.
var ErrInvalidPreferences = errors.New("invalid preferences")

// PreferencesError names the part of a patch that was refused, in words a client can show.
type PreferencesError struct {
	Message string
}

func (e *PreferencesError) Error() string { return "invalid preferences: " + e.Message }

// Is lets callers match with errors.Is(err, ErrInvalidPreferences).
func (e *PreferencesError) Is(target error) bool { return target == ErrInvalidPreferences }

// ValidatePreferencesPatch checks a merge patch for PATCH /v1/me/preferences and returns it
// in canonical form. Only known keys, and only booleans: an unknown key, a null, a string,
// or a nested value where a boolean belongs is refused with the path that broke the rule.
// "{}" is a valid patch that changes nothing.
func ValidatePreferencesPatch(body []byte) ([]byte, error) {
	top, err := decodeJSONObject(body, "the body")
	if err != nil {
		return nil, err
	}
	for _, key := range sortedKeys(top) {
		if key != preferencesNotificationsKey {
			return nil, &PreferencesError{Message: fmt.Sprintf("unknown preference %q", key)}
		}
	}

	canonical := map[string]map[string]bool{}
	if raw, ok := top[preferencesNotificationsKey]; ok {
		fields, err := decodeJSONObject(raw, preferencesNotificationsKey)
		if err != nil {
			return nil, err
		}
		notifications := make(map[string]bool, len(fields))
		for _, key := range sortedKeys(fields) {
			if !isNotificationKey(key) {
				return nil, &PreferencesError{Message: fmt.Sprintf("unknown preference %q", preferencesNotificationsKey+"."+key)}
			}
			on, ok := jsonBool(fields[key])
			if !ok {
				return nil, &PreferencesError{Message: fmt.Sprintf("%s.%s must be true or false", preferencesNotificationsKey, key)}
			}
			notifications[key] = on
		}
		canonical[preferencesNotificationsKey] = notifications
	}
	return json.Marshal(canonical)
}

// decodeJSONObject decodes raw as exactly one JSON object. json.Unmarshal would accept
// `null` into a map without complaint, so the leading brace is checked first.
func decodeJSONObject(raw []byte, name string) (map[string]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, &PreferencesError{Message: name + " must be a JSON object"}
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &object); err != nil {
		return nil, &PreferencesError{Message: name + " must be a JSON object"}
	}
	return object, nil
}

// jsonBool reads a raw JSON value as a boolean literal. null and "true" are not booleans.
func jsonBool(raw json.RawMessage) (bool, bool) {
	switch string(bytes.TrimSpace(raw)) {
	case "true":
		return true, true
	case "false":
		return false, true
	default:
		return false, false
	}
}

func isNotificationKey(key string) bool {
	for _, known := range notificationKeys {
		if key == known {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
