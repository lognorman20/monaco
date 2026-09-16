package app

import "testing"

func TestSwapLogHelpersDoNotPanic(t *testing.T) {
	t.Parallel()

	logSwapQuoteAttempt("group-1", "user-1", "AAPLx", 500_000)
	logSwapExecuteSubmit("group-1", "user-1", "AAPLx", "sig-xyz", "exec-req-1")
	logSwapPollTransition("group-1", "user-1", "AAPLx", "sig-xyz", "submitted", "success", 0)
	logSwapRefusal("group-1", "user-1", "BADx", "symbol not routable")
}
