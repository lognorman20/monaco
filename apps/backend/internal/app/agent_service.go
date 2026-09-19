package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/packages/domain"
)

var (
	ErrAgentNotFound       = errors.New("agent not found")
	ErrAgentAlreadyExists  = errors.New("agent already exists for group")
	ErrAgentInvalidState   = errors.New("agent invalid state for proposal")
	ErrInvalidAgentAPIKey  = errors.New("invalid agent api key")
	ErrAgentGroupMismatch  = errors.New("agent key does not match group")
	ErrAgentPaused         = errors.New("agent is paused")
	ErrAgentIntentRejected = errors.New("agent intent rejected")
)

// CreateAgentProposalInput is input for agent lifecycle proposals.
type CreateAgentProposalInput struct {
	GroupID              string
	ProposerID           string
	Kind                 domain.ProposalKind
	AgentDisplayName     string
	AllocationUsdcMicros int64
}

// AgentView is the public agent summary for group detail.
type AgentView struct {
	ID                   string
	Status               domain.AgentStatus
	AgentDisplayName     string
	AllocationUsdcMicros int64
	// APIKey is set only for cabal members when the agent is active or paused.
	APIKey string
}

func (g *GovernanceService) validateAgentProposal(ctx context.Context, in CreateAgentProposalInput) error {
	switch in.Kind {
	case domain.ProposalKindAddAgent:
		if strings.TrimSpace(in.AgentDisplayName) == "" {
			return fmt.Errorf("agent display name is required")
		}
		if in.AllocationUsdcMicros <= 0 {
			return fmt.Errorf("allocation must be positive")
		}
		if _, found, err := g.store.GetActiveOrPausedGroupAgentByGroupID(ctx, in.GroupID); err != nil {
			return err
		} else if found {
			return ErrAgentAlreadyExists
		}
		treasuryTotal, err := g.proposalTreasuryTotalMicros(ctx, in.GroupID)
		if err != nil {
			return err
		}
		if in.AllocationUsdcMicros > treasuryTotal {
			return ErrExceedsTreasuryUSDC
		}
	case domain.ProposalKindPauseAgent:
		agent, found, err := g.store.GetActiveOrPausedGroupAgentByGroupID(ctx, in.GroupID)
		if err != nil {
			return err
		}
		if !found || agent.Status != domain.AgentStatusActive {
			return ErrAgentInvalidState
		}
	case domain.ProposalKindResumeAgent:
		agent, found, err := g.store.GetActiveOrPausedGroupAgentByGroupID(ctx, in.GroupID)
		if err != nil {
			return err
		}
		if !found || agent.Status != domain.AgentStatusPaused {
			return ErrAgentInvalidState
		}
	case domain.ProposalKindRevokeAgent:
		_, found, err := g.store.GetActiveOrPausedGroupAgentByGroupID(ctx, in.GroupID)
		if err != nil {
			return err
		}
		if !found {
			return ErrAgentInvalidState
		}
	default:
		return fmt.Errorf("invalid agent proposal kind")
	}
	return nil
}

func (g *GovernanceService) createAgentProposalTx(ctx context.Context, tx *sql.Tx, in CreateAgentProposalInput, expiresAt time.Time) (Proposal, error) {
	row, err := g.store.InsertProposalTx(ctx, tx, postgres.InsertProposalParams{
		GroupID:              in.GroupID,
		ProposerID:           in.ProposerID,
		Kind:                 in.Kind,
		AgentDisplayName:     strings.TrimSpace(in.AgentDisplayName),
		AllocationUsdcMicros: in.AllocationUsdcMicros,
		ExpiresAt:            expiresAt,
	})
	if err != nil {
		return Proposal{}, err
	}
	return proposalFromRow(row), nil
}

