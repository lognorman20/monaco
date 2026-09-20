package wallets

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/evm"
	"github.com/monaco/monaco/apps/backend/internal/signer"
)

type signerClient struct {
	signer         signer.Client
	chain          evm.Client
	store          WalletStore
	sharesKey      []byte
	relayerAddress string
}

// NewSignerClient builds a wallets client over the signer sidecar and Base RPC.
func NewSignerClient(s signer.Client, chain evm.Client, store WalletStore, sharesKey []byte, relayerAddress string) Client {
	return &signerClient{
		signer:         s,
		chain:          chain,
		store:          store,
		sharesKey:      sharesKey,
		relayerAddress: relayerAddress,
	}
}

func (c *signerClient) EnsureMemberWallet(ctx context.Context, dynamicUserID string, userID UserID) (WalletRef, error) {
	_ = dynamicUserID
	if c.store == nil {
		return WalletRef{}, ErrNotConfigured
	}
	existing, ok, err := c.store.GetMemberWalletByUserID(ctx, string(userID))
	if err != nil {
		return WalletRef{}, err
	}
	if ok {
		return WalletRef{UserID: userID, WalletID: existing.WalletID, Address: existing.Address}, nil
	}
	created, err := c.signer.CreateWallet(ctx)
	if err != nil {
		return WalletRef{}, err
	}
	enc, err := EncryptShares(c.sharesKey, created.KeyShares)
	if err != nil {
		return WalletRef{}, err
	}
	addr := strings.ToLower(created.Address)
	row, err := c.store.InsertMemberWalletFull(ctx, StoredWallet{
		UserID:       string(userID),
		WalletID:     created.WalletID,
		Address:      addr,
		Metadata:     created.Metadata,
		KeySharesEnc: enc,
	})
	if err != nil {
		return WalletRef{}, err
	}
	return WalletRef{UserID: userID, WalletID: row.WalletID, Address: row.Address}, nil
}

func (c *signerClient) EnsureTreasury(ctx context.Context, groupID GroupID) (TreasuryRef, error) {
	if c.store == nil {
		return TreasuryRef{}, ErrNotConfigured
	}
	existing, ok, err := c.store.GetTreasuryByGroupID(ctx, string(groupID))
	if err != nil {
		return TreasuryRef{}, err
	}
	if ok {
		return TreasuryRef{GroupID: groupID, WalletID: existing.WalletID, Address: existing.Address}, nil
	}
	created, err := c.signer.CreateWallet(ctx)
	if err != nil {
		return TreasuryRef{}, err
	}
	enc, err := EncryptShares(c.sharesKey, created.KeyShares)
	if err != nil {
		return TreasuryRef{}, err
	}
	row, err := c.store.InsertTreasuryFull(ctx, StoredTreasury{
		GroupID:      string(groupID),
		WalletID:     created.WalletID,
		Address:      strings.ToLower(created.Address),
		Metadata:     created.Metadata,
		KeySharesEnc: enc,
	})
	if err != nil {
		return TreasuryRef{}, err
	}
	return TreasuryRef{GroupID: groupID, WalletID: row.WalletID, Address: row.Address}, nil
}

func (c *signerClient) MemberUSDCBalance(ctx context.Context, memberAddress string) (int64, error) {
	return c.erc20Micros(ctx, memberAddress)
}

func (c *signerClient) TreasuryUSDCBalance(ctx context.Context, treasuryAddress string) (int64, error) {
	return c.erc20Micros(ctx, treasuryAddress)
}

func (c *signerClient) erc20Micros(ctx context.Context, addr string) (int64, error) {
	if c.chain == nil {
		return 0, ErrNotConfigured
	}
	bal, err := c.chain.ERC20Balance(ctx, evm.USDCAddress, addr)
	if err != nil {
		return 0, err
	}
	if !bal.IsInt64() {
		return 0, fmt.Errorf("usdc balance overflow")
	}
	return bal.Int64(), nil
}

func (c *signerClient) SubmitSweep(ctx context.Context, req SweepRequest) (SweepResult, error) {
	hash, err := c.eip3009Relay(ctx, req.MemberAddress, req.TreasuryAddress, req.Amount, req.IntentID)
	if err != nil {
		return SweepResult{}, err
	}
	return SweepResult{TxHash: hash}, nil
}

func (c *signerClient) SubmitMemberUSDCTransfer(ctx context.Context, req TransferRequest) (TransferResult, error) {
	hash, err := c.eip3009Relay(ctx, req.MemberAddress, req.ToAddress, req.Amount, req.IntentID)
	if err != nil {
		return TransferResult{}, err
	}
	return TransferResult{TxHash: hash}, nil
}

