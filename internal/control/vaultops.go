package control

// The operations of the vault itself and of the settings the terminal interface edits.
const (
	// OpVaultCreate creates the vault with its first passphrase. It is allowed only while no vault exists
	// and needs no admin session, since there is no passphrase to prove yet. The vault is unlocked after it.
	OpVaultCreate = "vault.create"
	// OpVaultPassphrase changes the vault passphrase and needs the admin session.
	OpVaultPassphrase = "vault.passphrase"
	// OpSettingsShow reads the admin timeout without an admin session; OpSettingsAdminTimeout changes it and
	// needs one.
	OpSettingsShow         = "settings.show"
	OpSettingsAdminTimeout = "settings.admin-timeout"
)

// PassphraseChangeParams are the parameters of OpVaultPassphrase. Neither passphrase is repeated in any
// answer, log entry or error.
type PassphraseChangeParams struct {
	Old string `json:"old"`
	New string `json:"new"`
}

// AdminTimeoutParams are the parameters of OpSettingsAdminTimeout. AdminTimeout is a Go duration text such
// as "5m"; "0" makes every change ask for the passphrase, anything else must be 1s to 24h.
type AdminTimeoutParams struct {
	AdminTimeout string `json:"admin_timeout"`
}

// SettingsResult is the answer to OpSettingsShow and OpSettingsAdminTimeout. AdminTimeout is the effective
// timeout as a duration text such as "15m0s"; Explicit is false while sysmin.yaml does not set it and the
// default applies.
type SettingsResult struct {
	AdminTimeout string `json:"admin_timeout"`
	Explicit     bool   `json:"explicit"`
}
