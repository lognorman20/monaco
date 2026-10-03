package solana_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/errs"
	"github.com/monaco/monaco/apps/backend/internal/platform/chain"
	"github.com/monaco/monaco/apps/backend/internal/platform/chain/solana"
	"github.com/monaco/monaco/apps/backend/internal/platform/money"
)

func TestSOLBalance_readsLamportsAsNineDecimalBaseUnits(t *testing.T) {
	t.Parallel()
	c, u, _ := overFakes(t)
	got, err := c.SOLBalance(t.Context(), member)
	if err != nil || got != money.NewBaseUnits(50_000_000, 9) {
		t.Fatalf("SOLBalance = %v, %v", got, err)
	}
	req := u.requests()[0]
	if req.method != "getBalance" || req.path != "/rpc/" || req.query != "api-key="+hiddenPart {
		t.Fatalf("request = %+v, want getBalance at the configured URL", req)
	}
}

func TestTokenBalance_sumsEveryAccountForTheMint(t *testing.T) {
	t.Parallel()
	c, u, _ := overFakes(t)
	got, err := c.TokenBalance(t.Context(), member, usdc())
	if err != nil || got != money.NewBaseUnits(25_500_000, 6) {
		t.Fatalf("TokenBalance = %v, %v", got, err)
	}
	if p := string(u.requests()[0].params[1]); p != `{"mint":"`+string(usdcMint)+`"}` {
		t.Fatalf("filter = %s", p)
	}
}

func TestTokenBalance_rejectsBadAmounts(t *testing.T) {
	t.Parallel()
	account := func(amount string, decimals string) string {
		return `{"account":{"data":{"parsed":{"info":{"tokenAmount":{"amount":"` + amount + `","decimals":` + decimals + `}}}}}}`
	}
	for name, body := range map[string]string{
		"not a number":     `{"value":[` + account("x", "6") + `]}`,
		"decimals differ":  `{"value":[` + account("1", "9") + `]}`,
		"overflow the sum": `{"value":[` + account("18446744073709551615", "6") + `,` + account("1", "6") + `]}`,
	} {
		got, err := client(result(body)).TokenBalance(t.Context(), member, usdc())
		if errs.CodeOf(err) != errs.CodeDecodeFailed || !got.IsZero() {
			t.Fatalf("%s: TokenBalance = %v, %v, want decode_failed", name, got, err)
		}
	}
	_, err := client(replying(503, "")).TokenBalance(t.Context(), member, usdc())
	wantCode(t, err, errs.CodeRPCUnavailable)
}

