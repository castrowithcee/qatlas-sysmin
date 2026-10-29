package auditlog

// The actions an entry can record, grouped by the area that takes them. An entry names the action and the
// project it applied to, never an argument, a value, or a secret.
const (
	OperationConfigApply   = "config.apply"
	OperationConfigMigrate = "config.migrate"

	OperationCredentialCreate = "credential.create"
	OperationCredentialChange = "credential.change"
	OperationCredentialDelete = "credential.delete"
	OperationCredentialUse    = "credential.use"

	OperationApprovalRequest = "approval.request"
	OperationApprovalGrant   = "approval.grant"
	OperationApprovalDeny    = "approval.deny"

	OperationDeployPlan     = "deploy.plan"
	OperationDeployRun      = "deploy.run"
	OperationDeployRollback = "deploy.rollback"

	OperationVaultUnlock = "vault.unlock"
	OperationVaultLock   = "vault.lock"
	OperationVaultRekey  = "vault.rekey"

	OperationWebhookReceive = "webhook.receive"
	OperationWebhookReject  = "webhook.reject"
)

// knownOperations is every action Validate accepts, so a new one is added here.
var knownOperations = map[string]bool{
	OperationConfigApply: true, OperationConfigMigrate: true,
	OperationCredentialCreate: true, OperationCredentialChange: true, OperationCredentialDelete: true,
	OperationCredentialUse:   true,
	OperationApprovalRequest: true, OperationApprovalGrant: true, OperationApprovalDeny: true,
	OperationDeployPlan: true, OperationDeployRun: true, OperationDeployRollback: true,
	OperationVaultUnlock: true, OperationVaultLock: true, OperationVaultRekey: true,
	OperationWebhookReceive: true, OperationWebhookReject: true,
}
