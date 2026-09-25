package app

// Fund a ClawPump agent from the group treasury, on Solana.
//
// A passed deploy_agent vote records a pending_transfer deployment; the worker then sends
// that much SPL USDC from the group's Privy Solana treasury to the agent's own wallet, once.
// A passed recall_agent vote moves the deployment to recalling; when Monaco holds the agent's
// cpk_ operator key it asks the agent (ClawPump MCP) to send the USDC back. Either way the
// ledger credits a return only when a confirmed inbound SPL USDC transfer from that agent
// wallet lands in the treasury. Share units never change: this is not a deposit or a redeem.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/clawpump"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	soltreasury "github.com/monaco/monaco/apps/backend/internal/solana/treasury"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
	"github.com/monaco/monaco/packages/domain"
)

var (
	// ErrAgentDeploymentsUnavailable means the Solana treasury (Privy + Solana RPC) is not
	// configured on this server, so USDC cannot be deployed to or recalled from an agent.
	ErrAgentDeploymentsUnavailable = errors.New("agent deployments unavailable")
	ErrAgentWalletRequired         = errors.New("agent wallet address is required")
	ErrInvalidAgentWallet          = errors.New("agent wallet address is not a solana address")
	ErrInvalidOperatorKey          = errors.New("operator key must be a clawpump cpk_ key")
	ErrAgentDeploymentExists       = errors.New("agent wallet already has an active deployment")
	ErrAgentDeploymentNotFound     = errors.New("no deployed usdc for this agent wallet")
	ErrAgentDeploymentProposalOpen = errors.New("an open proposal already covers this agent wallet")
)

// TreasuryCashFunc returns the treasury USDC a pot NAV snapshot values, the same number the
// group view uses.
type TreasuryCashFunc func(ctx context.Context, groupID string) (int64, error)

// AgentDeploymentService owns the Solana side of agent deployments.
type AgentDeploymentService struct {
	store        *postgres.Store
	solana       soltreasury.Client
	clawpump     clawpump.Client
	operatorKey  []byte // AES-256 key sealing cpk_ operator keys at rest
	treasuryCash TreasuryCashFunc
}

// NewAgentDeploymentService wires the deployment job. operatorKeyKey is 32 bytes.
func NewAgentDeploymentService(store *postgres.Store, solana soltreasury.Client, claw clawpump.Client, operatorKeyKey []byte, treasuryCash TreasuryCashFunc) *AgentDeploymentService {
	return &AgentDeploymentService{
		store:        store,
		solana:       solana,
		clawpump:     claw,
		operatorKey:  operatorKeyKey,
		treasuryCash: treasuryCash,
	}
}

// SetAgentDeploymentService enables deploy_agent and recall_agent proposal create.
func (g *GovernanceService) SetAgentDeploymentService(svc *AgentDeploymentService) {
	g.agentDeployments = svc
}

func (s *AgentDeploymentService) available() bool {
	return s != nil && s.store != nil && s.solana != nil
}

// EnsureSolanaTreasury returns the group's Solana treasury, provisioning it in Privy once.
func (s *AgentDeploymentService) EnsureSolanaTreasury(ctx context.Context, groupID string) (soltreasury.TreasuryRef, error) {
	row, found, err := s.store.GetGroupSolanaTreasury(ctx, groupID)
	if err != nil {
		return soltreasury.TreasuryRef{}, err
	}
	if !found {
		ref, err := s.solana.EnsureTreasury(ctx, groupID)
		if err != nil {
			return soltreasury.TreasuryRef{}, fmt.Errorf("provision solana treasury: %w", err)
		}
		row, err = s.store.InsertGroupSolanaTreasury(ctx, postgres.SolanaTreasuryRow{
			GroupID:       groupID,
			PrivyWalletID: ref.PrivyWalletID,
			SolanaAddress: ref.SolanaAddress,
		})
		if err != nil {
			return soltreasury.TreasuryRef{}, err
		}
		slog.Info("solana treasury provisioned", "group_id", groupID, "solana_address", row.SolanaAddress)
	}
	return soltreasury.TreasuryRef{GroupID: row.GroupID, PrivyWalletID: row.PrivyWalletID, SolanaAddress: row.SolanaAddress}, nil
}