func TestAccounts_andTokenAccountsReadFinalizedParsedAccountState(t *testing.T) {
	t.Parallel()
	account := `{"owner":"` + string(chain.SPLProgram) + `","data":{"parsed":{"info":` +
		`{"mint":"` + string(usdcMint) + `","owner":"` + string(member) + `","state":"initialized",` +
		`"tokenAmount":{"amount":"2500000","decimals":6}}}}}`
	u := result(`{"context":{"slot":44},"value":[` + account + `]}`)
	c := client(u)
	slot, got, err := c.Accounts(t.Context(), []chain.SolanaAddress{sender}, 40)
	if err != nil {
		t.Fatal(err)
	}
	if slot != 44 {
		t.Fatalf("slot = %d, want 44", slot)
	}
	want := []solana.TokenAccountState{{
		Address: sender,
		Exists:  true,
		Program: chain.SPLProgram,
		Mint:    usdcMint,
		Owner:   member,
		State:   "initialized",
		Amount:  money.NewBaseUnits(2_500_000, 6),
	}}
	if !slices.Equal(got, want) {
		t.Fatalf("Accounts = %+v, want %+v", got, want)
	}
	request := u.requests()[0]
	if request.method != "getMultipleAccounts" {
		t.Fatalf("Accounts request = %+v", request)
	}
	wantOptions := `{"commitment":"finalized","encoding":"jsonParsed","minContextSlot":40}`
	if got := string(request.params[1]); got != wantOptions {
		t.Fatalf("Accounts options = %s", got)
	}
	tokenAccounts := `{"context":{"slot":44},"value":[{"pubkey":"` + string(sender) + `","account":` + account + `}]}`
	c = client(result(tokenAccounts))
	slot, got, err = c.TokenAccounts(t.Context(), member, usdc())
	if err != nil {
		t.Fatal(err)
	}
	if slot != 44 {
		t.Fatalf("slot = %d, want 44", slot)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("TokenAccounts = %+v, want %+v", got, want)
	}
	missingClient := client(result(`{"context":{"slot":45},"value":[null]}`))
	_, missing, err := missingClient.Accounts(t.Context(), []chain.SolanaAddress{sender}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(missing, []solana.TokenAccountState{{Address: sender}}) {
		t.Fatalf("missing account = %+v", missing)
	}
}

func TestAccounts_rejectsOverOneHundredAddressesAndMalformedResponses(t *testing.T) {
	t.Parallel()
	addrs := make([]chain.SolanaAddress, 101)
	for i := range addrs {
		addrs[i] = member
	}
	_, _, err := client(result(`null`)).Accounts(t.Context(), addrs, 0)
	wantCode(t, err, errs.CodeInvalidInput)
	empty := client(result(`{"context":{"slot":1},"value":[]}`))
	_, _, err = empty.Accounts(t.Context(), []chain.SolanaAddress{sender}, 0)
	wantCode(t, err, errs.CodeDecodeFailed)
	malformed := `{"context":{"slot":1},"value":[{"owner":"` + string(chain.SPLProgram) + `"}]}`
	_, _, err = client(result(malformed)).Accounts(
		t.Context(), []chain.SolanaAddress{sender}, 0,
	)
	wantCode(t, err, errs.CodeDecodeFailed)
}

func TestAccounts_andTokenAccountsMapRPCAndAccountDecodeFailures(t *testing.T) {
	t.Parallel()
	_, _, err := client(replying(503, "")).Accounts(t.Context(), []chain.SolanaAddress{sender}, 0)
	wantCode(t, err, errs.CodeRPCUnavailable)
	_, _, err = client(replying(503, "")).TokenAccounts(t.Context(), member, usdc())
	wantCode(t, err, errs.CodeRPCUnavailable)
	malformed := `{"context":{"slot":1},"value":[{"pubkey":"` + string(sender) + `","account":{}}]}`
	_, _, err = client(result(malformed)).TokenAccounts(t.Context(), member, usdc())
	wantCode(t, err, errs.CodeDecodeFailed)
}

func TestSignatureStatuses_mapsFinalizedProcessingAndNotFound(t *testing.T) {
	t.Parallel()
	c, u, _ := overFakes(t)
	sigs := []chain.Signature{"a", "b", "c", "d"}
	got, err := c.SignatureStatuses(t.Context(), sigs)
	if err != nil {
		t.Fatal(err)
	}
	const height = 380_000_000
	want := []solana.Status{
		{Signature: "a", State: solana.StateFinalized, BlockHeight: height},
		{Signature: "b", State: solana.StateProcessing, BlockHeight: height},
		{Signature: "c", State: solana.StateNotFound, BlockHeight: height},
		{Signature: "d", State: solana.StateFinalized, Failed: true, BlockHeight: height},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("SignatureStatuses = %+v, want %+v", got, want)
	}
	if m := u.methods(); !slices.Equal(m, []string{"getBlockHeight", "getSignatureStatuses"}) {
		t.Fatalf("calls = %v, want the finalized height read before the statuses", m)
	}
}

func TestStatus_blockhashExpiresOnlyWhenNotFoundPastTheLastValidHeight(t *testing.T) {
	t.Parallel()
	missing := solana.Status{State: solana.StateNotFound, BlockHeight: 101}
	if !missing.BlockhashExpired(100) || missing.BlockhashExpired(101) {
		t.Fatal("a missing signature expires once the finalized height passes lastValidBlockHeight")
	}
	if (solana.Status{State: solana.StateProcessing, BlockHeight: 500}).BlockhashExpired(100) {
		t.Fatal("a seen signature never expires")
	}
}

func TestBlockhashValid_readsTheConfirmedValidity(t *testing.T) {
	t.Parallel()
	hash := "7be9CjQttHDBAkNkocfWMgzW5yDB853CYkhN7ZmBJsgF"
	u := result(`{"value":true}`)
	valid, err := client(u).BlockhashValid(t.Context(), hash)
	if err != nil || !valid {
		t.Fatalf("BlockhashValid = %t, %v", valid, err)
	}
	if got := string(u.requests()[0].params[1]); got != `{"commitment":"confirmed"}` {
		t.Fatalf("commitment = %s, want confirmed", got)
	}
	if _, err = client(result(`{"value":false}`)).BlockhashValid(t.Context(), hash); err != nil {
		t.Fatal(err)
	}
	if _, err = client(result(`{"value":true}`)).BlockhashValid(
		t.Context(), "bad",
	); errs.CodeOf(err) != errs.CodeInvalidAddress {
		t.Fatalf("invalid hash = %v", err)
	}
	if _, err = client(replying(503, "")).BlockhashValid(
		t.Context(), hash,
	); errs.CodeOf(err) != errs.CodeRPCUnavailable {
		t.Fatalf("unavailable = %v", err)
	}
}

func TestSignatureStatuses_edges(t *testing.T) {
	t.Parallel()
	c, u, _ := overFakes(t)
	if got, err := c.SignatureStatuses(t.Context(), nil); got != nil || err != nil ||
		len(u.requests()) != 0 {
		t.Fatalf("no signatures = %v, %v with %d calls", got, err, len(u.requests()))
	}
	_, err := c.SignatureStatuses(t.Context(), make([]chain.Signature, 257))
	wantCode(t, err, errs.CodeInvalidInput)
	_, err = c.SignatureStatuses(t.Context(), []chain.Signature{"only-one"})
	wantCode(t, err, errs.CodeDecodeFailed)
	_, err = client(replying(503, "")).SignatureStatuses(t.Context(), []chain.Signature{"a"})
	wantCode(t, err, errs.CodeRPCUnavailable)
	_, err = client(result(`7`)).SignatureStatuses(t.Context(), []chain.Signature{"a"})
	wantCode(t, err, errs.CodeDecodeFailed)
}

func TestSignaturesFor_pagesBackFromBefore(t *testing.T) {
	t.Parallel()
	c, u, _ := overFakes(t)
	got, err := c.SignaturesFor(t.Context(), member, solana.SignaturesOpts{Before: olderSig, Limit: 50})
	want := []solana.SignatureInfo{
		{Signature: deposit, Slot: 450_999_500, BlockTime: time.Unix(1_790_000_000, 0).UTC()},
		{
			Signature: olderSig,
			Slot:      450_999_400,
			Failed:    true,
			BlockTime: time.Unix(1_789_999_990, 0).UTC(),
		},
	}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("SignaturesFor = %+v, %v", got, err)
	}
	if p := string(
		u.requests()[0].params[1],
	); p != `{"before":"`+string(
		olderSig,
	)+`","commitment":"finalized","limit":50}` {
		t.Fatalf("options = %s", p)
	}
	if _, err := c.SignaturesFor(t.Context(), member, solana.SignaturesOpts{Limit: 10}); err != nil {
		t.Fatal(err)
	}
	if p := string(u.requests()[1].params[1]); p != `{"commitment":"finalized","limit":10}` {
		t.Fatalf("options without before = %s", p)
	}
	_, err = client(replying(503, "")).SignaturesFor(t.Context(), member, solana.SignaturesOpts{Limit: 1})
	wantCode(t, err, errs.CodeRPCUnavailable)
}

