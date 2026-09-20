#!/usr/bin/env python3
"""Mechanical M6-T1 renames across apps/backend."""
from __future__ import annotations
import pathlib
import re

ROOT = pathlib.Path(__file__).resolve().parents[1] / "apps/backend"
SKIP_DIRS = {"internal/privy", "internal/jupiter", "internal/xstocks", "internal/solana"}

REPLACEMENTS = [
    ("github.com/monaco/monaco/apps/backend/internal/privy", "github.com/monaco/monaco/apps/backend/internal/wallets"),
    ("github.com/monaco/monaco/apps/backend/internal/jupiter", "github.com/monaco/monaco/apps/backend/internal/dex"),
    ("github.com/monaco/monaco/apps/backend/internal/xstocks", "github.com/monaco/monaco/apps/backend/internal/b20"),
    ("privy.Client", "wallets.Client"),
    ("privy.NewFakeClient", "wallets.NewFakeClient"),
    ("privy.NewHTTPClient", "wallets.NewSignerClient"),
    ("privy.GroupID", "wallets.GroupID"),
    ("privy.UserID", "wallets.UserID"),
    ("privy.WalletRef", "wallets.WalletRef"),
    ("privy.TreasuryRef", "wallets.TreasuryRef"),
    ("privy.SetTreasuryUSDCBalance", "wallets.SetTreasuryUSDCBalance"),
    ("privy.SetMemberUSDCBalance", "wallets.SetMemberUSDCBalance"),
    ("privy.RegisterToken", "auth.RegisterToken"),
    ("privy.AccessToken", "auth.AccessToken"),
    ("privy.Identity", "auth.Identity"),
    ("privy.ErrInvalidToken", "auth.ErrUnauthorized"),
    ("identity.PrivyUserID", "identity.DynamicUserID"),
    ("GetUserByPrivyUserID", "GetUserByDynamicUserID"),
    ("UniquePrivyID", "UniqueDynamicID"),
    ("SolanaAddress", "Address"),
    ("PrivyWalletID", "WalletID"),
    ("TxSignature", "TxHash"),
    ("txSignature", "txHash"),
    ("InputMint", "InputToken"),
    ("OutputMint", "OutputToken"),
    ("solanaMint", "tokenAddress"),
    ("SolanaMint", "TokenAddress"),
    ("jupiter.Client", "dex.Client"),
    ("jupiter.NewFakeClient", "dex.NewFakeClient"),
    ("jupiter.USDCMint", "evm.USDCAddress"),
    ("xstocks.", "b20."),
    ("pyth.Client", "marks.Client"),
    ("pyth.NewFakeClient", "chainlink.NewFakeClient"),
    ("pyth.TreasuryRef", "marks.TreasuryRef"),
    ("pyth.NavInput", "marks.NavInput"),
    ("pyth.CostBasis", "marks.CostBasis"),
    ("pyth.MarkedHolding", "marks.MarkedHolding"),
    ("pyth.RegisterUSDCOnlyPot", "chainlink.RegisterUSDCOnlyPot"),
    ("pyth.RegisterMarkedPot", "chainlink.RegisterMarkedPot"),
    ("pyth.RegisterMarkedPotError", "chainlink.RegisterMarkedPotError"),
    ("SolanaRPC", "Confirmer"),
    ("NewFakeSolanaRPC", "NewFakeConfirmer"),
    ("NewHTTPSolanaRPC", "NewEVMConfirmer"),
    ("fakeSolanaRPC", "fakeConfirmer"),
]

def should_skip(path: pathlib.Path) -> bool:
    rel = str(path.relative_to(ROOT)).replace("\\", "/")
    for s in SKIP_DIRS:
        if rel.startswith(s):
            return True
    if rel.startswith("internal/wallets/") and path.name in {"client_test.go", "auth_sign_test.go", "sweep_test.go", "tokens_test.go", "fake_test.go", "client.go"}:
        if path.name == "client.go" and "HTTPClient" in path.read_text():
            return True
    return False

def main() -> None:
    for path in ROOT.rglob("*.go"):
        if should_skip(path):
            continue
        text = path.read_text()
        orig = text
        for a, b in REPLACEMENTS:
            text = text.replace(a, b)
        if text != orig:
            path.write_text(text)
            print("patched", path.relative_to(ROOT))

if __name__ == "__main__":
    main()
