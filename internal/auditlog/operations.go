package auditlog

// The actions an entry can record, grouped by the area that takes them. An entry names the action and the
// project it applied to, never an argument, a value, or a secret.
const (
	OperationConfigApply   = "config.apply"
	OperationConfigMigrate = "config.migrate"
	OperationConfigRead    = "config.read"

	OperationCredentialCreate = "credential.create"
	OperationCredentialChange = "credential.change"
	OperationCredentialDelete = "credential.delete"
	OperationCredentialUse    = "credential.use"

	OperationApprovalRequest = "approval.request"
	OperationApprovalGrant   = "approval.grant"
	OperationApprovalDeny    = "approval.deny"
	OperationApprovalList    = "approval.list"
	OperationApprovalShow    = "approval.show"

	OperationDeployPlan     = "deploy.plan"
	OperationDeployRun      = "deploy.run"
	OperationDeployRollback = "deploy.rollback"
	OperationDeployRestore  = "deploy.restore"
	// OperationDeployRead is a read of the deploy state; OperationDeployControl a pause, resume, pin or
	// unpin of a project.
	OperationDeployRead    = "deploy.read"
	OperationDeployControl = "deploy.control"

	OperationVaultUnlock = "vault.unlock"
	OperationVaultLock   = "vault.lock"
	OperationVaultRekey  = "vault.rekey"

	OperationWebhookReceive = "webhook.receive"
	OperationWebhookReject  = "webhook.reject"

	// OperationNotifySend is one delivery to a notification channel; Project holds the channel name.
	OperationNotifySend = "notify.send"

	// The control socket: a request that names no known operation, is malformed, too large or too slow
	// is logged as OperationControlRequest; a peer turned away as OperationControlRefuse.
	OperationControlRequest = "control.request"
	OperationControlRefuse  = "control.refuse"
	OperationControlStatus  = "control.status"
	OperationVaultStatus    = "vault.status"
	OperationSessionOpen    = "session.open"
	OperationSessionEnd     = "session.end"
)

// knownOperations is every action Validate accepts, so a new one is added here.
var knownOperations = map[string]bool{
	OperationConfigApply: true, OperationConfigMigrate: true, OperationConfigRead: true,
	OperationCredentialCreate: true, OperationCredentialChange: true, OperationCredentialDelete: true,
	OperationCredentialUse:   true,
	OperationApprovalRequest: true, OperationApprovalGrant: true, OperationApprovalDeny: true,
	OperationApprovalList: true, OperationApprovalShow: true,
	OperationDeployPlan: true, OperationDeployRun: true, OperationDeployRollback: true, OperationDeployRestore: true,
	OperationDeployRead: true, OperationDeployControl: true,
	OperationVaultUnlock: true, OperationVaultLock: true, OperationVaultRekey: true,
	OperationWebhookReceive: true, OperationWebhookReject: true, OperationNotifySend: true,
	OperationControlRequest: true, OperationControlRefuse: true, OperationControlStatus: true,
	OperationVaultStatus: true, OperationSessionOpen: true, OperationSessionEnd: true,
}
