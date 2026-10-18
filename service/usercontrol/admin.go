package usercontrol

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

var (
	ErrNotFound          = errors.New("user control resource not found")
	ErrPolicyInUse       = errors.New("policy is still referenced")
	ErrPolicyDisabled    = errors.New("policy is disabled")
	ErrInvalidOverride   = errors.New("invalid user override")
	ErrSelfTarget        = errors.New("cannot control your own account")
	ErrRoleTarget        = errors.New("cannot control a user with the same or higher role")
	ErrRootTarget        = errors.New("the super administrator cannot be controlled")
	ErrTargetNotFound    = errors.New("control target does not exist")
	ErrOverrideNotExists = errors.New("user has no override")
)

// PolicyPayload is the create/update payload for a policy template.
type PolicyPayload struct {
	SchemaVersion int             `json:"schema_version"`
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	Enabled       *bool           `json:"enabled"`
	Priority      int             `json:"priority"`
	Limits        json.RawMessage `json:"limits"`
	Schedule      json.RawMessage `json:"schedule"`
}

// PolicyView adds parsed values so the UI can edit a policy without
// re-implementing the JSON schema.
type PolicyView struct {
	Policy   model.UserControlPolicy `json:"policy"`
	Limits   Limits                  `json:"limits"`
	Schedule *Schedule               `json:"schedule,omitempty"`
}

// ListPayload is the create/update payload for a control list.
type ListPayload struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	PolicyId    int    `json:"policy_id"`
	Enabled     *bool  `json:"enabled"`
}

// OverridePayload is the create/update payload for a per-user override.
type OverridePayload struct {
	PolicyId  int             `json:"policy_id"`
	Limits    json.RawMessage `json:"limits"`
	Schedule  json.RawMessage `json:"schedule"`
	Mode      string          `json:"mode"`
	ExpiresAt int64           `json:"expires_at"`
	Reason    string          `json:"reason"`
}

// SourceDetail explains one policy that contributed to an effective result.
type SourceDetail struct {
	PoolID       int      `json:"pool_id,omitempty"`
	Source       string   `json:"source"`
	SourceID     int      `json:"source_id"`
	PolicyID     int      `json:"policy_id"`
	PolicyName   string   `json:"policy_name"`
	Priority     int      `json:"priority"`
	Active       bool     `json:"active"`
	ScheduleHits []string `json:"schedule_hits,omitempty"`
	Limits       Limits   `json:"limits"`
}

// EffectiveView is the resolved policy plus a per-source explanation.
type EffectiveView struct {
	UserID       int            `json:"user_id"`
	Role         int            `json:"role"`
	ResolvedAt   string         `json:"resolved_at"`
	Timezone     string         `json:"timezone"`
	HasPolicy    bool           `json:"has_policy"`
	Sources      []string       `json:"sources"`
	PolicyIDs    []int          `json:"policy_ids"`
	ScheduleHits []string       `json:"schedule_hits"`
	Limits       Limits         `json:"limits"`
	Details      []SourceDetail `json:"details"`
}

// ApplyResult reports the outcome for one user in a bulk apply.
type ApplyResult struct {
	UserID   int    `json:"user_id"`
	Username string `json:"username"`
	Status   string `json:"status"`
	Message  string `json:"message,omitempty"`
}

func validateName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	if len(name) > 64 {
		return "", fmt.Errorf("%w: name must be at most 64 characters", ErrInvalidInput)
	}
	return name, nil
}

func trimDescription(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 255 {
		value = value[:255]
	}
	return value
}

// BuildPolicy validates a create payload and returns a persistable policy.
func BuildPolicy(payload PolicyPayload, actorID int) (*model.UserControlPolicy, error) {
	name, err := validateName(payload.Name)
	if err != nil {
		return nil, err
	}
	limits, limitsJSON, err := ParseLimitsJSON(payload.Limits)
	if err != nil {
		return nil, err
	}
	schedule, scheduleJSON, err := ParseScheduleJSON(payload.Schedule)
	if err != nil {
		return nil, err
	}
	if payload.SchemaVersion == 2 && (limits.RandomPool != nil || schedule != nil) {
		return nil, fmt.Errorf("%w: behavior policies cannot contain scheduling", ErrInvalidInput)
	}
	if err := ValidatePoolReferences(limits, schedule, 0); err != nil {
		return nil, err
	}
	enabled := true
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}
	now := common.GetTimestamp()
	return &model.UserControlPolicy{
		SchemaVersion: payload.SchemaVersion,
		Name:          name,
		Description:   trimDescription(payload.Description),
		Enabled:       enabled,
		Priority:      payload.Priority,
		Limits:        limitsJSON,
		Schedule:      scheduleJSON,
		Version:       1,
		CreatedBy:     actorID,
		UpdatedBy:     actorID,
		CreatedAt:     now,
		UpdatedAt:     now,
	}, nil
}

