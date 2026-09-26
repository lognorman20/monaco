package app

// Setters that wire the notifier into the services whose events it reports. Each service keeps
// working with none: a nil *Notifier does nothing.

// SetNotifier reports fund-to-cabal credits and watches for money arriving in the balance.
func (d *DepositService) SetNotifier(n *Notifier) { d.notifier = n }

// SetNotifier reports settled cash outs.
func (r *RedeemService) SetNotifier(n *Notifier) { r.notifier = n }

// SetNotifier reports chat messages to the rest of the cabal.
func (s *GroupChatService) SetNotifier(n *Notifier) { s.notifier = n }

// SetNotifier reports voted buys and sells once they fill.
func (s *ExecuteOnPassService) SetNotifier(n *Notifier) { s.notifier = n }

// SetNotifier reports the bot's trades to the cabal.
func (s *AgentIntentService) SetNotifier(n *Notifier) *AgentIntentService {
	s.notifier = n
	return s
}
