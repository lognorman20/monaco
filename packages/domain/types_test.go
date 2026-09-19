package domain

import "testing"

func TestParseJoinMode_validValues(t *testing.T) {
	for _, raw := range []string{"open", "request"} {
		mode, err := ParseJoinMode(raw)
		if err != nil {
			t.Fatalf("ParseJoinMode(%q): %v", raw, err)
		}
		if string(mode) != raw {
			t.Fatalf("got %q want %q", mode, raw)
		}
	}
}

func TestParseVoterSetMode_validValues(t *testing.T) {
	for _, raw := range []string{"all_members", "named_subset"} {
		mode, err := ParseVoterSetMode(raw)
		if err != nil {
			t.Fatalf("ParseVoterSetMode(%q): %v", raw, err)
		}
		if string(mode) != raw {
			t.Fatalf("got %q want %q", mode, raw)
		}
	}
}

func TestParseVoteThreshold_validValues(t *testing.T) {
	for _, raw := range []string{"unanimous", "majority"} {
		threshold, err := ParseVoteThreshold(raw)
		if err != nil {
			t.Fatalf("ParseVoteThreshold(%q): %v", raw, err)
		}
		if string(threshold) != raw {
			t.Fatalf("got %q want %q", threshold, raw)
		}
	}
}

func TestParseProposalKind_validValues(t *testing.T) {
	for _, raw := range []string{"buy", "sell", "add_agent", "pause_agent", "resume_agent", "revoke_agent"} {
		got, err := ParseProposalKind(raw)
		if err != nil || string(got) != raw {
			t.Fatalf("ParseProposalKind(%q) = %q, %v", raw, got, err)
		}
	}
}

func TestParseProposalKind_rejectsUnknown(t *testing.T) {
	if _, err := ParseProposalKind("redeem"); err == nil {
		t.Fatal("expected invalid proposal kind")
	}
}

func TestParseProposalStatus_validValues(t *testing.T) {
	for _, raw := range []string{"open", "passed", "failed", "expired"} {
		status, err := ParseProposalStatus(raw)
		if err != nil {
			t.Fatalf("ParseProposalStatus(%q): %v", raw, err)
		}
		if string(status) != raw {
			t.Fatalf("got %q want %q", status, raw)
		}
	}
}

func TestParseWithdrawalStatus_validValues(t *testing.T) {
	for _, raw := range []string{"pending", "settled"} {
		status, err := ParseWithdrawalStatus(raw)
		if err != nil {
			t.Fatalf("ParseWithdrawalStatus(%q): %v", raw, err)
		}
		if string(status) != raw {
			t.Fatalf("got %q want %q", status, raw)
		}
	}
}
