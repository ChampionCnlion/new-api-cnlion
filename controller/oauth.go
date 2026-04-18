package controller

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const pendingOAuthRegistrationSessionKey = "pending_oauth_registration"

type pendingOAuthRegistration struct {
	Provider       string `json:"provider"`
	ProviderUserID string `json:"provider_user_id"`
	Username       string `json:"username,omitempty"`
	DisplayName    string `json:"display_name,omitempty"`
	Email          string `json:"email,omitempty"`
}

func newPendingOAuthRegistration(providerName string, oauthUser *oauth.OAuthUser) *pendingOAuthRegistration {
	return &pendingOAuthRegistration{
		Provider:       providerName,
		ProviderUserID: oauthUser.ProviderUserID,
		Username:       oauthUser.Username,
		DisplayName:    oauthUser.DisplayName,
		Email:          oauthUser.Email,
	}
}

func (p *pendingOAuthRegistration) ToOAuthUser() *oauth.OAuthUser {
	return &oauth.OAuthUser{
		ProviderUserID: p.ProviderUserID,
		Username:       p.Username,
		DisplayName:    p.DisplayName,
		Email:          p.Email,
	}
}

type completeOAuthRegistrationRequest struct {
	InviteCode string `json:"invite_code"`
}

// providerParams returns map with Provider key for i18n templates
func providerParams(name string) map[string]any {
	return map[string]any{"Provider": name}
}

// GenerateOAuthCode generates a state code for OAuth CSRF protection
func GenerateOAuthCode(c *gin.Context) {
	session := sessions.Default(c)
	state := common.GetRandomString(12)
	affCode := c.Query("aff")
	if affCode != "" {
		session.Set("aff", affCode)
	} else {
		session.Delete("aff")
	}
	inviteCode := c.Query("invite_code")
	if inviteCode != "" {
		session.Set("invite_code", inviteCode)
	} else {
		session.Delete("invite_code")
	}
	session.Delete(pendingOAuthRegistrationSessionKey)
	session.Set("oauth_state", state)
	err := session.Save()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    state,
	})
}

// HandleOAuth handles OAuth callback for all standard OAuth providers
func HandleOAuth(c *gin.Context) {
	providerName := c.Param("provider")
	provider := oauth.GetProvider(providerName)
	if provider == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": i18n.T(c, i18n.MsgOAuthUnknownProvider),
		})
		return
	}

	session := sessions.Default(c)

	// 1. Validate state (CSRF protection)
	state := c.Query("state")
	if state == "" || session.Get("oauth_state") == nil || state != session.Get("oauth_state").(string) {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": i18n.T(c, i18n.MsgOAuthStateInvalid),
		})
		return
	}

	// 2. Check if user is already logged in (bind flow)
	username := session.Get("username")
	if username != nil {
		handleOAuthBind(c, provider)
		return
	}

	// 3. Check if provider is enabled
	if !provider.IsEnabled() {
		common.ApiErrorI18n(c, i18n.MsgOAuthNotEnabled, providerParams(provider.GetName()))
		return
	}

	// 4. Handle error from provider
	errorCode := c.Query("error")
	if errorCode != "" {
		errorDescription := c.Query("error_description")
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": errorDescription,
		})
		return
	}

	// 5. Exchange code for token
	code := c.Query("code")
	token, err := provider.ExchangeToken(c.Request.Context(), code, c)
	if err != nil {
		handleOAuthError(c, err)
		return
	}

	// 6. Get user info
	oauthUser, err := provider.GetUserInfo(c.Request.Context(), token)
	if err != nil {
		handleOAuthError(c, err)
		return
	}

	// 7. Find or create user
	user, err := findOrCreateOAuthUser(providerName, provider, oauthUser, session)
	if err != nil {
		switch e := err.(type) {
		case *OAuthInviteCodeRequiredError:
			common.ApiSuccess(c, gin.H{
				"action":   "require_invite",
				"provider": e.Provider,
			})
			return
		case *OAuthUserDeletedError:
			common.ApiErrorI18n(c, i18n.MsgOAuthUserDeleted)
		case *OAuthRegistrationDisabledError:
			common.ApiErrorI18n(c, i18n.MsgUserRegisterDisabled)
		default:
			common.ApiError(c, err)
		}
		return
	}

	// 8. Check user status
	if user.Status != common.UserStatusEnabled {
		common.ApiErrorI18n(c, i18n.MsgOAuthUserBanned)
		return
	}

	// 9. Setup login
	clearOAuthRegistrationSession(session)
	setupLogin(user, c)
}

