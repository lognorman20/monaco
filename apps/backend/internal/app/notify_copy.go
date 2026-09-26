package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/monaco/monaco/packages/domain"
)

// The words each notification says. Written once here so the inbox row and the lock screen
// match, in the product's own words: cabal, pot, slice, balance, propose, vote. Never wallet,
// treasury, stake, mint or any of MainFlowCopyAudit's other terms (notify_copy_test checks).

// notifyProposalSubject names what a proposal is about, as a noun phrase: "$250 of Alphabet",
// "selling Tesla", "adding Scout with $500".
func notifyProposalSubject(kind domain.ProposalKind, symbol string, usdcMicros int64, agentName string, allocationMicros int64) string {
	agent := notifyAgentName(agentName)
	switch kind {
	case domain.ProposalKindSell:
		return "selling " + notifyAssetName(symbol)
	case domain.ProposalKindAddAgent:
		if allocationMicros > 0 {
			return fmt.Sprintf("adding %s with %s", agent, formatNotifyUSD(allocationMicros))
		}
		return "adding " + agent
	case domain.ProposalKindPauseAgent:
		return "pausing " + agent
	case domain.ProposalKindResumeAgent:
		return "resuming " + agent
	case domain.ProposalKindRevokeAgent:
		return "removing " + agent
	default:
		if usdcMicros > 0 {
			return fmt.Sprintf("%s of %s", formatNotifyUSD(usdcMicros), notifyAssetName(symbol))
		}
		return notifyAssetName(symbol)
	}
}

// proposalCreatedCopy: "Jordan proposed $250 of Alphabet in Sunday Investors".
func proposalCreatedCopy(proposer, cabal, subject string, closesIn time.Duration) (string, string) {
	return fmt.Sprintf("%s proposed %s in %s", notifyPersonName(proposer), subject, notifyCabalName(cabal)),
		fmt.Sprintf("Voting closes in %s.", humanizeNotifyDuration(closesIn))
}

// proposalExpiringCopy: "Voting on $250 of Alphabet closes in 1 hour".
func proposalExpiringCopy(cabal, subject string, closesIn time.Duration) (string, string) {
	return fmt.Sprintf("Voting on %s closes in %s", subject, humanizeNotifyDuration(closesIn)),
		fmt.Sprintf("%s is waiting on your vote.", notifyCabalName(cabal))
}

// proposalNudgeCopy: "Jordan is waiting on your vote".
func proposalNudgeCopy(sender, cabal, subject string, closesIn time.Duration) (string, string) {
	return fmt.Sprintf("%s is waiting on your vote", notifyPersonName(sender)),
		fmt.Sprintf("%s in %s. Voting closes in %s.", capitalizeFirst(subject), notifyCabalName(cabal), humanizeNotifyDuration(closesIn))
}

// proposalPassedCopy is for proposals that change the cabal without a trade (the bot votes).
func proposalPassedCopy(kind domain.ProposalKind, cabal, proposer, agentName string, allocationMicros int64) (string, string) {
	agent := notifyAgentName(agentName)
	var decided string
	switch kind {
	case domain.ProposalKindAddAgent:
		decided = "voted to add " + agent
		if allocationMicros > 0 {
			decided += " with " + formatNotifyUSD(allocationMicros)
		}
	case domain.ProposalKindPauseAgent:
		decided = "voted to pause " + agent
	case domain.ProposalKindResumeAgent:
		decided = "voted to resume " + agent
	case domain.ProposalKindRevokeAgent:
		decided = "voted to remove " + agent
	default:
		decided = "passed a vote"
	}
	return fmt.Sprintf("%s %s", notifyCabalName(cabal), decided),
		fmt.Sprintf("Proposed by %s.", notifyPersonName(proposer))
}

// proposalFailedCopy: "Sunday Investors voted down $250 of Alphabet".
func proposalFailedCopy(cabal, proposer, subject string) (string, string) {
	return fmt.Sprintf("%s voted down %s", notifyCabalName(cabal), subject),
		fmt.Sprintf("Proposed by %s.", notifyPersonName(proposer))
}

// proposalExpiredCopy: "The vote on $250 of Alphabet ran out of time".
func proposalExpiredCopy(cabal, subject string) (string, string) {
	return fmt.Sprintf("The vote on %s ran out of time", subject),
		fmt.Sprintf("%s didn't reach a decision.", notifyCabalName(cabal))
}

// tradeBoughtCopy: "Sunday Investors bought Alphabet".
func tradeBoughtCopy(cabal, symbol string, spentMicros int64) (string, string) {
	body := "Voted in by the cabal."
	if spentMicros > 0 {
		body = fmt.Sprintf("%s from the pot, as voted.", formatNotifyUSD(spentMicros))
	}
	return fmt.Sprintf("%s bought %s", notifyCabalName(cabal), notifyAssetName(symbol)), body
}

