package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/auth"
)

// GroupHandlers serves group HTTP routes.
type GroupHandlers struct {
	Groups     *app.GroupService
	Governance *app.GovernanceService
	Home       *app.HomeService
	Redeem     *app.RedeemService
}

type joinPolicyRequest struct {
	Mode string `json:"mode"`
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

type joinGroupStatusResponse struct {
	Status string `json:"status"`
}

type joinRequestResponse struct {
	ID              string  `json:"id"`
	UserID          string  `json:"userId"`
	DisplayName     string  `json:"displayName"`
	ProfilePhotoURL *string `json:"profilePhotoUrl"`
	RequestedAt     string  `json:"requestedAt"`
}

type joinRequestsListResponse struct {
	Items []joinRequestResponse `json:"items"`
}

// CreateGroupHandler handles POST /v1/groups.
func (h *GroupHandlers) CreateGroupHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "POST /v1/groups")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}

	var req createGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logJSONError(ctx, log, "invalid_body", w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		logJSONError(ctx, log, "missing_name", w, http.StatusBadRequest, "name is required")
		return
	}

	rules, err := parseCreateGroupRules(req)
	if err != nil {
		logJSONError(ctx, log, "invalid_rules", w, http.StatusBadRequest, err.Error())
		return
	}

	result, err := h.Governance.CreateGroupWithRules(ctx, token, req.Name, rules)
	if err != nil {
		if errors.Is(err, auth.ErrUnauthorized) {
			logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token")
			return
		}
		if errors.Is(err, app.ErrUserNotFound) {
			logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found")
			return
		}
		if errors.Is(err, app.ErrInvalidGroupRules) {
			logJSONError(ctx, log, "invalid_group_rules", w, http.StatusBadRequest, "invalid group rules")
			return
		}
		logJSONError(ctx, log, "create_group_failed", w, http.StatusInternalServerError, "internal server error", "err", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(createGroupResponse{
		GroupID:         result.GroupID,
		Name:            result.Name,
		TreasuryAddress: result.TreasuryAddress,
	})
	logJSONOK(ctx, log, "group_created", "group_id", result.GroupID)
}

// JoinGroupHandler handles POST /v1/groups/{id}/join.
func (h *GroupHandlers) JoinGroupHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "POST /v1/groups/{id}/join")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}

	groupID := r.PathValue("id")
	if strings.TrimSpace(groupID) == "" {
		logJSONError(ctx, log, "missing_group_id", w, http.StatusNotFound, "group not found")
		return
	}

	outcome, err := h.Governance.JoinGroup(ctx, token, groupID)
	if err != nil {
		if writeFakerReadOnly(ctx, log, w, err, "group_id", groupID) {
			return
		}
		if errors.Is(err, auth.ErrUnauthorized) {
			logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token", "group_id", groupID)
			return
		}
		if errors.Is(err, app.ErrUserNotFound) || errors.Is(err, app.ErrGroupNotFound) {
			logJSONError(ctx, log, "group_not_found", w, http.StatusNotFound, "group not found", "group_id", groupID)
			return
		}
		logJSONError(ctx, log, "join_group_failed", w, http.StatusInternalServerError, "internal server error", "group_id", groupID, "err", err.Error())
		return
	}
	switch outcome {
	case app.JoinOutcomePending:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(joinGroupStatusResponse{Status: string(outcome)})
		logJSONOK(ctx, log, "join_pending", "group_id", groupID)
	case app.JoinOutcomeJoined, app.JoinOutcomeAlreadyMember:
		w.WriteHeader(http.StatusNoContent)
		logNoContent(ctx, log, "joined", "group_id", groupID)
	default:
		logJSONError(ctx, log, "join_group_failed", w, http.StatusInternalServerError, "internal server error", "group_id", groupID)
	}
}

type leaveGroupRequest struct {
	WithdrawStake bool `json:"withdrawStake"`
}

type withdrawToBalanceRequest struct {
	ShareAmountMicros *int64 `json:"shareAmountMicros"`
}

