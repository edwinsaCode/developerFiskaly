package sale

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"esaproperti/internal/allocation"
	"esaproperti/internal/document"
	"esaproperti/internal/domain"
	"esaproperti/internal/land"
	"esaproperti/internal/ledger"
	"esaproperti/internal/project"
)

// ── GORMRepository ────────────────────────────────────────────────────────────

type GORMRepository struct {
	db         *gorm.DB
	posting    *ledger.PostingService
	allocSvc   *allocation.Service
	taxAccruer PPhFinalAccruer       // optional; nil = no auto PPh Final accrual at BAST
	realizRec  RealizationRecognizer // optional; W-5: piutang biaya realisasi diakui saat BAST
	receiptTx  ReceiptTxGenerator    // optional; kwitansi atomik dalam transaksi commit
	invoiceTx  InvoiceTxUpdater      // optional; sinkron invoice atomik dalam transaksi commit
}

func NewGORMRepository(db *gorm.DB, posting *ledger.PostingService, allocSvc *allocation.Service) *GORMRepository {
	return &GORMRepository{db: db, posting: posting, allocSvc: allocSvc}
}

// SetTaxAccruer wires in the PPh Final auto-accrual (called from handler or main.go).
func (r *GORMRepository) SetTaxAccruer(a PPhFinalAccruer) {
	r.taxAccruer = a
}

// SetRealizationRecognizer memasang pengakuan piutang biaya realisasi (W-5).
func (r *GORMRepository) SetRealizationRecognizer(rec RealizationRecognizer) {
	r.realizRec = rec
}

// SetReceiptTxGenerator memasang generator kwitansi tx-aware (wiring layer).
func (r *GORMRepository) SetReceiptTxGenerator(g ReceiptTxGenerator) {
	r.receiptTx = g
}

// SetInvoiceTxUpdater memasang updater invoice tx-aware (wiring layer).
func (r *GORMRepository) SetInvoiceTxUpdater(u InvoiceTxUpdater) {
	r.invoiceTx = u
}

// ── AccountFinder ─────────────────────────────────────────────────────────────

func (r *GORMRepository) FindAccountIDByCode(ctx context.Context, tenantID uint64, code string) (uint64, error) {
	var acc ledger.Account
	err := r.db.WithContext(ctx).
		Select("id").
		Where("tenant_id = ? AND code = ?", tenantID, code).
		First(&acc).Error
	if err != nil {
		return 0, fmt.Errorf("akun %q tidak ditemukan: %w", code, err)
	}
	return acc.ID, nil
}

// ValidateCashBankAccount memuat akun (tenant-scoped) lalu memvalidasinya sebagai
// rekening pembayaran COA-driven: ada, milik tenant, aktif, kategori cash/bank.
func (r *GORMRepository) ValidateCashBankAccount(ctx context.Context, tenantID uint64, code string) error {
	var acc ledger.Account
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND code = ?", tenantID, code).
		First(&acc).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrPaymentAccountNotFound // tidak ada / bukan milik tenant
		}
		return fmt.Errorf("ValidateCashBankAccount: %w", err)
	}
	switch ledger.ValidatePaymentAccount(&acc) {
	case nil:
		return nil
	case ledger.ErrAccountInactive:
		return ErrPaymentAccountInactive
	default: // ErrNotCashBankAccount / ErrAccountNotFound
		return ErrInvalidBankAccount
	}
}

// ── JournalWriter (digunakan untuk Event 2 / termin) ─────────────────────────

func (r *GORMRepository) CreateJournal(ctx context.Context, tenantID uint64, date time.Time, description string, lines []JournalLineInput) (uint64, error) {
	req := ledger.CreateJournalRequest{
		TenantID:    tenantID,
		Date:        date,
		Description: description,
		Lines:       toledgerLines(lines),
	}
	entry, err := r.posting.Create(ctx, req)
	if err != nil {
		return 0, fmt.Errorf("buat jurnal: %w", err)
	}
	return entry.ID, nil
}

func (r *GORMRepository) PostJournal(ctx context.Context, tenantID, journalID uint64) error {
	_, err := r.posting.Post(ctx, tenantID, journalID)
	return err
}

// ── UnitReader ────────────────────────────────────────────────────────────────

func (r *GORMRepository) FindUnitSaleInfo(ctx context.Context, tenantID, unitID uint64) (*UnitSaleInfo, error) {
	var u project.Unit
	err := r.db.WithContext(ctx).
		Select("id, project_id, phase_id, status, unit_type, list_price, code").
		Where("id = ? AND tenant_id = ?", unitID, tenantID).
		First(&u).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUnitNotFound
		}
		return nil, fmt.Errorf("FindUnitSaleInfo: %w", err)
	}
	return &UnitSaleInfo{
		ID:          u.ID,
		ProjectID:   u.ProjectID,
		PhaseID:     u.PhaseID,
		Status:      string(u.Status),
		UnitType:    u.UnitType,
		ListPrice:   u.ListPrice,
		Code:        u.Code,
		ProjectName: projectNameByID(r.db.WithContext(ctx), tenantID, u.ProjectID),
	}, nil
}

// projectNameByID membaca projects.name untuk deskripsi jurnal. Best-effort:
// gagal/kosong → "" dan deskripsi jatuh ke bentuk tanpa segmen proyek; tidak
// pernah menggagalkan transaksi.
func projectNameByID(db *gorm.DB, tenantID, projectID uint64) string {
	var name string
	if err := db.Model(&project.Project{}).
		Select("name").
		Where("id = ? AND tenant_id = ?", projectID, tenantID).
		Scan(&name).Error; err != nil {
		return ""
	}
	return name
}

// ── TerminStore ───────────────────────────────────────────────────────────────

func (r *GORMRepository) SaveTermin(ctx context.Context, t *TerminPayment) error {
	if err := r.db.WithContext(ctx).Create(t).Error; err != nil {
		return fmt.Errorf("SaveTermin: %w", err)
	}
	return nil
}

// SumTerminsByUnit adalah PRIMITIF KANONIK "total dibayar terhadap harga"
// (registry #8). R4: hanya termin counts_toward_price=TRUE yang dihitung —
// booking fee kebijakan baru (FALSE) di LUAR harga, tidak mengurangi
// outstanding. Histori pra-R4 (semua TRUE) identik.
func (r *GORMRepository) SumTerminsByUnit(ctx context.Context, tenantID, unitID uint64) (domain.Money, error) {
	type result struct {
		Total domain.Money `gorm:"column:total"`
	}
	var res result
	err := r.db.WithContext(ctx).
		Table("termin_payments").
		Select("COALESCE(SUM(amount), 0) AS total").
		Where("tenant_id = ? AND unit_id = ? AND counts_toward_price = TRUE", tenantID, unitID).
		Scan(&res).Error
	if err != nil {
		return domain.Zero, fmt.Errorf("SumTerminsByUnit: %w", err)
	}
	return res.Total, nil
}

func (r *GORMRepository) SumTerminsByUnitAndCreditAccount(ctx context.Context, tenantID, unitID uint64, creditAccountCode string) (domain.Money, error) {
	type result struct {
		Total domain.Money `gorm:"column:total"`
	}
	var res result
	err := r.db.WithContext(ctx).
		Table("termin_payments").
		Select("COALESCE(SUM(amount), 0) AS total").
		Where("tenant_id = ? AND unit_id = ? AND credit_account_code = ?", tenantID, unitID, creditAccountCode).
		Scan(&res).Error
	if err != nil {
		return domain.Zero, fmt.Errorf("SumTerminsByUnitAndCreditAccount: %w", err)
	}
	return res.Total, nil
}

// FindSaleRecord mengembalikan SaleRecord (bukti BAST) sebuah unit, tenant-scoped.
// Mengembalikan ErrSaleRecordNotFound bila unit belum BAST.
func (r *GORMRepository) FindSaleRecord(ctx context.Context, tenantID, unitID uint64) (*SaleRecord, error) {
	var rec SaleRecord
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND unit_id = ?", tenantID, unitID).
		First(&rec).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSaleRecordNotFound
		}
		return nil, fmt.Errorf("FindSaleRecord: %w", err)
	}
	return &rec, nil
}

// ── UnitCostProvider — delegates to allocation.Service ────────────────────────

func (r *GORMRepository) GetUnitCost(ctx context.Context, tenantID, projectID, unitID uint64) (domain.UnitCostBreakdown, error) {
	results, err := r.allocSvc.ComputeAllocation(ctx, tenantID, projectID)
	if err != nil {
		return domain.UnitCostBreakdown{}, fmt.Errorf("GetUnitCost: %w", err)
	}
	for _, res := range results {
		if res.UnitID == unitID {
			return res.Total, nil
		}
	}
	return domain.UnitCostBreakdown{}, ErrUnitNotFound
}

// ── BASTAtomicWriter ──────────────────────────────────────────────────────────
//
// Execute menjalankan Event 3 + Event 4 + update status unit dalam satu DB transaction.
// Atomik: rollback semua jika satu langkah gagal.

