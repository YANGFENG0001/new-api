package model

import (
	"sort"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Pools own scheduling only. Behavior remains in the referenced policies.
type UserControlPool struct {
	Id          int    `json:"id" gorm:"primaryKey"`
	Name        string `json:"name" gorm:"type:varchar(64);uniqueIndex;not null"`
	Description string `json:"description" gorm:"type:varchar(255)"`
	Enabled     bool   `json:"enabled"`
	Priority    int    `json:"priority"`
	Config      string `json:"config" gorm:"type:text"`
	Version     int64  `json:"version"`
	CreatedBy   int    `json:"created_by"`
	UpdatedBy   int    `json:"updated_by"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

type UserControlTarget struct {
	Kind string `json:"kind"`
	Id   int    `json:"id"`
}

// An empty application is an explicit removal of the owner's legacy binding.
// One row per owner makes switching from the legacy engine atomic and reversible.
type UserControlApplication struct {
	UseLegacy bool   `json:"use_legacy"`
	Version   int64  `json:"version" gorm:"default:1"`
	Id        int    `json:"id" gorm:"primaryKey"`
	OwnerKind string `json:"owner_kind" gorm:"type:varchar(16);uniqueIndex:idx_uc_owner,priority:1;not null"`
	OwnerId   int    `json:"owner_id" gorm:"uniqueIndex:idx_uc_owner,priority:2;not null"`
	Targets   string `json:"targets" gorm:"type:text"`
	Mode      string `json:"mode" gorm:"type:varchar(16)"`
	ExpiresAt int64  `json:"expires_at"`
	Reason    string `json:"reason" gorm:"type:varchar(255)"`
	CreatedBy int    `json:"created_by"`
	UpdatedBy int    `json:"updated_by"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

func ListUserControlApplications() ([]UserControlApplication, error) {
	rows := []UserControlApplication{}
	err := DB.Order("id ASC").Find(&rows).Error
	return rows, err
}

func ListUserControlPools() ([]UserControlPool, error) {
	rows := []UserControlPool{}
	err := DB.Order("priority DESC, id ASC").Find(&rows).Error
	return rows, err
}

func SaveUserControlPool(row *UserControlPool) error {
	row.UpdatedAt = common.GetTimestamp()
	if row.Id == 0 {
		row.CreatedAt = row.UpdatedAt
		row.Version = 1
	}
	var err error
	if row.Id == 0 {
		err = DB.Create(row).Error
	} else {
		result := DB.Model(&UserControlPool{}).Where("id = ? AND version = ?", row.Id, row.Version).Updates(map[string]any{"name": row.Name, "description": row.Description, "enabled": row.Enabled, "priority": row.Priority, "config": row.Config, "updated_by": row.UpdatedBy, "updated_at": row.UpdatedAt, "version": gorm.Expr("version + 1")})
		err = result.Error
		if err == nil && result.RowsAffected != 1 {
			err = gorm.ErrInvalidTransaction
		}
		row.Version++
	}
	if err == nil {
		BumpUserControlEpoch()
	}
	return err
}

// ExpandApplications replaces legacy sources only for owners with an explicit
// application. Legacy records are kept intact as a rollback backup.
func ExpandUserControlApplications(userID int, now int64, sources []UserControlSource) ([]UserControlSource, error) {
	var apps []UserControlApplication
	err := DB.Where("(owner_kind = ? AND owner_id = ?) OR (owner_kind = ? AND owner_id IN (?)) OR (owner_kind = ? AND owner_id IN (?))",
		"user", userID, "list", DB.Model(&UserControlListMember{}).Select("list_id").Where("user_id = ?", userID),
		"plan", DB.Model(&UserSubscription{}).Select("plan_id").Where("user_id = ? AND status = ? AND start_time <= ? AND end_time > ?", userID, "active", now, now)).Order("owner_kind ASC, id ASC").Find(&apps).Error
	if err != nil {
		return nil, err
	}
	for _, app := range apps {
		if app.UseLegacy {
			continue
		}
		if app.OwnerKind == "list" {
			var list UserControlList
			if err := DB.First(&list, app.OwnerId).Error; err != nil {
				return nil, err
			}
			if !list.Enabled {
				continue
			}
		}
		kept := sources[:0]
		for _, source := range sources {
			remove := (app.OwnerKind == "user" && source.Source == "override") ||
				(app.OwnerKind == "list" && source.Source == "list" && source.OwnerId == app.OwnerId) ||
				(app.OwnerKind == "plan" && source.Source == "subscription" && source.OwnerId == app.OwnerId)
			if !remove {
				kept = append(kept, source)
			}
		}
		sources = kept
		// Expired personal settings restore inheritance, not the obsolete override.
		if app.ExpiresAt > 0 && app.ExpiresAt <= now {
			continue
		}
		var targets []UserControlTarget
		if err := common.UnmarshalJsonStr(app.Targets, &targets); err != nil {
			return nil, err
		}
		replace := app.OwnerKind == "user" && app.Mode == "replace"
		// A reset sentinel clears inheritance even when all selected pools miss.
		if replace {
			sources = append(sources, UserControlSource{Source: "override_reset", SourceId: app.Id, OwnerId: app.OwnerId, Replace: true})
		}
		for _, target := range targets {
			source := UserControlSource{Source: "application_" + app.OwnerKind, SourceId: app.Id, OwnerId: app.OwnerId}
			switch target.Kind {
			case "policy":
				if err := DB.First(&source.Policy, target.Id).Error; err != nil {
					return nil, err
				}
				if !source.Policy.Enabled {
					continue
				}
			case "pool":
				source.Pool = &UserControlPool{}
				if err := DB.First(source.Pool, target.Id).Error; err != nil {
					return nil, err
				}
				if !source.Pool.Enabled {
					continue
				}
			default:
				return nil, gorm.ErrInvalidData
			}
			sources = append(sources, source)
		}
	}
	// Personal overrides always run after inherited applications, including
	// during mixed-version migrations.
	sort.SliceStable(sources, func(i, j int) bool {
		rank := func(s UserControlSource) int {
			if s.Source == "override" || s.Source == "override_reset" || s.Source == "application_user" {
				return 1
			}
			return 0
		}
		return rank(sources[i]) < rank(sources[j])
	})
	return sources, nil
}

// SaveUserControlApplicationVersion prevents concurrent admins or retries from
// replacing a binding list based on a stale preview. Expected zero means create.
func SaveUserControlApplicationVersion(row *UserControlApplication, expected int64) error {
	row.UpdatedAt = common.GetTimestamp()
	if expected == 0 {
		row.Version = 1
		row.CreatedAt = row.UpdatedAt
		result := DB.Clauses(clause.OnConflict{DoNothing: true}).Create(row)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrInvalidTransaction
		}
	} else {
		result := DB.Model(&UserControlApplication{}).Where("owner_kind = ? AND owner_id = ? AND version = ?", row.OwnerKind, row.OwnerId, expected).Updates(map[string]any{"targets": row.Targets, "mode": row.Mode, "expires_at": row.ExpiresAt, "reason": row.Reason, "updated_by": row.UpdatedBy, "updated_at": row.UpdatedAt, "version": expected + 1, "use_legacy": false})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrInvalidTransaction
		}
		row.Version = expected + 1
		row.UseLegacy = false
	}
	BumpUserControlEpoch()
	return nil
}
