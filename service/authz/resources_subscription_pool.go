package authz

const ResourceSubscriptionPool = "subscription_pool"

func SubscriptionPoolPermission(action string) Permission {
	return Permission{Resource: ResourceSubscriptionPool, Action: action}
}

func init() {
	RegisterResource(ResourceDefinition{Resource: ResourceSubscriptionPool, LabelKey: "Upstream subscription pools", Actions: []ActionDefinition{
		{Action: "sync_read", LabelKey: "View upstream synchronization", DescriptionKey: "View upstream synchronization"},
		{Action: "sync_run", LabelKey: "Run upstream synchronization", DescriptionKey: "Run upstream synchronization"},
		{Action: "sync_credentials", LabelKey: "Manage synchronization connections", DescriptionKey: "Manage synchronization connections"},
		{Action: "sync_binding", LabelKey: "Manage synchronization bindings", DescriptionKey: "Manage synchronization bindings"},
		{Action: "sync_apply", LabelKey: "Apply upstream time and quota controls", DescriptionKey: "Apply upstream time and quota controls"},
		{Action: "read", LabelKey: "View upstream pools", DescriptionKey: "View upstream pools"},
		{Action: "manage", LabelKey: "Manage upstream pools", DescriptionKey: "Manage upstream pools"},
		{Action: "assign", LabelKey: "Assign subscription pools", DescriptionKey: "Assign subscription pools"},
		{Action: "calibrate", LabelKey: "Calibrate upstream usage", DescriptionKey: "Calibrate upstream usage"},
		{Action: "shared_reset", LabelKey: "Reset pool cycles", DescriptionKey: "Reset pool cycles"},
		{Action: "personal_reset", LabelKey: "Reset independent cycles", DescriptionKey: "Reset independent cycles"},
		{Action: "batch_migrate", LabelKey: "Move subscriptions in bulk", DescriptionKey: "Move subscriptions in bulk"},
		{Action: "audit_read", LabelKey: "View pool operations", DescriptionKey: "View pool operations"},
	}})
}
