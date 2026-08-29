package sale

import (
	"context"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/domain"
)

// PPhFinalAccruer is implemented by tax.GORMRepository.
// Injected into GORMRepository so the Akad atomic transaction also creates
// the PPh Final accrual journal in the same DB transaction.
type PPhFinalAccruer interface {
	AccruePPhFinalInTx(ctx context.Context, tx *gorm.DB, tenantID, unitID, projectID uint64, transferValue domain.Money, recognitionDate time.Time) error
}
