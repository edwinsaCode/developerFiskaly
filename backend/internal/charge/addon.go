package charge

// W-13 — PRODUK TAMBAHAN (addon) menunjuk MASTER katalog produk.
//
// Koreksi klien: "Produk kelebihan tanah harus sinkron ke produk tambahan pas
// mau jual unit, bukan diperlakukan sama cara jualnya seperti unit rumah."
//
// Yang salah sebelumnya bukan hanya tampilan. Kelebihan tanah bisa lahir sebagai
// UNIT (papan penjualan, lifecycle, seolah punya HPP) sekaligus sebagai item
// addon berlabel ketik-bebas yang uangnya mendarat di 2-2400 Titipan Realisasi —
// akun KEWAJIBAN kepada pihak ketiga. Tapi kelebihan tanah bukan titipan siapa
// pun: ia barang yang perusahaan jual. Uangnya milik perusahaan. Selama ia
// duduk di 2-2400, penjualannya tidak pernah muncul di laba rugi.
//
// W-13 memberi addon pola yang SAMA dengan yang W-1 berikan kepada biaya
// realisasi: masterlah yang menentukan akun, bukan pengetik, dan akunnya
// DI-SNAPSHOT ke item supaya jurnal pembalik selalu menemukan alamat yang dulu
// benar-benar dipakai.
//
// Bedanya hanya pada arah uangnya, dan itu justru inti pemisahannya:
//
//	realisasi (K-1) → kas masuk: Cr akun titipan jenis biaya. Berhenti di situ.
//	                  TIDAK PERNAH menjadi pendapatan.
//	addon    (W-13) → kas masuk: Cr Uang Muka Penjualan 2-2000 (Invariant #7 —
//	                  penerimaan pra-pengakuan adalah kewajiban), lalu pada saat
//	                  pengakuan: Dr Uang Muka + Dr Piutang / Cr Pendapatan produk.
//	                  Bentuk jurnalnya persis Event 3 harga rumah, karena
//	                  perlakuannya memang sama: produk yang dijual.
//
// Satu seam yang menjawab "boleh menyentuh akun pendapatan?" tetap
// domain.BillingTreatment — addon lewat ProductCategory.BillingTreatment(),
// realisasi lewat RealizationChargePolicy. Tidak ada `if kind == "addon"` yang
// memutuskan perlakuan akuntansi di luar file ini.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

// accountCodeUangMuka — Uang Muka Penjualan, kewajiban penerimaan pra-pengakuan
// (Invariant #7). Kode yang sama dipakai `sale` untuk termin harga rumah; addon
// menumpang akun itu dengan sengaja, sebab keduanya adalah uang pembeli atas
// barang yang belum diakui penjualannya. Satu akun, satu arti.
const accountCodeUangMuka = "2-2000"

// ProductPolicyResolver me-resolve kebijakan produk sebuah kode katalog
// (diimplementasi project.GORMRepository dari master product_types) — seam yang
// sama persis dengan sale.ProductPolicyResolver, dipasang di main.
//
// FAIL-CLOSED: kode tak terdaftar / produk nonaktif / mapping akun kosong /
// error DB semuanya menghasilkan error. Tidak ada fallback diam-diam ke akun
// default, karena keliru di sini berarti pendapatan mendarat di akun orang lain.
type ProductPolicyResolver interface {
	ResolveProductPolicy(ctx context.Context, tenantID uint64, code string) (domain.ProductPolicy, error)
}

// SetProductPolicyResolver memasang katalog produk (wiring produksi).
func (s *Service) SetProductPolicyResolver(r ProductPolicyResolver) { s.products = r }

func normalizeProductCode(s string) string { return strings.TrimSpace(s) }

// addonAccounts adalah pasangan akun satu item produk tambahan: tempat uangnya
// MENDARAT saat diterima, dan tempat ia BERPINDAH saat diakui.
type addonAccounts struct {
	code     string // kode produk ter-normalisasi (snapshot tautan master)
	deposit  string // 2-2000 Uang Muka Penjualan — pra-pengakuan (Invariant #7)
	revenue  string // akun pendapatan produk dari master (COA-driven)
	category domain.ProductCategory
}

