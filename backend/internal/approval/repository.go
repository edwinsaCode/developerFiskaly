package approval

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GORMRepository — akses DB tenant-scoped (Invariant #6, predikat manual).
type GORMRepository struct {
	db *gorm.DB
}

func NewGORMRepository(db *gorm.DB) *GORMRepository {
	return &GORMRepository{db: db}
}

func isDuplicateKey(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1062
}

// ── Workflow config ───────────────────────────────────────────────────────────

// CreateWorkflowWithSteps menyimpan workflow + steps atomik (satu transaksi).
func (r *GORMRepository) CreateWorkflowWithSteps(ctx context.Context, w *Workflow, steps []Step) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(w).Error; err != nil {
			if isDuplicateKey(err) {
				return ErrWorkflowActiveExists
			}
			return fmt.Errorf("CreateWorkflow: %w", err)
		}
		for i := range steps {
			steps[i].TenantID = w.TenantID
			steps[i].WorkflowID = w.ID
			if err := tx.Create(&steps[i]).Error; err != nil {
				return fmt.Errorf("CreateStep seq %d: %w", steps[i].Seq, err)
			}
		}
		w.Steps = steps
		return nil
	})
}

// FindActiveWorkflow mengembalikan workflow AKTIF (+steps terurut) untuk sebuah
// target_type, atau ErrNoActiveWorkflow.
func (r *GORMRepository) FindActiveWorkflow(ctx context.Context, tenantID uint64, target TargetType) (*Workflow, error) {
	var w Workflow
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND target_type = ? AND is_active = 1 AND active_key = 'Y'", tenantID, target).
		First(&w).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNoActiveWorkflow
		}
		return nil, fmt.Errorf("FindActiveWorkflow: %w", err)
	}
	if err := r.loadSteps(ctx, &w); err != nil {
		return nil, err
	}
	return &w, nil
}

func (r *GORMRepository) loadSteps(ctx context.Context, w *Workflow) error {
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND workflow_id = ?", w.TenantID, w.ID).
		Order("seq ASC").
		Find(&w.Steps).Error; err != nil {
		return fmt.Errorf("loadSteps: %w", err)
	}
	return nil
}

func (r *GORMRepository) FindWorkflowByID(ctx context.Context, tenantID, id uint64) (*Workflow, error) {
	var w Workflow
	err := r.db.WithContext(ctx).First(&w, "id = ? AND tenant_id = ?", id, tenantID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrWorkflowNotFound
		}
		return nil, fmt.Errorf("FindWorkflowByID: %w", err)
	}
	if err := r.loadSteps(ctx, &w); err != nil {
		return nil, err
	}
	return &w, nil
}

