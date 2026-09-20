package app

import (
	"context"
	"fmt"

	"github.com/monaco/monaco/apps/backend/internal/flash"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	solanakey "github.com/monaco/monaco/apps/backend/internal/solana/key"
	"github.com/monaco/monaco/apps/backend/internal/swapprovider"
)

// PrivyFlashSetupSubmitter lands Flash's one-time onchain setup (token accounts and
// SPL delegation) signed by the treasury wallet, with the relayer paying the SOL fee.
type PrivyFlashSetupSubmitter struct {
	client     *privy.HTTPClient
	relayerKey string
}

// NewPrivyFlashSetupSubmitter returns a setup submitter backed by Privy wallet RPC.
func NewPrivyFlashSetupSubmitter(client *privy.HTTPClient, relayerKey string) *PrivyFlashSetupSubmitter {
	return &PrivyFlashSetupSubmitter{client: client, relayerKey: relayerKey}
}

// SubmitTreasurySetup broadcasts the quote's setup instructions and returns the tx signature.
func (s *PrivyFlashSetupSubmitter) SubmitTreasurySetup(ctx context.Context, wallet swapprovider.Wallet, instructions []flash.Instruction) (string, error) {
	converted, err := flashInstructionsToPrivy(instructions)
	if err != nil {
		return "", err
	}
	return s.client.SubmitSponsoredInstructions(ctx, privy.SponsoredInstructionsRequest{
		WalletID:      wallet.PrivyWalletID,
		WalletAddress: wallet.SolanaAddress,
		RelayerKey:    s.relayerKey,
		Instructions:  converted,
	})
}

func flashInstructionsToPrivy(instructions []flash.Instruction) ([]privy.Instruction, error) {
	out := make([]privy.Instruction, 0, len(instructions))
	for i, ix := range instructions {
		data, err := solanakey.DecodeBase58(ix.Data)
		if err != nil {
			return nil, fmt.Errorf("flash setup instruction %d data: %w", i, err)
		}
		accounts := make([]privy.InstructionAccount, 0, len(ix.Accounts))
		for _, account := range ix.Accounts {
			accounts = append(accounts, privy.InstructionAccount{
				Pubkey:     account.Pubkey,
				IsSigner:   account.IsSigner,
				IsWritable: account.IsWritable,
			})
		}
		out = append(out, privy.Instruction{ProgramID: ix.ProgramID, Accounts: accounts, Data: data})
	}
	return out, nil
}
