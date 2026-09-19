package jupiter

import (
	"context"
	"testing"
)

func TestJupiterExecute_successRequiresStatusSuccessAndCodeZero(t *testing.T) {
	t.Parallel()

	// Arrange
	body := FixtureJupiterExecuteSuccess("confirmed-sig")

	// Act
	result, err := ParseExecuteResponse(body)

	// Assert
	if err != nil {
		t.Fatalf("ParseExecuteResponse() error = %v", err)
	}
	if !result.IsConfirmedSuccess() {
		t.Fatalf("expected confirmed success, got status=%s code=%d", result.Status, result.Code)
	}
	if result.Signature != "confirmed-sig" {
		t.Fatalf("Signature = %q, want confirmed-sig", result.Signature)
	}
	if result.OutputAmountResult != "500000" {
		t.Fatalf("OutputAmountResult = %q, want 500000", result.OutputAmountResult)
	}

	nonZero := FixtureJupiterExecutePendingSuccessCode()
	badResult, err := ParseExecuteResponse(nonZero)
	if err != nil {
		t.Fatalf("ParseExecuteResponse() non-zero error = %v", err)
	}
	if badResult.IsConfirmedSuccess() {
		t.Fatal("expected Success with code -1 to fail confirmed success check")
	}
}

func TestJupiterPoll_transitionsFromPendingToSuccess(t *testing.T) {
	t.Parallel()

	// Arrange
	const requestID = "req-poll-transition"
	client := NewFakeClient()
	RegisterExecutePoll(client, requestID, []ExecuteResult{
		{Status: ExecuteStatusPending, Code: -1},
		{
			Status:             ExecuteStatusSuccess,
			Code:               0,
			Signature:          "poll-success-sig",
			OutputAmountResult: "500000",
			InputAmountResult:  "1000000",
		},
	})

	// Act
	result, err := PollUntilConfirmed(context.Background(), client, PollExecuteParams{
		GroupID:           "group-1",
		UserID:            "user-1",
		Symbol:            "AAPLx",
		RequestID:         requestID,
		SignedTransaction: "signed-tx",
	}, PollConfig{MaxAttempts: 3, Interval: 1})

	// Assert
	if err != nil {
		t.Fatalf("PollUntilConfirmed() error = %v", err)
	}
	if !result.IsConfirmedSuccess() {
		t.Fatalf("expected confirmed success, got status=%s code=%d", result.Status, result.Code)
	}
	if result.Signature != "poll-success-sig" {
		t.Fatalf("Signature = %q, want poll-success-sig", result.Signature)
	}
}

func TestJupiterPoll_timeoutOrNonZeroCode_marksTransactionFailed(t *testing.T) {
	t.Parallel()

	// Arrange
	const requestID = "req-terminal-failure"
	client := NewFakeClient()
	RegisterExecutePoll(client, requestID, []ExecuteResult{
		{Status: ExecuteStatusFailed, Code: -1000, Signature: "failed-sig", Error: "failed to land"},
	})

	// Act
	_, err := PollUntilConfirmed(context.Background(), client, PollExecuteParams{
		GroupID:           "group-1",
		UserID:            "user-1",
		Symbol:            "AAPLx",
		RequestID:         requestID,
		SignedTransaction: "signed-tx",
	}, PollConfig{MaxAttempts: 2, Interval: 1})

	// Assert
	if err == nil {
		t.Fatal("expected terminal failure error")
	}
}
