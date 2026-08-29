package cancellation

// Refund engine — pembayaran keluar Hutang Refund 2-2200.
// Sumber: (a) cancellation processed (payable sudah dikredit di settlement);
// (b) booking fee pending_refund (Increment 7) → reklas Dr 2-2100 / Cr 2-2200
// saat refund dibuat. Pembayaran: Dr 2-2200 / Cr Bank.

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

// CreateBookingRefund membuat dokumen refund dari booking pending_refund +
// reklas titipan → hutang refund (satu tx).
func (s *Service) CreateBookingRefund(ctx context.Context, tenantID, bookingID uint64, actorID *uint64) (*Refund, error) {
	var out *Refund
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var b struct {
			ID             uint64
			ProjectID      uint64
			UnitID         uint64
			BookingFee     string
			Status         string
			FeeDisposition string
			CustomerID     uint64
		}
		if err := tx.Table("bookings").
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id, project_id, unit_id, booking_fee, status, fee_disposition, customer_id").
			Where("id = ? AND tenant_id = ?", bookingID, tenantID).
			Scan(&b).Error; err != nil {
			return fmt.Errorf("baca booking: %w", err)
		}
		if b.ID == 0 {
			return ErrRefundNotFound
		}
		if b.FeeDisposition != "pending_refund" {
			return ErrBookingNotRefundable
		}
		var existing int64
		if err := tx.Table("refunds").
			Where("tenant_id = ? AND booking_id = ? AND status <> 'cancelled'", tenantID, bookingID).
			Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			return ErrRefundAlreadyRequested
		}
		amount, err := domain.NewMoney(b.BookingFee)
		if err != nil {
			return err
		}
		if !amount.GreaterThan(domain.Zero) {
			return ErrNothingToRefund
		}

		accs, err := (&Service{db: tx}).accountsByCode(ctx, tenantID, []string{accTitipanBooking, accHutangRefund})
		if err != nil {
			return err
		}

		// Reklas: Dr Titipan Booking / Cr Hutang Refund.
		txLedgerRepo := ledger.NewGORMRepository(tx)
		txPosting := ledger.NewPostingService(txLedgerRepo, txLedgerRepo).WithPeriodChecker(txLedgerRepo)
		pid, uid := b.ProjectID, b.UnitID
		entry, err := txPosting.Create(ctx, ledger.CreateJournalRequest{
			TenantID: tenantID, Date: time.Now(),
			Description: fmt.Sprintf("Reklas titipan booking #%d → hutang refund", b.ID),
			Reference:   fmt.Sprintf("RF-BOOKING-%d", b.ID),
			Source:      "cancellation", CreatedBy: actorID,
			Lines: []ledger.LineInput{
				{AccountID: accs[accTitipanBooking].ID, Debit: amount, ProjectID: &pid, UnitID: &uid, Description: "Pelepasan titipan booking (refund)"},
				{AccountID: accs[accHutangRefund].ID, Credit: amount, ProjectID: &pid, UnitID: &uid, Description: "Hutang refund booking fee"},
			},
		})
		if err != nil {
			return fmt.Errorf("jurnal reklas refund: %w", err)
		}
		if _, err := txPosting.Post(ctx, tenantID, entry.ID); err != nil {
			return fmt.Errorf("posting reklas refund: %w", err)
		}

		// Payee: nama customer booking.
		var payee string
		tx.Table("customers").Select("name").Where("id = ?", b.CustomerID).Scan(&payee)

		bid := b.ID
		jid := entry.ID
		rf := &Refund{
			TenantID: tenantID, SourceType: RefundFromBooking, BookingID: &bid,
			UnitID: b.UnitID, Payee: payee, Amount: amount, Status: RefundPending,
			PayableJournalID: &jid, CreatedBy: actorID,
		}
		if err := tx.Create(rf).Error; err != nil {
			return fmt.Errorf("buat refund: %w", err)
		}
		out = rf
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// PayRefund membayar refund pending: Dr Hutang Refund / Cr Bank (satu tx).
// Booking-source: fee_disposition → 'refunded' (disposisi final).
func (s *Service) PayRefund(ctx context.Context, tenantID, refundID uint64, bankAccountCode string, payDate time.Time, actorID *uint64) (*Refund, error) {
	var out *Refund
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rf Refund
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ?", refundID, tenantID).
			First(&rf).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrRefundNotFound
			}
			return fmt.Errorf("lock refund: %w", err)
		}
		if rf.Status != RefundPending {
			return ErrRefundNotPending
		}

		// Validasi akun bank (COA-driven: aktif + kategori cash/bank).
		var bank struct {
			ID       uint64
			IsActive bool
			Category string
		}
		if err := tx.Table("accounts").Select("id, is_active, category").
			Where("tenant_id = ? AND code = ?", tenantID, bankAccountCode).
			Scan(&bank).Error; err != nil {
			return err
		}
		if bank.ID == 0 || !bank.IsActive || (bank.Category != "cash" && bank.Category != "bank") {
			return ErrBankAccountInvalid
		}
		accs, err := (&Service{db: tx}).accountsByCode(ctx, tenantID, []string{accHutangRefund})
		if err != nil {
			return err
		}

		txLedgerRepo := ledger.NewGORMRepository(tx)
		txPosting := ledger.NewPostingService(txLedgerRepo, txLedgerRepo).WithPeriodChecker(txLedgerRepo)
		uid := rf.UnitID
		// RFC — dana kembali ke customer/pembeli yang batal.
		entry, err := txPosting.CreateAndPost(ctx, ledger.CreateJournalRequest{
			TenantID: tenantID, Date: payDate,
			Description: fmt.Sprintf("Pembayaran refund #%d kepada %s", rf.ID, rf.Payee),
			Reference:   fmt.Sprintf("RF-%d-PAY", rf.ID),
			Source:      "cancellation", CreatedBy: actorID,
			Lines: []ledger.LineInput{
				{AccountID: accs[accHutangRefund].ID, Debit: rf.Amount, UnitID: &uid, Description: "Pelunasan hutang refund"},
				{AccountID: bank.ID, Credit: rf.Amount, UnitID: &uid, Description: "Kas keluar refund"},
			},
			Document: ledger.DocumentSpec{TypeCode: ledger.DocCustomerRefund},
		})
		if err != nil {
			return fmt.Errorf("posting pembayaran refund: %w", err)
		}

		now := time.Now()
		res := tx.Model(&Refund{}).
			Where("id = ? AND tenant_id = ? AND status = ?", rf.ID, tenantID, string(RefundPending)).
			Updates(map[string]interface{}{
				"status": string(RefundPaid), "payment_journal_id": entry.ID,
				"bank_account_code": bankAccountCode, "paid_at": &now, "paid_by": actorID,
			})
		if res.Error != nil {
			return fmt.Errorf("update refund paid: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return ErrRefundNotPending
		}

		if rf.SourceType == RefundFromBooking && rf.BookingID != nil {
			if err := tx.Table("bookings").
				Where("id = ? AND tenant_id = ? AND fee_disposition = 'pending_refund'", *rf.BookingID, tenantID).
				Update("fee_disposition", "refunded").Error; err != nil {
				return fmt.Errorf("update disposisi booking: %w", err)
			}
		}

		jid := entry.ID
		rf.Status, rf.PaymentJournalID, rf.BankAccountCode = RefundPaid, &jid, bankAccountCode
		rf.PaidAt, rf.PaidBy = &now, actorID
		out = &rf
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) GetRefund(ctx context.Context, tenantID, id uint64) (*Refund, error) {
	var rf Refund
	err := s.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).First(&rf).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRefundNotFound
		}
		return nil, fmt.Errorf("baca refund: %w", err)
	}
	return &rf, nil
}

func (s *Service) ListRefunds(ctx context.Context, tenantID uint64, status RefundStatus) ([]*Refund, error) {
	q := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID)
	if status != "" {
		q = q.Where("status = ?", string(status))
	}
	var out []*Refund
	if err := q.Order("id DESC").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("list refunds: %w", err)
	}
	return out, nil
}