// CompleteOAuthRegistration completes first-time OAuth registration after invite code verification.
func CompleteOAuthRegistration(c *gin.Context) {
	providerName := c.Param("provider")
	provider := oauth.GetProvider(providerName)
	if provider == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": i18n.T(c, i18n.MsgOAuthUnknownProvider),
		})
		return
	}
	if !provider.IsEnabled() {
		common.ApiErrorI18n(c, i18n.MsgOAuthNotEnabled, providerParams(provider.GetName()))
		return
	}

	session := sessions.Default(c)
	pending, err := loadPendingOAuthRegistration(session)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if pending == nil {
		common.ApiErrorMsg(c, "未找到待完成的第三方注册，请重新发起登录")
		return
	}
	if pending.Provider != providerName {
		common.ApiErrorMsg(c, "当前待完成注册的第三方来源不匹配，请重新发起登录")
		return
	}

	request := completeOAuthRegistrationRequest{}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	oauthUser := pending.ToOAuthUser()
	user, err := findExistingOAuthUser(provider, oauthUser)
	if err != nil {
		switch err.(type) {
		case *OAuthUserDeletedError:
			common.ApiErrorI18n(c, i18n.MsgOAuthUserDeleted)
		default:
			common.ApiError(c, err)
		}
		return
	}

	if user == nil {
		if !common.RegisterEnabled {
			common.ApiErrorI18n(c, i18n.MsgUserRegisterDisabled)
			return
		}
		user, err = createOAuthUser(provider, oauthUser, getInviterIdFromSession(session), request.InviteCode)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}

	if user.Status != common.UserStatusEnabled {
		common.ApiErrorI18n(c, i18n.MsgOAuthUserBanned)
		return
	}

	clearOAuthRegistrationSession(session)
	setupLogin(user, c)
}

// handleOAuthBind handles binding OAuth account to existing user
func handleOAuthBind(c *gin.Context, provider oauth.Provider) {
	if !provider.IsEnabled() {
		common.ApiErrorI18n(c, i18n.MsgOAuthNotEnabled, providerParams(provider.GetName()))
		return
	}

	// Exchange code for token
	code := c.Query("code")
	token, err := provider.ExchangeToken(c.Request.Context(), code, c)
	if err != nil {
		handleOAuthError(c, err)
		return
	}

	// Get user info
	oauthUser, err := provider.GetUserInfo(c.Request.Context(), token)
	if err != nil {
		handleOAuthError(c, err)
		return
	}

	// Check if this OAuth account is already bound (check both new ID and legacy ID)
	if provider.IsUserIDTaken(oauthUser.ProviderUserID) {
		common.ApiErrorI18n(c, i18n.MsgOAuthAlreadyBound, providerParams(provider.GetName()))
		return
	}
	// Also check legacy ID to prevent duplicate bindings during migration period
	if legacyID, ok := oauthUser.Extra["legacy_id"].(string); ok && legacyID != "" {
		if provider.IsUserIDTaken(legacyID) {
			common.ApiErrorI18n(c, i18n.MsgOAuthAlreadyBound, providerParams(provider.GetName()))
			return
		}
	}

	// Get current user from session
	session := sessions.Default(c)
	id := session.Get("id")
	user := model.User{Id: id.(int)}
	err = user.FillUserById()
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// Handle binding based on provider type
	if genericProvider, ok := provider.(*oauth.GenericOAuthProvider); ok {
		// Custom provider: use user_oauth_bindings table
		err = model.UpdateUserOAuthBinding(user.Id, genericProvider.GetProviderId(), oauthUser.ProviderUserID)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	} else {
		// Built-in provider: update user record directly
		provider.SetProviderUserID(&user, oauthUser.ProviderUserID)
		err = user.Update(false)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}

	clearOAuthRegistrationSession(session)
	err = session.Save()
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgUserSessionSaveFailed)
		return
	}

	common.ApiSuccessI18n(c, i18n.MsgOAuthBindSuccess, gin.H{
		"action": "bind",
	})
}

