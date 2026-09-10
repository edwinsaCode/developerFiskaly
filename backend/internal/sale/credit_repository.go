package sale

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"esaproperti/internal/domain"
)

// ── FE-3 · P2 — Buyer credit balance + apply (atomic) ────────────────────────

// creditBalance menghitung saldo kredit unit memakai handle DB (tx atau r.db):
//
//	available = Σ(buyer_credit via termin.unit) − Σ(credit_applications.unit)
func creditBalance(ctx context.Context, db *gorm.DB, tenantID, unitID uint64) (available, sources, applied domain.Money, err error) {
	if serr := db.WithContext(ctx).
		Table("payment_allocations AS pa").
		Joins("JOIN termin_payments tp ON tp.id = pa.termin_payment_id AND tp.tenant_id = pa.tenant_id").
		Where("pa.tenant_id = ? AND pa.allocation_type = ? AND tp.unit_id = ?", tenantID, AllocationTypeBuyerCredit, unitID).
		Select("COALESCE(SUM(pa.amount), 0)").Scan(&sources).Error; serr != nil {
		return domain.Zero, domain.Zero, domain.Zero, fmt.Errorf("sum buyer_credit: %w", serr)
	}
	if aerr := db.WithContext(ctx).
		Table("credit_applications").
		Where("tenant_id = ? AND unit_id = ?", tenantID, unitID).
		Select("COALESCE(SUM(amount), 0)").Scan(&applied).Error; aerr != nil {
		return domain.Zero, domain.Zero, domain.Zero, fmt.Errorf("sum credit_applications: %w", aerr)
	}
	return sources.Sub(applied), sources, applied, nil
}

// GetBuyerCredit mengembalikan ringkasan saldo kredit buyer sebuah unit + rincian.
func (r *GORMRepository) GetBuyerCredit(ctx context.Context, tenantID, unitID uint64) (*BuyerCreditView, error) {
	available, sources, applied, err := creditBalance(ctx, r.db, tenantID, unitID)
	if err != nil {
		return nil, err
	}
	view := &BuyerCreditView{
		UnitID:       unitID,
		Available:    available.String(),
		TotalSources: sources.String(),
		TotalApplied: applied.String(),
		Sources:      []BuyerCreditSource{},
		Applications: []BuyerCreditApplicationView{},
	}

	// Rincian sumber (baris buyer_credit + tanggal termin + nomor kwitansi).
	type srcRow struct {
		TerminPaymentID uint64       `gorm:"column:termin_payment_id"`
		Amount          domain.Money `gorm:"column:amount"`
		Date            time.Time    `gorm:"column:date"`
		ReceiptNumber   string       `gorm:"column:receipt_number"`
	}
	var srcs []srcRow
	if err := r.db.WithContext(ctx).Raw(`
		SELECT pa.termin_payment_id, pa.amount, tp.date,
		       COALESCE(rc.receipt_number, '') AS receipt_number
		FROM payment_allocations pa
		JOIN termin_payments tp ON tp.id = pa.termin_payment_id AND tp.tenant_id = pa.tenant_id
		LEFT JOIN receipts rc   ON rc.termin_payment_id = pa.termin_payment_id AND rc.tenant_id = pa.tenant_id
		WHERE pa.tenant_id = ? AND pa.allocation_type = ? AND tp.unit_id = ?
		ORDER BY tp.date ASC, pa.id ASC`, tenantID, AllocationTypeBuyerCredit, unitID).Scan(&srcs).Error; err != nil {
		return nil, fmt.Errorf("rincian sumber kredit: %w", err)
	}
	for _, s := range srcs {
		view.Sources = append(view.Sources, BuyerCreditSource{
			TerminPaymentID: s.TerminPaymentID,
			Amount:          s.Amount.String(),
			Date:            s.Date.Format("2006-01-02"),
			ReceiptNumber:   s.ReceiptNumber,
		})
	}

	// Rincian pemakaian (credit_applications + label cicilan). PaymentScheduleID
	// nullable — konsumsi otomatis (Akad/pembatalan) tidak menunjuk cicilan.
	type appRow struct {
		ID                uint64       `gorm:"column:id"`
		PaymentScheduleID *uint64      `gorm:"column:payment_schedule_id"`
		Amount            domain.Money `gorm:"column:amount"`
		Reason            string       `gorm:"column:reason"`
		CreatedAt         time.Time    `gorm:"column:created_at"`
		InstallmentNumber int          `gorm:"column:installment_number"`
		ScheduleType      string       `gorm:"column:schedule_type"`
	}
	var apps []appRow
	if err := r.db.WithContext(ctx).Raw(`
		SELECT ca.id, ca.payment_schedule_id, ca.amount, ca.reason, ca.created_at,
		       COALESCE(ps.installment_number, 0) AS installment_number,
		       COALESCE(ps.type, '')              AS schedule_type
		FROM credit_applications ca
		LEFT JOIN payment_schedules ps ON ps.id = ca.payment_schedule_id AND ps.tenant_id = ca.tenant_id
		WHERE ca.tenant_id = ? AND ca.unit_id = ?
		ORDER BY ca.created_at ASC, ca.id ASC`, tenantID, unitID).Scan(&apps).Error; err != nil {
		return nil, fmt.Errorf("rincian pemakaian kredit: %w", err)
	}
	for _, a := range apps {
		label := allocationLabel(AllocationTypeSchedule, ScheduleType(a.ScheduleType), a.InstallmentNumber)
		if a.PaymentScheduleID == nil {
			label = "Konsumsi otomatis"
		}
		view.Applications = append(view.Applications, BuyerCreditApplicationView{
			ID:                a.ID,
			PaymentScheduleID: a.PaymentScheduleID,
			Label:             label,
			Amount:            a.Amount.String(),
			Reason:            a.Reason,
			AppliedAt:         a.CreatedAt.Format("2006-01-02"),
		})
	}
	return view, nil
}