func (s *AgentDeploymentService) sealOperatorKey(raw string) (string, error) {
	if len(s.operatorKey) != 32 {
		return "", fmt.Errorf("%w: operator key encryption key missing", ErrAgentDeploymentsUnavailable)
	}
	return wallets.EncryptShares(s.operatorKey, []byte(raw))
}

func (s *AgentDeploymentService) openOperatorKey(enc string) (string, error) {
	plain, err := wallets.DecryptShares(s.operatorKey, enc)
	if err != nil {
		return "", fmt.Errorf("decrypt operator key: %w", err)
	}
	return string(plain), nil
}

// validateAgentDeploymentProposal applies the create gates for deploy_agent and recall_agent
// and returns the sealed operator key (deploy only, may be empty).
func (g *GovernanceService) validateAgentDeploymentProposal(ctx context.Context, in CreateProposalInput, kind domain.ProposalKind) (string, error) {
	svc := g.agentDeployments
	if !svc.available() {
		return "", ErrAgentDeploymentsUnavailable
	}
	wallet := strings.TrimSpace(in.AgentWalletAddress)
	if wallet == "" {
		return "", ErrAgentWalletRequired
	}
	if err := soltreasury.ValidateAddress(wallet); err != nil {
		return "", ErrInvalidAgentWallet
	}
	if in.TokenAmount != 0 || in.AllocationUsdcMicros != 0 {
		return "", fmt.Errorf("%s takes no token amount or allocation", kind)
	}
	if open, err := g.store.HasOpenAgentDeploymentProposal(ctx, in.GroupID, wallet, string(kind)); err != nil {
		return "", err
	} else if open {
		return "", ErrAgentDeploymentProposalOpen
	}

	switch kind {
	case domain.ProposalKindDeployAgent:
		if in.UsdcMicros <= 0 {
			return "", fmt.Errorf("usdc must be positive")
		}
		active, err := g.store.HasActiveAgentDeployment(ctx, in.GroupID, wallet)
		if err != nil {
			return "", err
		}
		if active {
			return "", ErrAgentDeploymentExists
		}
		ref, err := svc.EnsureSolanaTreasury(ctx, in.GroupID)
		if err != nil {
			return "", err
		}
		if ref.SolanaAddress == wallet {
			return "", ErrInvalidAgentWallet
		}
		// Only treasury cash can be sent, so the ceiling is on-chain SPL USDC, not pot NAV.
		cash, err := svc.solana.TreasuryUSDCBalance(ctx, ref.SolanaAddress)
		if err != nil {
			return "", fmt.Errorf("solana treasury usdc balance: %w", err)
		}
		if in.UsdcMicros > cash {
			logGovernanceBranchWarn("governance create proposal rejected", "deploy exceeds treasury usdc", "group_id", in.GroupID, "usdc_micros", in.UsdcMicros, "treasury_usdc_micros", cash)
			return "", ErrExceedsTreasuryUSDC
		}
		operatorKey := strings.TrimSpace(in.OperatorKey)
		if operatorKey == "" {
			return "", nil
		}
		if !strings.HasPrefix(operatorKey, "cpk_") {
			return "", ErrInvalidOperatorKey
		}
		return svc.sealOperatorKey(operatorKey)
	case domain.ProposalKindRecallAgent:
		if in.UsdcMicros != 0 {
			return "", fmt.Errorf("recall takes no usdc amount")
		}
		if strings.TrimSpace(in.OperatorKey) != "" {
			return "", fmt.Errorf("operator key is set on the deploy proposal")
		}
		deployment, found, err := g.store.GetAgentDeploymentByStatus(ctx, in.GroupID, wallet, postgres.AgentDeploymentDeployed)
		if err != nil {
			return "", err
		}
		if !found || deployment.OutstandingUsdcMicros() <= 0 {
			return "", ErrAgentDeploymentNotFound
		}
		return "", nil
	default:
		return "", fmt.Errorf("invalid proposal kind")
	}
}