// findOrCreateOAuthUser finds existing user or creates new user
func findOrCreateOAuthUser(providerName string, provider oauth.Provider, oauthUser *oauth.OAuthUser, session sessions.Session) (*model.User, error) {
	user, err := findExistingOAuthUser(provider, oauthUser)
	if err != nil || user != nil {
		return user, err
	}

	// User doesn't exist, create new user if registration is enabled
	if !common.RegisterEnabled {
		return nil, &OAuthRegistrationDisabledError{}
	}

	inviteCode := getInviteCodeFromSession(session)
	if common.InviteCodeRegisterEnabled && inviteCode == "" {
		if err := savePendingOAuthRegistration(session, providerName, oauthUser); err != nil {
			return nil, err
		}
		return nil, &OAuthInviteCodeRequiredError{Provider: providerName}
	}

	return createOAuthUser(provider, oauthUser, getInviterIdFromSession(session), inviteCode)
}

func findExistingOAuthUser(provider oauth.Provider, oauthUser *oauth.OAuthUser) (*model.User, error) {
	user := &model.User{}

	// Check if user already exists with new ID
	if provider.IsUserIDTaken(oauthUser.ProviderUserID) {
		err := provider.FillUserByProviderID(user, oauthUser.ProviderUserID)
		if err != nil {
			return nil, err
		}
		// Check if user has been deleted
		if user.Id == 0 {
			return nil, &OAuthUserDeletedError{}
		}
		return user, nil
	}

	// Try to find user with legacy ID (for GitHub migration from login to numeric ID)
	if legacyID, ok := oauthUser.Extra["legacy_id"].(string); ok && legacyID != "" {
		if provider.IsUserIDTaken(legacyID) {
			err := provider.FillUserByProviderID(user, legacyID)
			if err != nil {
				return nil, err
			}
			if user.Id != 0 {
				// Found user with legacy ID, migrate to new ID
				common.SysLog(fmt.Sprintf("[OAuth] Migrating user %d from legacy_id=%s to new_id=%s",
					user.Id, legacyID, oauthUser.ProviderUserID))
				if err := user.UpdateGitHubId(oauthUser.ProviderUserID); err != nil {
					common.SysError(fmt.Sprintf("[OAuth] Failed to migrate user %d: %s", user.Id, err.Error()))
					// Continue with login even if migration fails
				}
				return user, nil
			}
		}
	}

	return nil, nil
}

func createOAuthUser(provider oauth.Provider, oauthUser *oauth.OAuthUser, inviterId int, inviteCode string) (*model.User, error) {
	inviteCode = strings.TrimSpace(inviteCode)
	if common.InviteCodeRegisterEnabled && inviteCode == "" {
		return nil, errors.New("请输入邀请码")
	}

	user := &model.User{}
	// Set up new user
	user.Username = provider.GetProviderPrefix() + strconv.Itoa(model.GetMaxUserId()+1)

	if oauthUser.Username != "" {
		if exists, err := model.CheckUserExistOrDeleted(oauthUser.Username, ""); err == nil && !exists {
			// 防止索引退化
			if len(oauthUser.Username) <= model.UserNameMaxLength {
				user.Username = oauthUser.Username
			}
		}
	}

	if oauthUser.DisplayName != "" {
		user.DisplayName = oauthUser.DisplayName
	} else if oauthUser.Username != "" {
		user.DisplayName = oauthUser.Username
	} else {
		user.DisplayName = provider.GetName() + " User"
	}
	if oauthUser.Email != "" {
		user.Email = oauthUser.Email
	}
	user.Role = common.RoleCommonUser
	user.Status = common.UserStatusEnabled

	// Use transaction to ensure user creation and OAuth binding are atomic
	if genericProvider, ok := provider.(*oauth.GenericOAuthProvider); ok {
		// Custom provider: create user and binding in a transaction
		err := model.DB.Transaction(func(tx *gorm.DB) error {
			// Create user
			if err := user.InsertWithTx(tx, inviterId); err != nil {
				return err
			}
			if err := consumeInviteCodeForRegistration(tx, inviteCode, user.Id); err != nil {
				return err
			}

			// Create OAuth binding
			binding := &model.UserOAuthBinding{
				UserId:         user.Id,
				ProviderId:     genericProvider.GetProviderId(),
				ProviderUserId: oauthUser.ProviderUserID,
			}
			if err := model.CreateUserOAuthBindingWithTx(tx, binding); err != nil {
				return err
			}

			return nil
		})
		if err != nil {
			return nil, err
		}

		// Perform post-transaction tasks (logs, sidebar config, inviter rewards)
		user.FinalizeUserCreation(inviterId)
	} else {
		// Built-in provider: create user and update provider ID in a transaction
		err := model.DB.Transaction(func(tx *gorm.DB) error {
			// Create user
			if err := user.InsertWithTx(tx, inviterId); err != nil {
				return err
			}
			if err := consumeInviteCodeForRegistration(tx, inviteCode, user.Id); err != nil {
				return err
			}

			// Set the provider user ID on the user model and update
			provider.SetProviderUserID(user, oauthUser.ProviderUserID)
			if err := tx.Model(user).Updates(map[string]interface{}{
				"github_id":   user.GitHubId,
				"discord_id":  user.DiscordId,
				"oidc_id":     user.OidcId,
				"linux_do_id": user.LinuxDOId,
				"wechat_id":   user.WeChatId,
				"telegram_id": user.TelegramId,
			}).Error; err != nil {
				return err
			}

			return nil
		})
		if err != nil {
			return nil, err
		}

		// Perform post-transaction tasks
		user.FinalizeUserCreation(inviterId)
	}

	return user, nil
}