// insertCreditApplicationRowTx menulis SATU baris payment_allocations bertipe
// 'credit_application' (termin NULL — pool; credit_application_id terisi).
// scheduleID nil untuk konsumsi otomatis tanpa cicilan target spesifik.
func insertCreditApplicationRowTx(ctx context.Context, db *gorm.DB, tenantID, creditAppID uint64, scheduleID *uint64, amount domain.Money, createdBy *uint64) error {
	caid := creditAppID
	alloc := &PaymentAllocation{
		TenantID:            tenantID,
		CreditApplicationID: &caid,
		PaymentScheduleID:   scheduleID,
		AllocationType:      AllocationTypeCreditApplication,
		Amount:              amount,
		CreatedBy:           createdBy,
	}
	if err := db.WithContext(ctx).Create(alloc).Error; err != nil {
		return fmt.Errorf("insert alokasi credit_application: %w", err)
	}
	return nil
}

// consumeRemainingCreditInTx menutup habis SISA saldo kredit buyer yang tidak
// lagi bisa dianggap tersedia (dikonsumsi/didisposisi lewat jalur lain di
// LUAR ApplyCredit eksplisit — netting Uang Muka saat Akad, atau disposisi
// terminal saat pembatalan) dengan mencatatnya sebagai SATU credit_applications
// baru (PaymentScheduleID nil). No-op bila available <= 0 — idempoten by
// construction: begitu dijalankan, available jadi 0, panggilan ulang pada
// state yang sama tidak menulis apa pun. Dipanggil DI DALAM tx atomik
// pemanggil (BASTAtomicWriter.Execute / cancellation.Process / backfill).
//
// saleContractID 0 = unit belum/tidak punya sale_contract formal (mis.
// pembatalan pra-kontrak atas termin/booking) — disimpan sebagai NULL,
// BUKAN error: unit_id tetap cukup untuk audit & mencegah kredit itu terus
// terlihat "tersedia" tanpa harus mensyaratkan kontrak yang tidak ada.
func consumeRemainingCreditInTx(ctx context.Context, tx *gorm.DB, tenantID, unitID, saleContractID uint64, reason string, appliedBy *uint64) error {
	available, _, _, err := creditBalance(ctx, tx, tenantID, unitID)
	if err != nil {
		return err
	}
	if available.IsZero() || available.IsNeg() {
		return nil
	}
	var contractIDPtr *uint64
	if saleContractID != 0 {
		contractIDPtr = &saleContractID
	}
	ca := &CreditApplication{
		TenantID:       tenantID,
		SaleContractID: contractIDPtr,
		UnitID:         unitID,
		Amount:         available,
		Reason:         reason,
		AppliedBy:      appliedBy,
	}
	if err := tx.WithContext(ctx).Create(ca).Error; err != nil {
		return fmt.Errorf("simpan credit_application (konsumsi otomatis): %w", err)
	}
	return insertCreditApplicationRowTx(ctx, tx, tenantID, ca.ID, nil, available, appliedBy)
}