// ApplyPolicyPayload validates an update payload in place.
func ApplyPolicyPayload(policy *model.UserControlPolicy, payload PolicyPayload, actorID int) error {
	name, err := validateName(payload.Name)
	if err != nil {
		return err
	}
	limits, limitsJSON, err := ParseLimitsJSON(payload.Limits)
	if err != nil {
		return err
	}
	schedule, scheduleJSON, err := ParseScheduleJSON(payload.Schedule)
	if err != nil {
		return err
	}
	if policy.SchemaVersion == 2 || payload.SchemaVersion == 2 {
		if limits.RandomPool != nil || schedule != nil {
			return fmt.Errorf("%w: behavior policies cannot contain scheduling", ErrInvalidInput)
		}
		policy.SchemaVersion = 2
	}
	if err := ValidatePoolReferences(limits, schedule, policy.Id); err != nil {
		return err
	}
	enabled := policy.Enabled
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}
	if used, err := ApplicationReferences("policy", policy.Id); err != nil {
		return err
	} else if used && (schedule != nil || limits.RandomPool != nil) {
		return fmt.Errorf("%w: referenced behavior policy cannot become scheduled", ErrInvalidInput)
	}
	policy.Name = name
	policy.Description = trimDescription(payload.Description)
	policy.Enabled = enabled
	policy.Priority = payload.Priority
	policy.Limits = limitsJSON
	policy.Schedule = scheduleJSON
	policy.UpdatedBy = actorID
	return nil
}

// ParsePolicyView converts stored JSON into a typed view.
func ParsePolicyView(policy *model.UserControlPolicy) (PolicyView, error) {
	limits, err := ParseLimits(policy.Limits)
	if err != nil {
		return PolicyView{}, err
	}
	schedule, err := ParseSchedule(policy.Schedule)
	if err != nil {
		return PolicyView{}, err
	}
	return PolicyView{Policy: *policy, Limits: limits, Schedule: schedule}, nil
}

// BuildList validates a control-list payload.
func BuildList(payload ListPayload, actorID int) (*model.UserControlList, error) {
	name, err := validateName(payload.Name)
	if err != nil {
		return nil, err
	}
	if payload.PolicyId > 0 {
		if _, err := model.GetUserControlPolicy(payload.PolicyId); err != nil {
			return nil, fmt.Errorf("%w: policy does not exist", ErrInvalidInput)
		}
	}
	enabled := true
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}
	now := common.GetTimestamp()
	return &model.UserControlList{
		Name:        name,
		Description: trimDescription(payload.Description),
		PolicyId:    payload.PolicyId,
		Enabled:     enabled,
		CreatedBy:   actorID,
		UpdatedBy:   actorID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// ApplyListPayload validates an update payload in place.
func ApplyListPayload(list *model.UserControlList, payload ListPayload, actorID int) error {
	name, err := validateName(payload.Name)
	if err != nil {
		return err
	}
	if payload.PolicyId > 0 {
		if _, err := model.GetUserControlPolicy(payload.PolicyId); err != nil {
			return fmt.Errorf("%w: policy does not exist", ErrInvalidInput)
		}
	}
	enabled := list.Enabled
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}
	list.Name = name
	list.Description = trimDescription(payload.Description)
	list.PolicyId = payload.PolicyId
	list.Enabled = enabled
	list.UpdatedBy = actorID
	return nil
}

