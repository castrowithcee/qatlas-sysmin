// Package notify reports events over the approved notification channels (webhook, SMTP, Telegram and
// Slack). Sending never blocks the caller: Notify returns at once, delivers in the background with up to
// three attempts per channel and records the outcome in the audit log. Send delivers once to one named
// channel and returns the result.
//
// Only channels from the approved state are used, and each credential is looked up by the vault id that
// was approved. No secret appears in a message, an error or a log entry.
//
// While the vault is locked, the channels and credentials cannot be verified or read. The Notifier then
// sends only vault.locked, and only over channels that name no credential. It uses the last approved state
// it verified itself; without one, for example after a cold start, it reads the approved channel snapshot
// unverified, and only for this event. If that snapshot is missing or unreadable, nothing is sent.
package notify