func TestSignaturesFor_stopsAtUntil(t *testing.T) {
	t.Parallel()
	c, u, _ := overFakes(t)
	if _, err := c.SignaturesFor(t.Context(), member, solana.SignaturesOpts{Until: olderSig, Limit: 50}); err != nil {
		t.Fatal(err)
	}
	got := string(u.requests()[0].params[1])
	want := `{"commitment":"finalized","limit":50,"until":"` + string(olderSig) + `"}`
	if got != want {
		t.Fatalf("options = %s", got)
	}
}

func TestSignaturesFor_sendsMinimumContextSlotAndMapsLaggingNode(t *testing.T) {
	t.Parallel()
	c, u, _ := overFakes(t)
	opts := solana.SignaturesOpts{Limit: 50, MinContextSlot: 451_000_000}
	if _, err := c.SignaturesFor(t.Context(), member, opts); err != nil {
		t.Fatal(err)
	}
	want := `{"commitment":"finalized","limit":50,"minContextSlot":451000000}`
	if got := string(u.requests()[0].params[1]); got != want {
		t.Fatalf("options = %s", got)
	}
	laggingBody := `{"jsonrpc":"2.0","id":1,"error":{"code":-32016,` +
		`"message":"Minimum context slot has not been reached"}}`
	lagging := client(replying(200, laggingBody))
	_, err := lagging.SignaturesFor(
		t.Context(), member, solana.SignaturesOpts{Limit: 1, MinContextSlot: 451_000_000},
	)
	wantCode(t, err, errs.CodeRPCUnavailable)
}