// resolveAddonProduct menegakkan master katalog produk untuk satu item addon.
//
// Produk berkategori PROPERTY ditolak di sini, dan penolakan itu bukan
// formalitas: rumah/ruko dijual sebagai UNIT karena unitlah yang menerima
// alokasi biaya dan melahirkan HPP. Menjual rumah sebagai baris addon akan
// mengakui pendapatan tanpa lawan HPP — laba yang terlalu besar, permanen, dan
// sulit dilacak kembali. Aturannya adalah cermin dari aturan di sisi unit
// (project.validateUnitType menolak non_property menjadi unit): satu produk,
// satu jalan jual.
func (s *Service) resolveAddonProduct(ctx context.Context, tenantID uint64, raw string) (addonAccounts, error) {
	code := normalizeProductCode(raw)
	if code == "" {
		return addonAccounts{}, ErrProductRequired
	}
	if s.products == nil {
		return addonAccounts{}, ErrProductResolverMissing
	}
	pol, err := s.products.ResolveProductPolicy(ctx, tenantID, code)
	if err != nil {
		return addonAccounts{}, fmt.Errorf("%w: %v", ErrProductUnresolved, err)
	}
	if !pol.Category.Valid() || pol.RevenueAccountCode == "" {
		return addonAccounts{}, fmt.Errorf("%w: kebijakan produk %q tidak lengkap", ErrProductUnresolved, code)
	}
	if pol.Category.ParticipatesInHPP() {
		return addonAccounts{}, fmt.Errorf("%w: %q", ErrProductNotAddon, code)
	}
	// Pertanyaan "boleh menyentuh akun pendapatan?" dijawab seam kanonik, bukan
	// oleh kategori yang ditafsir ulang di sini.
	if !pol.Category.BillingTreatment().RecognizesRevenue() {
		return addonAccounts{}, fmt.Errorf("%w: %q bukan produk berpendapatan", ErrProductNotAddon, code)
	}
	return addonAccounts{
		code:     pol.Code,
		deposit:  accountCodeUangMuka,
		revenue:  pol.RevenueAccountCode,
		category: pol.Category,
	}, nil
}

// ── Pengakuan pendapatan addon saat BAST ─────────────────────────────────────

// addonItemForRecognition adalah satu baris addon yang siap diakui, sudah
// dilengkapi berapa yang benar-benar sudah dibayar customer.
type addonItemForRecognition struct {
	item *ChargeItem
	paid domain.Money
}