type redeemJobResponse struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	ShareUnits    int64  `json:"shareUnits"`
	SliceUsdc     int64  `json:"sliceUsdc"`
	PayoutAddress string `json:"payoutAddress,omitempty"`
}

func (h *GroupHandlers) LeaveGroupHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "POST /v1/groups/{id}/leave")
	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	groupID := r.PathValue("id")
	if strings.TrimSpace(groupID) == "" {
		logJSONError(ctx, log, "missing_group_id", w, http.StatusNotFound, "group not found")
		return
	}
	var leaveReq leaveGroupRequest
	if r.Body != nil {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			logJSONError(ctx, log, "invalid_body", w, http.StatusBadRequest, "invalid request body")
			return
		}
		if len(body) > 0 {
			if err := json.Unmarshal(body, &leaveReq); err != nil {
				logJSONError(ctx, log, "invalid_body", w, http.StatusBadRequest, "invalid request body")
				return
			}
		}
	}
	err := h.Governance.LeaveGroup(ctx, app.LeaveGroupRequest{
		AccessToken:   token,
		GroupID:       groupID,
		WithdrawStake: leaveReq.WithdrawStake,
	})
	if err != nil {
		if writeFakerReadOnly(ctx, log, w, err, "group_id", groupID) {
			return
		}
		if errors.Is(err, auth.ErrUnauthorized) {
			logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token", "group_id", groupID)
			return
		}
		if errors.Is(err, app.ErrUserNotFound) || errors.Is(err, app.ErrGroupNotFound) || errors.Is(err, app.ErrNotGroupMemberForLeave) {
			logJSONError(ctx, log, "group_not_found", w, http.StatusNotFound, "group not found", "group_id", groupID)
			return
		}
		var leaveErr *app.LeaveGroupError
		if errors.As(err, &leaveErr) {
			writeLeaveConflict(w, leaveErr.Reason, leaveConflictMessage(leaveErr.Reason))
			log.done(ctx, "leave_blocked", http.StatusConflict, "group_id", groupID, "reason", string(leaveErr.Reason))
			return
		}
		logJSONError(ctx, log, "leave_group_failed", w, http.StatusInternalServerError, "internal server error", "group_id", groupID, "err", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
	logNoContent(ctx, log, "left", "group_id", groupID)
}

func writeLeaveConflict(w http.ResponseWriter, reason app.LeaveBlockReason, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message, "reason": string(reason)})
}

func leaveConflictMessage(reason app.LeaveBlockReason) string {
	switch reason {
	case app.LeaveBlockShareUnits:
		return "redeem your slice before leaving the cabal"
	case app.LeaveBlockLastMemberTreasury:
		return "sole member cannot leave while the cabal treasury holds value"
	case app.LeaveBlockPendingRedeem:
		return "finish or cancel your pending redeem before leaving"
	case app.LeaveBlockSoleRemainingVote:
		return "cast your vote or wait for open proposals to settle before leaving"
	case app.LeaveBlockCreatorMustTransfer:
		return "transfer cabal ownership before leaving as creator"
	default:
		return "cannot leave cabal"
	}
}

// ListJoinRequestsHandler handles GET /v1/groups/{id}/join-requests.
func (h *GroupHandlers) ListJoinRequestsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/groups/{id}/join-requests")
	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	groupID := r.PathValue("id")
	if strings.TrimSpace(groupID) == "" {
		logJSONError(ctx, log, "missing_group_id", w, http.StatusNotFound, "group not found")
		return
	}
	items, err := h.Governance.ListPendingJoinRequests(ctx, token, groupID)
	if err != nil {
		if errors.Is(err, auth.ErrUnauthorized) {
			logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token", "group_id", groupID)
			return
		}
		if errors.Is(err, app.ErrUserNotFound) || errors.Is(err, app.ErrGroupNotFound) {
			logJSONError(ctx, log, "group_not_found", w, http.StatusNotFound, "group not found", "group_id", groupID)
			return
		}
		if errors.Is(err, app.ErrNotGroupAdmin) {
			logJSONError(ctx, log, "not_group_admin", w, http.StatusForbidden, "not group admin", "group_id", groupID)
			return
		}
		logJSONError(ctx, log, "list_join_requests_failed", w, http.StatusInternalServerError, "internal server error", "group_id", groupID, "err", err.Error())
		return
	}
	respItems := make([]joinRequestResponse, 0, len(items))
	for _, item := range items {
		respItems = append(respItems, joinRequestResponse{ID: item.ID, UserID: item.UserID, DisplayName: item.DisplayName, ProfilePhotoURL: optionalString(item.ProfilePhotoURL), RequestedAt: item.RequestedAt.UTC().Format(time.RFC3339)})
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(joinRequestsListResponse{Items: respItems})
	logJSONOK(ctx, log, "ok", "group_id", groupID, "count", len(respItems))
}

