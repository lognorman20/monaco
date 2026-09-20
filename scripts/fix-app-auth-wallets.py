#!/usr/bin/env python3
"""Split privy wallets.Client session verify into auth.Verifier + wallets.Client."""
from __future__ import annotations
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1] / "apps/backend/internal/app"

STRUCT_RE = re.compile(
    r"(type \w+Service struct \{\n(?:\t[^\n]+\n)*?\t)privy\s+wallets\.Client\n",
    re.MULTILINE,
)

def add_auth_import(text: str) -> str:
    needle = '"github.com/monaco/monaco/apps/backend/internal/auth"'
    if needle in text:
        return text
    if "auth." not in text and "auth.AccessToken" not in text:
        return text
    return text.replace(
        '"github.com/monaco/monaco/apps/backend/internal/postgres"',
        '"github.com/monaco/monaco/apps/backend/internal/auth"\n\t"github.com/monaco/monaco/apps/backend/internal/postgres"',
        1,
    )

def fix_struct(text: str) -> str:
    def repl(m: re.Match) -> str:
        return m.group(1) + "auth    auth.Verifier\n\twallets wallets.Client\n"
    return STRUCT_RE.sub(repl, text)

def fix_new_group_simple(text: str) -> str:
    # NewGroupService(store, privyClient wallets.Client)
    text = re.sub(
        r"func New(\w+Service)\(store \*postgres\.Store, privyClient wallets\.Client\)",
        r"func New\1(store *postgres.Store, verifier auth.Verifier, walletClient wallets.Client)",
        text,
    )
    text = re.sub(
        r"(\treturn &\w+Service\{\n\t\tstore: store,\n\t\t)privy: privyClient,",
        r"\1auth: verifier,\n\t\twallets: walletClient,",
        text,
    )
    return text

def fix_wallet_calls(text: str) -> str:
    text = text.replace(".privy.VerifySession", ".auth.VerifySession")
    for method in (
        "EnsureMemberWallet",
        "EnsureTreasury",
        "MemberUSDCBalance",
        "TreasuryUSDCBalance",
        "SubmitSweep",
        "SubmitMemberUSDCTransfer",
        "PayUSDC",
        "SendTreasuryTransaction",
    ):
        text = text.replace(f".privy.{method}", f".wallets.{method}")
    return text

def main() -> None:
    for path in sorted(ROOT.glob("*.go")):
        orig = path.read_text()
        text = orig
        if "privy wallets.Client" in text or ".privy." in text:
            text = fix_struct(text)
            text = fix_new_group_simple(text)
            text = fix_wallet_calls(text)
            text = add_auth_import(text)
        if text != orig:
            path.write_text(text)
            print("fixed", path.name)

if __name__ == "__main__":
    main()
