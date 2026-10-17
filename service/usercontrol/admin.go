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
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Enabled     *bool           `json:"enabled"`
	Priority    int             `json:"priority"`
	Limits      json.RawMessage `json:"limits"`
	Schedule    json.RawMessage `json:"schedule"`
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
	_, limitsJSON, err := ParseLimitsJSON(payload.Limits)
	if err != nil {
		return nil, err
	}
	_, scheduleJSON, err := ParseScheduleJSON(payload.Schedule)
	if err != nil {
		return nil, err
	}
	enabled := true
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}
	now := common.GetTimestamp()
	return &model.UserControlPolicy{
		Name:        name,
		Description: trimDescription(payload.Description),
		Enabled:     enabled,
		Priority:    payload.Priority,
		Limits:      limitsJSON,
		Schedule:    scheduleJSON,
		Version:     1,
		CreatedBy:   actorID,
		UpdatedBy:   actorID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// ApplyPolicyPayload validates an update payload in place.
func ApplyPolicyPayload(policy *model.UserControlPolicy, payload PolicyPayload, actorID int) error {
	name, err := validateName(payload.Name)
	if err != nil {
		return err
	}
	_, limitsJSON, err := ParseLimitsJSON(payload.Limits)
	if err != nil {
		return err
	}
	_, scheduleJSON, err := ParseScheduleJSON(payload.Schedule)
	if err != nil {
		return err
	}
	enabled := policy.Enabled
	if payload.Enabled != nil {
		enabled = *payload.Enabled
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
	_, scheduleJSON, err := ParseScheduleJSON(payload.Schedule)
	if err != nil {
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
		base, err := ParseLimits(source.Policy.Limits)
		if err != nil {
			return nil, err
		}
		schedule, err := ParseSchedule(source.Policy.Schedule)
		if err != nil {
			return nil, err
		}
		active, hits, _, err := schedule.ActiveLimits(at, base)
		if err != nil {
			return nil, err
		}
		detail := SourceDetail{
			Source:       source.Source,
			SourceID:     source.SourceId,
			PolicyID:     source.Policy.Id,
			PolicyName:   source.Policy.Name,
			Priority:     source.Policy.Priority,
			Active:       !active.Empty() || (schedule == nil && !base.Empty()),
			ScheduleHits: hits,
			Limits:       active,
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
		override, err := model.GetUserControlOverride(userID)
		if err != nil {
			override = nil
		}
		now := common.GetTimestamp()
		if override == nil {
			override = &model.UserControlUserOverride{
				UserId: userID, CreatedBy: actorID, CreatedAt: now,
			}
		}
		override.PolicyId = policyID
		override.Limits = ""
		override.Schedule = ""
		override.Mode = mode
		override.ExpiresAt = expiresAt
		override.Reason = trimDescription(reason)
		override.UpdatedBy = actorID
		override.UpdatedAt = now
		if err := model.UpsertUserControlOverride(override); err != nil {
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
