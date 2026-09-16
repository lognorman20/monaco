package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

// GroupHandlers serves group HTTP routes.
type GroupHandlers struct {
	Groups     *app.GroupService
	Governance *app.GovernanceService
}

type joinPolicyRequest struct {
	Mode     string `json:"mode"`
	Password string `json:"password"`
}

type voterSetRequest struct {
	Mode      string   `json:"mode"`
	MemberIDs []string `json:"memberIds"`
}

type createGroupRequest struct {
	Name              string             `json:"name"`
	JoinPolicy        *joinPolicyRequest `json:"joinPolicy"`
	VoterSet          *voterSetRequest   `json:"voterSet"`
	Threshold         string             `json:"threshold"`
	VoteExpirySeconds *int64             `json:"voteExpirySeconds"`
}

type createGroupResponse struct {
	GroupID         string `json:"groupId"`
	Name            string `json:"name"`
	TreasuryAddress string `json:"treasuryAddress"`
}

type getGroupResponse struct {
	Name            string `json:"name"`
	TreasuryAddress string `json:"treasuryAddress"`
}

type joinGroupRequest struct {
	Password string `json:"password"`
}

// CreateGroupHandler handles POST /v1/groups.
func (h *GroupHandlers) CreateGroupHandler(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerToken(r)
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}

	var req createGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeJSONError(w, http.StatusBadRequest, "name is required")
		return
	}

	rules, joinPassword, err := parseCreateGroupRules(req)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	result, err := h.Governance.CreateGroupWithRules(r.Context(), token, req.Name, rules, joinPassword)
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			writeJSONError(w, http.StatusUnauthorized, "invalid or expired access token")
			return
		}
		if errors.Is(err, app.ErrUserNotFound) {
			writeJSONError(w, http.StatusNotFound, "user not found")
			return
		}
		if errors.Is(err, app.ErrInvalidGroupRules) {
			writeJSONError(w, http.StatusBadRequest, "invalid group rules")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(createGroupResponse{
		GroupID:         result.GroupID,
		Name:            result.Name,
		TreasuryAddress: result.TreasuryAddress,
	})
}

// JoinGroupHandler handles POST /v1/groups/{id}/join.
func (h *GroupHandlers) JoinGroupHandler(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerToken(r)
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}

	groupID := r.PathValue("id")
	if strings.TrimSpace(groupID) == "" {
		writeJSONError(w, http.StatusNotFound, "group not found")
		return
	}

	var req joinGroupRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	err := h.Governance.JoinGroup(r.Context(), token, groupID, req.Password)
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			writeJSONError(w, http.StatusUnauthorized, "invalid or expired access token")
			return
		}
		if errors.Is(err, app.ErrUserNotFound) || errors.Is(err, app.ErrGroupNotFound) {
			writeJSONError(w, http.StatusNotFound, "group not found")
			return
		}
		if errors.Is(err, app.ErrWrongJoinPassword) {
			writeJSONError(w, http.StatusForbidden, "wrong join password")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetGroupHandler handles GET /v1/groups/{id}.
func (h *GroupHandlers) GetGroupHandler(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerToken(r)
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}

	groupID := r.PathValue("id")
	if strings.TrimSpace(groupID) == "" {
		writeJSONError(w, http.StatusNotFound, "group not found")
		return
	}

	result, err := h.Groups.GetGroup(r.Context(), token, groupID)
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			writeJSONError(w, http.StatusUnauthorized, "invalid or expired access token")
			return
		}
		if errors.Is(err, app.ErrUserNotFound) || errors.Is(err, app.ErrGroupNotFound) {
			writeJSONError(w, http.StatusNotFound, "group not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(getGroupResponse{
		Name:            result.Name,
		TreasuryAddress: result.TreasuryAddress,
	})
}

func parseCreateGroupRules(req createGroupRequest) (app.GroupRules, string, error) {
	rules := app.DefaultGroupRules()
	joinPassword := ""

	if req.JoinPolicy != nil {
		switch req.JoinPolicy.Mode {
		case "", string(app.JoinModeOpen):
			rules.JoinPolicy.Mode = app.JoinModeOpen
		case string(app.JoinModePassword):
			rules.JoinPolicy.Mode = app.JoinModePassword
			joinPassword = req.JoinPolicy.Password
		default:
			return app.GroupRules{}, "", errors.New("invalid join policy mode")
		}
	}

	if req.VoterSet != nil {
		switch req.VoterSet.Mode {
		case "", string(app.VoterSetAllMembers):
			rules.VoterSet.Mode = app.VoterSetAllMembers
		case string(app.VoterSetNamed):
			rules.VoterSet.Mode = app.VoterSetNamed
			rules.VoterSet.MemberIDs = req.VoterSet.MemberIDs
		default:
			return app.GroupRules{}, "", errors.New("invalid voter set mode")
		}
	}

	if req.Threshold != "" {
		switch req.Threshold {
		case string(app.ThresholdUnanimous):
			rules.Threshold = app.ThresholdUnanimous
		case string(app.ThresholdMajority):
			rules.Threshold = app.ThresholdMajority
		default:
			return app.GroupRules{}, "", errors.New("invalid threshold")
		}
	}

	if req.VoteExpirySeconds != nil {
		rules.VoteExpirySeconds = app.VoteExpirySeconds(*req.VoteExpirySeconds)
	}

	return rules, joinPassword, nil
}
