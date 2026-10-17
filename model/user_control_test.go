package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecoverStaleUserBatchJobsRefreshesCounts(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&UserBatchJob{}, &UserBatchJobItem{}))
	reset := func() {
		DB.Exec("DELETE FROM user_batch_job_items")
		DB.Exec("DELETE FROM user_batch_jobs")
	}
	reset()
	t.Cleanup(reset)

	stale := common.GetTimestamp() - 3600
	job := UserBatchJob{
		Id:             "job-recover-stale",
		Action:         "add_quota",
		Status:         "running",
		Total:          4,
		IdempotencyKey: "recover-stale",
		CreatedAt:      stale,
	}
	require.NoError(t, DB.Create(&job).Error)
	items := []UserBatchJobItem{
		{JobId: job.Id, UserId: 1, Username: "one", Status: "succeeded"},
		{JobId: job.Id, UserId: 2, Username: "two", Status: "pending"},
		{JobId: job.Id, UserId: 3, Username: "three", Status: "running"},
		{JobId: job.Id, UserId: 4, Username: "four", Status: "failed", ErrorCode: "already_enabled"},
	}
	require.NoError(t, DB.Create(&items).Error)

	fresh := UserBatchJob{
		Id:             "job-recover-fresh",
		Action:         "add_quota",
		Status:         "running",
		Total:          1,
		IdempotencyKey: "recover-fresh",
		CreatedAt:      common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(&fresh).Error)
	require.NoError(t, DB.Create(&UserBatchJobItem{JobId: fresh.Id, UserId: 9, Status: "pending"}).Error)

	recovered, err := RecoverStaleUserBatchJobs(common.GetTimestamp() - 60)
	require.NoError(t, err)
	assert.Equal(t, int64(1), recovered)

	var got UserBatchJob
	require.NoError(t, DB.First(&got, "id = ?", job.Id).Error)
	assert.Equal(t, "failed", got.Status)
	assert.Equal(t, 1, got.Succeeded)
	assert.Equal(t, 3, got.Failed)
	assert.NotZero(t, got.FinishedAt)

	var interrupted []UserBatchJobItem
	require.NoError(t, DB.Where("job_id = ? AND error_code = ?", job.Id, "job_interrupted").Find(&interrupted).Error)
	assert.Len(t, interrupted, 2)

	var stillFresh UserBatchJob
	require.NoError(t, DB.First(&stillFresh, "id = ?", fresh.Id).Error)
	assert.Equal(t, "running", stillFresh.Status)

	var freshItem UserBatchJobItem
	require.NoError(t, DB.First(&freshItem, "job_id = ?", fresh.Id).Error)
	assert.Equal(t, "pending", freshItem.Status)
}