func (h *GroupHandlers) ApproveJoinRequestHandler(w http.ResponseWriter, r *http.Request) {
	h.decideJoinRequest(w, r, true)
}
func (h *GroupHandlers) DenyJoinRequestHandler(w http.ResponseWriter, r *http.Request) {
	h.decideJoinRequest(w, r, false)
}

func (h *GroupHandlers) decideJoinRequest(w http.ResponseWriter, r *http.Request, approve bool) {
	ctx := r.Context()
	action := "deny"
	if approve {
		action = "approve"
	}
	log := newRequestLog(r, "POST /v1/groups/{id}/join-requests/{requestId}/"+action)
	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	groupID := r.PathValue("id")
	requestID := r.PathValue("requestId")
	if strings.TrimSpace(groupID) == "" || strings.TrimSpace(requestID) == "" {
		logJSONError(ctx, log, "missing_ids", w, http.StatusNotFound, "join request not found")
		return
	}
	var err error
	if approve {
		err = h.Governance.ApproveJoinRequest(ctx, token, groupID, requestID)
	} else {
		err = h.Governance.DenyJoinRequest(ctx, token, groupID, requestID)
	}
	if err != nil {
		if writeFakerReadOnly(ctx, log, w, err, "group_id", groupID) {
			return
		}
		if errors.Is(err, auth.ErrUnauthorized) {
			logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token", "group_id", groupID)
			return
		}
		if errors.Is(err, app.ErrUserNotFound) || errors.Is(err, app.ErrGroupNotFound) || errors.Is(err, app.ErrJoinRequestNotFound) {
			logJSONError(ctx, log, "join_request_not_found", w, http.StatusNotFound, "join request not found", "group_id", groupID)
			return
		}
		if errors.Is(err, app.ErrNotGroupAdmin) {
			logJSONError(ctx, log, "not_group_admin", w, http.StatusForbidden, "not group admin", "group_id", groupID)
			return
		}
		logJSONError(ctx, log, "decide_join_request_failed", w, http.StatusInternalServerError, "internal server error", "group_id", groupID, "err", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
	logNoContent(ctx, log, action+"d", "group_id", groupID, "request_id", requestID)
}

// GetGroupHandler handles GET /v1/groups/{id}.
func (h *GroupHandlers) GetGroupHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/groups/{id}")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}

	groupID := r.PathValue("id")
	if strings.TrimSpace(groupID) == "" {
		logJSONError(ctx, log, "missing_group_id", w, http.StatusNotFound, "group not found")
		return
	}

	result, err := h.Groups.GetGroup(ctx, token, groupID)
	if err != nil {
		if errors.Is(err, auth.ErrUnauthorized) {
			logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token", "group_id", groupID)
			return
		}
		if errors.Is(err, app.ErrUserNotFound) || errors.Is(err, app.ErrGroupNotFound) {
			logJSONError(ctx, log, "group_not_found", w, http.StatusNotFound, "group not found", "group_id", groupID)
			return
		}
		logJSONError(ctx, log, "get_group_failed", w, http.StatusInternalServerError, "internal server error", "group_id", groupID, "err", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(getGroupResponse{
		Name:            result.Name,
		TreasuryAddress: result.TreasuryAddress,
	})
	logJSONOK(ctx, log, "ok", "group_id", groupID)
}

type groupViewPotRowResponse struct {
	Symbol      string `json:"symbol"`
	Units       string `json:"units"`
	MarkUsd     string `json:"markUsd"`
	ValueUsd    string `json:"valueUsd"`
	DollarPnL   string `json:"dollarPnl"`
	AfterHours  *bool  `json:"afterHours"`
	TokenAmount string `json:"tokenAmount,omitempty"`
}

type groupViewMemberSliceResponse struct {
	ShareUnits    string  `json:"shareUnits"`
	EquityUsd     string  `json:"equityUsd"`
	SlicePercent  string  `json:"slicePercent"`
	DollarPnL     string  `json:"dollarPnl"`
	PercentReturn *string `json:"percentReturn"`
}

type groupViewMemberRowResponse struct {
	Rank            int     `json:"rank"`
	UserID          string  `json:"userId"`
	DisplayName     string  `json:"displayName"`
	ProfilePhotoURL *string `json:"profilePhotoUrl"`
	PercentReturn   *string `json:"percentReturn"`
	DollarPnL       string  `json:"dollarPnl"`
}

type groupViewAgentResponse struct {
	ID                   string `json:"id"`
	Status               string `json:"status"`
	AgentDisplayName     string `json:"agentDisplayName"`
	AllocationUsdcMicros string `json:"allocationUsdcMicros"`
}

type groupViewResponse struct {
	ID              string                       `json:"id"`
	Name            string                       `json:"name"`
	TreasuryAddress string                       `json:"treasuryAddress"`
	PotTotalUsd     string                       `json:"potTotalUsd"`
	Pot             []groupViewPotRowResponse    `json:"pot"`
	You             groupViewMemberSliceResponse `json:"you"`
	Members         []groupViewMemberRowResponse `json:"members"`
	Proposals       []any                        `json:"proposals"`
	Agent           *groupViewAgentResponse      `json:"agent,omitempty"`
}

// GetGroupViewHandler handles GET /v1/groups/{id}/view.
func (h *GroupHandlers) GetGroupViewHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/groups/{id}/view")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}

	groupID := r.PathValue("id")
	if strings.TrimSpace(groupID) == "" {
		logJSONError(ctx, log, "missing_group_id", w, http.StatusNotFound, "group not found")
		return
	}

	result, err := h.Home.GetGroupView(ctx, token, groupID)
	if err != nil {
		if errors.Is(err, auth.ErrUnauthorized) {
			logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token", "group_id", groupID)
			return
		}
		if errors.Is(err, app.ErrUserNotFound) || errors.Is(err, app.ErrGroupNotFound) {
			logJSONError(ctx, log, "group_not_found", w, http.StatusNotFound, "group not found", "group_id", groupID)
			return
		}
		logJSONError(ctx, log, "get_group_view_failed", w, http.StatusInternalServerError, "internal server error", "group_id", groupID, "err", err.Error())
		return
	}

	pot := make([]groupViewPotRowResponse, 0, len(result.Pot))
	for _, row := range result.Pot {
		pot = append(pot, groupViewPotRowResponse{
			Symbol:      row.Symbol,
			Units:       row.Units,
			MarkUsd:     row.MarkUsd,
			ValueUsd:    row.ValueUsd,
			DollarPnL:   row.DollarPnL,
			AfterHours:  row.AfterHours,
			TokenAmount: row.TokenAmount,
		})
	}
	members := make([]groupViewMemberRowResponse, 0, len(result.Members))
	for _, row := range result.Members {
		members = append(members, groupViewMemberRowResponse{
			Rank:            row.Rank,
			UserID:          row.UserID,
			DisplayName:     row.DisplayName,
			ProfilePhotoURL: optionalString(row.ProfilePhotoURL),
			PercentReturn:   row.PercentReturn,
			DollarPnL:       row.DollarPnL,
		})
	}

	var agentResp *groupViewAgentResponse
	if h.Governance != nil {
		if agentView, err := h.Governance.GetGroupAgentView(ctx, groupID); err == nil && agentView != nil {
			agentResp = &groupViewAgentResponse{
				ID:                   agentView.ID,
				Status:               string(agentView.Status),
				AgentDisplayName:     agentView.AgentDisplayName,
				AllocationUsdcMicros: strconv.FormatInt(agentView.AllocationUsdcMicros, 10),
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(groupViewResponse{
		ID:              result.ID,
		Name:            result.Name,
		TreasuryAddress: result.TreasuryAddress,
		PotTotalUsd:     result.PotTotalUsd,
		Pot:             pot,
		You: groupViewMemberSliceResponse{
			ShareUnits:    result.You.ShareUnits,
			EquityUsd:     result.You.EquityUsd,
			SlicePercent:  result.You.SlicePercent,
			DollarPnL:     result.You.DollarPnL,
			PercentReturn: result.You.PercentReturn,
		},
		Members:   members,
		Proposals: []any{},
		Agent:     agentResp,
	})
	logJSONOK(ctx, log, "ok", "group_id", groupID)
}

type groupActivityItemResponse struct {
	ID                 string `json:"id"`
	Kind               string `json:"kind"`
	Status             string `json:"status"`
	Symbol             string `json:"symbol,omitempty"`
	AmountMicros       int64  `json:"amountMicros"`
	TokenAmount        string `json:"tokenAmount,omitempty"`
	ProceedsUsdcMicros string `json:"proceedsUsdcMicros,omitempty"`
	CreatedAt          string `json:"createdAt"`
	TxHash             string `json:"txHash,omitempty"`
	InitiatedBy        string `json:"initiatedBy,omitempty"`
	AgentDisplayName   string `json:"agentDisplayName,omitempty"`
}

type groupActivityResponse struct {
	Items []groupActivityItemResponse `json:"items"`
}

// ListGroupActivityHandler handles GET /v1/groups/{id}/activity.
func (h *GroupHandlers) ListGroupActivityHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/groups/{id}/activity")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}

	groupID := r.PathValue("id")
	if strings.TrimSpace(groupID) == "" {
		logJSONError(ctx, log, "missing_group_id", w, http.StatusNotFound, "group not found")
		return
	}

	items, err := h.Home.ListGroupActivity(ctx, token, groupID)
	if err != nil {
		if errors.Is(err, auth.ErrUnauthorized) {
			logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token", "group_id", groupID)
			return
		}
		if errors.Is(err, app.ErrUserNotFound) || errors.Is(err, app.ErrGroupNotFound) {
			logJSONError(ctx, log, "group_not_found", w, http.StatusNotFound, "group not found", "group_id", groupID)
			return
		}
		logJSONError(ctx, log, "list_group_activity_failed", w, http.StatusInternalServerError, "internal server error", "group_id", groupID, "err", err.Error())
		return
	}

	respItems := make([]groupActivityItemResponse, 0, len(items))
	for _, item := range items {
		resp := groupActivityItemResponse{
			ID:               item.ID,
			Kind:             item.Kind,
			Status:           item.Status,
			Symbol:           item.Symbol,
			AmountMicros:     item.AmountMicros,
			CreatedAt:        item.CreatedAt.UTC().Format(time.RFC3339),
			TxHash:           item.TxHash,
			InitiatedBy:      item.InitiatedBy,
			AgentDisplayName: item.AgentDisplayName,
		}
		if item.TokenAmount > 0 {
			resp.TokenAmount = strconv.FormatInt(item.TokenAmount, 10)
		}
		if item.ProceedsUsdcMicros > 0 {
			resp.ProceedsUsdcMicros = strconv.FormatInt(item.ProceedsUsdcMicros, 10)
		}
		respItems = append(respItems, resp)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(groupActivityResponse{Items: respItems})
	logJSONOK(ctx, log, "ok", "group_id", groupID, "count", len(respItems))
}

// WithdrawToBalanceHandler handles POST /v1/groups/{id}/withdraw-to-balance.
func (h *GroupHandlers) WithdrawToBalanceHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "POST /v1/groups/{id}/withdraw-to-balance")
	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	groupID := r.PathValue("id")
	if strings.TrimSpace(groupID) == "" {
		logJSONError(ctx, log, "missing_group_id", w, http.StatusNotFound, "group not found")
		return
	}
	if h.Redeem == nil {
		logJSONError(ctx, log, "redeem_unavailable", w, http.StatusInternalServerError, "internal server error")
		return
	}

	var req withdrawToBalanceRequest
	if r.Body != nil {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			logJSONError(ctx, log, "invalid_body", w, http.StatusBadRequest, "invalid request body")
			return
		}
		if len(body) > 0 {
			if err := json.Unmarshal(body, &req); err != nil {
				logJSONError(ctx, log, "invalid_body", w, http.StatusBadRequest, "invalid request body")
				return
			}
		}
	}

	job, err := h.Redeem.WithdrawToBalance(ctx, app.WithdrawToBalanceRequest{
		AccessToken:       token,
		GroupID:           groupID,
		ShareAmountMicros: req.ShareAmountMicros,
	})
	if err != nil {
		if writeFakerReadOnly(ctx, log, w, err, "group_id", groupID) {
			return
		}
		if errors.Is(err, auth.ErrUnauthorized) {
			logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token", "group_id", groupID)
			return
		}
		if errors.Is(err, app.ErrUserNotFound) || errors.Is(err, app.ErrGroupNotFound) || errors.Is(err, app.ErrNotGroupMember) {
			logJSONError(ctx, log, "group_not_found", w, http.StatusNotFound, "group not found", "group_id", groupID)
			return
		}
		if errors.Is(err, app.ErrInvalidRedeemRequest) {
			logJSONError(ctx, log, "invalid_redeem", w, http.StatusBadRequest, "Cash out at least $0.10.", "group_id", groupID, "err", err.Error())
			return
		}
		if errors.Is(err, app.ErrRedeemAlreadyInProgress) {
			logJSONError(ctx, log, "redeem_in_progress", w, http.StatusConflict, "withdraw already in progress", "group_id", groupID)
			return
		}
		if errors.Is(err, app.ErrQuoteNotRoutable) {
			logJSONError(ctx, log, "quote_not_routable", w, http.StatusBadRequest,
				"That amount is too small to sell the pot's stock. Try a larger amount, or wait until the pot holds more USDC.",
				"group_id", groupID, "err", err.Error())
			return
		}
		if errors.Is(err, app.ErrRedeemPotIlliquid) {
			logJSONError(ctx, log, "redeem_pot_illiquid", w, http.StatusBadRequest,
				"The pot could not raise enough USDC to cash that out. Try a smaller amount, or try again in a minute.",
				"group_id", groupID, "err", err.Error())
			return
		}
		logJSONError(ctx, log, "withdraw_to_balance_failed", w, http.StatusInternalServerError, "internal server error", "group_id", groupID, "err", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(redeemJobResponse{
		ID:            job.ID,
		Status:        string(job.Status),
		ShareUnits:    job.ShareUnits,
		SliceUsdc:     job.SliceUsdc,
		PayoutAddress: job.PayoutAddress,
	})
	logJSONOK(ctx, log, "withdraw_to_balance_ok", "group_id", groupID, "job_id", job.ID, "status", string(job.Status))
}

func parseCreateGroupRules(req createGroupRequest) (app.GroupRules, error) {
	rules := app.DefaultGroupRules()
	if req.JoinPolicy != nil {
		switch req.JoinPolicy.Mode {
		case "", string(app.JoinModeOpen):
			rules.JoinPolicy.Mode = app.JoinModeOpen
		case string(app.JoinModeRequest):
			rules.JoinPolicy.Mode = app.JoinModeRequest
		default:
			return app.GroupRules{}, errors.New("invalid join policy mode")
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
			return app.GroupRules{}, errors.New("invalid voter set mode")
		}
	}
	if req.Threshold != "" {
		switch req.Threshold {
		case string(app.ThresholdUnanimous):
			rules.Threshold = app.ThresholdUnanimous
		case string(app.ThresholdMajority):
			rules.Threshold = app.ThresholdMajority
		default:
			return app.GroupRules{}, errors.New("invalid threshold")
		}
	}
	if req.VoteExpirySeconds != nil {
		rules.VoteExpirySeconds = app.VoteExpirySeconds(*req.VoteExpirySeconds)
	}
	return rules, nil
}
