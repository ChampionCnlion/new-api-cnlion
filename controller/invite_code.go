package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func GetAllInviteCodes(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	inviteCodes, total, err := model.GetAllInviteCodes(pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(inviteCodes)
	common.ApiSuccess(c, pageInfo)
}

func SearchInviteCodes(c *gin.Context) {
	keyword := c.Query("keyword")
	pageInfo := common.GetPageQuery(c)
	inviteCodes, total, err := model.SearchInviteCodes(keyword, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(inviteCodes)
	common.ApiSuccess(c, pageInfo)
}

func GetInviteCode(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	inviteCode, err := model.GetInviteCodeById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    inviteCode,
	})
}

func AddInviteCode(c *gin.Context) {
	inviteCode := model.InviteCode{}
	if err := common.DecodeJson(c.Request.Body, &inviteCode); err != nil {
		common.ApiError(c, err)
		return
	}
	if inviteCode.Count <= 0 {
		common.ApiErrorMsg(c, "邀请码数量必须大于 0")
		return
	}
	if inviteCode.Count > 100 {
		common.ApiErrorMsg(c, "一次最多只能生成 100 个邀请码")
		return
	}

	keys := make([]string, 0, inviteCode.Count)
	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		for i := 0; i < inviteCode.Count; i++ {
			cleanInviteCode := model.InviteCode{
				UserId:      c.GetInt("id"),
				Key:         common.GetUUID(),
				Status:      common.InviteCodeStatusEnabled,
				CreatedTime: common.GetTimestamp(),
			}
			if err := tx.Create(&cleanInviteCode).Error; err != nil {
				return err
			}
			keys = append(keys, cleanInviteCode.Key)
		}
		return nil
	}); err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    keys,
	})
}

func DeleteInviteCode(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := model.DeleteInviteCodeById(id); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

func UpdateInviteCode(c *gin.Context) {
	inviteCode := model.InviteCode{}
	if err := common.DecodeJson(c.Request.Body, &inviteCode); err != nil {
		common.ApiError(c, err)
		return
	}
	cleanInviteCode, err := model.GetInviteCodeById(inviteCode.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	switch inviteCode.Status {
	case common.InviteCodeStatusEnabled, common.InviteCodeStatusDisabled:
	default:
		common.ApiErrorMsg(c, "无效的邀请码状态")
		return
	}
	if cleanInviteCode.Status == common.InviteCodeStatusUsed {
		common.ApiErrorMsg(c, "已使用的邀请码不能修改状态")
		return
	}
	cleanInviteCode.Status = inviteCode.Status
	if err := cleanInviteCode.Update(); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    cleanInviteCode,
	})
}
