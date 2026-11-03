package auditlog

// The actions an entry can record, grouped by the area that takes them. An entry names the action, never an
// argument, a value, or a secret.
const (
	OperationConfigApply = "config.apply"
	OperationConfigRead  = "config.read"

	OperationVaultUnlock = "vault.unlock"
	OperationVaultLock   = "vault.lock"
	OperationVaultRekey  = "vault.rekey"
	OperationVaultCreate = "vault.create"

	// The control socket: a request that names no known operation, is malformed, too large or too slow
	// is logged as OperationControlRequest; a peer turned away as OperationControlRefuse.
	OperationControlRequest = "control.request"
	OperationControlRefuse  = "control.refuse"
	OperationControlStatus  = "control.status"
	OperationVaultStatus    = "vault.status"
	OperationSessionOpen    = "session.open"
	OperationSessionEnd     = "session.end"

	// OperationHostChange is a change of the host that root made with a registered action. Its entry names
	// the human behind sudo as the client and the action in Host, never an argument.
	OperationHostChange = "host.change"
)

// knownOperations is every action Validate accepts, so a new one is added here.
var knownOperations = map[string]bool{
	OperationConfigApply: true, OperationConfigRead: true,
	OperationVaultUnlock: true, OperationVaultLock: true, OperationVaultRekey: true, OperationVaultCreate: true,
	OperationControlRequest: true, OperationControlRefuse: true, OperationControlStatus: true,
	OperationVaultStatus: true, OperationSessionOpen: true, OperationSessionEnd: true,
	OperationHostChange: true,
}

// KnownOperation reports whether action is one of the Operation constants.
func KnownOperation(action string) bool { return knownOperations[action] }