func (r *GORMRepository) ListWorkflows(ctx context.Context, tenantID uint64) ([]*Workflow, error) {
	var out []*Workflow
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("target_type ASC, id DESC").
		Find(&out).Error; err != nil {
		return nil, fmt.Errorf("ListWorkflows: %w", err)
	}
	for _, w := range out {
		if err := r.loadSteps(ctx, w); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// SetWorkflowActive mengaktif/nonaktifkan workflow (active_key pattern).
func (r *GORMRepository) SetWorkflowActive(ctx context.Context, tenantID, id uint64, active bool) error {
	updates := map[string]interface{}{"is_active": active}
	if active {
		updates["active_key"] = "Y"
	} else {
		updates["active_key"] = nil
	}
	res := r.db.WithContext(ctx).Model(&Workflow{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(updates)
	if res.Error != nil {
		if isDuplicateKey(res.Error) {
			return ErrWorkflowActiveExists
		}
		return fmt.Errorf("SetWorkflowActive: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrWorkflowNotFound
	}
	return nil
}

// NextWorkflowRevision: revisi berikutnya per (tenant, target_type) — H3.
func (r *GORMRepository) NextWorkflowRevision(ctx context.Context, tenantID uint64, target TargetType) (int, error) {
	var max *int
	err := r.db.WithContext(ctx).Model(&Workflow{}).
		Select("MAX(revision)").
		Where("tenant_id = ? AND target_type = ?", tenantID, target).
		Scan(&max).Error
	if err != nil {
		return 0, fmt.Errorf("NextWorkflowRevision: %w", err)
	}
	if max == nil {
		return 1, nil
	}
	return *max + 1, nil
}

// ── Request runtime ───────────────────────────────────────────────────────────

func (r *GORMRepository) CreateRequest(ctx context.Context, req *Request) error {
	if err := r.db.WithContext(ctx).Create(req).Error; err != nil {
		if isDuplicateKey(err) {
			return ErrActiveRequestExists
		}
		return fmt.Errorf("CreateRequest: %w", err)
	}
	return nil
}

func (r *GORMRepository) FindRequestByID(ctx context.Context, tenantID, id uint64) (*Request, error) {
	var req Request
	err := r.db.WithContext(ctx).First(&req, "id = ? AND tenant_id = ?", id, tenantID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRequestNotFound
		}
		return nil, fmt.Errorf("FindRequestByID: %w", err)
	}
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND request_id = ?", tenantID, id).
		Order("id ASC").
		Find(&req.Actions).Error; err != nil {
		return nil, fmt.Errorf("load actions: %w", err)
	}
	return &req, nil
}

// ListRequestsByTarget mengembalikan seluruh riwayat request sebuah dokumen.
func (r *GORMRepository) ListRequestsByTarget(ctx context.Context, tenantID uint64, target TargetType, targetID uint64) ([]*Request, error) {
	var out []*Request
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND target_type = ? AND target_id = ?", tenantID, target, targetID).
		Order("id DESC").
		Find(&out).Error; err != nil {
		return nil, fmt.Errorf("ListRequestsByTarget: %w", err)
	}
	return out, nil
}

// HasApprovedRequest: gate konsumen — apakah dokumen punya request APPROVED.
func (r *GORMRepository) HasApprovedRequest(ctx context.Context, tenantID uint64, target TargetType, targetID uint64) (bool, error) {
	var n int64
	if err := r.db.WithContext(ctx).Model(&Request{}).
		Where("tenant_id = ? AND target_type = ? AND target_id = ? AND status = ?",
			tenantID, target, targetID, StatusApproved).
		Count(&n).Error; err != nil {
		return false, fmt.Errorf("HasApprovedRequest: %w", err)
	}
	return n > 0, nil
}

// ── Act (atomik: append action + proyeksi status request dlm satu transaksi) ──

// ApplyAction menulis Action (append-only) dan memperbarui proyeksi Request
// dalam SATU transaksi dengan row lock pada request (mencegah race dua approver).
// evaluate dipanggil DI DALAM transaksi dengan request terkunci + seluruh action
// step berjalan; ia mengembalikan status & seq berikutnya.
func (r *GORMRepository) ApplyAction(
	ctx context.Context,
	tenantID, requestID uint64,
	act *Action,
	evaluate func(req *Request, stepActions []Action) (RequestStatus, int, error),
) (*Request, error) {
	var out *Request
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var req Request
		if err := tx.Clauses(gormLockingClause()).
			First(&req, "id = ? AND tenant_id = ?", requestID, tenantID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrRequestNotFound
			}
			return fmt.Errorf("lock request: %w", err)
		}

		var stepActions []Action
		if err := tx.Where("tenant_id = ? AND request_id = ? AND step_seq = ?",
			tenantID, requestID, req.CurrentSeq).
			Order("id ASC").Find(&stepActions).Error; err != nil {
			return fmt.Errorf("load step actions: %w", err)
		}

		act.TenantID = tenantID
		act.RequestID = requestID
		act.StepSeq = req.CurrentSeq

		newStatus, newSeq, err := evaluate(&req, stepActions)
		if err != nil {
			return err
		}

		if err := tx.Create(act).Error; err != nil {
			return fmt.Errorf("append action: %w", err)
		}

		updates := map[string]interface{}{
			"status":      string(newStatus),
			"current_seq": newSeq,
		}
		if newStatus.Terminal() {
			updates["active_key"] = nil // lepas slot unique → request baru boleh dibuat
			updates["decided_at"] = tx.NowFunc()
		}
		if err := tx.Model(&Request{}).
			Where("id = ? AND tenant_id = ?", requestID, tenantID).
			Updates(updates).Error; err != nil {
			return fmt.Errorf("update request: %w", err)
		}

		req.Status = newStatus
		req.CurrentSeq = newSeq
		out = &req
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// CancelRequest membatalkan request aktif (hanya pemohon; guard di service)
// DAN meninggalkan jejak audit berupa Action decision='cancel' — atomik
// (hardening 5.1: pembatalan tercatat siapa-kapan di trail actions).
func (r *GORMRepository) CancelRequest(ctx context.Context, tenantID, requestID uint64, actorID uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var req Request
		if err := tx.Clauses(gormLockingClause()).
			First(&req, "id = ? AND tenant_id = ?", requestID, tenantID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrRequestNotFound
			}
			return fmt.Errorf("lock request: %w", err)
		}
		if req.Status.Terminal() {
			return ErrRequestTerminal
		}
		if err := tx.Model(&Request{}).
			Where("id = ? AND tenant_id = ?", requestID, tenantID).
			Updates(map[string]interface{}{
				"status":     string(StatusCancelled),
				"active_key": nil,
				"decided_at": tx.NowFunc(),
			}).Error; err != nil {
			return fmt.Errorf("CancelRequest: %w", err)
		}
		return tx.Create(&Action{
			TenantID:  tenantID,
			RequestID: requestID,
			StepSeq:   req.CurrentSeq,
			ActorID:   actorID,
			ActorRole: "requester",
			Decision:  DecisionCancel,
			Comment:   "request dibatalkan pemohon",
		}).Error
	})
}

// gormLockingClause: SELECT ... FOR UPDATE (row lock request saat evaluasi).
func gormLockingClause() clause.Locking {
	return clause.Locking{Strength: "UPDATE"}
}
