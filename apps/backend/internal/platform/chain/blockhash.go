package chain

import (
	"log/slog"

	"github.com/monaco/monaco/apps/backend/internal/errs"
)

func RecentBlockhash(raw []byte) (string, error) {
	tx, err := DecodeTransaction(raw)
	if err != nil {
		return "", err
	}
	message := tx.Message
	if len(message) > 0 && message[0]&0x80 != 0 {
		message = message[1:]
	}
	keys, rest, ok := compactU16(message[3:])
	if !ok || len(rest) < keys*32+32 {
		return "", errs.New(errs.CodeInvalidInput, "chain.RecentBlockhash", slog.String("reason", "accounts"))
	}
	return EncodeBase58(rest[keys*32 : keys*32+32]), nil
}
