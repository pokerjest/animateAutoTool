package service

import (
	"errors"
	"log"
	"strings"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/bootstrap"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/security"
	"github.com/pokerjest/animateAutoTool/internal/store"
	"golang.org/x/crypto/bcrypt"
)

type AuthService struct{}

var timeNow = time.Now

func NewAuthService() *AuthService {
	return &AuthService{}
}

func userStore() *store.UserStore {
	if db.DB == nil {
		return nil
	}
	return store.NewUserStore(db.DB)
}

// Login verifies username and password
func (s *AuthService) Login(username, password string) (*model.User, error) {
	st := userStore()
	if st == nil {
		return nil, errors.New("invalid username or password")
	}
	user, err := st.GetByUsername(username)
	if err != nil {
		return nil, errors.New("invalid username or password")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, errors.New("invalid username or password")
	}

	return user, nil
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

	st := userStore()
	if st == nil {
		return nil, errors.New("invalid database")
	}
	if err := st.Create(&user); err != nil {
		return nil, err
	}

	return &user, nil
}

// EnsureDefaultUser creates a default admin user if no users exist
func (s *AuthService) EnsureDefaultUser() {
	s.removeLegacyRecoveryAccount()

	st := userStore()
	if st == nil {
		return
	}
	count, err := st.Count()
	if err != nil {
		log.Printf("Failed to count users: %v", err)
		return
	}
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
			log.Printf("Bootstrap admin created successfully. Username: admin")
		}
	}
}

func (s *AuthService) removeLegacyRecoveryAccount() {
	st := userStore()
	if st == nil {
		return
	}
	if err := st.DeleteByUsername("backup_admin"); err != nil {
		log.Printf("Failed to remove legacy recovery account: %v", err)
	}
}

// ChangePassword updates the password for a given user
func (s *AuthService) ChangePassword(userID uint, oldPassword, newPassword string) error {
	st := userStore()
	if st == nil {
		return errors.New("user not found")
	}
	user, err := st.GetByID(userID)
	if err != nil {
		return errors.New("user not found")
	}

	// Verify old password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(oldPassword)); err != nil {
		return errors.New("incorrect old password")
	}

	return s.updatePassword(user, newPassword)
}

func (s *AuthService) SetPassword(userID uint, newPassword string) error {
	st := userStore()
	if st == nil {
		return errors.New("user not found")
	}
	user, err := st.GetByID(userID)
	if err != nil {
		return errors.New("user not found")
	}

	return s.updatePassword(user, newPassword)
}

func (s *AuthService) ResetPasswordByUsername(username, newPassword string) error {
	username = strings.TrimSpace(username)
	if username == "" {
		return errors.New("user not found")
	}

	st := userStore()
	if st == nil {
		return errors.New("user not found")
	}
	user, err := st.GetByUsername(username)
	if err != nil {
		return errors.New("user not found")
	}

	return s.updatePassword(user, newPassword)
}

func (s *AuthService) updatePassword(user *model.User, newPassword string) error {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	user.PasswordHash = string(hashedPassword)
	st := userStore()
	if st == nil {
		return errors.New("invalid database")
	}
	if err := st.Save(user); err != nil {
		return err
	}

	if info, err := bootstrap.LoadAdminBootstrapInfo(); err == nil && info.Username == user.Username {
		if err := bootstrap.ClearAdminBootstrapInfo(); err != nil {
			log.Printf("Failed to clear bootstrap admin info: %v", err)
		}
	}

	return nil
}
