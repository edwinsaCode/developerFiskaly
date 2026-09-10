package ap

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/cost"
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

// Pengakuan kewajiban vendor: draft → posted → reversed.
//
// KENAPA ADA TAHAP DRAFT (skenario A). `cost_entries.journal_entry_id` NOT NULL
// dengan foreign key, jadi baris biaya tidak bisa lahir sebelum ada baris
// jurnal. Jurnal draft memenuhi syarat itu tanpa menghidupkan apa pun: seluruh
// pembaca laporan di repo ini posted-only, sehingga tagihan draft tidak muncul
// di buku besar, tidak menambah realisasi RAB, dan tidak menambah saldo hutang
// (INV-AP-9). Yang membuatnya "ada" hanyalah posting.

// InvoiceLineInput adalah satu baris biaya pada tagihan.
//
// ProjectID sengaja TIDAK ada di sini: cakupan proyek milik TAGIHAN, bukan milik
// baris. Satu tagihan vendor adalah satu dokumen dengan satu konteks; membiarkan
// tiap baris memilih proyeknya sendiri berarti satu lembar tagihan bisa
// mengkapitalisasi biaya ke dua proyek sekaligus, dan tidak ada satu pun laporan
// yang akan menampilkannya sebagai satu kejadian.
type InvoiceLineInput struct {
	// ProjectID diisi service dari tagihan — bukan dari pemanggil.
	ProjectID uint64
	UnitID    *uint64
	PhaseID   *uint64
	Category  domain.CostCategory
	CostTier  domain.CostTier
	// HardSubcategory (UAT 2026-09-07): produksi_subsidi|produksi_komersial|
	// sarana_prasarana|perizinan — hanya bermakna saat Category=Hard, dan
	// WAJIB diisi saat baris ini tidak ditautkan ke UnitID (tier=shared),
	// karena cost.Service.validate() menegakkan aturan yang sama persis untuk
	// baris AP seperti untuk Cost Entry langsung (satu mesin, dua pintu).
	HardSubcategory domain.ConstructionSubcategory
	Amount          domain.Money
	BudgetItemID    *uint64
	Description     string
}

// CreateInvoiceRequest adalah input pencatatan tagihan vendor.
type CreateInvoiceRequest struct {
	VendorID  uint64
	ProjectID uint64 // 0 = tagihan tanpa proyek (biaya overhead)

	InvoiceNumber string
	InvoiceDate   time.Time
	DueDate       time.Time

	// DPPAmount OPSIONAL. Bila diisi, ia adalah angka yang tertulis di tagihan
	// vendor dan WAJIB sama dengan Σ baris (INV-AP-8) — selisihnya ditolak, tidak
	// dibulatkan dan tidak dipilih salah satunya. Bila kosong, DPP diturunkan
	// dari baris.
	DPPAmount domain.Money
	// PPNAmount adalah nilai dari FAKTUR PAJAK vendor, bukan hasil kali tarif
	// (A-2). Sistem tidak menebak tarif maupun kualifikasinya.
	//
	// TODO(tax-advisor): apakah ada keadaan di mana PPN Masukan tidak dapat
	// dikreditkan sehingga harus dikapitalisasi ke biaya perolehan? Selama
	// belum terjawab, seluruh PPN Masukan diperlakukan sebagai aset 1-5100.
	PPNAmount         domain.Money
	FakturPajakNumber string

	RetentionAmount  domain.Money
	RetentionDueDate *time.Time
	AdvanceApplied   domain.Money

	Description string
	Lines       []InvoiceLineInput
	ActorID     *uint64
}

// PreviewResult adalah pratinjau tagihan: jurnal yang AKAN terbit, apa adanya.
type PreviewResult struct {
	DPPAmount       string        `json:"dpp_amount"`
	PPNAmount       string        `json:"ppn_amount"`
	RetentionAmount string        `json:"retention_amount"`
	AdvanceApplied  string        `json:"advance_applied"`
	PayableAmount   string        `json:"payable_amount"`
	Lines           []PreviewLine `json:"lines"`
}

// ── Pratinjau ────────────────────────────────────────────────────────────────

// PreviewInvoice menjalankan SELURUH validasi dan komposisi yang dijalankan
// RecordInvoice, lalu berhenti tepat sebelum menulis.
//
// Ia memanggil fungsi komposisi yang sama, bukan fungsi kembar yang "seharusnya"
// setara. Pratinjau yang bisa berbeda dari hasilnya lebih buruk daripada tidak
// ada pratinjau sama sekali: yang pertama membuat orang berhenti memeriksa.
func (s *Service) PreviewInvoice(ctx context.Context, tenantID uint64, req CreateInvoiceRequest) (*PreviewResult, error) {
	comp, _, err := s.prepare(ctx, s.repo, tenantID, req)
	if err != nil {
		return nil, err
	}
	return &PreviewResult{
		DPPAmount:       comp.amounts.DPP.String(),
		PPNAmount:       comp.amounts.PPN.String(),
		RetentionAmount: comp.amounts.Retention.String(),
		AdvanceApplied:  comp.amounts.Advance.String(),
		PayableAmount:   comp.payable.String(),
		Lines:           comp.previewLines(),
	}, nil
}