// handleAgentDeploymentPassTx runs inside the transaction that marks the proposal passed.
func (g *GovernanceService) handleAgentDeploymentPassTx(ctx context.Context, tx *sql.Tx, proposal Proposal, row postgres.ProposalRow) error {
	switch proposal.Kind {
	case domain.ProposalKindDeployAgent:
		operatorKeyEnc, _, err := g.store.TakeAgentDeployOperatorKeyTx(ctx, tx, proposal.ID)
		if err != nil {
			return err
		}
		deployment, err := g.store.InsertAgentDeploymentTx(ctx, tx, postgres.InsertAgentDeploymentParams{
			GroupID:            proposal.GroupID,
			AgentWalletAddress: row.AgentWalletAddress,
			DeployedUsdcMicros: row.UsdcMicros,
			DeployProposalID:   proposal.ID,
			OperatorKeyEnc:     operatorKeyEnc,
		})
		if err != nil {
			if postgres.IsUniqueViolation(err) {
				return ErrAgentDeploymentExists
			}
			return err
		}
		slog.Info("agent deployment pending transfer",
			"deployment_id", deployment.ID,
			"group_id", proposal.GroupID,
			"agent_wallet", deployment.AgentWalletAddress,
			"usdc_micros", deployment.DeployedUsdcMicros,
			"operated", deployment.OperatorKeyEnc.Valid,
		)
		return nil
	case domain.ProposalKindRecallAgent:
		ok, err := g.store.MarkAgentDeploymentRecallingTx(ctx, tx, proposal.GroupID, row.AgentWalletAddress, proposal.ID)
		if err != nil {
			return err
		}
		if !ok {
			return ErrAgentDeploymentNotFound
		}
		slog.Info("agent deployment recalling", "group_id", proposal.GroupID, "agent_wallet", row.AgentWalletAddress, "proposal_id", proposal.ID)
		return nil
	default:
		return nil
	}
}

// discardAgentDeployOperatorKeyTx drops a sealed operator key when a deploy vote fails or
// expires.
func (g *GovernanceService) discardAgentDeployOperatorKeyTx(ctx context.Context, tx *sql.Tx, proposal Proposal) error {
	if proposal.Kind != domain.ProposalKindDeployAgent {
		return nil
	}
	_, _, err := g.store.TakeAgentDeployOperatorKeyTx(ctx, tx, proposal.ID)
	return err
}

// AgentDeploymentOutcome says what one job step did.
type AgentDeploymentOutcome string

const (
	AgentDeploymentOutcomeWaiting  AgentDeploymentOutcome = "waiting"
	AgentDeploymentOutcomeSent     AgentDeploymentOutcome = "sent"
	AgentDeploymentOutcomeDeployed AgentDeploymentOutcome = "deployed"
	AgentDeploymentOutcomeResend   AgentDeploymentOutcome = "resend"
	AgentDeploymentOutcomeCredited AgentDeploymentOutcome = "credited"
	AgentDeploymentOutcomeClosed   AgentDeploymentOutcome = "closed"
)

