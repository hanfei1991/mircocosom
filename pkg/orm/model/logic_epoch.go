package model

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	defaultEpochPK  = 1
	defaultMinEpoch = 1
)

// using transaction to generate increasing epoch
type LogicEpoch struct {
	Model
	Epoch int64 `gorm:"type:bigint not null default 1"`
}

func InitializeEpoch(ctx context.Context, db *gorm.DB) error {
	// Do nothing on conflict
	// INSERT INTO `logic_epoches` (`created_at`,`updated_at`,`epoch`,`seq_id`) VALUES
	// ('2022-05-04 14:02:08.624','2022-05-04 14:02:08.624',1,1) ON DUPLICATE KEY UPDATE `seq_id`=`seq_id`
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&LogicEpoch{
		Model: Model{
			SeqID: defaultEpochPK,
		},
		Epoch: defaultMinEpoch,
	}).Error
}

func GenEpoch(ctx context.Context, db *gorm.DB) (int64, error) {
	var epoch int64
	err := db.Transaction(func(tx *gorm.DB) error {
		//(1)update epoch = epoch + 1
		if err := tx.Model(&LogicEpoch{
			Model: Model{
				SeqID: defaultEpochPK,
			},
		}).Update("epoch", gorm.Expr("epoch + ?", 1)).Error; err != nil {
			// return any error will rollback
			return err
		}

		//(2)select epoch
		var logicEp LogicEpoch
		if err := tx.First(&logicEp, defaultEpochPK).Error; err != nil {
			return err
		}
		epoch = logicEp.Epoch

		// return nil will commit the whole transaction
		return nil
	})
	if err != nil {
		return 0, err
	}

	return epoch, nil
}
