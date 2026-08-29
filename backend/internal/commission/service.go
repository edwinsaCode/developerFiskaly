package commission

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

// ApprovalGate — seam opt-in Generic Approval Workflow (TargetCommission).
type ApprovalGate interface {
	RequireApproved(ctx context.Context, tenantID, commissionID uint64) error
}

// Service mengorkestrasi rule + entri komisi (pola cancellation/closing).
type Service struct {
	db           *gorm.DB
	approvalGate ApprovalGate
}

func NewService(db *gorm.DB) *Service { return &Service{db: db} }

func (s *Service) SetApprovalGate(g ApprovalGate) { s.approvalGate = g }

// ── Rules ─────────────────────────────────────────────────────────────────────

func (s *Service) CreateRule(ctx context.Context, tenantID uint64, r *Rule) (*Rule, error) {
	if r.Name == "" || !r.Basis.Valid() || !r.TriggerEvent.Valid() || r.EffectiveFrom.IsZero() {
		return nil, ErrRuleInvalid
	}
	if r.Basis == BasisTiered {
		return nil, ErrBasisNotImplemented
	}
	if r.TriggerEvent != TriggerAtBAST {
		return nil, ErrTriggerNotImplemented
	}
	if r.Basis == BasisPercentOfSale && !r.Rate.IsPositive() {
		return nil, fmt.Errorf("%w: rate harus > 0 untuk percent_of_sale", ErrRuleInvalid)
	}
	if r.Basis == BasisFlatPerUnit && (r.FlatAmount.IsZero() || r.FlatAmount.IsNeg() || !r.FlatAmount.IsWholeRupiah()) {
		return nil, fmt.Errorf("%w: flat_amount harus rupiah bulat > 0", ErrRuleInvalid)
	}
	if r.ExpenseAccount == "" {
		r.ExpenseAccount = "5-3100"
	}
	if r.PayableAccount == "" {
		r.PayableAccount = "2-6200"
	}
	r.TenantID = tenantID
	if err := s.db.WithContext(ctx).Create(r).Error; err != nil {
		return nil, fmt.Errorf("simpan rule: %w", err)
	}
	return r, nil
}

func (s *Service) SetRuleActive(ctx context.Context, tenantID, id uint64, active bool) (*Rule, error) {
	res := s.db.WithContext(ctx).Model(&Rule{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Update("is_active", active)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrRuleNotFound
	}
	return s.GetRule(ctx, tenantID, id)
}

func (s *Service) GetRule(ctx context.Context, tenantID, id uint64) (*Rule, error) {
	var r Rule
	err := s.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", id, tenantID).First(&r).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRuleNotFound
		}
		return nil, err
	}
	return &r, nil
}

func (s *Service) ListRules(ctx context.Context, tenantID uint64) ([]*Rule, error) {
	var out []*Rule
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).
		Order("id DESC").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// ── Calculate (sweep idempoten — trigger at_bast) ─────────────────────────────

// eligibleSale: BAST aktif ber-atribusi salesperson (kontrak) yang belum punya
// entri untuk rule terkait (UNIQUE sale×rule = guard kedua).
type eligibleSale struct {
	SaleRecordID  uint64
	ContractID    uint64
	ProjectID     uint64
	UnitID        uint64
	UnitType      string
	SalesPersonID   uint64
	SalePrice       string
	RecognitionDate time.Time
}