// ProcessPendingTransfer advances a pending_transfer deployment: sign and record the SPL USDC
// transfer, broadcast it, and mark the deployment deployed once the chain confirms it.
//
// Resume rules: a stored signature is never re-sent; it is confirmed, or, if it failed or its
// blockhash expired without landing, unlinked so the next tick signs a fresh one. A failure
// before a signature exists leaves the deployment pending_transfer for the next tick.
func (s *AgentDeploymentService) ProcessPendingTransfer(ctx context.Context, deployment postgres.AgentDeploymentRow) (AgentDeploymentOutcome, error) {
	if !s.available() {
		return "", ErrAgentDeploymentsUnavailable
	}
	if deployment.Status != postgres.AgentDeploymentPendingTransfer {
		return AgentDeploymentOutcomeWaiting, nil
	}

	if deployment.OutboundTransferID.Valid {
		transfer, found, err := s.store.GetAgentOutboundTransfer(ctx, deployment.OutboundTransferID.String)
		if err != nil {
			return "", err
		}
		if !found || !transfer.TxSignature.Valid {
			return "", fmt.Errorf("agent deployment %s links a transfer with no signature", deployment.ID)
		}
		return s.settleOutbound(ctx, deployment, transfer)
	}

	ref, err := s.EnsureSolanaTreasury(ctx, deployment.GroupID)
	if err != nil {
		return "", err
	}
	prepared, err := s.solana.PrepareUSDCPayout(ctx, soltreasury.PayUSDCRequest{
		TreasuryRef: ref,
		ToAddress:   deployment.AgentWalletAddress,
		Amount:      deployment.DeployedUsdcMicros,
	})
	if err != nil {
		return "", fmt.Errorf("prepare agent deployment transfer: %w", err)
	}
	// Persist the signature before anything reaches the chain.
	transfer, err := s.store.AttachAgentOutboundTransfer(ctx, deployment.ID, postgres.AgentOutboundTransferRow{
		GroupID:              deployment.GroupID,
		ToAddress:            deployment.AgentWalletAddress,
		Amount:               deployment.DeployedUsdcMicros,
		TxSignature:          sql.NullString{String: prepared.TxSignature, Valid: true},
		LastValidBlockHeight: sql.NullInt64{Int64: int64(prepared.LastValidBlockHeight), Valid: true},
	})
	if err != nil {
		return "", err
	}
	deployment.OutboundTransferID = sql.NullString{String: transfer.ID, Valid: true}
	slog.Info("agent deployment transfer signed",
		"deployment_id", deployment.ID,
		"group_id", deployment.GroupID,
		"agent_wallet", deployment.AgentWalletAddress,
		"usdc_micros", deployment.DeployedUsdcMicros,
		"tx_signature", prepared.TxSignature,
	)
	if err := s.solana.BroadcastUSDCPayout(ctx, prepared); err != nil {
		// Not proof it missed the chain; the next tick settles on the signature status.
		slog.Warn("agent deployment broadcast failed", "deployment_id", deployment.ID, "tx_signature", prepared.TxSignature, "err", err)
		return AgentDeploymentOutcomeSent, nil
	}
	outcome, err := s.settleOutbound(ctx, deployment, transfer)
	if err != nil || outcome == AgentDeploymentOutcomeWaiting {
		return AgentDeploymentOutcomeSent, err
	}
	return outcome, nil
}

func (s *AgentDeploymentService) settleOutbound(ctx context.Context, deployment postgres.AgentDeploymentRow, transfer postgres.AgentOutboundTransferRow) (AgentDeploymentOutcome, error) {
	status, err := s.solana.USDCPayoutStatus(ctx, soltreasury.PreparedPayout{
		TxSignature:          transfer.TxSignature.String,
		LastValidBlockHeight: uint64(transfer.LastValidBlockHeight.Int64),
	})
	if err != nil {
		return "", fmt.Errorf("agent deployment transfer status: %w", err)
	}
	switch status.State {
	case soltreasury.PayoutStateConfirmed:
		return s.markDeployed(ctx, deployment, transfer)
	case soltreasury.PayoutStateFailed, soltreasury.PayoutStateDropped:
		if err := s.store.FailAgentOutboundTransfer(ctx, deployment.ID, transfer.ID); err != nil {
			return "", err
		}
		slog.Warn("agent deployment transfer did not land; will re-sign",
			"deployment_id", deployment.ID, "tx_signature", transfer.TxSignature.String, "state", status.State, "reason", status.Reason)
		return AgentDeploymentOutcomeResend, nil
	default:
		return AgentDeploymentOutcomeWaiting, nil
	}
}

func (s *AgentDeploymentService) markDeployed(ctx context.Context, deployment postgres.AgentDeploymentRow, transfer postgres.AgentOutboundTransferRow) (AgentDeploymentOutcome, error) {
	cash, err := s.potTreasuryCash(ctx, deployment.GroupID)
	if err != nil {
		return "", err
	}
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		ok, err := s.store.ConfirmAgentDeploymentOutboundTx(ctx, tx, deployment.ID, transfer.ID)
		if err != nil || !ok {
			return err
		}
		return s.store.WriteNavSnapshotOnAgentDeploymentTx(ctx, tx, deployment.GroupID, cash)
	})
	if err != nil {
		return "", err
	}
	slog.Info("agent deployment deployed",
		"deployment_id", deployment.ID, "group_id", deployment.GroupID, "tx_signature", transfer.TxSignature.String)
	return AgentDeploymentOutcomeDeployed, nil
}