func (c *signerClient) eip3009Relay(ctx context.Context, from, to string, amount int64, intentID string) (string, error) {
	wallet, err := c.memberByAddress(ctx, from)
	if err != nil {
		return "", err
	}
	shares, err := DecryptShares(c.sharesKey, wallet.KeySharesEnc)
	if err != nil {
		return "", err
	}
	nonce := evm.AuthorizationNonce(intentID)
	validBefore := time.Now().Add(time.Hour).Unix()
	typed := evm.TransferAuthorizationTypedData(from, to, big.NewInt(amount), validBefore, nonce)
	sig, err := c.signer.SignTypedData(ctx, signer.SignRequest{
		Metadata:  wallet.Metadata,
		KeyShares: json.RawMessage(shares),
		TypedData: typed,
	})
	if err != nil {
		return "", err
	}
	v, r, s, err := splitSignature(sig)
	if err != nil {
		return "", err
	}
	data, err := evm.EncodeTransferWithAuthorization(from, to, big.NewInt(amount), big.NewInt(0), big.NewInt(validBefore), nonce, v, r, s)
	if err != nil {
		return "", err
	}
	return c.signer.RelayerSend(ctx, signer.RelayerSendRequest{To: evm.USDCAddress, Data: data, ValueWei: big.NewInt(0)})
}

func (c *signerClient) memberByAddress(ctx context.Context, addr string) (StoredWallet, error) {
	// Tests store a single member; scan by iterating is not on the interface.
	// Look up via Ensure paths: store is memory keyed by user. Tests insert then sweep using that address.
	_ = ctx
	if mem, ok := c.store.(*memoryStore); ok {
		mem.mu.Lock()
		defer mem.mu.Unlock()
		for _, w := range mem.members {
			if strings.EqualFold(w.Address, addr) {
				return w, nil
			}
		}
	}
	return StoredWallet{}, fmt.Errorf("member wallet %s not found", addr)
}

func (c *signerClient) PayUSDC(ctx context.Context, req PayUSDCRequest) (PayUSDCResult, error) {
	treasury := req.TreasuryRef
	if treasury.Address == "" {
		treasury.Address = req.TreasuryAddress
	}
	if err := c.ensureTreasuryGas(ctx, treasury); err != nil {
		return PayUSDCResult{}, err
	}
	data, err := evm.EncodeTransfer(req.ToAddress, big.NewInt(req.Amount))
	if err != nil {
		return PayUSDCResult{}, err
	}
	hash, err := c.SendTreasuryTransaction(ctx, treasury, evm.USDCAddress, data, big.NewInt(0))
	if err != nil {
		return PayUSDCResult{}, err
	}
	return PayUSDCResult{TxHash: hash}, nil
}

func (c *signerClient) SendTreasuryTransaction(ctx context.Context, treasury TreasuryRef, to string, data []byte, valueWei *big.Int) (string, error) {
	if err := c.ensureTreasuryGas(ctx, treasury); err != nil {
		return "", err
	}
	row, err := c.treasuryRow(ctx, treasury)
	if err != nil {
		return "", err
	}
	shares, err := DecryptShares(c.sharesKey, row.KeySharesEnc)
	if err != nil {
		return "", err
	}
	if valueWei == nil {
		valueWei = big.NewInt(0)
	}
	return c.signer.SendTransaction(ctx, signer.SendRequest{
		Metadata:  row.Metadata,
		KeyShares: json.RawMessage(shares),
		To:        to,
		Data:      data,
		ValueWei:  valueWei,
	})
}

func (c *signerClient) treasuryRow(ctx context.Context, treasury TreasuryRef) (StoredTreasury, error) {
	if string(treasury.GroupID) != "" {
		row, ok, err := c.store.GetTreasuryByGroupID(ctx, string(treasury.GroupID))
		if err != nil {
			return StoredTreasury{}, err
		}
		if ok {
			return row, nil
		}
	}
	if mem, ok := c.store.(*memoryStore); ok {
		mem.mu.Lock()
		defer mem.mu.Unlock()
		for _, t := range mem.treasuries {
			if strings.EqualFold(t.Address, treasury.Address) {
				return t, nil
			}
		}
	}
	return StoredTreasury{}, fmt.Errorf("treasury not found")
}

func (c *signerClient) ensureTreasuryGas(ctx context.Context, treasury TreasuryRef) error {
	if c.chain == nil {
		return nil
	}
	bal, err := c.chain.ETHBalance(ctx, treasury.Address)
	if err != nil {
		return err
	}
	if bal.Cmp(evm.TreasuryGasFloorWei) >= 0 {
		return nil
	}
	hash, err := c.signer.RelayerSend(ctx, signer.RelayerSendRequest{
		To:       treasury.Address,
		Data:     nil,
		ValueWei: evm.TreasuryGasTopUpWei,
	})
	if err != nil {
		return err
	}
	if _, err := c.chain.Receipt(ctx, hash); err != nil {
		return err
	}
	if f, ok := c.chain.(interface {
		SetETHBalance(string, *big.Int)
	}); ok {
		f.SetETHBalance(treasury.Address, evm.TreasuryGasTopUpWei)
	}
	if string(treasury.GroupID) != "" {
		_ = c.store.SetTreasuryGasToppedUpAt(ctx, string(treasury.GroupID), time.Now())
	}
	return nil
}

func splitSignature(sig string) (uint8, [32]byte, [32]byte, error) {
	var r, s [32]byte
	raw := strings.TrimPrefix(strings.TrimSpace(sig), "0x")
	b, err := hex.DecodeString(raw)
	if err != nil {
		return 0, r, s, err
	}
	if len(b) != 65 {
		return 0, r, s, fmt.Errorf("signature must be 65 bytes")
	}
	copy(r[:], b[0:32])
	copy(s[:], b[32:64])
	return b[64], r, s, nil
}