// consumeCreditToScheduleInTx menutup sebagian/seluruh SATU cicilan dengan
// jumlah tertentu dari saldo kredit buyer (di dalam tx), mereplikasi pola
// ApplyCredit langkah 6-9 (event credit_applications → baris sub-ledger →
// cache paid_amount/status) TANPA row-locking, karena cicilan sasaran (sch)
// baru dibuat DI DALAM tx pemanggil yang sama dan belum bisa diakses
// konkuren dari luar.
//
// Dipakai untuk netting advance Kelebihan Tanah pra-Akad terhadap jadwal
// tanah yang baru dibuat Execute (bug 2026-09-04): kontrak Tunai lump-sum
// menaruh SELURUH termin rumah+tanah sebagai satu Uang Muka Penjualan;
// bagian yang secara ekonomi milik tanah (amount di sini) sudah dinolkan
// dari Uang Muka via jurnal netting (buildLandAdvanceNettingLines) — tanpa
// panggilan ini payment_schedules.paid_amount tanah tetap 0 walau sebagian
// sudah lunas, melanggar guard #1 (paid_amount tak boleh berubah tanpa
// baris payment_allocations).
//
// amount diasumsikan sudah divalidasi <= sch.Amount oleh pemanggil (Service.
// RecordAkad: landAdvance <= landGross, dijamin oleh validasi ceiling
// sebelum Execute dipanggil) — no-op bila <= 0.
func consumeCreditToScheduleInTx(ctx context.Context, tx *gorm.DB, tenantID, unitID, saleContractID uint64, sch *PaymentSchedule, amount domain.Money, reason string, appliedBy *uint64) error {
	if amount.IsZero() || amount.IsNeg() {
		return nil
	}
	var contractIDPtr *uint64
	if saleContractID != 0 {
		contractIDPtr = &saleContractID
	}
	scheduleID := sch.ID
	ca := &CreditApplication{
		TenantID:          tenantID,
		SaleContractID:    contractIDPtr,
		UnitID:            unitID,
		PaymentScheduleID: &scheduleID,
		Amount:            amount,
		Reason:            reason,
		AppliedBy:         appliedBy,
	}
	if err := tx.WithContext(ctx).Create(ca).Error; err != nil {
		return fmt.Errorf("simpan credit_application (netting jadwal #%d): %w", scheduleID, err)
	}
	if err := insertCreditApplicationRowTx(ctx, tx, tenantID, ca.ID, &scheduleID, amount, appliedBy); err != nil {
		return err
	}

	newPaid := sch.PaidAmount.Add(amount)
	fullyPaid := !newPaid.LessThan(sch.Amount)
	updates := map[string]interface{}{"paid_amount": newPaid}
	if fullyPaid {
		updates["status"] = string(ScheduleStatusReceived)
		updates["received_at"] = time.Now()
	}
	res := tx.WithContext(ctx).Model(&PaymentSchedule{}).
		Where("id = ? AND tenant_id = ?", scheduleID, tenantID).
		Updates(updates)
	if res.Error != nil {
		return fmt.Errorf("update paid_amount jadwal #%d: %w", scheduleID, res.Error)
	}
	return nil
}

