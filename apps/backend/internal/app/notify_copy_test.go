package app

import (
	"strings"
	"testing"
	"time"

	"github.com/monaco/monaco/packages/domain"
)

func TestFormatNotifyUSD(t *testing.T) {
	cases := map[int64]string{
		250_000_000:       "$250",
		1_250_000_000:     "$1,250",
		12_500_000:        "$12.50",
		400_000:           "$0.40",
		120_504_999:       "$120.50",
		120_505_000:       "$120.51",
		0:                 "$0",
		-5_000_000:        "−$5",
		1_000_000_000_000: "$1,000,000",
	}
	for micros, want := range cases {
		if got := formatNotifyUSD(micros); got != want {
			t.Fatalf("formatNotifyUSD(%d) = %q, want %q", micros, got, want)
		}
	}
}

func TestHumanizeNotifyDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "under a minute"},
		{time.Minute, "1 minute"},
		{45 * time.Minute, "45 minutes"},
		{59*time.Minute + 45*time.Second, "1 hour"},
		{time.Hour, "1 hour"},
		{3*time.Hour + 20*time.Minute, "3 hours"},
		{24 * time.Hour, "1 day"},
		{23*time.Hour + 50*time.Minute, "1 day"},
		{30 * time.Hour, "30 hours"},
		{48 * time.Hour, "2 days"},
		{7 * 24 * time.Hour, "7 days"},
	}
	for _, tc := range cases {
		if got := humanizeNotifyDuration(tc.d); got != tc.want {
			t.Fatalf("humanize(%s) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestNotifyAssetName(t *testing.T) {
	cases := map[string]string{
		"AAPLx":    "Apple",
		"GOOGLx":   "Alphabet",
		"BRK.Bx":   "Berkshire Hathaway",
		"NVDA":     "Nvidia",
		"tSpaceX":  "SpaceX",
		"SPACEX":   "SpaceX",
		"FIGUREAI": "Figure",
		"ZZZx":     "ZZZ",
		"":         "a stock",
	}
	for symbol, want := range cases {
		if got := notifyAssetName(symbol); got != want {
			t.Fatalf("notifyAssetName(%q) = %q, want %q", symbol, got, want)
		}
	}
}

func TestNotifyProposalSubject(t *testing.T) {
	cases := []struct {
		kind domain.ProposalKind
		want string
	}{
		{domain.ProposalKindBuy, "$250 of Apple"},
		{domain.ProposalKindSell, "selling Apple"},
		{domain.ProposalKindAddAgent, "adding Scout with $500"},
		{domain.ProposalKindPauseAgent, "pausing Scout"},
		{domain.ProposalKindResumeAgent, "resuming Scout"},
		{domain.ProposalKindRevokeAgent, "removing Scout"},
	}
	for _, tc := range cases {
		if got := notifyProposalSubject(tc.kind, "AAPLx", 250_000_000, "Scout", 500_000_000); got != tc.want {
			t.Fatalf("%s: %q, want %q", tc.kind, got, tc.want)
		}
	}
}

// forbiddenNotifyTerms mirrors MainFlowCopyAudit.forbiddenTerms in MonacoCore: the words the
// app never shows a member. A push is shown to a member, so the same list applies.
var forbiddenNotifyTerms = []string{
	"wallet", "gas", "seed phrase", " mint", "mint ", "xstock", "club", " group", "group ",
	"treasury", "route", "quote", "bps", "units", "stake", "sweep", "http", "api key", "p&l",
}

func TestNotificationCopy_usesTheProductsWords(t *testing.T) {
	// Every template, filled the way production fills it.
	var copy []string
	add := func(title, body string) { copy = append(copy, title, body) }
	subject := notifyProposalSubject(domain.ProposalKindBuy, "AAPLx", 250_000_000, "", 0)
	add(proposalCreatedCopy("Jordan", "Sunday Investors", subject, 24*time.Hour))
	add(proposalExpiringCopy("Sunday Investors", subject, time.Hour))
	add(proposalNudgeCopy("Priya", "Sunday Investors", subject, 5*time.Hour))
	for _, kind := range []domain.ProposalKind{domain.ProposalKindAddAgent, domain.ProposalKindPauseAgent, domain.ProposalKindResumeAgent, domain.ProposalKindRevokeAgent} {
		add(proposalPassedCopy(kind, "Sunday Investors", "Jordan", "Scout", 500_000_000))
	}
	add(proposalFailedCopy("Sunday Investors", "Jordan", subject))
	add(proposalExpiredCopy("Sunday Investors", subject))
	add(tradeBoughtCopy("Sunday Investors", "GOOGLx", 250_000_000))
	add(tradeSoldCopy("Sunday Investors", "GOOGLx", 312_400_000))
	add(botTradeCopy("Scout", "Sunday Investors", "NVDAx", false, 40_000_000))
	add(botTradeCopy("Scout", "Sunday Investors", "NVDAx", true, 42_100_000))
	add(memberJoinedCopy("Priya", "Sunday Investors", 5))
	add(joinRequestCopy("Priya", "Sunday Investors"))
	add(joinApprovedCopy("Sunday Investors"))
	add(fundsArrivedCopy(500_000_000))
	add(fundCreditedCopy("Sunday Investors", 250_000_000))
	add(cashOutSettledCopy("Sunday Investors", 120_000_000, true))
	add(cashOutSettledCopy("Sunday Investors", 120_000_000, false))

	for _, line := range copy {
		lower := strings.ToLower(line)
		for _, term := range forbiddenNotifyTerms {
			if strings.Contains(lower, term) {
				t.Errorf("%q says %q", line, strings.TrimSpace(term))
			}
		}
		for _, word := range strings.FieldsFunc(lower, func(r rune) bool { return r < 'a' || r > 'z' }) {
			if word == "nav" {
				t.Errorf("%q says nav", line)
			}
		}
	}
}

func TestNotificationCopy_spellsOutTheExamples(t *testing.T) {
	title, _ := proposalCreatedCopy("Jordan", "Sunday Investors", notifyProposalSubject(domain.ProposalKindBuy, "GOOGLx", 250_000_000, "", 0), 24*time.Hour)
	if title != "Jordan proposed $250 of Alphabet in Sunday Investors" {
		t.Fatalf("created = %q", title)
	}
	if title, _ := tradeBoughtCopy("Sunday Investors", "GOOGLx", 250_000_000); title != "Sunday Investors bought Alphabet" {
		t.Fatalf("bought = %q", title)
	}
	if title, _ := memberJoinedCopy("Priya", "Sunday Investors", 4); title != "Priya joined Sunday Investors" {
		t.Fatalf("joined = %q", title)
	}
	if title, _ := fundsArrivedCopy(500_000_000); title != "$500 arrived in your balance" {
		t.Fatalf("arrived = %q", title)
	}
	if title, _ := botTradeCopy("Scout", "Sunday Investors", "NVDAx", false, 40_000_000); title != "Scout bought $40 of Nvidia" {
		t.Fatalf("bot = %q", title)
	}
}

func TestNotificationCopy_blankNamesFallBack(t *testing.T) {
	title, _ := proposalCreatedCopy(" ", "", "$5 of Apple", time.Hour)
	if title != "A member proposed $5 of Apple in your cabal" {
		t.Fatalf("title = %q", title)
	}
	if _, body := chatMessageCopy("Priya", "X", "line one\n\n  line   two"); body != "line one line two" {
		t.Fatalf("chat body = %q", body)
	}
	long := strings.Repeat("a", 400)
	if _, body := chatMessageCopy("Priya", "X", long); len([]rune(body)) != 160 || !strings.HasSuffix(body, "…") {
		t.Fatalf("long chat body is %d runes", len([]rune(body)))
	}
}

func TestNotificationCategory(t *testing.T) {
	cases := map[string]string{
		NotifyProposalCreated:  NotifyCategoryProposals,
		NotifyProposalExpiring: NotifyCategoryProposals,
		NotifyProposalNudge:    NotifyCategoryProposals,
		NotifyJoinRequest:      NotifyCategoryProposals,
		NotifyProposalPassed:   NotifyCategoryResults,
		NotifyProposalFailed:   NotifyCategoryResults,
		NotifyProposalExpired:  NotifyCategoryResults,
		NotifyTradeBought:      NotifyCategoryResults,
		NotifyTradeSold:        NotifyCategoryResults,
		NotifyBotTrade:         NotifyCategoryResults,
		NotifyMemberJoined:     NotifyCategoryResults,
		NotifyJoinApproved:     NotifyCategoryResults,
		NotifyChatMessage:      NotifyCategoryChat,
		NotifyFundsArrived:     NotifyCategoryMoney,
		NotifyFundCredited:     NotifyCategoryMoney,
		NotifyCashOutSettled:   NotifyCategoryMoney,
	}
	for kind, want := range cases {
		if got := NotificationCategory(kind); got != want {
			t.Fatalf("%s → %s, want %s", kind, got, want)
		}
	}
}
