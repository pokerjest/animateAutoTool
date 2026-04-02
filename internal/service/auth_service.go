package service

import (
	"errors"
	"log"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/bootstrap"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/security"
	"golang.org/x/crypto/bcrypt"
)

type AuthService struct{}

var timeNow = time.Now

func NewAuthService() *AuthService {
	return &AuthService{}
}

// Login verifies username and password
func (s *AuthService) Login(username, password string) (*model.User, error) {
	var user model.User
	if err := db.DB.Where("username = ?", username).First(&user).Error; err != nil {
		return nil, errors.New("invalid username or password")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, errors.New("invalid username or password")
	}

	return &user, nil
}

// CreateUser registers a new user (internal use mostly)
func (s *AuthService) CreateUser(username, password string) (*model.User, error) {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	user := model.User{
		Username:     username,
		PasswordHash: string(hashedPassword),
	}

	if err := db.DB.Create(&user).Error; err != nil {
		return nil, err
	}

	return &user, nil
}

// EnsureDefaultUser creates a default admin user if no users exist
func (s *AuthService) EnsureDefaultUser() {
	s.removeLegacyRecoveryAccount()

	var count int64
	db.DB.Model(&model.User{}).Count(&count)
	if count == 0 {
		password, err := security.RandomPassword(24)
		if err != nil {
			log.Printf("Failed to generate bootstrap admin password: %v", err)
			return
		}

		log.Println("No users found. Creating bootstrap admin user with a random password.")
		if _, err := s.CreateUser("admin", password); err != nil {
			log.Printf("Failed to create bootstrap admin user: %v", err)
		} else {
			if err := bootstrap.SaveAdminBootstrapInfo(bootstrap.AdminBootstrapInfo{
				Username:  "admin",
				Password:  password,
				CreatedAt: timeNow(),
			}); err != nil {
				log.Printf("Failed to persist bootstrap admin info: %v", err)
			}
			log.Printf("Bootstrap admin created successfully. Username: admin Password: %s", password)
		}
	}
}

func (s *AuthService) removeLegacyRecoveryAccount() {
	if err := db.DB.Where("username = ?", "backup_admin").Delete(&model.User{}).Error; err != nil {
		log.Printf("Failed to remove legacy recovery account: %v", err)
	}
}

// ChangePassword updates the password for a given user
func (s *AuthService) ChangePassword(userID uint, oldPassword, newPassword string) error {
	var user model.User
	if err := db.DB.First(&user, userID).Error; err != nil {
		return errors.New("user not found")
	}

	// Verify old password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(oldPassword)); err != nil {
		return errors.New("incorrect old password")
	}

	// Hash new password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	user.PasswordHash = string(hashedPassword)
	if err := db.DB.Save(&user).Error; err != nil {
		return err
	}

	if info, err := bootstrap.LoadAdminBootstrapInfo(); err == nil && info.Username == user.Username {
		if err := bootstrap.ClearAdminBootstrapInfo(); err != nil {
			log.Printf("Failed to clear bootstrap admin info: %v", err)
		}
	}

	return nil
}
