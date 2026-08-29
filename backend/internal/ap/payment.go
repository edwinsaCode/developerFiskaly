package ap

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/document"
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

// Pelunasan kewajiban vendor: kas keluar, kewajiban berkurang.
//
// SATU KALIMAT YANG MENJELASKAN SELURUH FILE INI:
//
//	Sisa tagihan TIDAK disimpan di mana pun. Ia dijumlahkan dari sub-ledger
//	alokasi setiap kali dibutuhkan (D-12) — dan penjumlahan itu hanya sah bila
//	baris tagihannya sedang dikunci.
//
// Kalimat kedua itulah yang membuat file ini panjang. Menjumlahkan alokasi tanpa
// mengunci tagihannya menghasilkan angka yang benar pada saat dibaca dan salah
// pada saat ditulis: dua pembayaran yang berjalan berbarengan sama-sama membaca
// "sisa 100 juta", keduanya lolos pemeriksaan, dan keduanya menghasilkan jurnal
// yang seimbang. Yang tertinggal adalah tagihan 100 juta yang dibayar 120 juta,
// tanpa satu pun error di mana pun. Karena itu SETIAP jalur di bawah — otomatis
// maupun manual — mengunci baris tagihannya lebih dulu (R-12).
//
// YANG SENGAJA BELUM ADA DI SINI (Tahap 4):
//   - Pembentukan uang muka vendor (`advance`) dan pelepasan retensi
//     (`retention`). Komposisi jurnalnya sudah lengkap di payment_journal.go dan
//     diuji di sana, tetapi PINTU MASUKNYA ditutup — sama seperti yang dilakukan
//     jalur pengakuan terhadap retensi & kompensasi uang muka. Membukanya
//     sekarang berarti melahirkan saldo 1-5300/2-1100 yang belum punya cara sah
//     untuk dikonsumsi.
//   - `allow_excess_as_advance` (D-18). Ia jalan keluar sah untuk kelebihan
//     bayar, tetapi muaranya uang muka vendor — yang pintunya baru saja ditutup.
//     Selama itu, kelebihan bayar SELALU ditolak.

// ── Input ─────────────────────────────────────────────────────────────────────

// AllocationInput adalah satu pasangan tagihan↔nominal yang diminta pemanggil.
//
// Amount boleh nol HANYA dalam mode otomatis (daftar alokasi kosong sama
// sekali). Nol dalam daftar eksplisit adalah baris yang tidak melakukan apa pun
// — dan baris yang tidak melakukan apa pun di dokumen pembayaran hampir selalu
// berarti operator salah mengisi, bukan bermaksud membayar nol.
type AllocationInput struct {
	InvoiceID uint64
	Amount    domain.Money
}

// CreatePaymentRequest adalah input satu pengeluaran kas ke vendor.
type CreatePaymentRequest struct {
	VendorID uint64
	// Kind: pada tahap ini hanya `invoice` yang diterima. Lihat catatan di
	// kepala file.
	Kind PaymentKind

	PaymentDate     time.Time
	Amount          domain.Money
	CashAccountCode string

	// Allocations kosong = MODE OTOMATIS: tagihan tertua dilunasi lebih dulu
	// (E.2). Terisi = MODE EKSPLISIT: Σ alokasi wajib sama dengan Amount.
	//
	// Keduanya melewati validasi dan penguncian yang sama persis. Mode otomatis
	// bukan jalur pintas — ia hanya menentukan bagaimana daftar alokasi disusun.
	Allocations []AllocationInput

	Description    string
	IdempotencyKey string
	ActorID        *uint64
}

// ── Output ────────────────────────────────────────────────────────────────────

// AllocationView adalah satu alokasi sebagaimana dibaca layar: sebelum, nilai,
// dan sesudah — supaya operator bisa memeriksa aritmetikanya sendiri.
type AllocationView struct {
	InvoiceID         uint64 `json:"invoice_id"`
	InvoiceNumber     string `json:"invoice_number"`
	DueDate           string `json:"due_date"`
	OutstandingBefore string `json:"outstanding_before"`
	Amount            string `json:"amount"`
	OutstandingAfter  string `json:"outstanding_after"`
}