// tradeSoldCopy: "Sunday Investors sold Alphabet".
func tradeSoldCopy(cabal, symbol string, proceedsMicros int64) (string, string) {
	body := "Voted in by the cabal."
	if proceedsMicros > 0 {
		body = fmt.Sprintf("%s back in the pot.", formatNotifyUSD(proceedsMicros))
	}
	return fmt.Sprintf("%s sold %s", notifyCabalName(cabal), notifyAssetName(symbol)), body
}

// botTradeCopy: "Scout bought $40 of Nvidia".
func botTradeCopy(agentName, cabal, symbol string, sell bool, usdcMicros int64) (string, string) {
	agent := notifyAgentName(agentName)
	if sell {
		body := fmt.Sprintf("Sold for %s.", notifyCabalName(cabal))
		if usdcMicros > 0 {
			body = fmt.Sprintf("%s back in %s's pot.", formatNotifyUSD(usdcMicros), notifyCabalName(cabal))
		}
		return fmt.Sprintf("%s sold %s", agent, notifyAssetName(symbol)), body
	}
	title := fmt.Sprintf("%s bought %s", agent, notifyAssetName(symbol))
	if usdcMicros > 0 {
		title = fmt.Sprintf("%s bought %s of %s", agent, formatNotifyUSD(usdcMicros), notifyAssetName(symbol))
	}
	return title, fmt.Sprintf("For %s, inside the budget the cabal voted.", notifyCabalName(cabal))
}

// memberJoinedCopy: "Priya joined Sunday Investors".
func memberJoinedCopy(member, cabal string, memberCount int) (string, string) {
	body := ""
	if memberCount > 1 {
		body = fmt.Sprintf("%d members now.", memberCount)
	}
	return fmt.Sprintf("%s joined %s", notifyPersonName(member), notifyCabalName(cabal)), body
}

// joinRequestCopy: "Priya asked to join Sunday Investors".
func joinRequestCopy(member, cabal string) (string, string) {
	return fmt.Sprintf("%s asked to join %s", notifyPersonName(member), notifyCabalName(cabal)),
		"Let them in or turn them down from the cabal."
}

// joinApprovedCopy: "You're in Sunday Investors".
func joinApprovedCopy(cabal string) (string, string) {
	return fmt.Sprintf("You're in %s", notifyCabalName(cabal)), "Your request to join was accepted."
}

// chatMessageCopy: "Priya in Sunday Investors" over the message itself.
func chatMessageCopy(author, cabal, message string) (string, string) {
	return fmt.Sprintf("%s in %s", notifyPersonName(author), notifyCabalName(cabal)),
		clampText(strings.Join(strings.Fields(message), " "), 160)
}

// fundsArrivedCopy: "$500 arrived in your balance".
func fundsArrivedCopy(amountMicros int64) (string, string) {
	return fmt.Sprintf("%s arrived in your balance", formatNotifyUSD(amountMicros)),
		"Put it into a cabal when you're ready."
}

// fundCreditedCopy: "$250 is in Sunday Investors".
func fundCreditedCopy(cabal string, amountMicros int64) (string, string) {
	return fmt.Sprintf("%s is in %s", formatNotifyUSD(amountMicros), notifyCabalName(cabal)),
		"It's in the pot and counted in your slice."
}

// cashOutSettledCopy: "$120 from Sunday Investors is in your balance".
func cashOutSettledCopy(cabal string, amountMicros int64, toBalance bool) (string, string) {
	if toBalance {
		return fmt.Sprintf("%s from %s is in your balance", formatNotifyUSD(amountMicros), notifyCabalName(cabal)),
			"Your cash out went through."
	}
	return fmt.Sprintf("You cashed out %s from %s", formatNotifyUSD(amountMicros), notifyCabalName(cabal)),
		"Sent to your payout address."
}

func notifyPersonName(name string) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	return "A member"
}

func notifyCabalName(name string) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	return "your cabal"
}

func notifyAgentName(name string) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	return "the bot"
}

func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	if runes[0] >= 'a' && runes[0] <= 'z' {
		runes[0] = runes[0] - 'a' + 'A'
	}
	return string(runes)
}

