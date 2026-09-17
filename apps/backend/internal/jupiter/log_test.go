package jupiter

import "testing"

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
	logExecuteResult("group-1", "user-1", "AAPLx", "req-123", "success", 0, nil)
}