// PaymentPreview adalah pembayaran yang AKAN terjadi, apa adanya.
type PaymentPreview struct {
	VendorID        uint64           `json:"vendor_id"`
	VendorName      string           `json:"vendor_name"`
	PaymentKind     PaymentKind      `json:"payment_kind"`
	PaymentDate     string           `json:"payment_date"`
	Amount          string           `json:"amount"`
	CashAccountCode string           `json:"cash_account_code"`
	CashAccountName string           `json:"cash_account_name"`
	Allocations     []AllocationView `json:"allocations"`
	Lines           []PreviewLine    `json:"lines"`
	// TotalOutstanding adalah Σ sisa SELURUH tagihan vendor yang bisa dibayar —
	// bukan hanya yang teralokasi. Angka ini yang membuat pratinjau bisa
	// menjawab "kenapa hanya segini yang terpakai".
	TotalOutstanding string `json:"total_outstanding"`
}

// PaymentResult adalah hasil pencatatan.
type PaymentResult struct {
	Payment        *Payment         `json:"payment"`
	Allocations    []AllocationView `json:"allocations"`
	JournalEntryID uint64           `json:"journal_entry_id"`
	DocumentNumber string           `json:"document_number"`
	// Replayed menandai jawaban yang berasal dari kunci idempotensi, bukan dari
	// pembayaran yang baru saja terjadi. Tanpa penanda ini, klien tidak bisa
	// membedakan "berhasil" dari "sudah pernah berhasil".
	Replayed bool `json:"replayed"`
}

// ── Pratinjau ─────────────────────────────────────────────────────────────────

// PreviewPayment menjalankan SELURUH validasi, penguncian, dan komposisi yang
// dijalankan RecordPayment, lalu berhenti tepat sebelum menulis.
//
// Ia berjalan di dalam transaksi yang di-ROLLBACK di ujungnya — bukan di luar
// transaksi. Alasannya sama dengan alasan RecordPayment mengunci: pratinjau yang
// membaca sisa tagihan tanpa kunci bisa menampilkan angka yang sudah kedaluwarsa
// pada detik ia ditampilkan. Kunci dilepas begitu transaksinya berakhir, jadi
// pratinjau tidak menahan apa pun lebih lama dari yang diperlukan.
func (s *Service) PreviewPayment(ctx context.Context, tenantID uint64, req CreatePaymentRequest) (*PaymentPreview, error) {
	var out *PaymentPreview
	errRollback := errors.New("pratinjau selesai")
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := s.repo.WithTx(tx)
		p, err := s.planPayment(ctx, repo, tenantID, req)
		if err != nil {
			return err
		}
		out = p.preview()
		// Sengaja gagal: seluruh kunci dilepas dan tidak satu baris pun tertulis.
		return errRollback
	})
	if err != nil && !errors.Is(err, errRollback) {
		return nil, err
	}
	if out == nil {
		return nil, fmt.Errorf("pratinjau pembayaran gagal tanpa sebab yang tercatat")
	}
	return out, nil
}

// ── Pencatatan ────────────────────────────────────────────────────────────────

