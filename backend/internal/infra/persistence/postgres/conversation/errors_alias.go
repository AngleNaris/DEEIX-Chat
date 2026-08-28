package conversation

import (
	"errors"

	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
)

var (
	ErrFileNotFound         = repository.ErrNotFound
	ErrStorageQuotaExceeded = errors.New("storage quota exceeded")
)
