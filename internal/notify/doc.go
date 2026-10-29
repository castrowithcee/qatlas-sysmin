// Package notify reports events over the approved notification channels (webhook, SMTP, Telegram and
// Slack). Sending never blocks the caller: Notify returns at once, delivers in the background with up to
// three attempts per channel and records the outcome in the audit log. Send delivers once to one named
// channel and returns the result.
//
// Only channels from the approved state are used, and each credential is looked up by the vault id that
// was approved. No secret appears in a message, an error or a log entry.
//
// While the vault is locked, the channels and credentials cannot be read. The Notifier then falls back to
// the last approved state it verified itself and sends only vault.locked, and only over channels that name
// no credential. It never reads an unverified snapshot, so a Notifier that has not seen the approved state
// since the process started cannot send anything while the vault is locked.
package notify
