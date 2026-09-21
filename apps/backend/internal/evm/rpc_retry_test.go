package evm

import (
	"math/big"
	"testing"
	"time"
)

func TestIsRPCRateLimited(t *testing.T) {
	t.Parallel()
	if !isRPCRateLimited(errRPC("over rate limit")) {
		t.Fatal("expected rate limit")
	}
	if isRPCRateLimited(errRPC("execution reverted")) {
		t.Fatal("reverts are not rate limits")
	}
}

func TestEncodeAggregate3_nonEmpty(t *testing.T) {
	t.Parallel()
	payload, err := encodeAggregate3([]string{
		"0x787f13dea48db0897cbcdd985de77809d837f988",
		"0xfaf869185383a24f8cb00e27bda6b63b9905dcb4",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) < 4 {
		t.Fatal("empty aggregate3 payload")
	}
}

func TestParseLatestRoundData_readsAnswer(t *testing.T) {
	t.Parallel()
	raw := make([]byte, 160)
	answer := big.NewInt(24_850_000_000)
	copy(raw[64-len(answer.Bytes()):64], answer.Bytes())
	updated := big.NewInt(time.Unix(1_700_000_000, 0).Unix())
	copy(raw[128-len(updated.Bytes()):128], updated.Bytes())
	got, err := parseLatestRoundData(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Answer.Cmp(answer) != 0 {
		t.Fatalf("answer = %s, want %s", got.Answer, answer)
	}
}

func TestDecodeAggregate3_roundTrip(t *testing.T) {
	t.Parallel()
	raw := make([]byte, 160)
	answer := big.NewInt(20_000_000_000)
	copy(raw[64-len(answer.Bytes()):64], answer.Bytes())
	packed, err := multicall3ABI.Methods["aggregate3"].Outputs.Pack([]multicall3Result{
		{Success: true, ReturnData: raw},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeAggregate3(packed)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].Success {
		t.Fatalf("results = %+v", got)
	}
	round, err := parseLatestRoundData(got[0].ReturnData)
	if err != nil {
		t.Fatal(err)
	}
	if round.Answer.Cmp(answer) != 0 {
		t.Fatalf("answer = %s", round.Answer)
	}
}

type errRPC string

func (e errRPC) Error() string { return string(e) }
