package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

type SubscriptionSyncResult struct {
	Preview  bool                            `json:"preview"`
	Accounts []model.SubscriptionSyncAccount `json:"accounts"`
	Changes  []model.SubscriptionSyncChange  `json:"changes"`
}

// Preview performs only the explicitly requested HTTP read. It neither acquires
// a scheduling lease nor saves a catalog, observation, reset or billing row.
func RunSubscriptionSync(ctx context.Context, id, actorID int, preview bool) (*SubscriptionSyncResult, error) {
	token := common.GetUUID()
	var source model.SubscriptionSyncSource
	if preview {
		if err := model.DB.First(&source, id).Error; err != nil {
			return nil, err
		}
	} else {
		claimed, err := model.ClaimSubscriptionSyncSource(id, token, true)
		if err != nil {
			return nil, err
		}
		source = *claimed
	}
	return executeSubscriptionSync(ctx, source, token, actorID, preview)
}

func executeSubscriptionSync(ctx context.Context, source model.SubscriptionSyncSource, token string, actorID int, preview bool) (result *SubscriptionSyncResult, runErr error) {
	result = &SubscriptionSyncResult{Preview: preview, Accounts: []model.SubscriptionSyncAccount{}, Changes: []model.SubscriptionSyncChange{}}
	raw, err := fetchSubscriptionSyncWithAuthentication(ctx, source, actorID, preview)
	if err == nil {
		var mapping model.SubscriptionSyncMapping
		err = common.UnmarshalJsonStr(source.MappingJSON, &mapping)
		if err == nil {
			result.Accounts, err = ParseSubscriptionSyncResponse(raw, mapping, model.GetDBTimestamp())
		}
	}
	if err != nil {
		if !preview {
			retry := int64(0)
			var rateLimit *subscriptionSyncRateLimitError
			if errors.As(err, &rateLimit) {
				retry = rateLimit.RetryAfter
			}
			_ = model.FinishSubscriptionSyncSource(source, token, nil, err.Error(), false, actorID, retry)
		}
		return nil, err
	}
	// A successful fetch is not a successful application. Release the lease and
	// audit a bounded, non-secret failure even if persisted state cannot be read.
	defer func() {
		if !preview && runErr != nil {
			_ = model.FinishSubscriptionSyncSource(source, token, nil, runErr.Error(), false, actorID)
		}
	}()
	var bindings []model.SubscriptionSyncBinding
	if err = model.DB.Where("source_id = ? AND enabled = ?", source.Id, true).Find(&bindings).Error; err != nil {
		return nil, errors.New("Synchronization could not apply pool changes")
	}
	for _, b := range bindings {
		input := model.SubscriptionSyncBindingInput{SubscriptionSyncBinding: b}
		var account *model.SubscriptionSyncAccount
		for i := range result.Accounts {
			if result.Accounts[i].ID == b.AccountID {
				account = &result.Accounts[i]
				break
			}
		}
		if account == nil {
			if !preview {
				if e := model.MarkSubscriptionSyncAccountMissing(source, token, b, actorID); e != nil {
					return nil, errors.New("Synchronization could not apply pool changes")
				}
			}
			result.Changes = append(result.Changes, model.SubscriptionSyncChange{PoolID: b.PoolID, Message: "Bound upstream account was not found"})
			continue
		}
		changes, e := model.ApplySubscriptionSyncAccount(source, token, input, *account, actorID, preview)
		if e != nil {
			if e.Error() == "Synchronization binding changed; retry the request" || e.Error() == "Synchronization configuration changed; retry the request" {
				return nil, e
			}
			return nil, errors.New("Synchronization could not apply pool changes")
		}
		result.Changes = append(result.Changes, changes...)
	}
	if !preview {
		applied := false
		for _, c := range result.Changes {
			if c.Message == "Upstream observation accepted" || c.Message == "Upstream expiry aligned" {
				applied = true
				break
			}
		}
		if err = model.FinishSubscriptionSyncSource(source, token, result.Accounts, "", applied, actorID); err != nil {
			if err.Error() == "Synchronization configuration changed; retry the request" {
				return nil, err
			}
			return nil, errors.New("Synchronization could not apply pool changes")
		}
	}
	return result, nil
}

var subscriptionSyncTaskOnce sync.Once

func StartSubscriptionSyncTask() {
	subscriptionSyncTaskOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			slots := make(chan struct{}, 4)
			for range ticker.C {
				var sources []model.SubscriptionSyncSource
				now := model.GetDBTimestamp()
				if err := model.DB.Select("id").Where("enabled = ? AND next_run <= ? AND lease_until <= ?", true, now, now).Order("next_run").Limit(20).Find(&sources).Error; err != nil {
					continue
				}
				for _, s := range sources {
					select {
					case slots <- struct{}{}:
						go func(id int) {
							defer func() { <-slots }()
							// Claim with the due check before network I/O. A simultaneous manual run
							// or another process must not bypass the source's polling interval.
							ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
							defer cancel()
							if err := runScheduledSubscriptionSync(ctx, id); err != nil && !errors.Is(err, context.Canceled) {
								common.SysError("Subscription synchronization did not complete")
							}
						}(s.Id)
					default:
					}
				}
			}
		}()
	})
}

func runScheduledSubscriptionSync(ctx context.Context, id int) error {
	token := common.GetUUID()
	claimed, err := model.ClaimSubscriptionSyncSource(id, token, false)
	if err != nil {
		return nil
	}
	_, err = executeSubscriptionSync(ctx, *claimed, token, 0, false)
	return err
}