// ProcessRecalling advances a recalling deployment. With a sealed operator key it asks the
// agent, once, to point its external wallet at the treasury and send the outstanding USDC.
// It then credits confirmed inbound SPL USDC transfers from the agent wallet, and only those.
func (s *AgentDeploymentService) ProcessRecalling(ctx context.Context, deployment postgres.AgentDeploymentRow) (AgentDeploymentOutcome, error) {
	if !s.available() {
		return "", ErrAgentDeploymentsUnavailable
	}
	if deployment.Status != postgres.AgentDeploymentRecalling {
		return AgentDeploymentOutcomeWaiting, nil
	}
	ref, err := s.EnsureSolanaTreasury(ctx, deployment.GroupID)
	if err != nil {
		return "", err
	}

	var commandErr error
	if deployment.OperatorKeyEnc.Valid && !deployment.RecallCommandSent.Valid {
		commandErr = s.commandRecall(ctx, deployment, ref.SolanaAddress)
		if commandErr != nil {
			// Stays recalling; the command is retried and inbound transfers still credit.
			slog.Warn("agent recall command failed", "deployment_id", deployment.ID, "err", commandErr)
		}
	}

	transfers, err := s.solana.ListInboundUSDCTransfers(ctx, soltreasury.InboundQuery{
		TreasuryAddress: ref.SolanaAddress,
		FromAddress:     deployment.AgentWalletAddress,
		Since:           deployment.CreatedAt,
	})
	if err != nil {
		return "", fmt.Errorf("list inbound agent transfers: %w", err)
	}

	outcome := AgentDeploymentOutcomeWaiting
	for _, transfer := range transfers {
		if transfer.Amount <= 0 || transfer.FromAddress != deployment.AgentWalletAddress {
			continue
		}
		cash, err := s.potTreasuryCash(ctx, deployment.GroupID)
		if err != nil {
			return "", err
		}
		var credit postgres.AgentInboundCredit
		err = s.inTx(ctx, func(tx *sql.Tx) error {
			var err error
			credit, err = s.store.CreditAgentInboundTransferTx(ctx, tx, deployment.ID, transfer.FromAddress, transfer.TxSignature, transfer.Amount)
			if err != nil || !credit.Recorded {
				return err
			}
			return s.store.WriteNavSnapshotOnAgentDeploymentTx(ctx, tx, deployment.GroupID, cash)
		})
		if err != nil {
			return "", err
		}
		if !credit.Recorded {
			continue
		}
		slog.Info("agent deployment return credited",
			"deployment_id", deployment.ID,
			"tx_signature", transfer.TxSignature,
			"amount", transfer.Amount,
			"credited", credit.CreditedMicros,
			"returned_usdc_micros", credit.Deployment.ReturnedUsdcMicros,
			"status", credit.Deployment.Status,
		)
		outcome = AgentDeploymentOutcomeCredited
		if credit.Deployment.Status == postgres.AgentDeploymentClosed {
			return AgentDeploymentOutcomeClosed, nil
		}
	}
	return outcome, commandErr
}

func (s *AgentDeploymentService) commandRecall(ctx context.Context, deployment postgres.AgentDeploymentRow, treasuryAddress string) error {
	if s.clawpump == nil {
		return fmt.Errorf("clawpump client not configured")
	}
	operatorKey, err := s.openOperatorKey(deployment.OperatorKeyEnc.String)
	if err != nil {
		return err
	}
	if err := s.clawpump.SetExternalWallet(ctx, operatorKey, treasuryAddress); err != nil {
		return err
	}
	if err := s.clawpump.AgentSend(ctx, operatorKey, treasuryAddress, deployment.OutstandingUsdcMicros()); err != nil {
		return err
	}
	return s.store.MarkAgentRecallCommandSent(ctx, deployment.ID)
}

func (s *AgentDeploymentService) potTreasuryCash(ctx context.Context, groupID string) (int64, error) {
	if s.treasuryCash == nil {
		return 0, fmt.Errorf("treasury cash reader not configured")
	}
	cash, err := s.treasuryCash(ctx, groupID)
	if err != nil {
		return 0, fmt.Errorf("treasury usdc for nav snapshot: %w", err)
	}
	return cash, nil
}

func (s *AgentDeploymentService) inTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.store.BeginTx(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}
