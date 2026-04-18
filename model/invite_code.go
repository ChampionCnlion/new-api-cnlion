package model

import (
	"errors"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type InviteCode struct {
	Id          int            `json:"id"`
	UserId      int            `json:"user_id"`
	Key         string         `json:"key" gorm:"type:varchar(32);uniqueIndex"`
	Status      int            `json:"status" gorm:"type:int;default:1;index"`
	CreatedTime int64          `json:"created_time" gorm:"bigint"`
	UsedTime    int64          `json:"used_time" gorm:"bigint"`
	UsedUserId  int            `json:"used_user_id" gorm:"index"`
	Count       int            `json:"count" gorm:"-:all"`
	DeletedAt   gorm.DeletedAt `gorm:"index"`
}

func GetAllInviteCodes(startIdx int, num int) (inviteCodes []*InviteCode, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	if err = tx.Model(&InviteCode{}).Count(&total).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Order("id desc").Limit(num).Offset(startIdx).Find(&inviteCodes).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return inviteCodes, total, nil
}

func SearchInviteCodes(keyword string, startIdx int, num int) (inviteCodes []*InviteCode, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := tx.Model(&InviteCode{})
	trimmedKeyword := strings.TrimSpace(keyword)
	if id, convErr := strconv.Atoi(trimmedKeyword); convErr == nil {
		query = query.Where("id = ? OR key LIKE ?", id, trimmedKeyword+"%")
	} else {
		query = query.Where("key LIKE ?", trimmedKeyword+"%")
	}

	if err = query.Count(&total).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = query.Order("id desc").Limit(num).Offset(startIdx).Find(&inviteCodes).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return inviteCodes, total, nil
}

func GetInviteCodeById(id int) (*InviteCode, error) {
	if id == 0 {
		return nil, errors.New("id 为空！")
	}
	inviteCode := InviteCode{Id: id}
	err := DB.First(&inviteCode, "id = ?", id).Error
	return &inviteCode, err
}

func (inviteCode *InviteCode) Insert() error {
	return DB.Create(inviteCode).Error
}

func (inviteCode *InviteCode) Update() error {
	return DB.Model(inviteCode).Select("status", "used_time", "used_user_id").Updates(inviteCode).Error
}

func (inviteCode *InviteCode) Delete() error {
	return DB.Delete(inviteCode).Error
}

func DeleteInviteCodeById(id int) error {
	if id == 0 {
		return errors.New("id 为空！")
	}
	inviteCode := InviteCode{Id: id}
	if err := DB.Where(inviteCode).First(&inviteCode).Error; err != nil {
		return err
	}
	return inviteCode.Delete()
}

func ConsumeInviteCodeTx(tx *gorm.DB, key string, userId int) error {
	trimmedKey := strings.TrimSpace(key)
	if trimmedKey == "" {
		return errors.New("邀请码不能为空")
	}
	if userId == 0 {
		return errors.New("无效的 user id")
	}

	inviteCode := &InviteCode{}
	common.RandomSleep()
	if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("key = ?", trimmedKey).First(inviteCode).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("邀请码无效")
		}
		return err
	}
	switch inviteCode.Status {
	case common.InviteCodeStatusDisabled:
		return errors.New("邀请码已禁用")
	case common.InviteCodeStatusUsed:
		return errors.New("邀请码已被使用")
	case common.InviteCodeStatusEnabled:
	default:
		return errors.New("邀请码状态无效")
	}

	inviteCode.Status = common.InviteCodeStatusUsed
	inviteCode.UsedTime = common.GetTimestamp()
	inviteCode.UsedUserId = userId
	return tx.Model(inviteCode).
		Select("status", "used_time", "used_user_id").
		Updates(inviteCode).Error
}
