package authz

const (
	ResourceUserBatch   = "user_batch"
	ResourceUserControl = "user_control"

	ActionExecute       = "execute"
	ActionQuota         = "quota"
	ActionDelete        = "delete"
	ActionRole          = "role"
	ActionSecurity      = "security"
	ActionSubscription  = "subscription"
	ActionListWrite     = "list_write"
	ActionPolicyWrite   = "policy_write"
	ActionOverrideWrite = "override_write"
	ActionOverrideRelax = "override_relax"
	ActionApply         = "apply"
	ActionAuditRead     = "audit_read"
)

var (
	UserBatchRead         = Permission{Resource: ResourceUserBatch, Action: ActionRead}
	UserBatchExecute      = Permission{Resource: ResourceUserBatch, Action: ActionExecute}
	UserBatchQuota        = Permission{Resource: ResourceUserBatch, Action: ActionQuota}
	UserBatchDelete       = Permission{Resource: ResourceUserBatch, Action: ActionDelete}
	UserBatchRole         = Permission{Resource: ResourceUserBatch, Action: ActionRole}
	UserBatchSecurity     = Permission{Resource: ResourceUserBatch, Action: ActionSecurity}
	UserBatchSubscription = Permission{Resource: ResourceUserBatch, Action: ActionSubscription}

	UserControlRead          = Permission{Resource: ResourceUserControl, Action: ActionRead}
	UserControlListWrite     = Permission{Resource: ResourceUserControl, Action: ActionListWrite}
	UserControlPolicyWrite   = Permission{Resource: ResourceUserControl, Action: ActionPolicyWrite}
	UserControlOverrideWrite = Permission{Resource: ResourceUserControl, Action: ActionOverrideWrite}
	UserControlOverrideRelax = Permission{Resource: ResourceUserControl, Action: ActionOverrideRelax}
	UserControlApply         = Permission{Resource: ResourceUserControl, Action: ActionApply}
	UserControlAuditRead     = Permission{Resource: ResourceUserControl, Action: ActionAuditRead}
)

func init() {
	RegisterResource(ResourceDefinition{
		Resource: ResourceUserBatch,
		LabelKey: "User Batch Operations",
		Actions: []ActionDefinition{
			{Action: ActionRead, LabelKey: "View batch jobs", DescriptionKey: "View batch previews, jobs and per-user results."},
			{Action: ActionExecute, LabelKey: "Execute basic batch actions", DescriptionKey: "Enable, disable and regroup users in bulk."},
			{Action: ActionQuota, LabelKey: "Adjust quota in bulk", DescriptionKey: "Add, subtract or override user quota in bulk."},
			{Action: ActionDelete, LabelKey: "Delete users in bulk", DescriptionKey: "Permanently remove users in bulk. Grant with care."},
			{Action: ActionRole, LabelKey: "Change roles in bulk", DescriptionKey: "Promote or demote users in bulk. Grant with care."},
			{Action: ActionSecurity, LabelKey: "Reset security in bulk", DescriptionKey: "Reset 2FA, passkeys or bound identities in bulk."},
			{Action: ActionSubscription, LabelKey: "Manage subscriptions in bulk", DescriptionKey: "Invalidate or delete user subscriptions in bulk."},
		},
	})
	RegisterResource(ResourceDefinition{
		Resource: ResourceUserControl,
		LabelKey: "User Control",
		Actions: []ActionDefinition{
			{Action: ActionRead, LabelKey: "View user control", DescriptionKey: "View policies, lists, overrides and effective limits."},
			{Action: ActionListWrite, LabelKey: "Manage control lists", DescriptionKey: "Create lists and add or remove members."},
			{Action: ActionPolicyWrite, LabelKey: "Manage policy templates", DescriptionKey: "Create and edit limits and time schedules."},
			{Action: ActionOverrideWrite, LabelKey: "Tighten user overrides", DescriptionKey: "Apply per-user limits that can only restrict access."},
			{Action: ActionOverrideRelax, LabelKey: "Relax user overrides", DescriptionKey: "Replace inherited limits with a looser per-user override."},
			{Action: ActionApply, LabelKey: "Apply control policies", DescriptionKey: "Batch apply or remove user control policies."},
			{Action: ActionAuditRead, LabelKey: "Read user control audit", DescriptionKey: "View user control change history."},
		},
	})
}