// Calculate memindai seluruh BAST non-cancelled ber-salesperson dan menerapkan
// rule aktif yang cocok → entri `calculated`. Idempoten (UNIQUE sale×rule).
func (s *Service) Calculate(ctx context.Context, tenantID uint64, actorID *uint64) (int, error) {
	var rules []*Rule
	if err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND is_active = 1 AND trigger_event = ?", tenantID, string(TriggerAtBAST)).
		Find(&rules).Error; err != nil {
		return 0, fmt.Errorf("baca rules: %w", err)
	}
	if len(rules) == 0 {
		return 0, nil
	}

	var sales []eligibleSale
	err := s.db.WithContext(ctx).Raw(`
		SELECT sr.id AS sale_record_id, sc.id AS contract_id, sr.project_id,
		       sr.unit_id, u.unit_type, sc.sales_person_id, sr.sale_price, sr.recognition_date
		FROM sale_records sr
		JOIN units u ON u.id = sr.unit_id
		JOIN sale_contracts sc ON sc.unit_id = sr.unit_id AND sc.tenant_id = sr.tenant_id
		WHERE sr.tenant_id = ? AND sr.cancelled_at IS NULL
		  AND sc.sales_person_id IS NOT NULL`, tenantID).Scan(&sales).Error
	if err != nil {
		return 0, fmt.Errorf("scan BAST: %w", err)
	}

	created := 0
	now := time.Now()
	for _, sl := range sales {
		price, perr := domain.NewMoney(sl.SalePrice)
		if perr != nil {
			return created, perr
		}
		for _, r := range rules {
			if !r.Matches(sl.SalesPersonID, sl.ProjectID, sl.UnitType, sl.RecognitionDate) {
				continue
			}
			amount, aerr := r.AmountFor(price)
			if aerr != nil || amount.IsZero() {
				continue
			}
			cid := sl.ContractID
			c := &Commission{
				TenantID: tenantID, RuleID: r.ID, SaleRecordID: sl.SaleRecordID,
				SaleContractID: &cid, ProjectID: sl.ProjectID, UnitID: sl.UnitID,
				SalesPersonID: sl.SalesPersonID,
				BasisAmount:   price, RateSnapshot: r.Rate, Amount: amount,
				Status: StatusCalculated, CalculatedAt: &now,
			}
			if err := s.db.WithContext(ctx).Create(c).Error; err != nil {
				if isDup(err) {
					continue // sudah ada (idempoten)
				}
				return created, fmt.Errorf("simpan komisi: %w", err)
			}
			created++
		}
	}
	_ = actorID
	return created, nil
}

func isDup(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for i := 0; i+15 <= len(msg); i++ {
		if msg[i:i+15] == "Duplicate entry" {
			return true
		}
	}
	return errors.Is(err, gorm.ErrDuplicatedKey)
}

// ── Lifecycle ─────────────────────────────────────────────────────────────────

// Approve: calculated → approved (gate opt-in TargetCommission).
func (s *Service) Approve(ctx context.Context, tenantID, id uint64, actorID *uint64) (*Commission, error) {
	if s.approvalGate != nil {
		if err := s.approvalGate.RequireApproved(ctx, tenantID, id); err != nil {
			return nil, err
		}
	}
	now := time.Now()
	res := s.db.WithContext(ctx).Model(&Commission{}).
		Where("id = ? AND tenant_id = ? AND status = ?", id, tenantID, string(StatusCalculated)).
		Updates(map[string]interface{}{"status": string(StatusApproved), "approved_at": &now, "approved_by": actorID})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, s.transitionErr(ctx, tenantID, id, StatusApproved)
	}
	return s.Get(ctx, tenantID, id)
}