// ApplyCredit memakai saldo kredit buyer ke sebuah cicilan secara ATOMIK, TANPA
// jurnal & TANPA kas (reklasifikasi sub-ledger). Lock sale_contracts row FOR
// UPDATE menyerialkan apply-credit per unit (cegah overdraw saldo). Cicilan
// target juga dikunci (cegah overpay). Append-only: baris buyer_credit lama
// tak diubah — hanya event + baris alokasi baru.
func (r *GORMRepository) ApplyCredit(ctx context.Context, tenantID, contractID uint64, req ApplyCreditRequest) (*ApplyCreditResult, error) {
	var result ApplyCreditResult

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. Lock kontrak (serialisasi apply-credit per unit).
		var contract SaleContract
		if err := tx.WithContext(ctx).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ?", contractID, tenantID).
			First(&contract).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrContractNotFound
			}
			return fmt.Errorf("lock kontrak %d: %w", contractID, err)
		}

		// 2. Idempotency (di dalam tx, setelah lock): kunci sama → tidak apply lagi.
		if req.IdempotencyKey != "" {
			var existing CreditApplication
			ferr := tx.WithContext(ctx).
				Where("tenant_id = ? AND idempotency_key = ?", tenantID, req.IdempotencyKey).
				First(&existing).Error
			if ferr == nil {
				avail, _, _, cerr := creditBalance(ctx, tx, tenantID, contract.UnitID)
				if cerr != nil {
					return cerr
				}
				var existingScheduleID uint64
				if existing.PaymentScheduleID != nil {
					existingScheduleID = *existing.PaymentScheduleID
				}
				result = ApplyCreditResult{
					CreditApplicationID: existing.ID,
					ScheduleID:          existingScheduleID,
					Applied:             existing.Amount.String(),
					RemainingCredit:     avail.String(),
					AlreadyExisted:      true,
				}
				return nil
			} else if !errors.Is(ferr, gorm.ErrRecordNotFound) {
				return fmt.Errorf("cek idempotency: %w", ferr)
			}
		}

		// 3. Lock cicilan target (milik kontrak & tenant).
		var sch PaymentSchedule
		if err := tx.WithContext(ctx).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ? AND sale_contract_id = ?", req.ScheduleID, tenantID, contractID).
			First(&sch).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrScheduleNotFound
			}
			return fmt.Errorf("lock cicilan %d: %w", req.ScheduleID, err)
		}

		// 4. Saldo tersedia (di bawah lock kontrak) + sisa cicilan.
		available, _, _, cerr := creditBalance(ctx, tx, tenantID, contract.UnitID)
		if cerr != nil {
			return cerr
		}
		remaining := sch.Amount.Sub(sch.PaidAmount)

		// 5. Tentukan & validasi jumlah.
		amt, verr := resolveCreditAmount(req.Amount, available, remaining)
		if verr != nil {
			return verr
		}

		// 6. Event credit_applications (audit who/when/why + idempotency).
		scheduleID := req.ScheduleID
		cid := contractID
		ca := &CreditApplication{
			TenantID:          tenantID,
			SaleContractID:    &cid,
			UnitID:            contract.UnitID,
			PaymentScheduleID: &scheduleID,
			Amount:            amt,
			Reason:            req.Reason,
			AppliedBy:         req.AppliedBy,
			IdempotencyKey:    idemPtr(req.IdempotencyKey),
		}
		if err := tx.WithContext(ctx).Create(ca).Error; err != nil {
			return fmt.Errorf("simpan credit_application: %w", err)
		}

		// 7. Baris sub-ledger credit_application (append-only, no journal).
		if err := insertCreditApplicationRowTx(ctx, tx, tenantID, ca.ID, &scheduleID, amt, req.AppliedBy); err != nil {
			return err
		}

		// 8. Update cache paid_amount cicilan (+ status bila lunas). TIDAK set
		//    termin_payment_id (tak ada termin/kwitansi untuk pemakaian kredit).
		newPaid := sch.PaidAmount.Add(amt)
		fullyPaid := !newPaid.LessThan(sch.Amount)
		updates := map[string]interface{}{"paid_amount": newPaid}
		if fullyPaid {
			updates["status"] = string(ScheduleStatusReceived)
			updates["received_at"] = time.Now()
		}
		res := tx.WithContext(ctx).Model(&PaymentSchedule{}).
			Where("id = ? AND tenant_id = ?", req.ScheduleID, tenantID).
			Updates(updates)
		if res.Error != nil {
			return fmt.Errorf("update paid_amount cicilan #%d: %w", req.ScheduleID, res.Error)
		}

		// 9. Sinkron invoice bila cicilan lunas (dalam tx).
		if fullyPaid && r.invoiceTx != nil {
			if err := r.invoiceTx.MarkPaidByScheduleIDInTx(ctx, tx, tenantID, req.ScheduleID); err != nil {
				return fmt.Errorf("sinkron invoice cicilan #%d: %w", req.ScheduleID, err)
			}
		}

		result = ApplyCreditResult{
			CreditApplicationID: ca.ID,
			ScheduleID:          req.ScheduleID,
			Applied:             amt.String(),
			SchedulePaidTotal:   newPaid.String(),
			ScheduleFullyPaid:   fullyPaid,
			RemainingCredit:     available.Sub(amt).String(),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}