// recognizeAddonRevenueInTx mengakui pendapatan seluruh grup addon sebuah unit,
// DI DALAM transaksi BAST. Bentuk jurnalnya sama dengan Event 3 harga rumah:
//
//	Dr Uang Muka Penjualan   sebesar yang SUDAH dibayar (kewajiban ditutup)
//	Dr Piutang Customer      sisanya (tagihan yang belum dibayar)
//	  Cr Pendapatan produk   nilai penuh addon
//
// Sisi debit memakai akun yang DI-SNAPSHOT di item, bukan konstanta hari ini:
// baris addon pra-W-13 dulu dikredit ke 2-2400, jadi merekalah yang harus
// didebit untuk baris-baris itu. Menutup kewajiban di akun yang tidak pernah
// dikredit hanya akan memindahkan salah saji, bukan memperbaikinya.
//
// Item yang akunnya tidak bisa ditentukan TIDAK dilewati diam-diam — ia
// menggagalkan transaksi. Melewatkannya berarti unit ter-BAST dengan pendapatan
// addon yang hilang tanpa jejak, dan tidak ada laporan yang akan menunjukkannya.
func (s *Service) recognizeAddonRevenueInTx(ctx context.Context, tx *gorm.DB, tenantID, unitID uint64, createdBy *uint64) error {
	var groups []*ChargeGroup
	if err := tx.WithContext(ctx).Clauses(gormLockingUpdate()).
		Where("tenant_id = ? AND unit_id = ? AND kind = ? AND status = ?",
			tenantID, unitID, string(KindAddon), string(GroupOpen)).
		Order("id ASC").Find(&groups).Error; err != nil {
		return fmt.Errorf("muat grup produk tambahan unit: %w", err)
	}
	for _, g := range groups {
		if err := s.recognizeAddonGroupInTx(ctx, tx, tenantID, g, createdBy); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) recognizeAddonGroupInTx(ctx context.Context, tx *gorm.DB, tenantID uint64, g *ChargeGroup, createdBy *uint64) error {
	items, err := listItemsDB(ctx, tx, tenantID, g.ID, true)
	if err != nil {
		return err
	}
	paid, err := paidByItemDB(ctx, tx, tenantID, g.ID)
	if err != nil {
		return err
	}

	pending := make([]addonItemForRecognition, 0, len(items))
	for _, it := range items {
		if it.Status != ItemOpen || it.Amount.IsZero() {
			continue
		}
		// RecognizedAmount menandai apa yang sudah diakui. Grup yang sudah
		// pernah lewat sini (BAST diulang / dua grup addon) tidak diakui dua
		// kali — idempoten by data, bukan by flag terpisah.
		if !it.Amount.Sub(it.RecognizedAmount).GreaterThan(domain.Zero) {
			continue
		}
		p := paid[it.ID]
		if p.GreaterThan(it.Amount) {
			p = it.Amount // kelebihan bayar bukan pendapatan — ia tetap kewajiban
		}
		pending = append(pending, addonItemForRecognition{item: it, paid: p})
	}
	if len(pending) == 0 {
		return nil
	}
	return s.postAddonRecognitionJournal(ctx, tx, tenantID, g, pending, createdBy)
}

// accountCodeAddonRevenueLegacy — akun pendapatan untuk baris addon HISTORIS
// (pra-W-13) yang tidak menunjuk produk apa pun, jadi akun pendapatannya tidak
// bisa ditanyakan ke master. Nilainya sama dengan
// project.DefaultNonPropertyRevenueAccount; disalin, bukan diimpor, agar charge
// tidak bergantung pada package project di luar seam ProductPolicyResolver.
//
// Ini fallback yang DISENGAJA dan sempit, bukan kelonggaran: item baru selalu
// ditolak lebih dulu oleh resolveAddonProduct. Alternatifnya adalah menggagalkan
// BAST unit-unit yang tagihan addon-nya dibuat sebelum W-13 — menghukum data
// lama karena aturan baru, padahal jurnalnya justru memperbaiki keadaan mereka:
// uang yang tersangkut di 2-2400 akhirnya sampai ke laba rugi.
const accountCodeAddonRevenueLegacy = "4-2000"

// addonRevenueCode membaca snapshot akun pendapatan item. Lihat konstanta di
// atas untuk alasan fallback-nya.
func addonRevenueCode(it *ChargeItem) string {
	if it.RevenueAccountCode != nil {
		if c := strings.TrimSpace(*it.RevenueAccountCode); c != "" {
			return c
		}
	}
	return accountCodeAddonRevenueLegacy
}

// postAddonRecognitionJournal menerbitkan SATU jurnal pengakuan untuk seluruh
// item addon sebuah grup, lalu menstempel pengakuannya di item + jejak audit.
//
// Sisi debit pecah per akun kewajiban yang DI-SNAPSHOT di item (2-2000 untuk
// item W-13, 2-2400 untuk baris historis) dan sisi kredit pecah per akun
// pendapatan produk — satu grup boleh memuat dua produk dengan akun berbeda.
// Piutangnya tetap satu baris 1-2000, karena piutang customer memang satu
// (keputusan klien D-3: tidak ada mekanisme piutang kedua).
func (s *Service) postAddonRecognitionJournal(ctx context.Context, tx *gorm.DB, tenantID uint64,
	g *ChargeGroup, pending []addonItemForRecognition, createdBy *uint64) error {

	perLiability := map[string]domain.Money{} // akun kewajiban → yang sudah dibayar
	perRevenue := map[string]domain.Money{}   // akun pendapatan → nilai penuh
	receivable := domain.Zero                 // sisa yang belum dibayar → 1-2000
	for _, p := range pending {
		liab := itemDepositCode(p.item)
		rev := addonRevenueCode(p.item)
		perLiability[liab] = moneyOrZero(perLiability[liab]).Add(p.paid)
		perRevenue[rev] = moneyOrZero(perRevenue[rev]).Add(p.item.Amount)
		receivable = receivable.Add(p.item.Amount.Sub(p.paid))
	}

	u, err := findUnitRefDB(ctx, tx, tenantID, g.UnitID)
	if err != nil {
		return err
	}
	pid, uid := u.ProjectID, g.UnitID
	dim := func(desc string, accID uint64) ledger.LineInput {
		return ledger.LineInput{AccountID: accID, ProjectID: &pid, PhaseID: u.PhaseID, UnitID: &uid,
			Description: desc}
	}

	lines := make([]ledger.LineInput, 0, len(perLiability)+len(perRevenue)+1)
	for _, p := range sortedDepositAmounts(perLiability) {
		accID, err := depositAccountIDByCode(ctx, tx, tenantID, p.AccountCode)
		if err != nil {
			return err
		}
		l := dim("Penutupan kewajiban penerimaan di muka produk tambahan", accID)
		l.Debit = p.Amount
		lines = append(lines, l)
	}
	if receivable.GreaterThan(domain.Zero) {
		arID, err := accountIDByCode(ctx, tx, tenantID, AccountCodePiutangCustomer)
		if err != nil || arID == 0 {
			return fmt.Errorf("akun %s tidak ditemukan di COA: %w", AccountCodePiutangCustomer, err)
		}
		l := dim("Piutang produk tambahan (sisa belum dibayar)", arID)
		l.Debit = receivable
		lines = append(lines, l)
	}
	for _, p := range sortedDepositAmounts(perRevenue) {
		accID, err := accountIDByCode(ctx, tx, tenantID, p.AccountCode)
		if err != nil || accID == 0 {
			// Fail-closed: pendapatan yang tidak punya alamat TIDAK boleh
			// dibuang ke akun lain diam-diam.
			return fmt.Errorf("%w: akun pendapatan %s tidak ada/aktif di COA",
				ErrProductUnresolved, p.AccountCode)
		}
		l := dim("Pendapatan produk tambahan", accID)
		l.Credit = p.Amount
		lines = append(lines, l)
	}

	at := time.Now()
	desc := fmt.Sprintf("Pengakuan pendapatan produk tambahan — %s (grup #%d)", g.Label, g.ID)
	journalID, err := postJournalTx(ctx, tx, tenantID, at, desc, createdBy, lines)
	if err != nil {
		return err
	}

	for _, p := range pending {
		updates := map[string]any{"recognized_amount": p.item.Amount}
		if p.item.RecognizedAt == nil {
			updates["recognized_at"] = at
		}
		// Aging butuh tanggal. Item addon jarang punya jatuh tempo sendiri;
		// tanpa fallback ini piutangnya ada di buku besar tapi hilang dari
		// laporan umur piutang (filternya tepat pada kolom ini).
		if p.item.DueDate == nil && p.item.RecognizedDueDate == nil {
			updates["recognized_due_date"] = at
		}
		if err := tx.WithContext(ctx).Model(&ChargeItem{}).
			Where("id = ? AND tenant_id = ?", p.item.ID, tenantID).
			Updates(updates).Error; err != nil {
			return fmt.Errorf("stempel pengakuan produk tambahan: %w", err)
		}
		delta := p.item.Amount.Sub(p.paid)
		if delta.IsZero() {
			continue
		}
		if err := recordRecognitionInTx(ctx, tx, tenantID, g, p.item, delta,
			ReasonRecAddonBAST, &journalID, nil, createdBy); err != nil {
			return err
		}
	}
	return nil
}
