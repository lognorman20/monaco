package app

import "testing"

func TestAppLogHelpersDoNotPanic(t *testing.T) {
	t.Parallel()

	logDepositCreateStart("group-1", 500_000)
	logDepositCreateSuccess("user-1", "group-1", "dep-1", 500_000)
	logDepositObserveSweepStart("dep-1", "group-1", "user-1", 500_000, "sig-1")
	logDepositObserveSweepIdempotent("dep-1", "sig-1")
	logDepositObserveSweepConfirmed("dep-1", "group-1", "user-1", "sig-1", true)
	logDepositBranchWarn("deposit test", "reason")
	logDepositBranchError("deposit test", nil)

	logRedeemStart("group-1", "user-1", "")
	logRedeemLockContended("user-1", "group-1")
	logRedeemDebited("job-1", "user-1", "group-1", 100, 50_000)
	logRedeemStatusTransition("job-1", "debited", "paying")
	logRedeemSettled("job-1", "user-1", "group-1", "wd-1", 50_000)

	logGovernanceCreateGroupStart("user-1", "Alpha")
	logGovernanceCreateGroupSuccess("group-1", "user-1", "Alpha")
	logGovernanceJoinGroupStart("user-1", "group-1")
	logGovernanceJoinGroupAlreadyMember("user-1", "group-1")
	logGovernanceCreateProposalStart("group-1", "user-1", "AAPLx", 500_000)
	logGovernanceCreateProposalSuccess("prop-1", "group-1", "user-1")
	logGovernanceCastVoteStart("prop-1", "user-1", "yes")
	logGovernanceCastVoteIdempotent("prop-1", "user-1")
	logGovernanceProposalStatusTransition("prop-1", "open", "passed")

	logSessionOpenStart()
	logSessionOpenSuccess("user-1")
	logSessionGetMeStart()
	logSessionEnsureWalletExisting("user-1")
	logSessionEnsureWalletCreated("user-1")

	logHomeGetStart()
	logHomeGetSuccess("user-1", 2, 3)

	logGroupCreateStart("user-1", "Alpha")
	logGroupCreateSuccess("group-1", "user-1", "Alpha")
	logGroupGetStart("user-1", "group-1")
	logGroupGetSuccess("group-1")

	logExecuteOnPassStart("prop-1", "group-1", "AAPLx", 500_000)
	logExecuteOnPassIdempotent("prop-1", "tx-1")
	logExecuteOnPassSuccess("prop-1", "tx-1", true)

	logSwapBuyStart("group-1", "user-1", "AAPLx", 500_000)
	logSwapBuySuccess("group-1", "user-1", "AAPLx", "tx-1", true)
	logSwapSellStart("group-1", "user-1", "AAPLx", 100)
	logSwapSellSuccess("group-1", "user-1", "AAPLx", "tx-2", false)
}