// ── Pencatatan (draft) ───────────────────────────────────────────────────────

// RecordInvoice mencatat tagihan sebagai DRAFT: jurnal draft + baris tagihan +
// baris biaya, dalam SATU transaksi.
//
// Kalau ketiganya tidak lahir bersama, yang mungkin tertinggal adalah jurnal
// draft yatim yang tidak bisa ditelusuri ke tagihan mana pun, atau baris biaya
// yang menunjuk jurnal yang tidak ada. Keduanya baru terlihat saat rekonsiliasi
// berikutnya — berbulan-bulan kemudian.
func (s *Service) RecordInvoice(ctx context.Context, tenantID uint64, req CreateInvoiceRequest) (*Invoice, error) {
	var out *Invoice
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := s.repo.WithTx(tx)

		comp, planned, err := s.prepare(ctx, repo, tenantID, req)
		if err != nil {
			return err
		}
		vendor, err := repo.FindVendor(ctx, tenantID, req.VendorID)
		if err != nil {
			return err
		}

		txLedger := ledger.NewGORMRepository(tx)
		posting := ledger.NewPostingService(txLedger, txLedger).WithPeriodChecker(txLedger)

		desc := invoiceDescription(vendor.Name, req.InvoiceNumber, req.Description)
		entry, err := posting.Create(ctx, ledger.CreateJournalRequest{
			TenantID:    tenantID,
			Date:        req.InvoiceDate,
			Description: desc,
			Reference:   req.InvoiceNumber,
			Source:      SourceAPInvoice,
			CreatedBy:   req.ActorID,
			Lines:       comp.ledgerLines(),
		})
		if err != nil {
			if errors.Is(err, ledger.ErrPeriodClosed) {
				return fmt.Errorf("%w: %v", ErrPeriodClosed, err)
			}
			return fmt.Errorf("buat jurnal pengakuan hutang: %w", err)
		}

		inv := &Invoice{
			TenantID:          tenantID,
			VendorID:          vendor.ID,
			ProjectID:         nullableID(req.ProjectID),
			InvoiceNumber:     strings.TrimSpace(req.InvoiceNumber),
			InvoiceDate:       req.InvoiceDate,
			DueDate:           req.DueDate,
			DPPAmount:         comp.amounts.DPP,
			PPNAmount:         comp.amounts.PPN,
			RetentionAmount:   comp.amounts.Retention,
			AdvanceApplied:    comp.amounts.Advance,
			PayableAmount:     comp.payable,
			FakturPajakNumber: strings.TrimSpace(req.FakturPajakNumber),
			RetentionDueDate:  req.RetentionDueDate,
			Status:            InvoiceDraft,
			JournalEntryID:    entry.ID,
			Description:       req.Description,
		}
		if req.ActorID != nil {
			inv.CreatedBy = *req.ActorID
		}
		if err := repo.CreateInvoice(ctx, inv); err != nil {
			return fmt.Errorf("simpan tagihan: %w", err)
		}

		// Baris biaya mendarat di cost_entries (D-4) — bukan di tabel milik AP.
		for _, pl := range planned {
			e := &cost.CostEntry{
				TenantID:        tenantID,
				ProjectID:       nullableID(pl.in.ProjectID),
				UnitID:          pl.in.UnitID,
				PhaseID:         pl.in.PhaseID,
				Category:        pl.in.Category,
				CostTier:        pl.plan.Tier,
				HardSubcategory: pl.in.HardSubcategory,
				Amount:          pl.in.Amount,
				PaymentMethod:  cost.PaymentMethodPayable,
				Date:           req.InvoiceDate,
				Vendor:         vendor.Name,
				Description:    pl.in.Description,
				JournalEntryID: entry.ID,
				BudgetItemID:   pl.in.BudgetItemID,
				VendorID:       &vendor.ID,
				APInvoiceID:    &inv.ID,
			}
			if err := repo.CreateCostEntry(ctx, e); err != nil {
				return fmt.Errorf("simpan baris biaya: %w", err)
			}
		}
		out = inv
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ── Posting ──────────────────────────────────────────────────────────────────

// PostInvoice menerbitkan kewajiban ke buku besar.
//
// Urutannya disengaja: kunci baris → gate approval → periode → post. Gate
// dijalankan SETELAH baris dikunci supaya dua permintaan post yang berbarengan
// tidak bisa sama-sama lolos, dan periode diperiksa lagi di sini (D-9) karena
// draft bisa saja dibuat sebelum periodenya ditutup.
func (s *Service) PostInvoice(ctx context.Context, tenantID, id uint64) (*Invoice, error) {
	var out *Invoice
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := s.repo.WithTx(tx)

		inv, err := repo.FindInvoice(ctx, tenantID, id, true)
		if err != nil {
			return err
		}
		switch inv.Status {
		case InvoicePosted:
			return ErrInvoiceNotDraft
		case InvoiceReversed:
			return ErrInvoiceReversed
		}

		// INV-AP-19 — gate OPT-IN. Tenant tanpa workflow aktif tidak terhalang.
		if s.gate != nil {
			if err := s.gate.RequireApproved(ctx, tenantID, inv.ID); err != nil {
				return err
			}
		}

		txLedger := ledger.NewGORMRepository(tx)
		closed, err := txLedger.IsPeriodClosed(ctx, tenantID, inv.InvoiceDate)
		if err != nil {
			return fmt.Errorf("cek periode: %w", err)
		}
		if closed {
			return fmt.Errorf("%w: %s", ErrPeriodClosed, inv.InvoiceDate.Format("2006-01"))
		}

		posting := ledger.NewPostingService(txLedger, txLedger).WithPeriodChecker(txLedger)
		if _, err := posting.Post(ctx, tenantID, inv.JournalEntryID); err != nil {
			return fmt.Errorf("posting jurnal pengakuan hutang: %w", err)
		}

		now := time.Now()
		if err := repo.UpdateInvoice(ctx, tenantID, inv.ID, map[string]any{
			"status":    InvoicePosted,
			"posted_at": now,
		}); err != nil {
			return err
		}
		inv.Status = InvoicePosted
		inv.PostedAt = &now
		out = inv
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ── Pembalikan ───────────────────────────────────────────────────────────────

// ReverseInvoice membalik pengakuan lewat jurnal pembalik (Invariant #5).
//
// INV-AP-6: ditolak bila tagihan masih punya alokasi pembayaran yang belum
// dibalik. Membalik pengakuan sementara pembayarannya masih berdiri akan
// meninggalkan kas yang keluar untuk kewajiban yang — menurut buku — tidak
// pernah ada.
//
// ASIMETRI YANG DISENGAJA (W-11b). Jurnalnya terbalik, jadi realisasi RAB
// per-PROYEK (yang membaca ledger) kembali turun. Realisasi per-ITEM RAB
// membaca Σ cost_entries.amount atas jurnal yang POSTED — dan jurnal asli tetap
// posted setelah dibalik, jadi baris biayanya MASIH terhitung di sana. Ini
// diketahui dan dibiarkan apa adanya sesuai keputusan W-11b; yang dilarang
// adalah menambalnya dengan kolom `reversed_at` di cost_entries, karena tambalan
// itu akan menjadi definisi kedua tentang "biaya yang berlaku".
func (s *Service) ReverseInvoice(ctx context.Context, tenantID, id uint64, reverseDate time.Time) (*Invoice, error) {
	if reverseDate.IsZero() {
		reverseDate = time.Now()
	}
	var out *Invoice
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := s.repo.WithTx(tx)

		inv, err := repo.FindInvoice(ctx, tenantID, id, true)
		if err != nil {
			return err
		}
		switch inv.Status {
		case InvoiceDraft:
			return ErrInvoiceNotPosted
		case InvoiceReversed:
			return ErrInvoiceReversed
		}

		n, err := repo.CountActiveAllocations(ctx, tenantID, inv.ID)
		if err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("%w: %d alokasi masih aktif", ErrHasAllocations, n)
		}

		txLedger := ledger.NewGORMRepository(tx)
		posting := ledger.NewPostingService(txLedger, txLedger).WithPeriodChecker(txLedger)
		rev, err := posting.Reverse(ctx, tenantID, inv.JournalEntryID, reverseDate)
		if err != nil {
			if errors.Is(err, ledger.ErrPeriodClosed) {
				return fmt.Errorf("%w: %v", ErrPeriodClosed, err)
			}
			return fmt.Errorf("balik jurnal pengakuan hutang: %w", err)
		}

		if err := repo.UpdateInvoice(ctx, tenantID, inv.ID, map[string]any{
			"status":                    InvoiceReversed,
			"reversal_journal_entry_id": rev.ID,
		}); err != nil {
			return err
		}
		inv.Status = InvoiceReversed
		inv.ReversalJournalEntryID = &rev.ID
		out = inv
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ── Validasi & komposisi bersama ─────────────────────────────────────────────

// prepare memvalidasi permintaan dan menyusun jurnalnya — tanpa menulis apa pun.
// Dipakai bersama oleh PreviewInvoice dan RecordInvoice.
func (s *Service) prepare(
	ctx context.Context,
	repo *Repository,
	tenantID uint64,
	req CreateInvoiceRequest,
) (*composition, []plannedLine, error) {
	if strings.TrimSpace(req.InvoiceNumber) == "" {
		return nil, nil, ErrInvoiceNumberReq
	}
	if req.InvoiceDate.IsZero() {
		return nil, nil, fmt.Errorf("tanggal tagihan wajib diisi")
	}
	if req.DueDate.IsZero() {
		return nil, nil, fmt.Errorf("tanggal jatuh tempo wajib diisi")
	}
	if req.DueDate.Before(truncDay(req.InvoiceDate)) {
		return nil, nil, ErrDueBeforeInvoice
	}
	if len(req.Lines) == 0 {
		return nil, nil, ErrNoLines
	}

	vendor, err := repo.FindVendor(ctx, tenantID, req.VendorID)
	if err != nil {
		return nil, nil, err
	}
	if !vendor.IsActive {
		return nil, nil, fmt.Errorf("%w: %s", ErrVendorInactive, vendor.Name)
	}

	// PPN-1/PPN-5: PPN Masukan hanya lahir dari vendor PKP, dan hanya bila ada
	// faktur pajaknya. Tanpa faktur, angka itu bukan kredit pajak — hanya
	// tambahan aset yang tidak punya dasar.
	if req.PPNAmount.GreaterThan(domain.Zero) {
		if !vendor.IsPKP {
			return nil, nil, fmt.Errorf("%w: %s", ErrPPNRequiresPKP, vendor.Name)
		}
		if strings.TrimSpace(req.FakturPajakNumber) == "" {
			return nil, nil, ErrFakturRequired
		}
	}

	// Retensi & kompensasi uang muka: komposisinya sudah lengkap dan teruji di
	// journal.go, tetapi jalur PELEPASAN retensi dan PEMBENTUKAN uang muka
	// belum ada. Menerimanya sekarang berarti melahirkan saldo yang tidak punya
	// cara sah untuk diselesaikan.
	if req.RetentionAmount.GreaterThan(domain.Zero) {
		return nil, nil, ErrRetentionNotEnabled
	}
	if req.AdvanceApplied.GreaterThan(domain.Zero) {
		return nil, nil, ErrAdvanceNotEnabled
	}

	// Seluruh aturan baris biaya berasal dari cost.PlanAPLine (A-1). Tidak ada
	// satu pun pemeriksaan tier/kategori/unit/RAB yang ditulis ulang di sini.
	planned := make([]plannedLine, 0, len(req.Lines))
	sum := domain.Zero
	for i, ln := range req.Lines {
		ln.ProjectID = req.ProjectID // cakupan proyek milik tagihan, bukan baris
		plan, err := s.planner.PlanAPLine(ctx, tenantID, cost.CreateCostEntryRequest{
			ProjectID:       ln.ProjectID,
			UnitID:          ln.UnitID,
			PhaseID:         ln.PhaseID,
			Category:        ln.Category,
			CostTier:        ln.CostTier,
			HardSubcategory: ln.HardSubcategory,
			Amount:          ln.Amount,
			Date:            req.InvoiceDate,
			Vendor:          vendor.Name,
			Description:     ln.Description,
			BudgetItemID:    ln.BudgetItemID,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("baris %d: %w", i+1, err)
		}
		if ln.Description == "" {
			ln.Description = plan.Description
		}
		planned = append(planned, plannedLine{in: ln, plan: plan})
		sum = sum.Add(ln.Amount)
	}

	// DPP: kalau pemanggil menyebutkan angkanya, ia harus cocok. Kalau tidak,
	// diturunkan. Yang tidak boleh terjadi adalah memilih salah satu diam-diam.
	dpp := req.DPPAmount
	if dpp.IsZero() {
		dpp = sum
	} else if !dpp.Equal(sum) {
		return nil, nil, fmt.Errorf("%w: Σ baris %s, DPP diminta %s", ErrLineSumMismatch, sum.String(), dpp.String())
	}

	comp, err := compose(ctx, repo, tenantID, Amounts{
		DPP:       dpp,
		PPN:       req.PPNAmount,
		Retention: req.RetentionAmount,
		Advance:   req.AdvanceApplied,
	}, planned)
	if err != nil {
		return nil, nil, err
	}
	return comp, planned, nil
}

func invoiceDescription(vendorName, invoiceNumber, note string) string {
	desc := fmt.Sprintf("Tagihan %s no. %s", vendorName, strings.TrimSpace(invoiceNumber))
	if n := strings.TrimSpace(note); n != "" {
		desc += " — " + n
	}
	return desc
}

func nullableID(id uint64) *uint64 {
	if id == 0 {
		return nil
	}
	v := id
	return &v
}

func truncDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}