// humanizeNotifyDuration rounds to the unit a person would say: "45 minutes", "1 hour",
// "3 hours", "1 day". Under a minute is "under a minute".
func humanizeNotifyDuration(d time.Duration) string {
	if d < time.Minute {
		return "under a minute"
	}
	if d < time.Hour {
		m := int((d + 30*time.Second) / time.Minute)
		if m >= 60 {
			return "1 hour"
		}
		return plural(m, "minute")
	}
	if d < 36*time.Hour {
		h := int((d + 30*time.Minute) / time.Hour)
		if h >= 36 {
			return "2 days"
		}
		if h == 24 {
			return "1 day"
		}
		return plural(h, "hour")
	}
	days := int((d + 12*time.Hour) / (24 * time.Hour))
	return plural(days, "day")
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// formatNotifyUSD writes micros as dollars the way a person would: "$250", "$1,250",
// "$12.50", "$0.40". Whole dollars drop the cents.
func formatNotifyUSD(micros int64) string {
	negative := micros < 0
	if negative {
		micros = -micros
	}
	cents := (micros + 5_000) / 10_000
	dollars := cents / 100
	rem := cents % 100
	whole := groupThousands(dollars)
	out := "$" + whole
	if rem != 0 {
		out = fmt.Sprintf("$%s.%02d", whole, rem)
	}
	if negative {
		return "−" + out
	}
	return out
}

func groupThousands(n int64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	lead := len(s) % 3
	if lead > 0 {
		b.WriteString(s[:lead])
	}
	for i := lead; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

// notifyAssetNames are the names people know these stocks by. Kept in step with the app's
// AssetDisplayNames so a push and the screen it opens name the stock the same way.
var notifyAssetNames = map[string]string{
	"AAPL": "Apple", "ABBV": "AbbVie", "ABT": "Abbott", "ACN": "Accenture", "AMBR": "Amber",
	"AMZN": "Amazon", "APP": "AppLovin", "AVGO": "Broadcom", "AZN": "AstraZeneca",
	"BAC": "Bank of America", "BRK.B": "Berkshire Hathaway", "CMCSA": "Comcast", "COIN": "Coinbase",
	"CRCL": "Circle", "CRM": "Salesforce", "CRWD": "CrowdStrike", "CSCO": "Cisco", "CVX": "Chevron",
	"DHR": "Danaher", "GLD": "Gold", "GME": "GameStop", "GOOGL": "Alphabet", "GS": "Goldman Sachs",
	"HD": "Home Depot", "HOOD": "Robinhood", "IBM": "IBM", "INTC": "Intel", "JNJ": "Johnson & Johnson",
	"JPM": "JPMorgan", "KO": "Coca-Cola", "LIN": "Linde", "LLY": "Eli Lilly", "MA": "Mastercard",
	"MCD": "McDonald's", "MDT": "Medtronic", "META": "Meta", "MRK": "Merck", "MRVL": "Marvell",
	"MSFT": "Microsoft", "MSTR": "Strategy", "NFLX": "Netflix", "NVDA": "Nvidia", "NVO": "Novo Nordisk",
	"ORCL": "Oracle", "PEP": "PepsiCo", "PFE": "Pfizer", "PG": "Procter & Gamble", "PLTR": "Palantir",
	"PM": "Philip Morris", "QQQ": "Nasdaq 100", "SPY": "S&P 500", "TBLL": "Treasury bills",
	"TMO": "Thermo Fisher", "TQQQ": "Nasdaq 100 3x", "TSLA": "Tesla", "UNH": "UnitedHealth", "V": "Visa",
	"VTI": "US total market", "WMT": "Walmart", "XOM": "Exxon Mobil",
}

// notifyPreIPONames covers both issuers' symbols for the same company.
var notifyPreIPONames = map[string]string{
	"TSPACEX": "SpaceX", "SPACEX": "SpaceX", "TOPENAI": "OpenAI", "OPENAI": "OpenAI",
	"TKALSHI": "Kalshi", "KALSHI": "Kalshi", "ANTHROPIC": "Anthropic", "ANDURIL": "Anduril",
	"NEURALINK": "Neuralink", "FIGUREAI": "Figure", "POLYMARKET": "Polymarket",
}

// notifyAssetName is "Apple" for AAPLx, "SpaceX" for tSpaceX, and the plain ticker otherwise.
func notifyAssetName(symbol string) string {
	s := strings.TrimSpace(symbol)
	if s == "" {
		return "a stock"
	}
	if name, ok := notifyPreIPONames[strings.ToUpper(s)]; ok {
		return name
	}
	ticker := s
	if len(ticker) >= 2 && strings.HasSuffix(ticker, "x") {
		ticker = ticker[:len(ticker)-1]
	}
	if name, ok := notifyAssetNames[strings.ToUpper(ticker)]; ok {
		return name
	}
	return strings.ToUpper(ticker)
}
