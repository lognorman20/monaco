package app

import (
	"bytes"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/flash"
)

func TestFlashInstructionsToPrivy_decodesBase58Data(t *testing.T) {
	// Arrange: "2" is the idempotent create-ATA discriminator (0x01); "4h6bzpF8MKT4" is Approve(u64 max).
	instructions := []flash.Instruction{
		{ProgramID: "ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL", Data: "2",
			Accounts: []flash.AccountMeta{{Pubkey: "payer", IsSigner: true, IsWritable: true}}},
		{ProgramID: "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA", Data: "4h6bzpF8MKT4"},
	}

	// Act
	got, err := flashInstructionsToPrivy(instructions)

	// Assert
	if err != nil {
		t.Fatalf("flashInstructionsToPrivy: %v", err)
	}
	if !bytes.Equal(got[0].Data, []byte{1}) {
		t.Fatalf("create-ATA data = %v, want [1]", got[0].Data)
	}
	if want := []byte{4, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}; !bytes.Equal(got[1].Data, want) {
		t.Fatalf("approve data = %v, want %v", got[1].Data, want)
	}
	if !got[0].Accounts[0].IsSigner || !got[0].Accounts[0].IsWritable || got[0].Accounts[0].Pubkey != "payer" {
		t.Fatalf("account flags lost: %+v", got[0].Accounts[0])
	}
}

func TestFlashInstructionsToPrivy_invalidData_returnsError(t *testing.T) {
	_, err := flashInstructionsToPrivy([]flash.Instruction{{ProgramID: "p", Data: "0OIl"}})

	if err == nil {
		t.Fatal("expected base58 decode error")
	}
}
