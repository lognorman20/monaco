package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestFormatUSDCAtomic(t *testing.T) {
	tests := []struct {
		raw  int64
		want string
	}{
		{0, "0.000000 USDC"},
		{1_500_000, "1.500000 USDC"},
		{44765, "0.044765 USDC"},
	}
	for _, tc := range tests {
		if got := formatUSDCAtomic(tc.raw); got != tc.want {
			t.Fatalf("formatUSDCAtomic(%d) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestFormatAddress(t *testing.T) {
	addr := "EfFBEMVogFPxpTNvfVFxdYc89KojsRNDzHDuuoKwqtrF"
	got := formatAddress(addr)
	want := "EfFB…qtrF"
	if got != want {
		t.Fatalf("formatAddress() = %q, want %q", got, want)
	}
}

func TestFormatActionLine(t *testing.T) {
	tests := []struct {
		name   string
		action sweepAction
		want   string
	}{
		{
			name: "ok usdc sweep with tx",
			action: sweepAction{
				kind:      "usdc-sweep",
				label:     "USDC",
				mint:      "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
				rawAmount: 2_000_000,
				status:    actionOK,
				txSig:     "5kLmNopQrStUvWxYzAbCdEfGhIjKlMnOpQrStUvWxYzAbCdEfGh",
			},
			want: "ok usdc-sweep USDC mint=EPjFWd…Dt1v amount=2.000000 USDC (2000000 raw) tx=5kLm…EfGh",
		},
		{
			name: "fail jupiter sell",
			action: sweepAction{
				kind:      "jupiter-sell",
				label:     "TSLAx",
				mint:      "So11111111111111111111111111111111111111112",
				rawAmount: 1000,
				status:    actionFail,
				errMsg:    "no route",
			},
			want: "FAIL jupiter-sell TSLAx mint=So1111…1112 amount=1000 raw err=no route",
		},
		{
			name: "skipped zero usdc",
			action: sweepAction{
				kind:      "usdc-sweep",
				label:     "USDC",
				mint:      "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
				rawAmount: 0,
				status:    actionSkipped,
				note:      "zero USDC balance",
			},
			want: "skipped usdc-sweep USDC mint=EPjFWd…Dt1v amount=0.000000 USDC (0 raw) (zero USDC balance)",
		},
		{
			name: "dry-run jupiter sell",
			action: sweepAction{
				kind:      "jupiter-sell",
				label:     "AAPLx",
				mint:      "So11111111111111111111111111111111111111112",
				rawAmount: 500,
				status:    actionDryRun,
				note:      "no tx sent",
			},
			want: "dry-run jupiter-sell AAPLx mint=So1111…1112 amount=500 raw (no tx sent)",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatActionLine(tc.action); got != tc.want {
				t.Fatalf("formatActionLine() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPrintSweepRecap_ShowsPerActionStatus(t *testing.T) {
	est := int64(1_500_000)
	var out bytes.Buffer
	printSweepRecap(&out, sweepRecap{
		dryRun:      false,
		destination: "Dest111111111111111111111111111111111111111",
		wallets: []walletPlan{
			{
				kind:    "member",
				address: "EfFBEMVogFPxpTNvfVFxdYc89KojsRNDzHDuuoKwqtrF",
				actions: []sweepAction{
					{
						kind:       "jupiter-sell",
						label:      "TSLAx",
						mint:       "So11111111111111111111111111111111111111112",
						rawAmount:  1000,
						estUSDCOut: &est,
						status:     actionOK,
						txSig:      "5kLmNopQrStUvWxYzAbCdEfGhIjKlMnOpQrStUvWxYzAbCdEfGh",
					},
					{
						kind:      "usdc-sweep",
						label:     "USDC",
						mint:      "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
						rawAmount: 2_000_000,
						status:    actionOK,
						txSig:     "6mNoPqRsTuVwXyZaBcDeFgHiJkLmNoPqRsTuVwXyZaBcDeFg",
					},
				},
				drainTotal: 3_500_000,
			},
			{
				kind:    "member",
				address: "AbCdEfGhIjKlMnOpQrStUvWxYzAbCdEfGhIjKlMnOpQrStUvWx",
				failed:  true,
				actions: []sweepAction{
					{
						kind:      "jupiter-sell",
						label:     "AAPLx",
						mint:      "So11111111111111111111111111111111111111112",
						rawAmount: 500,
						status:    actionFail,
						errMsg:    "no route",
					},
					{
						kind:      "usdc-sweep",
						label:     "USDC",
						mint:      "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
						rawAmount: 1_000_000,
						status:    actionOK,
						txSig:     "7nOpQrStUvWxYzAbCdEfGhIjKlMnOpQrStUvWxYzAbCdEfGhIj",
					},
				},
				drainTotal: 1_000_000,
			},
		},
		grandTotal: 4_500_000,
	})

	text := out.String()
	for _, want := range []string{
		"=== Sweep summary (live) ===",
		"Wallets: 2 (1 with FAIL actions)",
		"[1] member EfFB…qtrF",
		"ok jupiter-sell",
		"ok usdc-sweep",
		"Drain total: 3.500000 USDC",
		"[2] member AbCd…UvWx FAIL",
		"FAIL jupiter-sell",
		"err=no route",
		"Grand total: 4.500000 USDC across 2 wallet(s)",
		"Actions: ok=3 fail=1 skipped=0 dry-run=0",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("recap missing %q\n%s", want, text)
		}
	}
}