func (r *GORMRepository) Execute(ctx context.Context, params BASTAtomicParams) (*SaleRecord, error) {
	var record SaleRecord

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txLedgerRepo := ledger.NewGORMRepository(tx)
		txPosting := ledger.NewPostingService(txLedgerRepo, txLedgerRepo).WithPeriodChecker(txLedgerRepo)

		// ── Guard lifecycle (Increment 6.1 / F-1) ─────────────────────────────
		// Akad hanya sah dari status yang matriks izinkan → sold (reserved|ppjb).
		// Validasi DI DALAM tx, sebelum jurnal apa pun dibuat; sumber aturan =
		// project.UnitStatus.CanTransitionTo (satu definisi, tanpa duplikasi).
		var unitRow struct {
			Status string
			Code   string
		}
		if err := tx.Model(&project.Unit{}).
			Select("status, code").
			Where("id = ? AND tenant_id = ?", params.UnitID, params.TenantID).
			Scan(&unitRow).Error; err != nil {
			return fmt.Errorf("baca status unit: %w", err)
		}
		projectName := projectNameByID(tx, params.TenantID, params.ProjectID)
		fromStatus := unitRow.Status
		switch {
		case fromStatus == "":
			return ErrUnitNotFound
		case project.UnitStatus(fromStatus) == project.UnitStatusSold:
			return ErrUnitAlreadySold
		case !project.UnitStatus(fromStatus).CanTransitionTo(project.UnitStatusSold):
			return ErrUnitNotBASTReady
		}

		// ── Event 3: Pengakuan Pendapatan ─────────────────────────────────────
		revenueReq := ledger.CreateJournalRequest{
			TenantID:    params.TenantID,
			Date:        params.BASTDate,
			Description: DescribeWithUnit("Akad pengakuan pendapatan", projectName, unitRow.Code),
			Lines:       toledgerLines(params.RevenueLines),
		}
		revEntry, err := txPosting.Create(ctx, revenueReq)
		if err != nil {
			return fmt.Errorf("buat jurnal Event 3: %w", err)
		}
		if _, err := txPosting.Post(ctx, params.TenantID, revEntry.ID); err != nil {
			return fmt.Errorf("posting Event 3: %w", err)
		}

		// ── Event 4: Pengakuan HPP (jika ada biaya) ───────────────────────────
		var cogsJournalID *uint64
		if len(params.COGSLines) > 0 {
			cogsReq := ledger.CreateJournalRequest{
				TenantID:    params.TenantID,
				Date:        params.BASTDate,
				Description: DescribeWithUnit("Akad HPP", projectName, unitRow.Code),
				Lines:       toledgerLines(params.COGSLines),
			}
			cogsEntry, err := txPosting.Create(ctx, cogsReq)
			if err != nil {
				return fmt.Errorf("buat jurnal Event 4: %w", err)
			}
			if _, err := txPosting.Post(ctx, params.TenantID, cogsEntry.ID); err != nil {
				return fmt.Errorf("posting Event 4: %w", err)
			}
			cogsJournalID = &cogsEntry.ID
		}

		// ── Produk Tambahan: Kelebihan Tanah (kelebihan-tanah-booking-integration-2026-08) ──
		// Sudah sepenuhnya disiapkan oleh land.Service.PrepareBundledAkad
		// (service.go) — di sini hanya diteruskan ke land.RecordAkadTx DI
		// DALAM tx yang sama: rumah dan tanah diakui atomik bersama, gagal
		// salah satu = keduanya batal (Invariant #1/#5).
		if params.Land != nil {
			landSale, err := land.RecordAkadTx(ctx, tx, params.TenantID, *params.Land)
			if err != nil {
				return fmt.Errorf("Akad Kelebihan Tanah: %w", err)
			}

			// ── Bug UAT: land_sales sebagai anchor AR (migrasi 000095) ────────
			// Sebelum ini, piutang Kelebihan Tanah sudah benar di GL (Cr akun
			// Piutang Customer yang sama dengan rumah) tapi TIDAK PERNAH punya
			// baris payment_schedules — sehingga ReceivePayment/planAllocation
			// tidak pernah melihatnya sebagai piutang yang bisa dialokasikan
			// (Neraca benar, Penerimaan tidak lengkap). Baris ini dibuat ATOMIK
			// di transaksi yang sama dengan land.RecordAkadTx di atas, dengan
			// tipe khusus ScheduleTypeLand + LandSaleID — bukan payment engine
			// kedua, murni baris tambahan di sub-ledger yang sudah ada supaya
			// waterfall (planAllocation), alokasi (payment_allocations), dan
			// cancellation-supersede eksisting otomatis mencakupnya.
			// InstallmentNumber sentinel besar: default land dilunasi PALING
			// AKHIR dalam waterfall level-kontrak (cicilan rumah presedensi),
			// tanpa menghalangi pembayaran langsung ke land via ScheduleID.
			landSchedule := &PaymentSchedule{
				TenantID:          params.TenantID,
				SaleContractID:    params.SaleContractID,
				UnitID:            params.UnitID,
				LandSaleID:        &landSale.ID,
				InstallmentNumber: 9999,
				DueDate:           params.Land.RecognitionDate,
				Amount:            landSale.GrossAmount,
				Type:              ScheduleTypeLand,
				Status:            ScheduleStatusScheduled,
				ScheduleVersion:   1,
			}
			if err := tx.Create(landSchedule).Error; err != nil {
				return fmt.Errorf("simpan jadwal piutang Kelebihan Tanah: %w", err)
			}

			// ── Netting advance Kelebihan Tanah pra-Akad (bug 2026-09-04) ─────
			// Kontrak Tunai lump-sum menaruh SELURUH termin rumah+tanah sebagai
			// SATU Uang Muka Penjualan; Event 3 (buildEvent3Lines, dipanggil di
			// Service.RecordAkad) hanya menolkan bagian rumah (dicap ke gross
			// rumah) — bagian yang secara ekonomi milik tanah (LandAdvance)
			// dinolkan di sini: jurnal Dr UMP / Cr Piutang Kelebihan Tanah
			// (LandAdvanceLines, sudah dibangun Service) DIPOSTING atomik +
			// sub-ledgernya dicatat (credit_applications + payment_allocations
			// + cache paid_amount landSchedule) SEBELUM sweep buyer-credit
			// generik di bawah — urutan ini WAJIB: kalau sweep generik jalan
			// duluan, ia akan menyapu SELURUH sisa saldo kredit (termasuk
			// LandAdvance) ke SATU credit_application pool, lalu panggilan di
			// sini akan menulis credit_application KEDUA untuk jumlah yang
			// SAMA — double count pada creditBalance() (Σapplied > Σsources).
			if !params.LandAdvance.IsZero() && !params.LandAdvance.IsNeg() {
				nettingReq := ledger.CreateJournalRequest{
					TenantID:    params.TenantID,
					Date:        params.BASTDate,
					Description: DescribeWithUnit("Netting uang muka Kelebihan Tanah saat BAST", projectName, unitRow.Code),
					Lines:       toledgerLines(params.LandAdvanceLines),
				}
				nettingEntry, err := txPosting.Create(ctx, nettingReq)
				if err != nil {
					return fmt.Errorf("buat jurnal netting Kelebihan Tanah: %w", err)
				}
				if _, err := txPosting.Post(ctx, params.TenantID, nettingEntry.ID); err != nil {
					return fmt.Errorf("posting jurnal netting Kelebihan Tanah: %w", err)
				}
				if err := consumeCreditToScheduleInTx(ctx, tx, params.TenantID, params.UnitID, params.SaleContractID,
					landSchedule, params.LandAdvance,
					"Konsumsi otomatis saat Akad — uang muka gabungan dinetkan ke piutang Kelebihan Tanah",
					params.CreatedBy); err != nil {
					return fmt.Errorf("netting advance Kelebihan Tanah ke jadwal: %w", err)
				}
			}

			// ── Baris jadwal RUMAH fallback (bug UAT — ketimpangan jadwal) ─────
			// Kontrak Tunai/lunas-langsung TIDAK PERNAH punya baris
			// payment_schedules rumah eksplisit (CreatePaymentSchedule hanya
			// dipakai jalur cicilan/skema KPR) — sebelumnya itu aman karena
			// outstandingForContract punya fallback gross−collected. Begitu
			// kontrak SEKARANG juga punya baris land (di atas), fallback itu
			// jadi bias: baris jadwal "ada" (landSchedule) tapi hanya mewakili
			// tanah, rumah tetap tanpa jadwal — kalau dibiarkan waterfall
			// kontrak (planForContract) hanya melihat cicilan tanah, dan
			// pembayaran gabungan rumah+tanah salah alokasi (sisa rumah jatuh
			// ke buyer-credit, bukan melunasi piutang rumah).
			//
			// Fix: kalau kontrak ini BELUM punya baris jadwal non-land sama
			// sekali (bukan KPR/skema cicilan — itu sudah bikin baris sendiri
			// lebih awal), buat SATU baris lump-sum mewakili SISA piutang
			// rumah pasca-Akad (gross − total advance, angka yang SAMA dengan
			// yang didebit ke akun Piutang di Event 3 — lihat buildEvent3Lines)
			// supaya waterfall kontrak & outstandingForContract melihat rumah
			// dan tanah dengan cara yang konsisten. Amount 0 (rumah lunas
			// penuh via advance sebelum Akad) → tidak perlu baris.
			if params.SaleContractID != 0 {
				var nonLandCount int64
				if err := tx.Model(&PaymentSchedule{}).
					Where("tenant_id = ? AND sale_contract_id = ? AND type <> ? AND status <> ?",
						params.TenantID, params.SaleContractID, ScheduleTypeLand, ScheduleStatusSuperseded).
					Count(&nonLandCount).Error; err != nil {
					return fmt.Errorf("cek jadwal rumah eksisting: %w", err)
				}
				if nonLandCount == 0 {
					gross := params.SalePrice
					if params.IsVAT {
						gross = gross.Add(vatAmountOf(params.SalePrice, params.VATRate))
					}
					houseRemaining := gross.Sub(params.TotalAdvance)
					if houseRemaining.GreaterThan(domain.Zero) {
						houseSchedule := &PaymentSchedule{
							TenantID:          params.TenantID,
							SaleContractID:    params.SaleContractID,
							UnitID:            params.UnitID,
							InstallmentNumber: 1,
							DueDate:           params.BASTDate,
							Amount:            houseRemaining,
							Type:              ScheduleTypeFinal,
							Status:            ScheduleStatusScheduled,
							ScheduleVersion:   1,
						}
						if err := tx.Create(houseSchedule).Error; err != nil {
							return fmt.Errorf("simpan jadwal sisa piutang rumah: %w", err)
						}
					}
				}
			}
		}

		// ── Buyer Credit: konsumsi otomatis oleh netting Uang Muka (bug fix) ──
		// Event 3 (buildEvent3Lines) mendebit bagian RUMAH dari Uang Muka
		// Penjualan terkumpul unit ini ("nolkan uang muka penjualan saat
		// BAST"), dan blok Kelebihan Tanah di atas (bila ada) sudah menolkan
		// bagian TANAH-nya secara eksplisit ke jadwal tanah — jadi dipanggil
		// DI SINI, SETELAH blok tanah, supaya hanya menyapu sisa saldo kredit
		// yang BENAR-BENAR tidak tercakup jalur mana pun (mis. overpay yang
		// melebihi gross rumah+tanah gabungan). Termasuk saldo kredit buyer
		// yang masih berupa BELUM dialokasikan ke cicilan mana pun
		// (payment_allocations allocation_type=buyer_credit, payment_
		// schedule_id NULL). Sebelum perbaikan ini, konsumsi tsb tidak pernah
		// tercatat di credit_applications — GetBuyerCredit terus menganggapnya
		// "tersedia" selamanya meski GL Uang Muka sudah nol (kasus nyata:
		// kontrak #4407, Rp37.000.000). credit_applications adalah
		// SATU-SATUNYA source of truth untuk seluruh konsumsi (eksplisit via
		// ApplyCredit MAUPUN otomatis di sini) — no-op bila tidak ada sisa
		// saldo untuk unit ini.
		if err := consumeRemainingCreditInTx(ctx, tx, params.TenantID, params.UnitID, params.SaleContractID,
			"Konsumsi otomatis saat Akad — Uang Muka Penjualan dinetkan ke pengakuan pendapatan", params.CreatedBy); err != nil {
			return fmt.Errorf("konsumsi saldo kredit buyer saat Akad: %w", err)
		}

		// ── Update unit status → sold ──────────────────────────────────────────
		// Predikat di-pin ke status yang tervalidasi di atas (bukan `!= sold`):
		// perubahan status konkuren apa pun ⇒ 0 baris ⇒ konflik eksplisit, dan
		// from_status di log dijamin akurat.
		saleDate := params.BASTDate
		saleDecimal := params.SalePrice.Decimal()
		buyerRef := params.BuyerRef
		res := tx.Model(&project.Unit{}).
			Where("id = ? AND tenant_id = ? AND status = ?", params.UnitID, params.TenantID, fromStatus).
			Updates(map[string]interface{}{
				"status":     string(project.UnitStatusSold),
				"buyer_ref":  buyerRef,
				"sale_date":  &saleDate,
				"sale_price": &saleDecimal,
			})
		if res.Error != nil {
			return fmt.Errorf("update status unit: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return project.ErrUnitTransitionConflict
		}

		// ── Event 5: PPh Final accrual (otomatis dalam satu transaksi) ───────
		if r.taxAccruer != nil {
			if err := r.taxAccruer.AccruePPhFinalInTx(ctx, tx,
				params.TenantID, params.UnitID, params.ProjectID,
				params.SalePrice, params.BASTDate); err != nil {
				return fmt.Errorf("akrual PPh Final: %w", err)
			}
		}

		// ── Piutang Biaya Realisasi (W-5 / D-3) ──────────────────────────────
		// Sisa biaya realisasi yang belum tertagih diterbitkan invoice-nya di
		// sini, sehingga pada saat BAST ia sudah menjadi Piutang Customer —
		// piutang yang SAMA dengan piutang rumah (1-2000), bukan mekanisme
		// kedua. Ini bukan gate: BAST tidak pernah ditolak karena realisasi
		// belum lunas.
		if r.realizRec != nil {
			if err := r.realizRec.RecognizeOnBASTInTx(ctx, tx,
				params.TenantID, params.UnitID, params.CreatedBy); err != nil {
				return fmt.Errorf("pengakuan piutang biaya realisasi: %w", err)
			}
		}

		// ── Persist allocation snapshot (metode budgeted) ─────────────────────
		// Append-only (Invariant #5): ditulis atomik bersama jurnal + sale_record.
		// Tidak ada HPP budgeted terposting tanpa snapshot penyertanya.
		hppMethod := params.HPPMethod
		if hppMethod == "" {
			hppMethod = HPPMethodActual
		}
		var snapshotID, budgetPlanID *uint64
		var budgetPlanVersion *int
		if params.Snapshot != nil {
			snap := params.Snapshot.toSnapshot(params.TenantID, unitRow.Code)
			if err := tx.Create(snap).Error; err != nil {
				return fmt.Errorf("simpan allocation snapshot: %w", err)
			}
			for i := range snap.Lines {
				snap.Lines[i].SnapshotID = snap.ID
			}
			if err := tx.Create(&snap.Lines).Error; err != nil {
				return fmt.Errorf("simpan allocation snapshot lines: %w", err)
			}
			snapshotID = &snap.ID
			planID := params.Snapshot.BudgetPlanID
			planVer := params.Snapshot.BudgetPlanVersion
			budgetPlanID, budgetPlanVersion = &planID, &planVer
		}

		// ── Simpan SaleRecord ─────────────────────────────────────────────────
		record = SaleRecord{
			TenantID:             params.TenantID,
			UnitID:               params.UnitID,
			ProjectID:            params.ProjectID,
			PhaseID:              params.PhaseID,
			SalePrice:            params.SalePrice,
			IsVAT:                params.IsVAT,
			VATRate:              params.VATRate,
			TotalAdvanceAtBAST:   params.TotalAdvance,
			HPPLand:              params.HPPLand,
			HPPHard:              params.HPPHard,
			HPPSoft:              params.HPPSoft,
			HPPFinancing:         params.HPPFinancing,
			HPPMethod:            hppMethod,
			AllocationSnapshotID: snapshotID,
			BudgetPlanID:         budgetPlanID,
			BudgetPlanVersion:    budgetPlanVersion,
			BuyerRef:             params.BuyerRef,
			RecognitionDate:      params.BASTDate,
			RevenueJournalID:     revEntry.ID,
			COGSJournalID:        cogsJournalID,
		}
		if err := tx.Create(&record).Error; err != nil {
			return fmt.Errorf("simpan SaleRecord: %w", err)
		}

		// ── Log transisi unit → sold DI DALAM tx yang sama (Increment 6) ──────
		// Atomik dengan jurnal + snapshot + sale_record: gagal Akad = tanpa log.
		// event_date = tanggal Akad (kejadian bisnis); reference = sale_record.
		// from_status akurat: UPDATE di atas di-pin ke status yang sama.
		refID := record.ID
		if err := project.LogTransitionTx(tx, &project.UnitStatusTransition{
			TenantID:      params.TenantID,
			UnitID:        params.UnitID,
			FromStatus:    project.UnitStatus(fromStatus),
			ToStatus:      project.UnitStatusSold,
			Event:         project.EventAkadExecuted,
			EventDate:     params.BASTDate,
			ReferenceType: project.RefTypeSaleRecord,
			ReferenceID:   &refID,
		}); err != nil {
			return fmt.Errorf("log transisi unit (Akad): %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &record, nil
}

// ── HandoverWriter ────────────────────────────────────────────────────────────
//
// MarkPhysicalHandover (Temuan #7): serah terima fisik, TERPISAH dari Akad.
// TIDAK ADA jurnal, TIDAK ADA resolusi HPP — pendapatan+HPP sudah diakui di
// Execute (Akad). Transaksi atomik kecil: status unit sold→occupied +
// sale_records.handed_over_at + log transisi, atau semuanya batal.

func (r *GORMRepository) MarkPhysicalHandover(ctx context.Context, params PhysicalHandoverParams) (*SaleRecord, error) {
	var record SaleRecord

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var unitRow struct{ Status string }
		if err := tx.Model(&project.Unit{}).
			Select("status").
			Where("id = ? AND tenant_id = ?", params.UnitID, params.TenantID).
			Scan(&unitRow).Error; err != nil {
			return fmt.Errorf("baca status unit: %w", err)
		}
		switch {
		case unitRow.Status == "":
			return ErrUnitNotFound
		case project.UnitStatus(unitRow.Status) != project.UnitStatusSold:
			return ErrUnitNotHandoverReady
		}

		if err := tx.Where("tenant_id = ? AND unit_id = ?", params.TenantID, params.UnitID).
			First(&record).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrSaleRecordNotFound
			}
			return fmt.Errorf("baca sale record: %w", err)
		}

		res := tx.Model(&project.Unit{}).
			Where("id = ? AND tenant_id = ? AND status = ?", params.UnitID, params.TenantID, string(project.UnitStatusSold)).
			Update("status", string(project.UnitStatusOccupied))
		if res.Error != nil {
			return fmt.Errorf("update status unit: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return project.ErrUnitTransitionConflict
		}

		handoverDate := params.HandoverDate
		if err := tx.Model(&SaleRecord{}).Where("id = ?", record.ID).
			Update("handed_over_at", &handoverDate).Error; err != nil {
			return fmt.Errorf("update handed_over_at: %w", err)
		}
		record.HandedOverAt = &handoverDate

		if err := project.LogTransitionTx(tx, &project.UnitStatusTransition{
			TenantID:      params.TenantID,
			UnitID:        params.UnitID,
			FromStatus:    project.UnitStatusSold,
			ToStatus:      project.UnitStatusOccupied,
			Event:         project.EventPhysicallyOccupied,
			EventDate:     params.HandoverDate,
			ReferenceType: project.RefTypeSaleRecord,
			ReferenceID:   &record.ID,
		}); err != nil {
			return fmt.Errorf("log transisi unit (serah terima fisik): %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &record, nil
}

// ── ContractStore ─────────────────────────────────────────────────────────────

// SaveContract simpan/update satu SaleContract. Bila c.LandQuantityM2 != nil
// (kontrak dibuat LANGSUNG tanpa Booking — CreateContractRequest.LandQuantityM2,
// kelebihan-tanah-booking-integration-2026-08) reservasi Kelebihan Tanah
// dibuat ATOMIK di dalam transaksi yang sama SEBELUM kontrak disimpan: kontrak
// tidak pernah lahir dengan land_quantity_m2 terisi tapi tanpa reservasi yang
// sah (§D3 — pola identik alur booking di CreateBookingAtomic).
func (r *GORMRepository) SaveContract(ctx context.Context, c *SaleContract) error {
	if c.LandQuantityM2 == nil {
		if err := r.db.WithContext(ctx).Save(c).Error; err != nil {
			return fmt.Errorf("SaveContract: %w", err)
		}
		return nil
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var unit project.Unit
		if uerr := tx.WithContext(ctx).
			Select("project_id").
			Where("id = ? AND tenant_id = ?", c.UnitID, c.TenantID).
			First(&unit).Error; uerr != nil {
			return fmt.Errorf("baca proyek unit %d: %w", c.UnitID, uerr)
		}
		pool, perr := land.FindPoolByProjectTx(ctx, tx, c.TenantID, unit.ProjectID)
		if perr != nil {
			return fmt.Errorf("resolusi pool Kelebihan Tanah: %w", perr)
		}
		var custID uint64
		if c.CustomerID != nil {
			custID = *c.CustomerID
		}
		res, rerr := land.ReserveTx(ctx, tx, c.TenantID, pool.ID, land.ReserveInput{
			ProjectID:     pool.ProjectID,
			CustomerID:    custID,
			SalesPersonID: c.SalesPersonID,
			QuantityM2:    *c.LandQuantityM2,
			ReservedAt:    c.ContractDate,
		})
		if rerr != nil {
			return fmt.Errorf("reservasi Kelebihan Tanah: %w", rerr)
		}
		poolID, resID, price := pool.ID, res.ID, res.UnitPriceSnapshot
		c.LandStockID = &poolID
		c.LandReservationID = &resID
		c.LandUnitPriceSnapshot = &price
		if err := tx.WithContext(ctx).Save(c).Error; err != nil {
			return fmt.Errorf("SaveContract: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

func (r *GORMRepository) FindContractByID(ctx context.Context, tenantID, id uint64) (*SaleContract, error) {
	var c SaleContract
	err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrContractNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("FindContractByID: %w", err)
	}
	return &c, nil
}

// ListActiveContracts mengembalikan kontrak non-cancelled (scheme_state NULL =
// legacy, dianggap aktif) — dipakai agregat portfolio kanonik.
func (r *GORMRepository) ListActiveContracts(ctx context.Context, tenantID uint64) ([]*SaleContract, error) {
	var cs []*SaleContract
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND (scheme_state IS NULL OR scheme_state <> 'cancelled')", tenantID).
		Find(&cs).Error
	if err != nil {
		return nil, fmt.Errorf("ListActiveContracts: %w", err)
	}
	return cs, nil
}

// UnitProjectIDs memetakan unit → project (agregasi portfolio per proyek).
func (r *GORMRepository) UnitProjectIDs(ctx context.Context, tenantID uint64, unitIDs []uint64) (map[uint64]uint64, error) {
	if len(unitIDs) == 0 {
		return map[uint64]uint64{}, nil
	}
	var rows []struct {
		ID        uint64
		ProjectID uint64
	}
	if err := r.db.WithContext(ctx).
		Table("units").
		Select("id, project_id").
		Where("tenant_id = ? AND id IN ?", tenantID, unitIDs).
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("UnitProjectIDs: %w", err)
	}
	out := make(map[uint64]uint64, len(rows))
	for _, r2 := range rows {
		out[r2.ID] = r2.ProjectID
	}
	return out, nil
}

func (r *GORMRepository) FindContractByUnitID(ctx context.Context, tenantID, unitID uint64) (*SaleContract, error) {
	var c SaleContract
	err := r.db.WithContext(ctx).
		Where("unit_id = ? AND tenant_id = ?", unitID, tenantID).
		First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrContractNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("FindContractByUnitID: %w", err)
	}
	return &c, nil
}

func (r *GORMRepository) ListSchedulesByContract(ctx context.Context, tenantID, contractID uint64) ([]*PaymentSchedule, error) {
	var items []*PaymentSchedule
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND sale_contract_id = ?", tenantID, contractID).
		Order("installment_number ASC").
		Find(&items).Error
	if err != nil {
		return nil, fmt.Errorf("ListSchedulesByContract: %w", err)
	}
	return items, nil
}

// ── HouseARStore (W-8) ────────────────────────────────────────────────────────

// ListHouseARUnits mengembalikan unit yang piutangnya SUDAH LAHIR: ada
// `sale_records` (bukti BAST) dan belum dibatalkan (BD-1). Jadwal pembayaran
// TIDAK ikut menentukan keanggotaan di sini — unit ber-BAST tanpa jadwal tetap
// punya piutang, dan jadwal tanpa BAST belum punya piutang sama sekali.
//
// `collected` memakai primitif kanonik yang sama dengan SumTerminsByUnit
// (counts_toward_price = TRUE, registry #8) — ditulis sebagai subquery agar
// seluruh tenant selesai dalam satu perjalanan ke database, bukan N+1.
//
// `gross` mengikuti saleRecordGross: DPP + PPN bila PKP. Aritmetikanya DECIMAL
// di sisi MySQL (kolom DECIMAL(20,4)), tidak pernah menyentuh floating point.
func (r *GORMRepository) ListHouseARUnits(ctx context.Context, tenantID uint64) ([]HouseARUnit, error) {
	const q = `
SELECT
    COALESCE(c.id, 0)                AS contract_id,
    sr.unit_id                       AS unit_id,
    COALESCE(u.code, '')             AS unit_code,
    COALESCE(c.buyer_name, sr.buyer_ref, '') AS buyer_name,
    COALESCE(cu.phone, '')           AS buyer_phone,
    COALESCE(cu.email, '')           AS buyer_email,
    CASE WHEN sr.is_vat = 1
         THEN sr.sale_price + (sr.sale_price * sr.vat_rate)
         ELSE sr.sale_price END      AS gross,
    COALESCE((
        SELECT SUM(tp.amount) FROM termin_payments tp
        WHERE tp.tenant_id = sr.tenant_id
          AND tp.unit_id   = sr.unit_id
          AND tp.counts_toward_price = TRUE
    ), 0)                            AS collected,
    sr.recognition_date              AS recognition_date
FROM sale_records sr
LEFT JOIN units u          ON u.id  = sr.unit_id AND u.tenant_id  = sr.tenant_id
LEFT JOIN sale_contracts c ON c.unit_id = sr.unit_id AND c.tenant_id = sr.tenant_id
LEFT JOIN customers cu     ON cu.id = c.customer_id  AND cu.tenant_id = c.tenant_id
WHERE sr.tenant_id = ? AND sr.cancelled_at IS NULL
ORDER BY sr.unit_id ASC`

	var out []HouseARUnit
	if err := r.db.WithContext(ctx).Raw(q, tenantID).Scan(&out).Error; err != nil {
		return nil, fmt.Errorf("ListHouseARUnits: %w", err)
	}
	return out, nil
}

// ListHouseARSchedules mengembalikan jadwal non-superseded unit-unit tersebut.
// Jadwal di sini hanya memasok TANGGAL JATUH TEMPO dan nomor invoice; berapa
// piutangnya sudah ditentukan buku besar (D-W8-5).
func (r *GORMRepository) ListHouseARSchedules(ctx context.Context, tenantID uint64, unitIDs []uint64) ([]HouseARSchedule, error) {
	if len(unitIDs) == 0 {
		return nil, nil
	}
	const q = `
SELECT
    ps.id                          AS id,
    ps.unit_id                     AS unit_id,
    ps.installment_number          AS installment_number,
    ps.type                        AS type,
    ps.due_date                    AS due_date,
    ps.amount                      AS amount,
    ps.paid_amount                 AS paid_amount,
    COALESCE(i.invoice_number, '') AS invoice_number
FROM payment_schedules ps
LEFT JOIN invoices i ON i.schedule_id = ps.id AND i.tenant_id = ps.tenant_id
WHERE ps.tenant_id = ? AND ps.unit_id IN (?) AND ps.status <> 'superseded'
ORDER BY ps.unit_id ASC, ps.due_date ASC, ps.id ASC`

	var out []HouseARSchedule
	if err := r.db.WithContext(ctx).Raw(q, tenantID, unitIDs).Scan(&out).Error; err != nil {
		return nil, fmt.Errorf("ListHouseARSchedules: %w", err)
	}
	return out, nil
}

// ListPreBASTSchedules mengembalikan jadwal milik kontrak yang unitnya BELUM
// diserahterimakan — komplemen persis dari ListHouseARUnits (W-8/P6).
//
// "Belum BAST" = tidak ada `sale_records` aktif untuk unit itu. Syaratnya ditulis
// sebagai NOT EXISTS terhadap tabel yang sama yang dipakai house AR, bukan sebagai
// pemeriksaan status unit: status unit adalah turunan yang bisa tertinggal,
// sedangkan `sale_records` adalah baris yang lahir bersama jurnal BAST-nya.
func (r *GORMRepository) ListPreBASTSchedules(ctx context.Context, tenantID uint64) ([]PreBASTSchedule, error) {
	const q = `
SELECT
    c.id                           AS contract_id,
    c.unit_id                      AS unit_id,
    COALESCE(u.code, '')           AS unit_code,
    COALESCE(NULLIF(c.buyer_name, ''), cu.name, '') AS buyer_name,
    COALESCE(cu.phone, '')         AS buyer_phone,
    c.gross_amount                 AS contract_value,
    COALESCE(c.scheme_state, '')   AS scheme_state,
    ps.id                          AS schedule_id,
    ps.installment_number          AS installment_number,
    ps.type                        AS type,
    ps.due_date                    AS due_date,
    ps.amount                      AS amount,
    ps.paid_amount                 AS paid_amount,
    ps.status                      AS status,
    COALESCE(i.invoice_number, '') AS invoice_number
FROM payment_schedules ps
JOIN sale_contracts c  ON c.id = ps.sale_contract_id AND c.tenant_id = ps.tenant_id
LEFT JOIN units u      ON u.id  = c.unit_id     AND u.tenant_id  = c.tenant_id
LEFT JOIN customers cu ON cu.id = c.customer_id AND cu.tenant_id = c.tenant_id
LEFT JOIN invoices i   ON i.schedule_id = ps.id AND i.tenant_id  = ps.tenant_id
WHERE ps.tenant_id = ?
  AND ps.status <> 'superseded'
  AND NOT EXISTS (
      SELECT 1 FROM sale_records sr
      WHERE sr.tenant_id = ps.tenant_id
        AND sr.unit_id   = ps.unit_id
        AND sr.cancelled_at IS NULL
  )
ORDER BY ps.unit_id ASC, ps.due_date ASC, ps.id ASC`

	var out []PreBASTSchedule
	if err := r.db.WithContext(ctx).Raw(q, tenantID).Scan(&out).Error; err != nil {
		return nil, fmt.Errorf("ListPreBASTSchedules: %w", err)
	}
	return out, nil
}

// ListOwnedJournalIDs mengembalikan id jurnal yang KEPEMILIKANNYA ada pada
// domain penjualan: jurnal pendapatan/HPP BAST, jurnal penerimaan termin, dan
// jurnal reklas piutang antar-state scheme (T-3).
//
// Kepemilikan ditentukan oleh DATA, bukan oleh label `journal_entries.source` —
// seluruh jurnal domain ini tercatat `source = "system"`, sama seperti jurnal
// pengakuan biaya realisasi. Yang membedakan hanyalah: id-nya tersimpan di baris
// milik paket ini. Dipakai dua kali dalam W-8, dan sengaja hanya ditulis sekali:
// rekonsiliasi GL ↔ sub-ledger (klaim atas mutasi akun kontrol) dan penjaga
// reverse (menolak pembalikan generik atas jurnal ber-sub-ledger).
func (r *GORMRepository) ListOwnedJournalIDs(ctx context.Context, tenantID uint64) ([]uint64, error) {
	const q = `
SELECT revenue_journal_id AS id FROM sale_records
  WHERE tenant_id = ? AND revenue_journal_id > 0
UNION
SELECT cogs_journal_id AS id FROM sale_records
  WHERE tenant_id = ? AND cogs_journal_id IS NOT NULL
UNION
SELECT journal_entry_id AS id FROM termin_payments
  WHERE tenant_id = ? AND journal_entry_id > 0
UNION
SELECT journal_entry_id AS id FROM contract_payment_events
  WHERE tenant_id = ? AND journal_entry_id IS NOT NULL
ORDER BY id ASC`

	var out []uint64
	if err := r.db.WithContext(ctx).Raw(q, tenantID, tenantID, tenantID, tenantID).Scan(&out).Error; err != nil {
		return nil, fmt.Errorf("ListOwnedJournalIDs: %w", err)
	}
	return out, nil
}

// FindJournalOwner menjawab apakah satu jurnal dimiliki domain penjualan —
// penjaga reverse R-1 (ledger.JournalOwnershipReader). nil = bukan milik kami.
//
// Jurnal yang dibalik ikut diperiksa lewat `reverses_id`: membalik SEBUAH
// PEMBALIK adalah cara paling halus untuk menggerakkan kembali akun kontrol
// tanpa melewati domain, dan hasilnya sama-sama membuat GL ≠ sub-ledger.
func (r *GORMRepository) FindJournalOwner(ctx context.Context, tenantID, journalID uint64) (*ledger.JournalOwnerInfo, error) {
	type hit struct{ Kind string }
	const q = `
SELECT k.kind FROM (
  SELECT 'bast' AS kind FROM sale_records
    WHERE tenant_id = ? AND (revenue_journal_id = ? OR cogs_journal_id = ?)
  UNION ALL
  SELECT 'termin' AS kind FROM termin_payments
    WHERE tenant_id = ? AND journal_entry_id = ?
  UNION ALL
  SELECT 'reclass' AS kind FROM contract_payment_events
    WHERE tenant_id = ? AND journal_entry_id = ?
) k LIMIT 1`

	target := journalID
	// Bila yang diminta adalah pembalik, yang menentukan kepemilikan adalah
	// jurnal ASALNYA.
	var orig struct{ ReversesID *uint64 }
	if err := r.db.WithContext(ctx).
		Raw(`SELECT reverses_id FROM journal_entries WHERE tenant_id = ? AND id = ?`, tenantID, journalID).
		Scan(&orig).Error; err != nil {
		return nil, fmt.Errorf("FindJournalOwner (asal): %w", err)
	}
	if orig.ReversesID != nil && *orig.ReversesID > 0 {
		target = *orig.ReversesID
	}

	var h hit
	err := r.db.WithContext(ctx).
		Raw(q, tenantID, target, target, tenantID, target, tenantID, target).
		Scan(&h).Error
	if err != nil {
		return nil, fmt.Errorf("FindJournalOwner: %w", err)
	}
	if h.Kind == "" {
		return nil, nil
	}
	return &ledger.JournalOwnerInfo{
		Owner: "penjualan",
		Hint:  journalOwnerHint(h.Kind),
	}, nil
}

// journalOwnerHint menerjemahkan jenis jurnal menjadi jalan keluar yang bisa
// ditempuh operator. Penolakan tanpa alternatif hanya akan dicari jalan
// pintasnya — biasanya jurnal manual yang justru merusak lebih banyak.
func journalOwnerHint(kind string) string {
	switch kind {
	case "bast":
		return "Batalkan penjualannya lewat Pembatalan Penjualan; pembalik pendapatan dan HPP terbit dari sana bersama pemulihan status unit."
	case "termin":
		return "Batalkan penerimaannya lewat Pembatalan Penjualan agar kwitansi, alokasi, dan piutangnya ikut dipulihkan."
	case "reclass":
		return "Reklas piutang mengikuti state kontrak; ubah state pembiayaannya lewat kontrak, jangan membalik jurnalnya langsung."
	}
	return "Gunakan jalur domain penjualan untuk membatalkannya."
}

func (r *GORMRepository) SaveScheduleItems(ctx context.Context, items []*PaymentSchedule) error {
	if len(items) == 0 {
		return nil
	}
	if err := r.db.WithContext(ctx).Create(&items).Error; err != nil {
		return fmt.Errorf("SaveScheduleItems: %w", err)
	}
	return nil
}

func (r *GORMRepository) FindScheduleByID(ctx context.Context, tenantID, id uint64) (*PaymentSchedule, error) {
	var s PaymentSchedule
	err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrScheduleNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("FindScheduleByID: %w", err)
	}
	return &s, nil
}

func (r *GORMRepository) UpdateScheduleStatus(ctx context.Context, tenantID, id uint64, status ScheduleStatus, terminPaymentID *uint64, receivedAt *time.Time) error {
	updates := map[string]interface{}{"status": string(status)}
	if terminPaymentID != nil {
		updates["termin_payment_id"] = *terminPaymentID
	}
	if receivedAt != nil {
		updates["received_at"] = receivedAt
	}
	res := r.db.WithContext(ctx).Model(&PaymentSchedule{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(updates)
	if res.Error != nil {
		return fmt.Errorf("UpdateScheduleStatus: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrScheduleNotFound
	}
	return nil
}

// ── FE-2 · Payment allocation writers ────────────────────────────────────────

// applyScheduleAllocationTx menulis SATU baris payment_allocations + memperbarui
// cache paid_amount/status cicilan MEMAKAI handle DB yang diberikan (bisa tx atau
// r.db). Guard #1: alokasi dan cache selalu ditulis bersama. Status hanya diubah
// menjadi 'received' saat cicilan lunas; partial tidak menyentuh status.
func applyScheduleAllocationTx(ctx context.Context, db *gorm.DB, tenantID uint64, in ScheduleAllocationInput) error {
	if err := insertScheduleAllocationRowTx(ctx, db, tenantID, in.TerminPaymentID, in.ScheduleID, in.Apply, in.CreatedBy); err != nil {
		return err
	}
	updates := map[string]interface{}{"paid_amount": in.NewPaid}
	if in.FullyPaid {
		updates["status"] = string(ScheduleStatusReceived)
		updates["termin_payment_id"] = in.TerminPaymentID
		updates["received_at"] = in.ReceivedAt
	}
	res := db.WithContext(ctx).Model(&PaymentSchedule{}).
		Where("id = ? AND tenant_id = ?", in.ScheduleID, tenantID).
		Updates(updates)
	if res.Error != nil {
		return fmt.Errorf("update cache paid_amount cicilan #%d: %w", in.ScheduleID, res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrScheduleNotFound
	}
	return nil
}

// insertScheduleAllocationRowTx menulis SATU baris payment_allocations tipe
// 'schedule' (tanpa menyentuh cache paid_amount). Dipakai P2 (via
// applyScheduleAllocationTx) dan P3 backfill (rekonstruksi sub-ledger).
func insertScheduleAllocationRowTx(ctx context.Context, db *gorm.DB, tenantID, terminID, scheduleID uint64, amount domain.Money, createdBy *uint64) error {
	sid := scheduleID
	tid := terminID
	alloc := &PaymentAllocation{
		TenantID:          tenantID,
		TerminPaymentID:   &tid,
		PaymentScheduleID: &sid,
		AllocationType:    AllocationTypeSchedule,
		Amount:            amount,
		CreatedBy:         createdBy,
	}
	if err := db.WithContext(ctx).Create(alloc).Error; err != nil {
		return fmt.Errorf("insert alokasi cicilan #%d: %w", scheduleID, err)
	}
	return nil
}

// insertBuyerCreditTx menulis baris sisa tak-berjadwal (schedule NULL) —
// buyer_credit atau direct, lihat BuyerCreditAllocationInput.Type.
func insertBuyerCreditTx(ctx context.Context, db *gorm.DB, tenantID uint64, in BuyerCreditAllocationInput) error {
	tid := in.TerminPaymentID
	allocType := in.Type
	if allocType == "" {
		allocType = AllocationTypeBuyerCredit
	}
	alloc := &PaymentAllocation{
		TenantID:        tenantID,
		TerminPaymentID: &tid,
		AllocationType:  allocType,
		Amount:          in.Amount,
		CreatedBy:       in.CreatedBy,
	}
	if err := db.WithContext(ctx).Create(alloc).Error; err != nil {
		return fmt.Errorf("insert alokasi saldo kredit: %w", err)
	}
	return nil
}

// ApplyScheduleAllocation (non-tx) — dipakai defaultCommitter. Produksi memakai
// CommitPayment (atomik); method ini menulis di luar transaksi commit.
func (r *GORMRepository) ApplyScheduleAllocation(ctx context.Context, tenantID uint64, in ScheduleAllocationInput) error {
	return applyScheduleAllocationTx(ctx, r.db, tenantID, in)
}

// InsertBuyerCreditAllocation (non-tx) — dipakai defaultCommitter.
func (r *GORMRepository) InsertBuyerCreditAllocation(ctx context.Context, tenantID uint64, in BuyerCreditAllocationInput) error {
	return insertBuyerCreditTx(ctx, r.db, tenantID, in)
}

// CommitPayment mempersist SATU penerimaan pembayaran secara ATOMIK dalam satu
// transaksi DB (guard #2/#4, keputusan #4): jurnal (create+post) → termin →
// alokasi sub-ledger + cache → saldo kredit → kwitansi → sinkron invoice. Jika
// langkah mana pun gagal, SELURUHNYA rollback — tidak ada jurnal tanpa alokasi.
// Pola tx-scoped mengikuti BASTAtomicWriter.Execute.
func (r *GORMRepository) CommitPayment(ctx context.Context, tenantID uint64, p PaymentCommitParams) (*PaymentCommitResult, error) {
	var result PaymentCommitResult

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. Rencana alokasi DI DALAM transaksi dengan cicilan DIKUNCI (FOR UPDATE).
		//    Mencegah race: dua pembayaran konkuren atas kontrak yang sama tidak
		//    bisa membaca paid_amount yang sama lalu double-apply — yang kedua
		//    menunggu lock, lalu me-replan atas nilai terbaru (waterfall benar).
		scheduleAllocs, buyerCredit, countsTowardPrice, err := planAllocationsLocked(ctx, tx, tenantID, p)
		if err != nil {
			return err
		}

		// 2. Jurnal (tx-scoped posting service) — Dr Bank / Cr kredit, balanced.
		txLedgerRepo := ledger.NewGORMRepository(tx)
		txPosting := ledger.NewPostingService(txLedgerRepo, txLedgerRepo).WithPeriodChecker(txLedgerRepo)
		entry, err := txPosting.Create(ctx, ledger.CreateJournalRequest{
			TenantID:    tenantID,
			Date:        p.Date,
			Description: p.Description,
			Lines:       toledgerLines(p.JournalLines),
		})
		if err != nil {
			return fmt.Errorf("buat jurnal termin: %w", err)
		}
		// Jurnalnya sengaja masih DRAFT di sini. Kwitansinya baru lahir di
		// langkah 6 (butuh termin, yang butuh id jurnal), dan W-3.5 menolak
		// jurnal kas yang selesai diposting tanpa dokumen. Posting dipindah ke
		// langkah 6c — sesudah buktinya ada dan tertaut.

		// 3. Termin.
		termin := &TerminPayment{
			TenantID:          tenantID,
			UnitID:            p.UnitID,
			ProjectID:         p.ProjectID,
			PhaseID:           p.PhaseID,
			Amount:            p.Amount,
			BankAccountCode:   p.BankAccountCode,
			Date:              p.Date,
			Description:       p.Description,
			JournalEntryID:    entry.ID,
			CreditAccountCode: p.CreditAccountCode,
			CreatedBy:         p.CreatedBy,
			IdempotencyKey:    p.IdempotencyKey,
			PaymentSource:     p.Source,
			FinancingSourceID: p.FinancingSourceID,
			Kind:              p.Kind,
			InstallmentNo:     p.InstallmentNo,
			CountsTowardPrice: countsTowardPrice, // R4 + land (000095): false hanya jika SELURUH alokasi ke land
		}
		if err := tx.WithContext(ctx).Create(termin).Error; err != nil {
			return fmt.Errorf("simpan termin: %w", err)
		}

		// 4. Alokasi ke cicilan + cache (guard #1: satu jalur).
		for _, a := range scheduleAllocs {
			if err := applyScheduleAllocationTx(ctx, tx, tenantID, ScheduleAllocationInput{
				ScheduleID:      a.ScheduleID,
				TerminPaymentID: termin.ID,
				Apply:           a.Apply,
				NewPaid:         a.NewPaid,
				FullyPaid:       a.FullyPaid,
				ReceivedAt:      p.Date,
				CreatedBy:       p.CreatedBy,
			}); err != nil {
				return err
			}
		}

		// 5. Sisa tak-berjadwal: saldo kredit buyer (pra-BAST) atau direct (pasca-BAST).
		if buyerCredit.GreaterThan(domain.Zero) {
			if err := insertBuyerCreditTx(ctx, tx, tenantID, BuyerCreditAllocationInput{
				TerminPaymentID: termin.ID,
				Amount:          buyerCredit,
				Type:            allocationTypeForUnapplied(p.CreditAccountCode),
				CreatedBy:       p.CreatedBy,
			}); err != nil {
				return err
			}
		}

		// 6. Kwitansi (idempoten) — DI DALAM transaksi (guard #2).
		//
		// W-3.2 (I-1/I-3): TIDAK LAGI opsional. Jurnal di langkah 2 mendebit
		// kas, jadi percabangan `if p.GenerateReceipt && r.receiptTx != nil`
		// berarti kas bisa bergerak tanpa bukti bernomor — lubang yang persis
		// hendak ditutup INV-DOC-1. Seam yang tidak terpasang sekarang menjadi
		// kegagalan wiring yang terlihat, bukan kwitansi yang diam-diam hilang.
		if r.receiptTx == nil {
			return fmt.Errorf("wiring tidak lengkap: penerimaan kas tanpa generator kwitansi")
		}
		var cb uint64
		if p.CreatedBy != nil {
			cb = *p.CreatedBy
		}
		num, id, rerr := r.receiptTx.GenerateReceiptInTx(ctx, tx, tenantID, cb, termin.ID, p.UnitID, p.Amount, p.BankAccountCode, p.Date, p.ReceiptNotes)
		if rerr != nil {
			return fmt.Errorf("buat kwitansi: %w", rerr)
		}
		result.ReceiptNumber = num
		result.ReceiptID = id

		// 6b. Tautkan kwitansi ke jurnal kasnya (INV-DOC-1, arah dua-arah).
		// Kwitansi lahir dari baris bisnis `receipts`, jadi dokumennya sudah
		// terbit di langkah 6 — yang kurang hanya tautannya ke jurnal.
		if _, derr := document.LinkJournalBySource(tx.WithContext(ctx), tenantID, "receipts", id, entry.ID); derr != nil {
			return fmt.Errorf("tautkan kwitansi ke jurnal: %w", derr)
		}

		// 6c. Baru sekarang kas boleh bergerak: buktinya sudah bernomor dan
		// tertaut. Spec kosong karena dokumennya sudah terpasang di 6b —
		// PostDraft tinggal memverifikasinya (W-3.5).
		if _, err := txPosting.PostDraft(ctx, tenantID, entry.ID, ledger.DocumentSpec{}); err != nil {
			return fmt.Errorf("posting jurnal termin: %w", err)
		}

		// 7. Sinkron status invoice untuk cicilan lunas — DI DALAM transaksi.
		if r.invoiceTx != nil {
			for _, a := range scheduleAllocs {
				if a.FullyPaid {
					if err := r.invoiceTx.MarkPaidByScheduleIDInTx(ctx, tx, tenantID, a.ScheduleID); err != nil {
						return fmt.Errorf("sinkron invoice cicilan #%d: %w", a.ScheduleID, err)
					}
				}
			}
		}

		result.TerminID = termin.ID
		result.JournalID = entry.ID
		result.AppliedSchedules = scheduleAllocs
		result.BuyerCredit = buyerCredit
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// planAllocationsLocked merencanakan alokasi dengan MENGUNCI cicilan terkait
// (SELECT ... FOR UPDATE) memakai transaksi tx. Urutan lock deterministik
// (installment, due_date, id) untuk mencegah deadlock antar transaksi.
//
// Return ketiga (countsTowardPrice) menjawab "apakah pembayaran ini mengurangi
// harga rumah" (R4/CountsTowardPrice) — DEFAULT true (perilaku lama, semua
// pembayaran harga), kecuali SELURUH alokasi menyasar baris ScheduleTypeLand
// (migrasi 000095) DAN tidak ada sisa saldo kredit. Pembayaran campuran
// unit+land dalam satu termin (unapplied>0 dari kontrak, atau salah satu
// alokasi menyasar cicilan rumah) tetap true — aproksimasi yang sama seperti
// perilaku pra-land untuk kombinasi lain, bukan regresi baru.
func planAllocationsLocked(ctx context.Context, tx *gorm.DB, tenantID uint64, p PaymentCommitParams) ([]ScheduleAllocation, domain.Money, bool, error) {
	switch {
	case p.ScheduleID != nil:
		var sch PaymentSchedule
		err := tx.WithContext(ctx).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ?", *p.ScheduleID, tenantID).
			First(&sch).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, domain.Zero, true, ErrScheduleNotFound
			}
			return nil, domain.Zero, true, fmt.Errorf("lock cicilan #%d: %w", *p.ScheduleID, err)
		}
		allocs, unapplied := planForSchedule(&sch, p.Amount)
		return allocs, unapplied, sch.Type != ScheduleTypeLand, nil
	case p.ContractID != nil:
		var schedules []*PaymentSchedule
		err := tx.WithContext(ctx).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ? AND sale_contract_id = ?", tenantID, *p.ContractID).
			Order("installment_number ASC, due_date ASC, id ASC").
			Find(&schedules).Error
		if err != nil {
			return nil, domain.Zero, true, fmt.Errorf("lock cicilan kontrak %d: %w", *p.ContractID, err)
		}
		allocs, unapplied := planForContract(schedules, p.Amount)
		typeByID := make(map[uint64]ScheduleType, len(schedules))
		for _, s := range schedules {
			typeByID[s.ID] = s.Type
		}
		countsTowardPrice := unapplied.GreaterThan(domain.Zero)
		for _, a := range allocs {
			if typeByID[a.ScheduleID] != ScheduleTypeLand {
				countsTowardPrice = true
				break
			}
		}
		return allocs, unapplied, countsTowardPrice, nil
	default:
		return nil, p.Amount, true, nil // tanpa kontrak → seluruhnya saldo kredit
	}
}

// FindTerminByIdempotencyKey mengembalikan termin dengan idempotency_key tsb.
func (r *GORMRepository) FindTerminByIdempotencyKey(ctx context.Context, tenantID uint64, key string) (*TerminPayment, error) {
	var t TerminPayment
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND idempotency_key = ?", tenantID, key).
		First(&t).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTerminNotFound
		}
		return nil, fmt.Errorf("FindTerminByIdempotencyKey: %w", err)
	}
	return &t, nil
}

func (r *GORMRepository) ListDueInPeriod(ctx context.Context, tenantID uint64, from, to time.Time) ([]*PaymentSchedule, error) {
	var items []*PaymentSchedule
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND due_date BETWEEN ? AND ?", tenantID, from, to).
		Order("due_date ASC").
		Find(&items).Error
	if err != nil {
		return nil, fmt.Errorf("ListDueInPeriod: %w", err)
	}
	return items, nil
}

func (r *GORMRepository) ListScheduledBefore(ctx context.Context, tenantID uint64, before time.Time) ([]*PaymentSchedule, error) {
	var items []*PaymentSchedule
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND status = ? AND due_date < ?", tenantID, string(ScheduleStatusScheduled), before).
		Order("due_date ASC").
		Find(&items).Error
	if err != nil {
		return nil, fmt.Errorf("ListScheduledBefore: %w", err)
	}
	return items, nil
}

// ── FE-2 · P3 — Backfill payment_allocations ─────────────────────────────────

// listUnitsWithTermins mengembalikan unit_id unik yang punya termin (tenant-scoped).
func (r *GORMRepository) listUnitsWithTermins(ctx context.Context, tenantID uint64) ([]uint64, error) {
	var ids []uint64
	err := r.db.WithContext(ctx).
		Table("termin_payments").
		Where("tenant_id = ?", tenantID).
		Distinct().
		Order("unit_id ASC").
		Pluck("unit_id", &ids).Error
	if err != nil {
		return nil, fmt.Errorf("listUnitsWithTermins: %w", err)
	}
	return ids, nil
}

// ListTerminsByUnit mengembalikan semua termin sebuah unit (tenant-scoped),
// urut tanggal. Dipakai handler listTermins + backfill.
func (r *GORMRepository) ListTerminsByUnit(ctx context.Context, tenantID, unitID uint64) ([]*TerminPayment, error) {
	var items []*TerminPayment
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND unit_id = ?", tenantID, unitID).
		Order("date ASC, id ASC").
		Find(&items).Error
	if err != nil {
		return nil, fmt.Errorf("listTerminsByUnit: %w", err)
	}
	return items, nil
}

// terminHasAllocation mengecek apakah sebuah termin sudah punya baris alokasi
// (idempotensi backfill: termin P2/yang sudah di-backfill dilewati).
func terminHasAllocation(ctx context.Context, db *gorm.DB, tenantID, terminID uint64) (bool, error) {
	var n int64
	err := db.WithContext(ctx).
		Model(&PaymentAllocation{}).
		Where("tenant_id = ? AND termin_payment_id = ?", tenantID, terminID).
		Count(&n).Error
	if err != nil {
		return false, fmt.Errorf("terminHasAllocation: %w", err)
	}
	return n > 0, nil
}

// BackfillAllocations merekonstruksi payment_allocations untuk termin lama secara
// BEST-EFFORT (replay waterfall per unit) + laporan rekonsiliasi. apply=false =
// dry-run (tidak menulis). Unit yang tidak rekonsiliasi DITANDAI, tidak ditulis
// (keputusan #1: no auto-merge). Idempoten: termin yang sudah punya alokasi dilewati.
func (r *GORMRepository) BackfillAllocations(ctx context.Context, tenantID uint64, apply bool) (*BackfillReport, error) {
	report := &BackfillReport{TenantID: tenantID, Apply: apply}

	units, err := r.listUnitsWithTermins(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	for _, unitID := range units {
		termins, err := r.ListTerminsByUnit(ctx, tenantID, unitID)
		if err != nil {
			return report, err
		}
		var contractID uint64
		var schedules []*PaymentSchedule
		if c, cerr := r.FindContractByUnitID(ctx, tenantID, unitID); cerr == nil {
			contractID = c.ID
			schedules, err = r.ListSchedulesByContract(ctx, tenantID, contractID)
			if err != nil {
				return report, err
			}
		}

		plan := planUnitBackfill(unitID, contractID, termins, schedules)
		report.UnitsProcessed++
		if !plan.reconciled {
			report.UnitsFlagged++
			report.Flags = append(report.Flags, BackfillFlag{
				UnitID: unitID, ContractID: contractID, Reason: plan.reason, Detail: plan.detail,
			})
			continue
		}
		report.UnitsReconciled++
		if err := r.applyUnitBackfill(ctx, tenantID, plan, apply, report); err != nil {
			return report, err
		}
	}
	return report, nil
}

// applyUnitBackfill menyisipkan baris alokasi hasil replay untuk satu unit
// (idempoten per termin). Dalam mode apply, semua tulisan satu unit dibungkus
// satu transaksi. TIDAK menyentuh paid_amount (target rekonsiliasi).
func (r *GORMRepository) applyUnitBackfill(ctx context.Context, tenantID uint64, plan unitBackfillPlan, apply bool, report *BackfillReport) error {
	var toWrite []terminAllocationPlan
	for _, tp := range plan.termins {
		has, err := terminHasAllocation(ctx, r.db, tenantID, tp.terminID)
		if err != nil {
			return err
		}
		if has {
			report.TerminsSkipped++
			continue
		}
		toWrite = append(toWrite, tp)
	}

	allocCount := 0
	for _, tp := range toWrite {
		allocCount += len(tp.schedule)
		if tp.buyerCredit.GreaterThan(domain.Zero) {
			allocCount++
		}
	}

	if apply && len(toWrite) > 0 {
		err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			for _, tp := range toWrite {
				for _, a := range tp.schedule {
					if err := insertScheduleAllocationRowTx(ctx, tx, tenantID, tp.terminID, a.ScheduleID, a.Apply, tp.createdBy); err != nil {
						return err
					}
				}
				if tp.buyerCredit.GreaterThan(domain.Zero) {
					if err := insertBuyerCreditTx(ctx, tx, tenantID, BuyerCreditAllocationInput{
						TerminPaymentID: tp.terminID, Amount: tp.buyerCredit, CreatedBy: tp.createdBy,
					}); err != nil {
						return err
					}
				}
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("tulis backfill unit %d: %w", plan.unitID, err)
		}
	}

	report.TerminsBackfilled += len(toWrite)
	report.AllocationsToWrite += allocCount
	return nil
}

// ── FE-2 · P4 — Reader queries (sub-ledger) ──────────────────────────────────

// allocationRow adalah hasil scan mentah join alokasi + termin + cicilan + kwitansi.
type allocationRow struct {
	ID                uint64       `gorm:"column:id"`
	TerminPaymentID   uint64       `gorm:"column:termin_payment_id"`
	PaymentScheduleID *uint64      `gorm:"column:payment_schedule_id"`
	AllocationType    string       `gorm:"column:allocation_type"`
	Amount            domain.Money `gorm:"column:amount"`
	TerminDate        time.Time    `gorm:"column:termin_date"`
	InstallmentNumber int          `gorm:"column:installment_number"`
	ScheduleType      string       `gorm:"column:schedule_type"`
	ReceiptNumber     string       `gorm:"column:receipt_number"`
}

const allocationSelectJoin = `
	SELECT pa.id, pa.termin_payment_id, pa.payment_schedule_id, pa.allocation_type, pa.amount,
	       tp.date AS termin_date,
	       COALESCE(ps.installment_number, 0)  AS installment_number,
	       COALESCE(ps.type, '')               AS schedule_type,
	       COALESCE(r.receipt_number, '')      AS receipt_number
	FROM payment_allocations pa
	JOIN termin_payments tp        ON tp.id = pa.termin_payment_id AND tp.tenant_id = pa.tenant_id
	LEFT JOIN payment_schedules ps ON ps.id = pa.payment_schedule_id AND ps.tenant_id = pa.tenant_id
	LEFT JOIN receipts r           ON r.termin_payment_id = pa.termin_payment_id AND r.tenant_id = pa.tenant_id`

func mapAllocationRows(rows []allocationRow) []AllocationView {
	out := make([]AllocationView, 0, len(rows))
	for _, rw := range rows {
		v := AllocationView{
			ID:                rw.ID,
			TerminPaymentID:   rw.TerminPaymentID,
			PaymentScheduleID: rw.PaymentScheduleID,
			AllocationType:    rw.AllocationType,
			Amount:            rw.Amount.String(),
			Label:             allocationLabel(AllocationType(rw.AllocationType), ScheduleType(rw.ScheduleType), rw.InstallmentNumber),
			TerminDate:        rw.TerminDate.Format("2006-01-02"),
			ReceiptNumber:     rw.ReceiptNumber,
		}
		if rw.PaymentScheduleID != nil {
			n := rw.InstallmentNumber
			v.InstallmentNumber = &n
		}
		out = append(out, v)
	}
	return out
}

// ListAllocationsByUnit mengembalikan alokasi seluruh termin sebuah unit,
// diperkaya (label cicilan, tanggal, nomor kwitansi). Tenant-scoped.
func (r *GORMRepository) ListAllocationsByUnit(ctx context.Context, tenantID, unitID uint64) ([]AllocationView, error) {
	var rows []allocationRow
	err := r.db.WithContext(ctx).
		Raw(allocationSelectJoin+`
			WHERE pa.tenant_id = ? AND tp.unit_id = ?
			ORDER BY tp.date ASC, pa.id ASC`, tenantID, unitID).
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("ListAllocationsByUnit: %w", err)
	}
	return mapAllocationRows(rows), nil
}

// ListAllocationsByTermin mengembalikan alokasi satu penerimaan (ke mana uangnya).
func (r *GORMRepository) ListAllocationsByTermin(ctx context.Context, tenantID, terminID uint64) ([]AllocationView, error) {
	var rows []allocationRow
	err := r.db.WithContext(ctx).
		Raw(allocationSelectJoin+`
			WHERE pa.tenant_id = ? AND pa.termin_payment_id = ?
			ORDER BY pa.id ASC`, tenantID, terminID).
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("ListAllocationsByTermin: %w", err)
	}
	return mapAllocationRows(rows), nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func toledgerLines(lines []JournalLineInput) []ledger.LineInput {
	out := make([]ledger.LineInput, len(lines))
	for i, l := range lines {
		out[i] = ledger.LineInput{
			AccountID:   l.AccountID,
			Debit:       l.Debit,
			Credit:      l.Credit,
			ProjectID:   l.ProjectID,
			PhaseID:     l.PhaseID,
			UnitID:      l.UnitID,
			Description: l.Description,
		}
	}
	return out
}
