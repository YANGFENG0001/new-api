package userbatch

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPermissionForActionCoversEveryAction(t *testing.T) {
	cases := map[string]authz.Permission{
		ActionEnable:                 authz.UserBatchExecute,
		ActionDisable:                authz.UserBatchExecute,
		ActionGroup:                  authz.UserBatchExecute,
		ActionBlock:                  authz.UserBatchExecute,
		ActionUnblock:                authz.UserBatchExecute,
		ActionAddQuota:               authz.UserBatchQuota,
		ActionSubtractQuota:          authz.UserBatchQuota,
		ActionOverrideQuota:          authz.UserBatchQuota,
		ActionDelete:                 authz.UserBatchDelete,
		ActionPromote:                authz.UserBatchRole,
		ActionDemote:                 authz.UserBatchRole,
		ActionReset2FA:               authz.UserBatchSecurity,
		ActionResetPasskey:           authz.UserBatchSecurity,
		ActionClearBindings:          authz.UserBatchSecurity,
		ActionInvalidateSubscription: authz.UserBatchSubscription,
		ActionDeleteSubscription:     authz.UserBatchSubscription,
	}
	for action, want := range cases {
		got, ok := PermissionForAction(action)
		require.True(t, ok, "action %q must be mapped", action)
		assert.Equal(t, want, got, "action %q", action)
	}
	_, ok := PermissionForAction("drop_database")
	assert.False(t, ok)
}

func TestCanOperateRejectsSelfRootAndPeers(t *testing.T) {
	admin := Actor{UserID: 10, Role: common.RoleAdminUser}

	self := &model.User{Id: 10, Role: common.RoleAdminUser}
	assert.Error(t, canOperate(admin, self))

	root := &model.User{Id: 1, Role: common.RoleRootUser}
	assert.Error(t, canOperate(admin, root))

	peer := &model.User{Id: 11, Role: common.RoleAdminUser}
	assert.Error(t, canOperate(admin, peer))

	higher := &model.User{Id: 12, Role: common.RoleRootUser}
	assert.Error(t, canOperate(admin, higher))

	assert.Error(t, canOperate(admin, nil))

	lower := &model.User{Id: 13, Role: common.RoleCommonUser}
	assert.NoError(t, canOperate(admin, lower))
}

func TestCanOperateRootCanManageAdminsButNotItself(t *testing.T) {
	root := Actor{UserID: 1, Role: common.RoleRootUser}
	assert.Error(t, canOperate(root, &model.User{Id: 1, Role: common.RoleRootUser}))
	assert.Error(t, canOperate(root, &model.User{Id: 2, Role: common.RoleRootUser}))
	assert.NoError(t, canOperate(root, &model.User{Id: 3, Role: common.RoleAdminUser}))
	assert.NoError(t, canOperate(root, &model.User{Id: 4, Role: common.RoleCommonUser}))
}

func TestNormalizeRequestDeduplicatesAndSortsUsers(t *testing.T) {
	req := Request{
		Action:  " ADD_QUOTA ",
		UserIDs: []int{9, 3, 9, 5},
		Quota:   100,
		Reason:  "  monthly top-up  ",
	}
	require.NoError(t, normalizeRequest(&req))
	assert.Equal(t, ActionAddQuota, req.Action)
	assert.Equal(t, []int{3, 5, 9}, req.UserIDs)
	assert.Equal(t, "monthly top-up", req.Reason)
}

func TestNormalizeRequestValidatesActionSpecificFields(t *testing.T) {
	base := func() Request { return Request{Action: ActionEnable, UserIDs: []int{1}} }

	missingGroup := base()
	missingGroup.Action = ActionGroup
	assert.True(t, errors.Is(normalizeRequest(&missingGroup), ErrInvalidRequest))

	zeroQuota := base()
	zeroQuota.Action = ActionAddQuota
	zeroQuota.Quota = 0
	assert.True(t, errors.Is(normalizeRequest(&zeroQuota), ErrInvalidRequest))

	negativeOverride := base()
	negativeOverride.Action = ActionOverrideQuota
	negativeOverride.Quota = -1
	assert.True(t, errors.Is(normalizeRequest(&negativeOverride), ErrInvalidRequest))

	noUsers := base()
	noUsers.UserIDs = nil
	assert.True(t, errors.Is(normalizeRequest(&noUsers), ErrInvalidRequest))

	unknown := base()
	unknown.Action = "nope"
	assert.True(t, errors.Is(normalizeRequest(&unknown), ErrInvalidRequest))
}

func TestBatchHashIgnoresUserOrderButTracksParameters(t *testing.T) {
	hashOf := func(req Request) string {
		require.NoError(t, normalizeRequest(&req))
		return BatchHash(req)
	}

	first := hashOf(Request{Action: ActionAddQuota, UserIDs: []int{1, 2, 3}, Quota: 10})
	second := hashOf(Request{Action: ActionAddQuota, UserIDs: []int{3, 1, 2}, Quota: 10})
	assert.Equal(t, first, second)

	changedQuota := hashOf(Request{Action: ActionAddQuota, UserIDs: []int{1, 2, 3}, Quota: 11})
	assert.NotEqual(t, first, changedQuota)

	otherAction := hashOf(Request{Action: ActionSubtractQuota, UserIDs: []int{1, 2, 3}, Quota: 10})
	assert.NotEqual(t, first, otherAction)
}