// MakePayable: approved → payable + jurnal akrual Dr Beban / Cr Utang Komisi.
func (s *Service) MakePayable(ctx context.Context, tenantID, id uint64, accrualDate time.Time, actorID *uint64) (*Commission, error) {
	var out *Commission
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		c, rule, err := s.lockWithRule(ctx, tx, tenantID, id)
		if err != nil {
			return err
		}
		if c.Status != StatusApproved {
			return fmt.Errorf("%w: %s → payable", ErrInvalidStateTransition, c.Status)
		}
		accs, err := accountIDs(ctx, tx, tenantID, []string{rule.ExpenseAccount, rule.PayableAccount})
		if err != nil {
			return err
		}
		entry, err := postJournal(ctx, tx, tenantID, accrualDate,
			fmt.Sprintf("Akrual komisi #%d (sale %d)", c.ID, c.SaleRecordID),
			fmt.Sprintf("CM-%d-ACCRUE", c.ID), actorID,
			[]ledger.LineInput{
				{AccountID: accs[rule.ExpenseAccount], Debit: c.Amount, ProjectID: &c.ProjectID, UnitID: &c.UnitID, Description: "Beban komisi penjualan"},
				{AccountID: accs[rule.PayableAccount], Credit: c.Amount, ProjectID: &c.ProjectID, UnitID: &c.UnitID, Description: "Utang komisi"},
			})
		if err != nil {
			return err
		}
		now := time.Now()
		res := tx.Model(&Commission{}).
			Where("id = ? AND tenant_id = ? AND status = ?", id, tenantID, string(StatusApproved)).
			Updates(map[string]interface{}{
				"status": string(StatusPayable), "payable_at": &now, "payable_by": actorID,
				"accrual_journal_id": entry,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("%w: berubah bersamaan", ErrInvalidStateTransition)
		}
		c.Status, c.PayableAt, c.PayableBy, c.AccrualJournalID = StatusPayable, &now, actorID, &entry
		out = c
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Pay: payable → paid + jurnal Dr Utang Komisi / Cr Bank.
func (s *Service) Pay(ctx context.Context, tenantID, id uint64, bankAccountCode string, payDate time.Time, actorID *uint64) (*Commission, error) {
	var out *Commission
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		c, rule, err := s.lockWithRule(ctx, tx, tenantID, id)
		if err != nil {
			return err
		}
		if c.Status != StatusPayable {
			return fmt.Errorf("%w: %s → paid", ErrInvalidStateTransition, c.Status)
		}
		bankID, err := cashBankAccountID(ctx, tx, tenantID, bankAccountCode)
		if err != nil {
			return err
		}
		accs, err := accountIDs(ctx, tx, tenantID, []string{rule.PayableAccount})
		if err != nil {
			return err
		}
		// BKK — kas perusahaan sendiri yang keluar (bukan titipan pihak ketiga).
		entry, err := postCashJournal(ctx, tx, tenantID, payDate,
			fmt.Sprintf("Pembayaran komisi #%d", c.ID),
			fmt.Sprintf("CM-%d-PAY", c.ID), actorID,
			[]ledger.LineInput{
				{AccountID: accs[rule.PayableAccount], Debit: c.Amount, ProjectID: &c.ProjectID, UnitID: &c.UnitID, Description: "Pelunasan utang komisi"},
				{AccountID: bankID, Credit: c.Amount, ProjectID: &c.ProjectID, UnitID: &c.UnitID, Description: "Kas keluar komisi"},
			}, ledger.DocumentSpec{TypeCode: ledger.DocCashOut})
		if err != nil {
			return err
		}
		now := time.Now()
		res := tx.Model(&Commission{}).
			Where("id = ? AND tenant_id = ? AND status = ?", id, tenantID, string(StatusPayable)).
			Updates(map[string]interface{}{
				"status": string(StatusPaid), "paid_at": &now, "paid_by": actorID,
				"payment_journal_id": entry, "bank_account_code": bankAccountCode,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("%w: berubah bersamaan", ErrInvalidStateTransition)
		}
		c.Status, c.PaidAt, c.PaidBy, c.PaymentJournalID = StatusPaid, &now, actorID, &entry
		out = c
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Cancel: calculated|approved (tanpa jurnal) atau payable (reversing akrual) →
// cancelled. paid → gunakan Clawback.
func (s *Service) Cancel(ctx context.Context, tenantID, id uint64, reason string, actorID *uint64) (*Commission, error) {
	var out *Commission
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		c, _, err := s.lockWithRule(ctx, tx, tenantID, id)
		if err != nil {
			return err
		}
		if !c.Status.CanTransitionTo(StatusCancelled) {
			return fmt.Errorf("%w: %s → cancelled", ErrInvalidStateTransition, c.Status)
		}
		if c.Status == StatusPayable && c.AccrualJournalID != nil {
			txLedgerRepo := ledger.NewGORMRepository(tx)
			txPosting := ledger.NewPostingService(txLedgerRepo, txLedgerRepo).WithPeriodChecker(txLedgerRepo)
			if _, err := txPosting.Reverse(ctx, tenantID, *c.AccrualJournalID, time.Now()); err != nil {
				return fmt.Errorf("reversing akrual komisi: %w", err)
			}
		}
		now := time.Now()
		res := tx.Model(&Commission{}).
			Where("id = ? AND tenant_id = ? AND status = ?", id, tenantID, string(c.Status)).
			Updates(map[string]interface{}{
				"status": string(StatusCancelled), "cancelled_at": &now, "cancelled_by": actorID,
				"cancel_reason": reason,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("%w: berubah bersamaan", ErrInvalidStateTransition)
		}
		c.Status, c.CancelledAt, c.CancelledBy, c.CancelReason = StatusCancelled, &now, actorID, reason
		out = c
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Clawback: paid → clawed_back + jurnal Dr Piutang Lain-lain / Cr Beban Komisi
// (recovery — sale dibatalkan setelah komisi terlanjur dibayar, blueprint §1↔§3).
func (s *Service) Clawback(ctx context.Context, tenantID, id uint64, reason string, actorID *uint64) (*Commission, error) {
	var out *Commission
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		c, rule, err := s.lockWithRule(ctx, tx, tenantID, id)
		if err != nil {
			return err
		}
		if c.Status != StatusPaid {
			return fmt.Errorf("%w: %s → clawed_back (hanya paid)", ErrInvalidStateTransition, c.Status)
		}
		accs, err := accountIDs(ctx, tx, tenantID, []string{"1-2100", rule.ExpenseAccount})
		if err != nil {
			return err
		}
		entry, err := postJournal(ctx, tx, tenantID, time.Now(),
			fmt.Sprintf("Clawback komisi #%d (sale dibatalkan)", c.ID),
			fmt.Sprintf("CM-%d-CLAWBACK", c.ID), actorID,
			[]ledger.LineInput{
				{AccountID: accs["1-2100"], Debit: c.Amount, ProjectID: &c.ProjectID, UnitID: &c.UnitID, Description: "Piutang clawback komisi dari sales"},
				{AccountID: accs[rule.ExpenseAccount], Credit: c.Amount, ProjectID: &c.ProjectID, UnitID: &c.UnitID, Description: "Pemulihan beban komisi"},
			})
		if err != nil {
			return err
		}
		now := time.Now()
		res := tx.Model(&Commission{}).
			Where("id = ? AND tenant_id = ? AND status = ?", id, tenantID, string(StatusPaid)).
			Updates(map[string]interface{}{
				"status": string(StatusClawedBack), "cancelled_at": &now, "cancelled_by": actorID,
				"cancel_reason": reason, "clawback_journal_id": entry,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("%w: berubah bersamaan", ErrInvalidStateTransition)
		}
		c.Status, c.ClawbackJournalID = StatusClawedBack, &entry
		out = c
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// SyncCancellations — sweep idempoten (pola mark-overdue): komisi milik sale
// yang DIBATALKAN (sale_records.cancelled_at) otomatis di-cancel (belum paid)
// atau di-clawback (sudah paid). Blueprint §1: "Clawback dipicu bila sudah dibayar".
func (s *Service) SyncCancellations(ctx context.Context, tenantID uint64, actorID *uint64) (cancelled, clawed int, err error) {
	var ids []struct {
		ID     uint64
		Status string
	}
	err = s.db.WithContext(ctx).Raw(`
		SELECT c.id, c.status
		FROM commissions c
		JOIN sale_records sr ON sr.id = c.sale_record_id
		WHERE c.tenant_id = ? AND sr.cancelled_at IS NOT NULL
		  AND c.status IN ('calculated','approved','payable','paid')
		ORDER BY c.id`, tenantID).Scan(&ids).Error
	if err != nil {
		return 0, 0, fmt.Errorf("scan komisi sale batal: %w", err)
	}
	const reason = "sale dibatalkan (sync otomatis)"
	var firstErr error
	for _, row := range ids {
		if row.Status == string(StatusPaid) {
			if _, cerr := s.Clawback(ctx, tenantID, row.ID, reason, actorID); cerr != nil {
				if firstErr == nil {
					firstErr = cerr
				}
				continue
			}
			clawed++
		} else {
			if _, cerr := s.Cancel(ctx, tenantID, row.ID, reason, actorID); cerr != nil {
				if errors.Is(cerr, ErrInvalidStateTransition) {
					continue // race: sudah terminal
				}
				if firstErr == nil {
					firstErr = cerr
				}
				continue
			}
			cancelled++
		}
	}
	return cancelled, clawed, firstErr
}

// ── Reads ─────────────────────────────────────────────────────────────────────

func (s *Service) Get(ctx context.Context, tenantID, id uint64) (*Commission, error) {
	var c Commission
	err := s.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", id, tenantID).First(&c).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &c, nil
}

func (s *Service) List(ctx context.Context, tenantID uint64, status Status, salesPersonID uint64) ([]*Commission, error) {
	q := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID)
	if status != "" {
		q = q.Where("status = ?", string(status))
	}
	if salesPersonID != 0 {
		q = q.Where("sales_person_id = ?", salesPersonID)
	}
	var out []*Commission
	if err := q.Order("id DESC").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// ── internals ─────────────────────────────────────────────────────────────────

func (s *Service) lockWithRule(ctx context.Context, tx *gorm.DB, tenantID, id uint64) (*Commission, *Rule, error) {
	var c Commission
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&c).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}
	var r Rule
	if err := tx.Where("id = ? AND tenant_id = ?", c.RuleID, tenantID).First(&r).Error; err != nil {
		return nil, nil, fmt.Errorf("baca rule komisi: %w", err)
	}
	return &c, &r, nil
}

func (s *Service) transitionErr(ctx context.Context, tenantID, id uint64, target Status) error {
	c, err := s.Get(ctx, tenantID, id)
	if err != nil {
		return err
	}
	return fmt.Errorf("%w: %s → %s", ErrInvalidStateTransition, c.Status, target)
}

func accountIDs(ctx context.Context, tx *gorm.DB, tenantID uint64, codes []string) (map[string]uint64, error) {
	var rows []struct {
		ID   uint64
		Code string
	}
	if err := tx.WithContext(ctx).Table("accounts").Select("id, code").
		Where("tenant_id = ? AND code IN ?", tenantID, codes).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := map[string]uint64{}
	for _, r := range rows {
		out[r.Code] = r.ID
	}
	for _, c := range codes {
		if out[c] == 0 {
			return nil, fmt.Errorf("akun %s tidak ditemukan (seed COA/migration)", c)
		}
	}
	return out, nil
}

func cashBankAccountID(ctx context.Context, tx *gorm.DB, tenantID uint64, code string) (uint64, error) {
	var bank struct {
		ID       uint64
		IsActive bool
		Category string
	}
	if err := tx.WithContext(ctx).Table("accounts").Select("id, is_active, category").
		Where("tenant_id = ? AND code = ?", tenantID, code).
		Scan(&bank).Error; err != nil {
		return 0, err
	}
	if bank.ID == 0 || !bank.IsActive || (bank.Category != "cash" && bank.Category != "bank") {
		return 0, ErrBankAccountInvalid
	}
	return bank.ID, nil
}

// postJournal — jurnal komisi yang TIDAK menyentuh kas (akrual, clawback).
func postJournal(ctx context.Context, tx *gorm.DB, tenantID uint64, date time.Time, desc, ref string, actorID *uint64, lines []ledger.LineInput) (uint64, error) {
	return postCashJournal(ctx, tx, tenantID, date, desc, ref, actorID, lines, ledger.DocumentSpec{})
}

// postCashJournal menambahkan penerbitan dokumen kas di transaksi yang sama.
// Create+Post digabung lewat CreateAndPost: dua panggilan terpisah bisa berhenti
// di antaranya dan meninggalkan draft yatim (W-3.0).
func postCashJournal(ctx context.Context, tx *gorm.DB, tenantID uint64, date time.Time, desc, ref string,
	actorID *uint64, lines []ledger.LineInput, spec ledger.DocumentSpec) (uint64, error) {
	txLedgerRepo := ledger.NewGORMRepository(tx)
	txPosting := ledger.NewPostingService(txLedgerRepo, txLedgerRepo).WithPeriodChecker(txLedgerRepo)
	entry, err := txPosting.CreateAndPost(ctx, ledger.CreateJournalRequest{
		TenantID: tenantID, Date: date, Description: desc, Reference: ref,
		Source: "commission", CreatedBy: actorID, Lines: lines, Document: spec,
	})
	if err != nil {
		return 0, fmt.Errorf("posting jurnal: %w", err)
	}
	return entry.ID, nil
}