func TestSignaturesFor_acceptsOneToAThousandAndRefusesOtherLimitsWithoutACall(t *testing.T) {
	t.Parallel()
	c, u, _ := overFakes(t)
	for _, limit := range []int{0, -1, 1001} {
		_, err := c.SignaturesFor(t.Context(), member, solana.SignaturesOpts{Limit: limit})
		wantCode(t, err, errs.CodeInvalidInput)
	}
	if n := len(u.requests()); n != 0 {
		t.Fatalf("%d RPC calls for out-of-range limits", n)
	}
	for _, limit := range []int{1, 1000} {
		if _, err := c.SignaturesFor(t.Context(), member, solana.SignaturesOpts{Limit: limit}); err != nil {
			t.Fatalf("limit %d: %v", limit, err)
		}
	}
	if n := len(u.requests()); n != 2 {
		t.Fatalf("%d RPC calls for limits 1 and 1000, want 2", n)
	}
}

func TestReads_refuseInvalidAddressesWithoutACall(t *testing.T) {
	t.Parallel()
	c, u, _ := overFakes(t)
	ctx := t.Context()
	_, err := c.SOLBalance(ctx, "not-an-address")
	wantCode(t, err, errs.CodeInvalidAddress)
	_, err = c.TokenBalance(ctx, member, chain.Mint{Address: "bad"})
	wantCode(t, err, errs.CodeInvalidAddress)
	_, _, err = c.Accounts(ctx, []chain.SolanaAddress{"bad"}, 0)
	wantCode(t, err, errs.CodeInvalidAddress)
	_, _, err = c.TokenAccounts(ctx, "bad", usdc())
	wantCode(t, err, errs.CodeInvalidAddress)
	_, err = c.SignaturesFor(ctx, "bad", solana.SignaturesOpts{Limit: 1})
	wantCode(t, err, errs.CodeInvalidAddress)
	if n := len(u.requests()); n != 0 {
		t.Fatalf("%d RPC calls for invalid addresses", n)
	}
}

func TestSignatureStatuses_acceptsExactlyTheMostItAsksForAndRefusesOneMore(t *testing.T) {
	t.Parallel()
	const most = 256
	body := func(method string) string {
		if method == "getBlockHeight" {
			return `100`
		}
		return `{"value":[` + strings.TrimSuffix(strings.Repeat("null,", most), ",") + `]}`
	}
	got, err := client(byMethod(body)).SignatureStatuses(t.Context(), make([]chain.Signature, most))
	if err != nil || len(got) != most || got[most-1].State != solana.StateNotFound {
		t.Fatalf(
			"%d signatures = %d statuses, %v; want one not-found status each",
			most,
			len(got),
			err,
		)
	}
	_, err = client(byMethod(body)).SignatureStatuses(t.Context(), make([]chain.Signature, most+1))
	wantCode(t, err, errs.CodeInvalidInput)
}