// BuildOverride validates a per-user override payload.
func BuildOverride(userID int, payload OverridePayload, actorID int) (*model.UserControlUserOverride, error) {
	if userID <= 0 {
		return nil, fmt.Errorf("%w: invalid user id", ErrInvalidInput)
	}
	mode := strings.ToLower(strings.TrimSpace(payload.Mode))
	if mode == "" {
		mode = "merge"
	}
	if mode != "merge" && mode != "replace" {
		return nil, fmt.Errorf("%w: mode must be merge or replace", ErrInvalidInput)
	}
	limits, limitsJSON, err := ParseLimitsJSON(payload.Limits)
	if err != nil {
		return nil, err
	}
	schedule, scheduleJSON, err := ParseScheduleJSON(payload.Schedule)
	if err != nil {
		return nil, err
	}
	if err := ValidatePoolReferences(limits, schedule, 0); err != nil {
		return nil, err
	}
	if limits.Empty() && scheduleJSON == "" && payload.PolicyId == 0 {
		return nil, fmt.Errorf("%w: provide a policy, limits or a schedule", ErrInvalidInput)
	}
	if payload.PolicyId > 0 {
		policy, err := model.GetUserControlPolicy(payload.PolicyId)
		if err != nil {
			return nil, fmt.Errorf("%w: policy does not exist", ErrInvalidInput)
		}
		if !policy.Enabled {
			return nil, ErrPolicyDisabled
		}
	}
	expires := payload.ExpiresAt
	if expires < 0 {
		return nil, fmt.Errorf("%w: expires_at cannot be negative", ErrInvalidInput)
	}
	if expires > 0 && expires <= common.GetTimestamp() {
		return nil, fmt.Errorf("%w: expires_at must be in the future", ErrInvalidInput)
	}
	reason := strings.TrimSpace(payload.Reason)
	if len(reason) > 255 {
		reason = reason[:255]
	}
	now := common.GetTimestamp()
	return &model.UserControlUserOverride{
		UserId:    userID,
		PolicyId:  payload.PolicyId,
		Limits:    limitsJSON,
		Schedule:  scheduleJSON,
		Mode:      mode,
		ExpiresAt: expires,
		Reason:    reason,
		CreatedBy: actorID,
		UpdatedBy: actorID,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// ExplainAt resolves a user's effective limits and explains every source.
func ExplainAt(userID, role int, at time.Time) (*EffectiveView, error) {
	if at.IsZero() {
		at = time.Now()
	}
	effective, err := ResolveAt(userID, role, at)
	if err != nil {
		return nil, err
	}
	view := &EffectiveView{
		UserID:       userID,
		Role:         role,
		ResolvedAt:   at.Format(time.RFC3339),
		Timezone:     effective.Timezone,
		HasPolicy:    effective.HasPolicy,
		Sources:      effective.Sources,
		PolicyIDs:    effective.PolicyIDs,
		ScheduleHits: effective.ScheduleHits,
		Limits:       effective.Limits,
		Details:      []SourceDetail{},
	}
	if role == common.RoleRootUser {
		return view, nil
	}
	sources, err := model.UserControlSourcesForUser(userID, at.Unix())
	if err != nil {
		return nil, err
	}
	for _, source := range sources {
		active, hits, _, _, policy, err := resolveSource(source, userID, at)
		if err != nil {
			return nil, err
		}

		detail := SourceDetail{
			Source:       source.Source,
			SourceID:     source.SourceId,
			PolicyID:     policy.Id,
			PolicyName:   policy.Name,
			Priority:     source.Policy.Priority,
			Active:       !active.Empty(),
			ScheduleHits: hits,
			Limits:       active,
		}
		if source.Pool != nil {
			detail.PoolID = source.Pool.Id
			detail.Priority = source.Pool.Priority
			if detail.PolicyName == "" {
				detail.PolicyName = source.Pool.Name
			}
		}
		view.Details = append(view.Details, detail)
	}
	return view, nil
}

func CheckControlTarget(actorID, actorRole, userID int) error {
	if userID == actorID {
		return ErrSelfTarget
	}
	user, err := model.GetUserById(userID, false)
	if err != nil {
		return fmt.Errorf("%w: user %d", ErrTargetNotFound, userID)
	}
	if user.Role >= common.RoleRootUser {
		return ErrRootTarget
	}
	if actorRole < common.RoleRootUser && user.Role >= actorRole {
		return ErrRoleTarget
	}
	return nil
}

// ApplyToUsers installs one policy as a per-user override for every target.
func ApplyToUsers(actorID, actorRole, policyID int, userIDs []int, mode string, expiresAt int64, reason string) ([]ApplyResult, error) {
	policy, err := model.GetUserControlPolicy(policyID)
	if err != nil {
		return nil, fmt.Errorf("%w: policy does not exist", ErrInvalidInput)
	}
	if !policy.Enabled {
		return nil, ErrPolicyDisabled
	}
	if !BehaviorOnly(*policy) {
		return nil, fmt.Errorf("%w: select a behavior-only policy", ErrInvalidInput)
	}
	if strings.EqualFold(strings.TrimSpace(mode), "replace") && strings.TrimSpace(reason) == "" {
		return nil, fmt.Errorf("%w: replacement requires a reason", ErrInvalidInput)
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "merge"
	}
	if mode != "merge" && mode != "replace" {
		return nil, fmt.Errorf("%w: mode must be merge or replace", ErrInvalidInput)
	}
	ids := normalizeIDs(userIDs)
	if len(ids) == 0 || len(ids) > 5000 {
		return nil, fmt.Errorf("%w: user_ids must contain 1..5000 ids", ErrInvalidInput)
	}
	if expiresAt < 0 {
		return nil, fmt.Errorf("%w: expires_at cannot be negative", ErrInvalidInput)
	}
	if expiresAt > 0 && expiresAt <= common.GetTimestamp() {
		return nil, fmt.Errorf("%w: expires_at must be in the future", ErrInvalidInput)
	}
	results := make([]ApplyResult, 0, len(ids))
	for _, userID := range ids {
		result := ApplyResult{UserID: userID, Status: "applied"}
		user, err := model.GetUserById(userID, false)
		if err != nil {
			result.Status, result.Message = "skipped", "user does not exist"
			results = append(results, result)
			continue
		}
		result.Username = user.Username
		if err := CheckControlTarget(actorID, actorRole, userID); err != nil {
			result.Status, result.Message = "skipped", err.Error()
			results = append(results, result)
			continue
		}
		var application model.UserControlApplication
		query := model.DB.Where("owner_kind = ? AND owner_id = ?", "user", userID).Find(&application)
		if query.Error != nil {
			result.Status, result.Message = "failed", query.Error.Error()
			results = append(results, result)
			continue
		}
		if application.Id == 0 || application.UseLegacy {
			var legacy int64
			if err := model.DB.Model(&model.UserControlUserOverride{}).Where("user_id = ?", userID).Count(&legacy).Error; err != nil {
				result.Status, result.Message = "failed", err.Error()
				results = append(results, result)
				continue
			}
			if legacy > 0 {
				result.Status, result.Message = "failed", "Migrate legacy settings before appending or removing bindings"
				results = append(results, result)
				continue
			}
		}
		var targets []model.UserControlTarget
		if application.Targets != "" {
			if err := common.UnmarshalJsonStr(application.Targets, &targets); err != nil {
				result.Status, result.Message = "failed", err.Error()
				results = append(results, result)
				continue
			}
		}
		found := false
		for _, target := range targets {
			if target.Kind == "policy" && target.Id == policyID {
				found = true
			}
		}
		if !found {
			targets = append(targets, model.UserControlTarget{Kind: "policy", Id: policyID})
		}
		raw, err := common.Marshal(targets)
		if err != nil {
			return nil, err
		}
		expected := application.Version
		// The legacy compatibility endpoint only appends. It cannot silently
		// clear inline overrides or downgrade an existing replacement mode.
		if application.Id > 0 && (application.Mode != mode || application.ExpiresAt != expiresAt) {
			result.Status, result.Message = "failed", "Use the personal settings editor to change mode or expiry"
			results = append(results, result)
			continue
		}
		application.OwnerKind = "user"
		application.OwnerId = userID
		application.Targets = string(raw)
		application.Mode = mode
		application.ExpiresAt = expiresAt
		application.Reason = trimDescription(reason)
		application.UpdatedBy = actorID
		application.CreatedBy = actorID
		if err := model.SaveUserControlApplicationVersion(&application, expected); err != nil {
			result.Status, result.Message = "failed", err.Error()
		}

		results = append(results, result)
	}
	model.BumpUserControlEpoch()
	Invalidate()
	return results, nil
}

func normalizeIDs(ids []int) []int {
	seen := make(map[int]struct{}, len(ids))
	out := make([]int, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Ints(out)
	return out
}
