#!/usr/bin/env python3
"""Mechanical test fixes for M6-T1 auth/wallets/dex migration."""
from __future__ import annotations

import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1] / "apps" / "backend"

REPLACEMENTS = [
    (r"openTestSession\(t, ([^,]+), ([^,]+), h\.Privy,", r"openTestSession(t, \1, \2, h.Auth,"),
    (r"openTestSession\(t, ([^,]+), ([^,]+), fx\.h\.Privy,", r"openTestSession(t, \1, \2, fx.h.Auth,"),
    (r"openTestSession\(t, ([^,]+), ([^,]+), h\.Privy\)", r"openTestSession(t, \1, \2, h.Auth)"),
    (r"privy\.LastPayUSDCRequest\(h\.Privy\)", r"wallets.LastPayUSDCRequest(h.Wallets)"),
    (r"privy\.LastTransferRequest\(h\.Privy\)", r"wallets.LastTransferRequest(h.Wallets)"),
    (r"privy\.SetForcedTransferSignature\(h\.Privy,", r"wallets.SetForcedTransferSignature(h.Wallets,"),
    (r"privy\.RegisterPrivyMemberWallet\(", r"wallets.RegisterMemberWallet("),
    (r"NewSessionService\(([^,]+), ([^)]+)\)", r"NewSessionService(\1, auth.NewFakeVerifier(), \2)"),
    (r"app\.NewFakePrivyTreasurySigner\(\)", r"evm.NewFakeClient()"),
    (r"NewFakePrivyTreasurySigner\(\)", r""),
    (r", app\.NewFakePrivyTreasurySigner\(\)", r""),
    (r", NewFakePrivyTreasurySigner\(\)", r""),
    (r"jupiter\.RegisterQuoteBuy\([^)]+\)", r"/* migrated to dex.RegisterQuote */"),
    (r"b20\.RegisterTokenAddress\(", r"b20.RegisterAsset(catalog, b20.Asset{Symbol: "),
]

SWAP_OLD = re.compile(
    r"NewSwapService\(\s*([^,]+),\s*([^,]+),\s*([^,]+),\s*([^,]+),\s*NewFakePrivyTreasurySigner\(\),\s*\"\",\s*([^)]+)\)"
)
SWAP_NEW = r"NewSwapService(\1, \2, \3, \4, evm.NewFakeClient(), \5)"


def fix_file(path: Path) -> bool:
    text = path.read_text()
    orig = text
    for pat, repl in REPLACEMENTS:
        text = re.sub(pat, repl, text)
    text = SWAP_OLD.sub(SWAP_NEW, text)
    # Broken NewSessionService with double verifier from bad replace — skip if already has auth
    text = re.sub(
        r"NewSessionService\((store|h\.Store), auth\.NewFakeVerifier\(\), (h\.Auth, h\.Wallets)\)",
        r"NewSessionService(\1, \2)",
        text,
    )
    if text != orig:
        path.write_text(text)
        return True
    return False


def main() -> None:
    changed = []
    for path in ROOT.rglob("*_test.go"):
        if fix_file(path):
            changed.append(path)
    print(f"updated {len(changed)} files")


if __name__ == "__main__":
    main()