func (g *GovernanceService) handleAgentProposalPassTx(ctx context.Context, tx *sql.Tx, proposal Proposal, row postgres.ProposalRow) error {
	switch proposal.Kind {
	case domain.ProposalKindAddAgent:
		plaintext, hash, prefix, err := MintAgentAPIKey()
		if err != nil {
			return err
		}
		agentRow := postgres.GroupAgentRow{
			GroupID:              proposal.GroupID,
			Status:               domain.AgentStatusActive,
			InstallProposalID:    sql.NullString{String: proposal.ID, Valid: true},
			AgentDisplayName:     row.AgentDisplayName,
			AllocationUsdcMicros: row.AllocationUsdcMicros,
			APIKeyHash:           sql.NullString{String: hash, Valid: true},
			APIKeyPrefix:         sql.NullString{String: prefix, Valid: true},
			APIKey:               sql.NullString{String: plaintext, Valid: true},
		}
		_, err = g.store.InsertGroupAgentTx(ctx, tx, agentRow)
		return err
	case domain.ProposalKindPauseAgent:
		agent, found, err := g.store.GetActiveOrPausedGroupAgentByGroupID(ctx, proposal.GroupID)
		if err != nil {
			return err
		}
		if !found {
			return ErrAgentNotFound
		}
		return g.store.UpdateGroupAgentStatusTx(ctx, tx, agent.ID, domain.AgentStatusPaused, proposal.ID, "", "")
	case domain.ProposalKindResumeAgent:
		agent, found, err := g.store.GetActiveOrPausedGroupAgentByGroupID(ctx, proposal.GroupID)
		if err != nil {
			return err
		}
		if !found {
			return ErrAgentNotFound
		}
		return g.store.UpdateGroupAgentStatusTx(ctx, tx, agent.ID, domain.AgentStatusActive, "", proposal.ID, "")
	case domain.ProposalKindRevokeAgent:
		agent, found, err := g.store.GetActiveOrPausedGroupAgentByGroupID(ctx, proposal.GroupID)
		if err != nil {
			return err
		}
		if !found {
			return ErrAgentNotFound
		}
		if err := g.store.RevokeGroupAgentKeyTx(ctx, tx, agent.ID); err != nil {
			return err
		}
		return g.store.UpdateGroupAgentStatusTx(ctx, tx, agent.ID, domain.AgentStatusRevoked, "", "", proposal.ID)
	default:
		return nil
	}
}

// AuthorizeGroupReader returns the viewer user id after session and read-access checks.
func (g *GovernanceService) AuthorizeGroupReader(ctx context.Context, accessToken, groupID string) (string, error) {
	return authorizeGroupReader(ctx, g.store, g.privy, accessToken, groupID)
}

// GetGroupAgentView returns the current agent summary for a group.
func (g *GovernanceService) GetGroupAgentView(ctx context.Context, groupID string) (*AgentView, error) {
	return g.GetGroupAgentViewForMember(ctx, groupID, "")
}

// GetGroupAgentViewForMember returns the agent summary. APIKey is included only for cabal members.
func (g *GovernanceService) GetGroupAgentViewForMember(ctx context.Context, groupID, viewerID string) (*AgentView, error) {
	agent, found, err := g.store.GetActiveOrPausedGroupAgentByGroupID(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	view := &AgentView{
		ID:                   agent.ID,
		Status:               agent.Status,
		AgentDisplayName:     agent.AgentDisplayName,
		AllocationUsdcMicros: agent.AllocationUsdcMicros,
	}
	if viewerID == "" {
		return view, nil
	}
	member, err := g.store.IsGroupMember(ctx, groupID, viewerID)
	if err != nil {
		return nil, err
	}
	if member && agent.APIKey.Valid {
		view.APIKey = agent.APIKey.String
	}
	return view, nil
}

// AgentAPIKeyForMember returns the stored agent key when the viewer is a cabal member.
func (g *GovernanceService) AgentAPIKeyForMember(ctx context.Context, groupID, viewerID string, kind domain.ProposalKind, status ProposalStatus) (string, error) {
	if kind != domain.ProposalKindAddAgent || status != ProposalPassed {
		return "", nil
	}
	member, err := g.store.IsGroupMember(ctx, groupID, viewerID)
	if err != nil {
		return "", err
	}
	if !member {
		return "", nil
	}
	agent, found, err := g.store.GetActiveOrPausedGroupAgentByGroupID(ctx, groupID)
	if err != nil {
		return "", err
	}
	if !found || !agent.APIKey.Valid {
		return "", nil
	}
	return agent.APIKey.String, nil
}

func groupAgentFromRow(row postgres.GroupAgentRow) domain.GroupAgent {
	return domain.GroupAgent{
		ID:                   row.ID,
		GroupID:              row.GroupID,
		Status:               row.Status,
		AgentDisplayName:     row.AgentDisplayName,
		AllocationUsdcMicros: row.AllocationUsdcMicros,
	}
}
