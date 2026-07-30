package api

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/safeio"
	"github.com/pokerjest/animateAutoTool/internal/service"
)

type backupPasswordRequest struct {
	Password        string `json:"password" form:"password"`
	PasswordConfirm string `json:"password_confirm" form:"password_confirm"`
	AdminPassword   string `json:"admin_password" form:"admin_password"`
}

// resolveBackupArchivePassword is used when creating a new archive. The
// default mode verifies the current administrator password before reusing the
// exact input as the archive password. Custom passwords are never persisted.
func resolveBackupArchivePassword(c *gin.Context, request backupPasswordRequest) (string, error) {
	if request.Password != "" {
		if len(request.Password) < 8 {
			return "", errors.New("自定义备份密码至少需要 8 个字符")
		}
		if request.PasswordConfirm == "" {
			return "", errors.New("请再次输入自定义备份密码")
		}
		if request.PasswordConfirm != request.Password {
			return "", errors.New("两次输入的自定义备份密码不一致")
		}
		return request.Password, nil
	}

	if request.AdminPassword == "" {
		return "", errors.New("请输入当前管理员登录密码，或设置自定义备份密码")
	}
	user, err := currentSessionUser(c)
	if err != nil || user == nil {
		return "", errors.New("当前登录状态已失效")
	}
	if _, err := service.NewAuthService().Login(user.Username, request.AdminPassword); err != nil {
		return "", errors.New("管理员登录密码不正确")
	}
	return request.AdminPassword, nil
}

// resolveBackupRestorePassword accepts the password that was used when the
// archive was created. Restore does not require the password to match the
// current administrator account because a custom password may have been used.
// The archive authentication check is authoritative.
func resolveBackupRestorePassword(request backupPasswordRequest) (string, error) {
	switch {
	case request.Password != "":
		return request.Password, nil
	case request.AdminPassword != "":
		return request.AdminPassword, nil
	default:
		return "", errors.New("请输入备份压缩包密码")
	}
}

func backupPasswordRequestFromForm(c *gin.Context) backupPasswordRequest {
	return backupPasswordRequest{
		Password:        c.PostForm("password"),
		PasswordConfirm: c.PostForm("password_confirm"),
		AdminPassword:   c.PostForm("admin_password"),
	}
}

// prepareBackupDatabase turns an encrypted ZIP into an app-owned temporary
// SQLite file. Legacy raw SQLite backups remain readable for compatibility.
// The caller owns the returned path.
func prepareBackupDatabase(sourcePath, password string) (string, error) {
	if isValidSQLite(sourcePath) {
		return sourcePath, nil
	}
	if !service.IsBackupArchive(sourcePath) {
		return "", errors.New("无效的数据库备份文件")
	}

	tempFile, err := os.CreateTemp("", "restore_decrypted_*.db")
	if err != nil {
		return "", fmt.Errorf("创建解包临时文件: %w", err)
	}
	databasePath := tempFile.Name()
	if err := tempFile.Close(); err != nil {
		safeio.Remove(databasePath)
		return "", fmt.Errorf("完成解包临时文件: %w", err)
	}

	if _, err := service.ExtractEncryptedBackupArchive(sourcePath, password, databasePath); err != nil {
		safeio.Remove(databasePath)
		return "", err
	}
	return databasePath, nil
}

func inspectBackupForRestore(sourcePath, password string) (service.BackupDescriptor, string, error) {
	databasePath, err := prepareBackupDatabase(sourcePath, password)
	if err != nil {
		safeio.Remove(sourcePath)
		return service.BackupDescriptor{}, "", err
	}
	if databasePath != sourcePath {
		safeio.Remove(sourcePath)
	}

	stats, err := service.InspectBackup(databasePath)
	if err != nil {
		safeio.Remove(databasePath)
		return service.BackupDescriptor{}, "", err
	}
	return stats, databasePath, nil
}

func backupArchiveErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, service.ErrBackupArchivePassword):
		return "备份密码不正确"
	case errors.Is(err, service.ErrBackupArchiveFormat):
		return "备份压缩包格式无效或不受支持"
	case strings.Contains(strings.ToLower(err.Error()), "invalid sqlite backup file"):
		return "无效的数据库备份文件"
	default:
		return strings.TrimSpace(err.Error())
	}
}
