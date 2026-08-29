package sale

// Implementasi SchemeFlowStore pada GORMRepository (Increment 3).
// Semua query tenant-scoped manual (Invariant #6).

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"esaproperti/internal/scheme"
)

func (r *GORMRepository) FindSchemeByID(ctx context.Context, tenantID, id uint64) (*scheme.PaymentScheme, error) {
	var m scheme.PaymentScheme
	err := r.db.WithContext(ctx).First(&m, "id = ? AND tenant_id = ?", id, tenantID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, scheme.ErrSchemeNotFound
		}
		return nil, fmt.Errorf("FindSchemeByID: %w", err)
	}
	return &m, nil
}

func (r *GORMRepository) FindFinancingSourceByID(ctx context.Context, tenantID, id uint64) (*scheme.FinancingSource, error) {
	var f scheme.FinancingSource
	err := r.db.WithContext(ctx).First(&f, "id = ? AND tenant_id = ?", id, tenantID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, scheme.ErrFinSourceNotFound
		}
		return nil, fmt.Errorf("FindFinancingSourceByID: %w", err)
	}
	return &f, nil
}

func (r *GORMRepository) UpdateContractSchemeFields(ctx context.Context, tenantID, contractID uint64, state string, financingSourceID *uint64) error {
	updates := map[string]interface{}{"scheme_state": state}
	if financingSourceID != nil {
		updates["financing_source_id"] = *financingSourceID
	}
	res := r.db.WithContext(ctx).Model(&SaleContract{}).
		Where("id = ? AND tenant_id = ?", contractID, tenantID).
		Updates(updates)
	if res.Error != nil {
		return fmt.Errorf("UpdateContractSchemeFields: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrContractNotFound
	}
	return nil
}

func (r *GORMRepository) RebindContractScheme(ctx context.Context, tenantID, contractID, schemeID uint64, snapshot, state string, financingSourceID *uint64) error {
	res := r.db.WithContext(ctx).Model(&SaleContract{}).
		Where("id = ? AND tenant_id = ?", contractID, tenantID).
		Updates(map[string]interface{}{
			"payment_scheme_id":      schemeID,
			"scheme_params_snapshot": snapshot,
			"scheme_state":           state,
			"financing_source_id":    financingSourceID, // nil = hapus (konversi ke non-KPR)
		})
	if res.Error != nil {
		return fmt.Errorf("RebindContractScheme: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrContractNotFound
	}
	return nil
}

// AppendPaymentEvent menulis satu event lifecycle (APPEND-ONLY — tidak ada
// method update/delete untuk tabel ini di seluruh aplikasi).
func (r *GORMRepository) AppendPaymentEvent(ctx context.Context, ev *ContractPaymentEvent) error {
	if err := r.db.WithContext(ctx).Create(ev).Error; err != nil {
		return fmt.Errorf("AppendPaymentEvent: %w", err)
	}
	return nil
}

func (r *GORMRepository) ListPaymentEvents(ctx context.Context, tenantID, contractID uint64) ([]*ContractPaymentEvent, error) {
	var out []*ContractPaymentEvent
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND sale_contract_id = ?", tenantID, contractID).
		Order("id ASC").
		Find(&out).Error; err != nil {
		return nil, fmt.Errorf("ListPaymentEvents: %w", err)
	}
	return out, nil
}

// SupersedeUnpaidSchedules menandai baris jadwal TANPA pembayaran menjadi
// superseded (BS-5). Baris dengan paid_amount > 0 atau status received TIDAK
// disentuh — alokasinya adalah fakta sub-ledger.
func (r *GORMRepository) SupersedeUnpaidSchedules(ctx context.Context, tenantID, contractID uint64) error {
	err := r.db.WithContext(ctx).Model(&PaymentSchedule{}).
		Where("tenant_id = ? AND sale_contract_id = ? AND status IN ? AND paid_amount = 0",
			tenantID, contractID,
			[]ScheduleStatus{ScheduleStatusScheduled, ScheduleStatusOverdue}).
		Update("status", ScheduleStatusSuperseded).Error
	if err != nil {
		return fmt.Errorf("SupersedeUnpaidSchedules: %w", err)
	}
	return nil
}

func (r *GORMRepository) MaxScheduleVersion(ctx context.Context, tenantID, contractID uint64) (int, error) {
	var v *int
	err := r.db.WithContext(ctx).Model(&PaymentSchedule{}).
		Select("MAX(schedule_version)").
		Where("tenant_id = ? AND sale_contract_id = ?", tenantID, contractID).
		Scan(&v).Error
	if err != nil {
		return 0, fmt.Errorf("MaxScheduleVersion: %w", err)
	}
	if v == nil {
		return 0, nil
	}
	return *v, nil
}
