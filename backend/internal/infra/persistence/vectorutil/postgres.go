package vectorutil

import "gorm.io/gorm"

// ConfigurePostgresCandidateSearch enables filtered HNSW scans for the current transaction.
func ConfigurePostgresCandidateSearch(tx *gorm.DB) error {
	if err := tx.Exec(`SET LOCAL hnsw.iterative_scan = strict_order`).Error; err != nil {
		return err
	}
	return tx.Exec(`SET LOCAL hnsw.ef_search = 100`).Error
}
