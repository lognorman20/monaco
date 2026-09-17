package jupiter

import (
	"fmt"
	"testing"
)

func TestLogHelpersDoNotPanic(t *testing.T) {
	t.Parallel()

	logQuoteAttempt("group-1", "user-1", "AAPLx", 1_000_000)
	logQuoteRefusal("group-1", "user-1", "AAPLx", "no route")
	logExecuteSubmit("group-1", "user-1", "AAPLx", "sig-abc", "req-123")
	logExecuteSubmit("group-1", "user-1", "AAPLx", "", "req-only")
	logPollTransition("group-1", "user-1", "AAPLx", "sig-abc", "pending", "success", 0)
	logPollTransition("group-1", "user-1", "AAPLx", "", "processing", "failed", 1)
	logQuoteSuccess("group-1", "user-1", "AAPLx", "req-123", true)
	logOrderAttempt("group-1", "user-1", "AAPLx", 1_000_000)
	logOrderResult("group-1", "user-1", "AAPLx", "req-123", nil)
	logExecuteResult("group-1", "user-1", "AAPLx", "req-123", "success", 200, 0, nil, "", nil)
	logOrderHTTPFailure("group-1", "user-1", "AAPLx", buyOrderRequest{
		InputMint:  USDCMint,
		OutputMint: "mint-out",
		Amount:     1_000_000,
		Taker:      "taker-pubkey",
	}, "payer-pubkey", 400, []byte(`{"error":"bad route"}`), fmt.Errorf("jupiter: order status 400"))
	logPollExhausted("group-1", "user-1", "AAPLx", "req-123", "Pending", -1, "")
	logPollTerminalFailure("group-1", "user-1", "AAPLx", "req-123", "Failed", -1000, "failed to land")
}