// RecordPayment mencatat satu pengeluaran kas ke vendor: jurnal terposting +
// dokumen BKK + baris pembayaran + baris alokasi, dalam SATU transaksi.
//
// Urutannya disengaja dan tidak boleh ditukar:
//
//	idempotensi → kunci tagihan → hitung sisa → validasi → jurnal+BKK → sub-ledger
//
// Jurnal terbit SEBELUM sub-ledger karena `ap_payments.journal_entry_id` NOT
// NULL. Keduanya tetap satu transaksi, jadi tidak ada keadaan di mana kas sudah
// bergerak di buku besar sementara alokasinya belum ada — keadaan yang akan
// membuat tagihan tampak belum dibayar padahal uangnya sudah keluar.
func (s *Service) RecordPayment(ctx context.Context, tenantID uint64, req CreatePaymentRequest) (*PaymentResult, error) {
	// Idempotensi diperiksa lebih dulu di luar transaksi supaya klik ganda
	// mengembalikan hasil yang sama, bukan 500 dari unique key.
	if key := strings.TrimSpace(req.IdempotencyKey); key != "" {
		prev, err := s.repo.FindPaymentByIdemKey(ctx, tenantID, key)
		if err != nil {
			return nil, err
		}
		if prev != nil {
			return s.replayPayment(ctx, tenantID, prev, req)
		}
	}

	var out *PaymentResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := s.repo.WithTx(tx)

		plan, err := s.planPayment(ctx, repo, tenantID, req)
		if err != nil {
			return err
		}

		txLedger := ledger.NewGORMRepository(tx)
		posting := ledger.NewPostingService(txLedger, txLedger).WithPeriodChecker(txLedger)

		entry, err := posting.CreateAndPost(ctx, ledger.CreateJournalRequest{
			TenantID:    tenantID,
			Date:        plan.date,
			Description: paymentDescription(plan.vendor.Name, req.Description),
			Reference:   paymentReference(plan.plans),
			Source:      SourceAPPayment,
			CreatedBy:   req.ActorID,
			Lines:       plan.comp.ledgerLines(),
			// INV-DOC-1 / INV-AP-5: satu pergerakan kas = satu dokumen bernomor.
			// BKK terbit DI DALAM transaksi ini; bila penerbitannya gagal, jurnal
			// kasnya ikut batal — bukan terbit tanpa bukti.
			Document: ledger.DocumentSpec{TypeCode: ledger.DocCashOut},
		})
		if err != nil {
			if errors.Is(err, ledger.ErrPeriodClosed) {
				return fmt.Errorf("%w: %v", ErrPeriodClosed, err)
			}
			return fmt.Errorf("posting jurnal pembayaran: %w", err)
		}

		docNo := ""
		if doc, err := document.FindByJournal(tx.WithContext(ctx), tenantID, entry.ID); err != nil {
			return fmt.Errorf("baca dokumen pembayaran: %w", err)
		} else if doc != nil {
			docNo = doc.Number
		}

		pay := &Payment{
			TenantID:        tenantID,
			VendorID:        plan.vendor.ID,
			Kind:            plan.kind,
			PaymentDate:     plan.date,
			Amount:          plan.total,
			CashAccountCode: plan.comp.cash.Code,
			JournalEntryID:  entry.ID,
			DocumentNumber:  docNo,
			Description:     strings.TrimSpace(req.Description),
		}
		if key := strings.TrimSpace(req.IdempotencyKey); key != "" {
			pay.IdempotencyKey = &key
		}
		if req.ActorID != nil {
			pay.CreatedBy = *req.ActorID
		}
		if err := repo.CreatePayment(ctx, pay); err != nil {
			if isDuplicateKeyErr(err) {
				// Permintaan kembar yang tiba sebelum yang pertama commit. Yang
				// benar adalah membatalkan yang kedua — bukan membayar dua kali.
				return fmt.Errorf("%w: permintaan dengan kunci yang sama sedang diproses", ErrIdempotencyConflict)
			}
			return fmt.Errorf("simpan pembayaran: %w", err)
		}

		// Sub-ledger. Inilah satu-satunya yang membuat sisa tagihan berkurang;
		// tidak ada kolom pada Invoice yang ikut diubah (D-12).
		for _, p := range plan.plans {
			alloc := &Allocation{
				TenantID:  tenantID,
				PaymentID: &pay.ID,
				InvoiceID: p.invoice.ID,
				Type:      allocationTypeFor(plan.kind),
				Amount:    p.amount,
			}
			if err := repo.CreateAllocation(ctx, alloc); err != nil {
				return fmt.Errorf("simpan alokasi tagihan %d: %w", p.invoice.ID, err)
			}
		}

		out = &PaymentResult{
			Payment:        pay,
			Allocations:    plan.allocationViews(),
			JournalEntryID: entry.ID,
			DocumentNumber: docNo,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ── Pembalikan ────────────────────────────────────────────────────────────────

// ReversePayment membatalkan satu pembayaran lewat jurnal pembalik (Invariant #5).
//
// Yang TIDAK terjadi di sini, dan tidak boleh terjadi: jurnal asli tidak diedit,
// baris pembayaran tidak dihapus, baris alokasi tidak dihapus. Yang berubah
// hanyalah stempel `reversed_at` — dan karena seluruh penjumlahan sisa tagihan
// mengabaikan baris yang berstempel, sisa tagihan naik kembali dengan sendirinya
// tanpa satu pun angka yang ditulis ulang.
//
// Dokumen pembalik (JR) terbit otomatis lewat ledger.Reverse dan menunjuk balik
// ke BKK yang dibatalkan, sehingga "bukti mana membatalkan bukti mana" bisa
// dibaca tanpa mencocokkan nominal.
func (s *Service) ReversePayment(ctx context.Context, tenantID, id uint64, reverseDate time.Time) (*Payment, error) {
	if reverseDate.IsZero() {
		reverseDate = time.Now()
	}
	var out *Payment
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := s.repo.WithTx(tx)

		// R-12: baris pembayaran dikunci LEBIH DULU. Tanpa ini dua permintaan
		// pembalikan yang berbarengan sama-sama membaca `reversed_at IS NULL`,
		// dan keduanya menerbitkan jurnal pembalik — kas yang keluar sekali
		// kembali dua kali.
		pay, err := repo.FindPayment(ctx, tenantID, id, true)
		if err != nil {
			return err
		}
		if pay.ReversedAt != nil {
			return ErrPaymentReversed
		}

		// Tagihan yang terdampak ikut dikunci, dengan urutan ID menaik — urutan
		// yang sama dengan yang dipakai RecordPayment, sehingga tidak ada siklus
		// tunggu yang bisa terbentuk di antara keduanya.
		allocs, err := repo.ListAllocationsByPayment(ctx, tenantID, pay.ID)
		if err != nil {
			return err
		}
		ids := make([]uint64, 0, len(allocs))
		for _, a := range allocs {
			if a.ReversedAt == nil {
				ids = append(ids, a.InvoiceID)
			}
		}
		if _, err := repo.LockInvoices(ctx, tenantID, ids); err != nil {
			return err
		}

		txLedger := ledger.NewGORMRepository(tx)
		posting := ledger.NewPostingService(txLedger, txLedger).WithPeriodChecker(txLedger)
		rev, err := posting.Reverse(ctx, tenantID, pay.JournalEntryID, reverseDate)
		if err != nil {
			if errors.Is(err, ledger.ErrPeriodClosed) {
				return fmt.Errorf("%w: %v", ErrPeriodClosed, err)
			}
			return fmt.Errorf("balik jurnal pembayaran: %w", err)
		}

		now := time.Now()
		if _, err := repo.ReverseAllocationsOfPayment(ctx, tenantID, pay.ID, now); err != nil {
			return err
		}
		if err := repo.UpdatePayment(ctx, tenantID, pay.ID, map[string]any{"reversed_at": now}); err != nil {
			return err
		}
		pay.ReversedAt = &now
		out = pay
		_ = rev
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ── Perencanaan bersama (dipakai pratinjau DAN pencatatan) ───────────────────

// paymentPlan adalah pembayaran yang sudah tervalidasi sepenuhnya terhadap data
// yang SEDANG DIKUNCI. Tipe ini tidak bisa dibuat tanpa melewati planPayment —
// itu memang tujuannya.
type paymentPlan struct {
	vendor           *Vendor
	kind             PaymentKind
	date             time.Time
	total            domain.Money
	plans            []allocationPlan
	comp             *paymentComposition
	totalOutstanding domain.Money
}

// planPayment adalah SATU-SATUNYA tempat aturan pembayaran ditegakkan.
//
// WAJIB dipanggil di dalam transaksi: ia mengunci baris tagihan dan menghitung
// sisa di bawah kunci itu. Memanggilnya di luar transaksi menghasilkan angka
// yang benar sesaat dan salah saat dipakai.
func (s *Service) planPayment(
	ctx context.Context,
	repo *Repository,
	tenantID uint64,
	req CreatePaymentRequest,
) (*paymentPlan, error) {
	// ── Bentuk permintaan ───────────────────────────────────────────────────
	kind := req.Kind
	if kind == "" {
		kind = PaymentKindInvoice
	}
	if !kind.Valid() {
		return nil, fmt.Errorf("%w: %q", ErrPaymentKind, string(req.Kind))
	}
	// Pintu masuk uang muka & retensi ditutup pada tahap ini — lihat kepala file.
	switch kind {
	case PaymentKindAdvance:
		return nil, ErrAdvanceNotEnabled
	case PaymentKindRetention:
		return nil, ErrRetentionNotEnabled
	}
	if strings.TrimSpace(req.CashAccountCode) == "" {
		return nil, ErrCashAccount
	}
	if !req.Amount.GreaterThan(domain.Zero) {
		return nil, fmt.Errorf("%w: nilai pembayaran %s", ErrNegAmount, req.Amount.String())
	}
	if !req.Amount.IsWholeRupiah() {
		return nil, ErrAmountFrac
	}
	date := req.PaymentDate
	if date.IsZero() {
		date = time.Now()
	}
	date = truncDay(date)

	// Vendor yang sudah dinonaktifkan TETAP boleh dibayar. Menonaktifkan vendor
	// berarti "jangan bertransaksi baru", bukan "kewajiban yang sudah lahir
	// hangus" — menolak pembayarannya akan meninggalkan saldo 2-1000 yang tidak
	// punya cara sah untuk diselesaikan.
	vendor, err := repo.FindVendor(ctx, tenantID, req.VendorID)
	if err != nil {
		return nil, err
	}

	// ── Kunci dulu, hitung kemudian (R-12) ──────────────────────────────────
	locked, err := s.lockTargets(ctx, repo, tenantID, vendor.ID, req)
	if err != nil {
		return nil, err
	}
	if len(locked) == 0 {
		return nil, ErrNothingToPay
	}

	outstanding := make(map[uint64]domain.Money, len(locked))
	totalOutstanding := domain.Zero
	for _, inv := range locked {
		// Terkunci, bukan sekadar dibaca. Baris tagihannya sudah dikunci di atas,
		// tetapi sisa tagihan hidup di tabel LAIN — dan di REPEATABLE READ
		// pembacaan biasa atas tabel itu masih dilayani dari snapshot lama.
		// Lihat SumAllocationsLocked.
		paid, err := repo.SumAllocationsLocked(ctx, tenantID, inv.ID)
		if err != nil {
			return nil, err
		}
		left := inv.PayableAmount.Sub(paid)
		if left.IsNeg() {
			left = domain.Zero
		}
		outstanding[inv.ID] = left
		totalOutstanding = totalOutstanding.Add(left)
	}

	// ── Susun alokasi ───────────────────────────────────────────────────────
	var plans []allocationPlan
	if len(req.Allocations) == 0 {
		plans, err = allocateOldestFirst(locked, outstanding, req.Amount)
	} else {
		plans, err = allocateExplicit(locked, outstanding, req.Allocations, req.Amount)
	}
	if err != nil {
		return nil, err
	}

	// ── Komposisi jurnal ────────────────────────────────────────────────────
	comp, err := composePayment(ctx, repo, tenantID, kind, strings.TrimSpace(req.CashAccountCode), plans, req.Amount)
	if err != nil {
		return nil, err
	}

	return &paymentPlan{
		vendor:           vendor,
		kind:             kind,
		date:             date,
		total:            req.Amount,
		plans:            plans,
		comp:             comp,
		totalOutstanding: totalOutstanding,
	}, nil
}

// lockTargets mengunci baris tagihan yang akan disentuh, dalam urutan ID menaik.
//
// URUTANNYA YANG PENTING, bukan sekadar keberadaan kuncinya. Dua pembayaran yang
// menyentuh tagihan yang sama dalam urutan berbeda akan saling menunggu selamanya
// (deadlock). Urutan ID menaik dipakai di SELURUH paket ini — di sini dan di
// ReversePayment — sehingga siklus tunggu mustahil terbentuk.
func (s *Service) lockTargets(
	ctx context.Context,
	repo *Repository,
	tenantID, vendorID uint64,
	req CreatePaymentRequest,
) ([]*Invoice, error) {
	if len(req.Allocations) == 0 {
		// Mode otomatis: kandidat dibaca tanpa kunci hanya untuk mengetahui ID
		// mana yang perlu dikunci; SELURUH nilai yang dipakai perhitungan dibaca
		// ulang setelah kuncinya didapat.
		cands, err := repo.ListPayableInvoices(ctx, tenantID, vendorID)
		if err != nil {
			return nil, err
		}
		ids := make([]uint64, 0, len(cands))
		for _, c := range cands {
			ids = append(ids, c.ID)
		}
		if len(ids) == 0 {
			return nil, ErrNothingToPay
		}
		locked, err := repo.LockInvoices(ctx, tenantID, ids)
		if err != nil {
			return nil, err
		}
		// Keadaan dibaca ULANG dari baris yang sudah terkunci. Daftar kandidat di
		// atas berasal dari snapshot; sebuah tagihan bisa dibalik tepat setelah ia
		// terbaca dan sebelum ia terkunci. Berbeda dengan mode eksplisit, di sini
		// tagihan seperti itu DILEWATI, bukan menggagalkan seluruh pembayaran —
		// operator tidak pernah menyebut tagihan itu, jadi ia tidak salah apa pun.
		keep := locked[:0]
		for _, inv := range locked {
			if inv.VendorID == vendorID && inv.Status == InvoicePosted {
				keep = append(keep, inv)
			}
		}
		return keep, nil
	}

	seen := make(map[uint64]bool, len(req.Allocations))
	ids := make([]uint64, 0, len(req.Allocations))
	for _, a := range req.Allocations {
		if a.InvoiceID == 0 {
			return nil, fmt.Errorf("%w: invoice_id kosong", ErrInvoiceNotFound)
		}
		if seen[a.InvoiceID] {
			return nil, fmt.Errorf("%w: tagihan %d", ErrDuplicateAllocTgt, a.InvoiceID)
		}
		seen[a.InvoiceID] = true
		ids = append(ids, a.InvoiceID)
	}
	locked, err := repo.LockInvoices(ctx, tenantID, ids)
	if err != nil {
		return nil, err
	}
	// Tagihan yang tidak ditemukan di tenant ini TIDAK diabaikan diam-diam.
	// Mengabaikannya berarti pembayaran tetap terbit dengan alokasi yang lebih
	// sedikit dari yang diminta — dan selisihnya tidak akan pernah terlihat.
	if len(locked) != len(ids) {
		found := make(map[uint64]bool, len(locked))
		for _, inv := range locked {
			found[inv.ID] = true
		}
		for _, id := range ids {
			if !found[id] {
				return nil, fmt.Errorf("%w: tagihan %d", ErrInvoiceNotFound, id)
			}
		}
	}
	// Vendor & status diperiksa SETELAH dikunci: keduanya bisa berubah antara
	// pembacaan dan penguncian.
	for _, inv := range locked {
		if inv.VendorID != vendorID {
			return nil, fmt.Errorf("%w: tagihan %s", ErrInvoiceWrongVendor, inv.InvoiceNumber)
		}
		switch inv.Status {
		case InvoiceDraft:
			return nil, fmt.Errorf("%w: tagihan %s", ErrInvoiceNotPosted, inv.InvoiceNumber)
		case InvoiceReversed:
			return nil, fmt.Errorf("%w: tagihan %s", ErrInvoiceReversed, inv.InvoiceNumber)
		}
	}
	return locked, nil
}

// allocateOldestFirst membagi nilai pembayaran ke tagihan tertua lebih dulu (E.2).
//
// "Tertua" = jatuh tempo paling awal, lalu tanggal tagihan, lalu ID. Ketiganya
// dipakai berurutan supaya hasilnya deterministik: dua tagihan dengan jatuh
// tempo sama tidak boleh berpindah urutan dari satu pemanggilan ke pemanggilan
// berikutnya — pratinjau dan pencatatan harus menghasilkan alokasi yang sama.
func allocateOldestFirst(
	invoices []*Invoice,
	outstanding map[uint64]domain.Money,
	amount domain.Money,
) ([]allocationPlan, error) {
	ordered := make([]*Invoice, len(invoices))
	copy(ordered, invoices)
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if !a.DueDate.Equal(b.DueDate) {
			return a.DueDate.Before(b.DueDate)
		}
		if !a.InvoiceDate.Equal(b.InvoiceDate) {
			return a.InvoiceDate.Before(b.InvoiceDate)
		}
		return a.ID < b.ID
	})

	total := domain.Zero
	for _, inv := range ordered {
		total = total.Add(outstanding[inv.ID])
	}
	if total.IsZero() {
		return nil, ErrNothingToPay
	}
	if amount.GreaterThan(total) {
		return nil, &OverpaymentError{Outstanding: total, Requested: amount}
	}

	remaining := amount
	var plans []allocationPlan
	for _, inv := range ordered {
		if !remaining.GreaterThan(domain.Zero) {
			break
		}
		left := outstanding[inv.ID]
		if !left.GreaterThan(domain.Zero) {
			continue
		}
		take := left
		if remaining.LessThan(left) {
			take = remaining
		}
		plans = append(plans, allocationPlan{invoice: inv, outstandingBefore: left, amount: take})
		remaining = remaining.Sub(take)
	}
	if remaining.GreaterThan(domain.Zero) {
		// Tidak boleh terjadi: Σ sisa sudah diperiksa di atas. Kalau toh terjadi,
		// yang salah adalah aritmetikanya — dan pembayaran dengan sisa yang tidak
		// teralokasi adalah kas yang keluar tanpa kewajiban yang berkurang.
		return nil, fmt.Errorf("%w: %s tidak teralokasi", ErrAllocationSumMismatch, remaining.String())
	}
	return plans, nil
}

// allocateExplicit memakai daftar alokasi yang ditentukan operator.
//
// Σ-nya WAJIB sama dengan nilai pembayaran. Membiarkan selisihnya berarti kas
// yang keluar tidak sama dengan kewajiban yang berkurang — dan jurnalnya tetap
// seimbang, karena selisihnya cukup ditutup di baris kas.
func allocateExplicit(
	invoices []*Invoice,
	outstanding map[uint64]domain.Money,
	inputs []AllocationInput,
	amount domain.Money,
) ([]allocationPlan, error) {
	byID := make(map[uint64]*Invoice, len(invoices))
	for _, inv := range invoices {
		byID[inv.ID] = inv
	}

	sum := domain.Zero
	plans := make([]allocationPlan, 0, len(inputs))
	for _, in := range inputs {
		inv := byID[in.InvoiceID]
		if inv == nil {
			return nil, fmt.Errorf("%w: tagihan %d", ErrInvoiceNotFound, in.InvoiceID)
		}
		if !in.Amount.GreaterThan(domain.Zero) {
			return nil, fmt.Errorf("%w: tagihan %s", ErrAllocationZero, inv.InvoiceNumber)
		}
		left := outstanding[inv.ID]
		if in.Amount.GreaterThan(left) {
			return nil, &OverpaymentError{
				InvoiceID:     inv.ID,
				InvoiceNumber: inv.InvoiceNumber,
				Outstanding:   left,
				Requested:     in.Amount,
			}
		}
		plans = append(plans, allocationPlan{invoice: inv, outstandingBefore: left, amount: in.Amount})
		sum = sum.Add(in.Amount)
	}
	if !sum.Equal(amount) {
		return nil, fmt.Errorf("%w: Σ alokasi %s, nilai pembayaran %s",
			ErrAllocationSumMismatch, sum.String(), amount.String())
	}
	return plans, nil
}

// ── Idempotensi ───────────────────────────────────────────────────────────────

// replayPayment menjawab permintaan berkunci-sama tanpa membayar ulang.
//
// Kalau permintaannya BERBEDA, jawabannya bukan hasil lama dan bukan pembayaran
// baru — keduanya salah. Mengembalikan hasil lama menyembunyikan permintaan
// kedua; menjalankannya membayar dua kali. Yang benar adalah menolak (409).
func (s *Service) replayPayment(
	ctx context.Context,
	tenantID uint64,
	prev *Payment,
	req CreatePaymentRequest,
) (*PaymentResult, error) {
	kind := req.Kind
	if kind == "" {
		kind = PaymentKindInvoice
	}
	same := prev.VendorID == req.VendorID &&
		prev.Kind == kind &&
		prev.Amount.Equal(req.Amount) &&
		prev.CashAccountCode == strings.TrimSpace(req.CashAccountCode)
	if !same {
		return nil, fmt.Errorf("%w: kunci %q sudah dipakai pembayaran #%d",
			ErrIdempotencyConflict, strings.TrimSpace(req.IdempotencyKey), prev.ID)
	}

	allocs, err := s.repo.ListAllocationsByPayment(ctx, tenantID, prev.ID)
	if err != nil {
		return nil, err
	}
	views := make([]AllocationView, 0, len(allocs))
	for _, a := range allocs {
		v := AllocationView{InvoiceID: a.InvoiceID, Amount: a.Amount.String()}
		if inv, err := s.repo.FindInvoice(ctx, tenantID, a.InvoiceID, false); err == nil {
			v.InvoiceNumber = inv.InvoiceNumber
			v.DueDate = inv.DueDate.Format("2006-01-02")
		}
		views = append(views, v)
	}
	return &PaymentResult{
		Payment:        prev,
		Allocations:    views,
		JournalEntryID: prev.JournalEntryID,
		DocumentNumber: prev.DocumentNumber,
		Replayed:       true,
	}, nil
}

// ── Turunan tampilan ──────────────────────────────────────────────────────────

func (p *paymentPlan) allocationViews() []AllocationView {
	out := make([]AllocationView, 0, len(p.plans))
	for _, a := range p.plans {
		out = append(out, AllocationView{
			InvoiceID:         a.invoice.ID,
			InvoiceNumber:     a.invoice.InvoiceNumber,
			DueDate:           a.invoice.DueDate.Format("2006-01-02"),
			OutstandingBefore: a.outstandingBefore.String(),
			Amount:            a.amount.String(),
			OutstandingAfter:  a.outstandingBefore.Sub(a.amount).String(),
		})
	}
	return out
}

func (p *paymentPlan) preview() *PaymentPreview {
	return &PaymentPreview{
		VendorID:         p.vendor.ID,
		VendorName:       p.vendor.Name,
		PaymentKind:      p.kind,
		PaymentDate:      p.date.Format("2006-01-02"),
		Amount:           p.total.String(),
		CashAccountCode:  p.comp.cash.Code,
		CashAccountName:  p.comp.cash.Name,
		Allocations:      p.allocationViews(),
		Lines:            p.comp.previewLines(),
		TotalOutstanding: p.totalOutstanding.String(),
	}
}

// ── Kecil-kecil ───────────────────────────────────────────────────────────────

// allocationTypeFor memetakan jenis pembayaran ke jenis alokasi.
//
// Keduanya sengaja tipe yang berbeda walau nilainya bertumpang tindih:
// `advance` sebagai PaymentKind berarti membentuk uang muka, sementara `advance`
// sebagai AllocationType berarti mengonsumsinya. Menyatukan keduanya menjadi satu
// tipe akan membuat perbedaan itu hilang di tempat yang paling mudah salah.
func allocationTypeFor(kind PaymentKind) AllocationType {
	switch kind {
	case PaymentKindRetention:
		return AllocationRetention
	default:
		return AllocationInvoice
	}
}

func paymentDescription(vendorName, note string) string {
	desc := "Pembayaran ke " + vendorName
	if n := strings.TrimSpace(note); n != "" {
		desc += " — " + n
	}
	return desc
}

// paymentReference merangkum tagihan yang dilunasi supaya jurnalnya bisa
// ditelusuri ke dokumen vendor tanpa membuka sub-ledger.
func paymentReference(plans []allocationPlan) string {
	if len(plans) == 0 {
		return ""
	}
	if len(plans) == 1 {
		return trunc(plans[0].invoice.InvoiceNumber, 100)
	}
	return trunc(fmt.Sprintf("%s +%d tagihan", plans[0].invoice.InvoiceNumber, len(plans)-1), 100)
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// isDuplicateKeyErr mengenali pelanggaran unique key MySQL (1062).
func isDuplicateKeyErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	return strings.Contains(err.Error(), "1062") || strings.Contains(strings.ToLower(err.Error()), "duplicate entry")
}