func consumeInviteCodeForRegistration(tx *gorm.DB, inviteCode string, userId int) error {
	if !common.InviteCodeRegisterEnabled {
		return nil
	}
	inviteCode = strings.TrimSpace(inviteCode)
	if inviteCode == "" {
		return errors.New("请输入邀请码")
	}
	return model.ConsumeInviteCodeTx(tx, inviteCode, userId)
}

func getInviteCodeFromSession(session sessions.Session) string {
	rawInviteCode := session.Get("invite_code")
	inviteCode, ok := rawInviteCode.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(inviteCode)
}

func getInviterIdFromSession(session sessions.Session) int {
	rawAffCode := session.Get("aff")
	affCode, ok := rawAffCode.(string)
	if !ok || affCode == "" {
		return 0
	}
	inviterId, _ := model.GetUserIdByAffCode(affCode)
	return inviterId
}

func savePendingOAuthRegistration(session sessions.Session, providerName string, oauthUser *oauth.OAuthUser) error {
	payload, err := common.Marshal(newPendingOAuthRegistration(providerName, oauthUser))
	if err != nil {
		return err
	}
	session.Set(pendingOAuthRegistrationSessionKey, string(payload))
	session.Delete("invite_code")
	return session.Save()
}

func loadPendingOAuthRegistration(session sessions.Session) (*pendingOAuthRegistration, error) {
	rawPending := session.Get(pendingOAuthRegistrationSessionKey)
	if rawPending == nil {
		return nil, nil
	}
	pendingStr, ok := rawPending.(string)
	if !ok || pendingStr == "" {
		return nil, errors.New("待完成的第三方注册信息已损坏，请重新发起登录")
	}
	pending := &pendingOAuthRegistration{}
	if err := common.UnmarshalJsonStr(pendingStr, pending); err != nil {
		return nil, errors.New("待完成的第三方注册信息已损坏，请重新发起登录")
	}
	if pending.Provider == "" || pending.ProviderUserID == "" {
		return nil, errors.New("待完成的第三方注册信息不完整，请重新发起登录")
	}
	return pending, nil
}

func clearOAuthRegistrationSession(session sessions.Session) {
	session.Delete("oauth_state")
	session.Delete("invite_code")
	session.Delete("aff")
	session.Delete(pendingOAuthRegistrationSessionKey)
}

// Error types for OAuth
type OAuthUserDeletedError struct{}

func (e *OAuthUserDeletedError) Error() string {
	return "user has been deleted"
}

type OAuthRegistrationDisabledError struct{}

func (e *OAuthRegistrationDisabledError) Error() string {
	return "registration is disabled"
}

type OAuthInviteCodeRequiredError struct {
	Provider string
}

func (e *OAuthInviteCodeRequiredError) Error() string {
	return "invite code is required"
}

// handleOAuthError handles OAuth errors and returns translated message
func handleOAuthError(c *gin.Context, err error) {
	switch e := err.(type) {
	case *oauth.OAuthError:
		if e.Params != nil {
			common.ApiErrorI18n(c, e.MsgKey, e.Params)
		} else {
			common.ApiErrorI18n(c, e.MsgKey)
		}
	case *oauth.AccessDeniedError:
		common.ApiErrorMsg(c, e.Message)
	case *oauth.TrustLevelError:
		common.ApiErrorI18n(c, i18n.MsgOAuthTrustLevelLow)
	default:
		common.ApiError(c, err)
	}
}
