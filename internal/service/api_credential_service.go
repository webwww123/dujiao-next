package service

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/models"
	"github.com/dujiao-next/internal/repository"
	"gorm.io/gorm"
)

var (
	ErrApiCredentialExists       = errors.New("api credential already exists for this user")
	ErrApiCredentialNotFound     = errors.New("api credential not found")
	ErrApiCredentialNotApproved  = errors.New("api credential is not approved")
	ErrApiCredentialPendingExist = errors.New("pending application already exists")
)

// ApiCredentialService API 凭证服务
type ApiCredentialService struct {
	credRepo repository.ApiCredentialRepository
}

// NewApiCredentialService 创建凭证服务
func NewApiCredentialService(credRepo repository.ApiCredentialRepository) *ApiCredentialService {
	return &ApiCredentialService{credRepo: credRepo}
}

// Apply 用户申请 API 对接权限
func (s *ApiCredentialService) Apply(userID uint) (*models.ApiCredential, error) {
	existing, err := s.credRepo.GetAnyByUserID(userID)
	if err != nil {
		return nil, err
	}

	if existing != nil {
		if existing.DeletedAt.Valid {
			if err := approveApiCredential(existing); err != nil {
				return nil, err
			}
			if err := s.credRepo.UpdateAny(existing); err != nil {
				return nil, err
			}
			return existing, nil
		}

		switch existing.Status {
		case constants.ApiCredentialStatusPendingReview:
			if err := approveApiCredential(existing); err != nil {
				return nil, err
			}
			if err := s.credRepo.Update(existing); err != nil {
				return nil, err
			}
			return existing, nil
		case constants.ApiCredentialStatusApproved:
			return nil, ErrApiCredentialExists
		case constants.ApiCredentialStatusRejected:
			// 允许重新申请，并直接自动通过。
			if err := approveApiCredential(existing); err != nil {
				return nil, err
			}
			if err := s.credRepo.Update(existing); err != nil {
				return nil, err
			}
			return existing, nil
		case constants.ApiCredentialStatusDisabled:
			return nil, ErrApiCredentialExists
		}
	}

	cred := &models.ApiCredential{
		UserID: userID,
	}
	if err := approveApiCredential(cred); err != nil {
		return nil, err
	}
	if err := s.credRepo.Create(cred); err != nil {
		return nil, err
	}
	return cred, nil
}

func approveApiCredential(cred *models.ApiCredential) error {
	apiKey, err := generateRandomHex(32)
	if err != nil {
		return err
	}
	apiSecret, err := generateRandomHex(64)
	if err != nil {
		return err
	}
	now := time.Now()
	cred.ApiKey = apiKey
	cred.ApiSecret = apiSecret
	cred.Status = constants.ApiCredentialStatusApproved
	cred.RejectReason = ""
	cred.ApprovedAt = &now
	cred.LastUsedAt = nil
	cred.IsActive = true
	cred.DeletedAt = gorm.DeletedAt{}
	return nil
}

// Approve admin 审核通过
func (s *ApiCredentialService) Approve(id uint) (*models.ApiCredential, string, error) {
	cred, err := s.credRepo.GetByID(id)
	if err != nil {
		return nil, "", err
	}
	if cred == nil {
		return nil, "", ErrApiCredentialNotFound
	}

	if err := approveApiCredential(cred); err != nil {
		return nil, "", err
	}
	apiSecret := cred.ApiSecret

	if err := s.credRepo.Update(cred); err != nil {
		return nil, "", err
	}

	return cred, apiSecret, nil
}

// Reject admin 审核拒绝
func (s *ApiCredentialService) Reject(id uint, reason string) error {
	cred, err := s.credRepo.GetByID(id)
	if err != nil {
		return err
	}
	if cred == nil {
		return ErrApiCredentialNotFound
	}

	cred.Status = constants.ApiCredentialStatusRejected
	cred.RejectReason = reason
	return s.credRepo.Update(cred)
}

// SetActive 启用/禁用
func (s *ApiCredentialService) SetActive(id uint, active bool) error {
	cred, err := s.credRepo.GetByID(id)
	if err != nil {
		return err
	}
	if cred == nil {
		return ErrApiCredentialNotFound
	}
	if cred.Status != constants.ApiCredentialStatusApproved {
		return ErrApiCredentialNotApproved
	}

	cred.IsActive = active
	return s.credRepo.Update(cred)
}

// SetActiveByUserID 用户自行启用/禁用
func (s *ApiCredentialService) SetActiveByUserID(userID uint, active bool) error {
	cred, err := s.credRepo.GetByUserID(userID)
	if err != nil {
		return err
	}
	if cred == nil {
		return ErrApiCredentialNotFound
	}
	if cred.Status != constants.ApiCredentialStatusApproved {
		return ErrApiCredentialNotApproved
	}

	cred.IsActive = active
	return s.credRepo.Update(cred)
}

// Regenerate 重新生成 Secret
func (s *ApiCredentialService) Regenerate(id uint) (string, error) {
	cred, err := s.credRepo.GetByID(id)
	if err != nil {
		return "", err
	}
	if cred == nil {
		return "", ErrApiCredentialNotFound
	}
	if cred.Status != constants.ApiCredentialStatusApproved {
		return "", ErrApiCredentialNotApproved
	}

	newSecret, err := generateRandomHex(64)
	if err != nil {
		return "", err
	}

	cred.ApiSecret = newSecret
	if err := s.credRepo.Update(cred); err != nil {
		return "", err
	}
	return newSecret, nil
}

// RegenerateByUserID 用户重新生成 Secret
func (s *ApiCredentialService) RegenerateByUserID(userID uint) (string, error) {
	cred, err := s.credRepo.GetByUserID(userID)
	if err != nil {
		return "", err
	}
	if cred == nil {
		return "", ErrApiCredentialNotFound
	}
	return s.Regenerate(cred.ID)
}

// GetByUserID 获取用户的凭证
func (s *ApiCredentialService) GetByUserID(userID uint) (*models.ApiCredential, error) {
	return s.credRepo.GetByUserID(userID)
}

// GetByID 根据 ID 获取凭证
func (s *ApiCredentialService) GetByID(id uint) (*models.ApiCredential, error) {
	return s.credRepo.GetByID(id)
}

// List 列表查询
func (s *ApiCredentialService) List(filter repository.ApiCredentialListFilter) ([]models.ApiCredential, int64, error) {
	return s.credRepo.List(filter)
}

// Delete 删除凭证
func (s *ApiCredentialService) Delete(id uint) error {
	return s.credRepo.Delete(id)
}

func generateRandomHex(byteLen int) (string, error) {
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
