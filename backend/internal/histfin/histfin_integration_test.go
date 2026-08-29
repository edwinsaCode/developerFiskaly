//go:build integration

package histfin_test

// W-6 — SNAPSHOT KEUANGAN HISTORIS.
//
// Yang dijaga suite ini bukan "apakah CRUD-nya jalan", melainkan tiga janji yang
// kalau dilanggar akan merusak pembukuan berjalan:
//
//	1. Snapshot TIDAK PERNAH menjadi jurnal. Seluruh siklus hidup (buat → isi →
//	   final → buka → isi lagi) tidak boleh menambah satu baris pun di
//	   journal_entries / journal_lines.
//	2. Tiap tahun berdiri sendiri. Mengubah 2025 tidak menggeser 2024 sedikit pun.
//	3. Tahun yang bukunya sudah hidup di sistem ini menolak snapshot manual —
//	   laporannya dihasilkan dari buku besar.

import (
	"context"
	"os"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"esaproperti/internal/domain"
	"esaproperti/internal/histfin"
	"esaproperti/internal/ledger"
)

const (
	hfTenant      uint64 = 9_900_681
	hfOtherTenant uint64 = 9_900_682
)

type hfEnv struct {
	db  *gorm.DB
	svc *histfin.Service
	ctx context.Context
}

func hfSetup(t *testing.T) *hfEnv {
	t.Helper()
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN tidak di-set — lewati")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("koneksi DB: %v", err)
	}
	ctx := context.Background()
	for _, tenant := range []uint64{hfTenant, hfOtherTenant} {
		for _, tbl := range []string{
			"financial_snapshot_audits", "financial_snapshot_lines", "financial_snapshots",
			"journal_lines", "journal_entries", "accounts",
		} {
			if err := db.Exec("DELETE FROM "+tbl+" WHERE tenant_id = ?", tenant).Error; err != nil {
				t.Fatalf("cleanup %s: %v", tbl, err)
			}
		}
		if err := ledger.SeedCOA(ctx, db, tenant); err != nil {
			t.Fatalf("seed COA: %v", err)
		}
		db.Exec(`INSERT IGNORE INTO tenants (id, name) VALUES (?, 'HF Test')`, tenant)
	}
	return &hfEnv{db: db, svc: histfin.NewService(db), ctx: ctx}
}

// accID mengambil id akun dari COA tenant. Snapshot memang memakai bagan akun
// yang sudah ada — tidak ada daftar akun khusus historis.
func (e *hfEnv) accID(t *testing.T, tenant uint64, code string) uint64 {
	t.Helper()
	var id uint64
	if err := e.db.Raw(`SELECT id FROM accounts WHERE tenant_id = ? AND code = ?`, tenant, code).
		Scan(&id).Error; err != nil || id == 0 {
		t.Fatalf("akun %s tidak ada di COA tenant %d (err=%v)", code, tenant, err)
	}
	return id
}

// journalCount menghitung SELURUH jurnal tenant — angka inilah yang harus tetap
// nol sepanjang hidup snapshot.
func (e *hfEnv) journalCount(t *testing.T, tenant uint64) int64 {
	t.Helper()
	var n int64
	if err := e.db.Raw(`SELECT COUNT(*) FROM journal_entries WHERE tenant_id = ?`, tenant).
		Scan(&n).Error; err != nil {
		t.Fatalf("hitung jurnal: %v", err)
	}
	return n
}

func (e *hfEnv) lineCount(t *testing.T, tenant uint64) int64 {
	t.Helper()
	var n int64
	if err := e.db.Raw(`SELECT COUNT(*) FROM journal_lines WHERE tenant_id = ?`, tenant).
		Scan(&n).Error; err != nil {
		t.Fatalf("hitung baris jurnal: %v", err)
	}
	return n
}

// seedNeraca2024 mengisi satu tahun yang seimbang: aset 500jt = utang 120jt +
// modal 300jt + laba 80jt.
func (e *hfEnv) balancedLines(t *testing.T, tenant uint64) []histfin.LineInput {
	t.Helper()
	return []histfin.LineInput{
		{AccountID: e.accID(t, tenant, "1-1100"), Amount: domain.MustParse("500000000"), SortOrder: 1},
		{AccountID: e.accID(t, tenant, "2-1000"), Amount: domain.MustParse("120000000"), SortOrder: 1},
		{AccountID: e.accID(t, tenant, "3-1000"), Amount: domain.MustParse("300000000"), SortOrder: 1},
		{AccountID: e.accID(t, tenant, "4-1000"), Amount: domain.MustParse("200000000"), SortOrder: 1},
		{AccountID: e.accID(t, tenant, "5-1000"), Amount: domain.MustParse("120000000"), SortOrder: 1},
	}
}

// ── 1. Snapshot tidak pernah menjadi jurnal ──────────────────────────────────

func TestIntegration_W6_SnapshotTidakMenyentuhLedger(t *testing.T) {
	e := hfSetup(t)
	if got := e.journalCount(t, hfTenant); got != 0 {
		t.Fatalf("prasyarat: tenant harus mulai tanpa jurnal, ada %d", got)
	}

	if _, err := e.svc.Create(e.ctx, hfTenant, 2023, nil); err != nil {
		t.Fatalf("create 2023: %v", err)
	}
	if _, err := e.svc.Save(e.ctx, hfTenant, 2023, histfin.SaveRequest{
		Notes: "Audited FY2023", Lines: e.balancedLines(t, hfTenant),
	}); err != nil {
		t.Fatalf("save 2023: %v", err)
	}
	if _, err := e.svc.Finalize(e.ctx, hfTenant, 2023, nil); err != nil {
		t.Fatalf("finalize 2023: %v", err)
	}
	if _, err := e.svc.Reopen(e.ctx, hfTenant, 2023, "koreksi angka persediaan hasil audit", nil); err != nil {
		t.Fatalf("reopen 2023: %v", err)
	}
	if _, err := e.svc.Save(e.ctx, hfTenant, 2023, histfin.SaveRequest{
		Lines: e.balancedLines(t, hfTenant),
	}); err != nil {
		t.Fatalf("save ulang 2023: %v", err)
	}

	if got := e.journalCount(t, hfTenant); got != 0 {
		t.Fatalf("snapshot membuat %d jurnal — W-6 wajib nol", got)
	}
	if got := e.lineCount(t, hfTenant); got != 0 {
		t.Fatalf("snapshot membuat %d baris jurnal — W-6 wajib nol", got)
	}
}

// ── 2. Tiap tahun berdiri sendiri ────────────────────────────────────────────

func TestIntegration_W6_TahunIndependen(t *testing.T) {
	e := hfSetup(t)
	kas := e.accID(t, hfTenant, "1-1100")
	modal := e.accID(t, hfTenant, "3-1000")

	write := func(year int, amount string) {
		if _, err := e.svc.Create(e.ctx, hfTenant, year, nil); err != nil {
			t.Fatalf("create %d: %v", year, err)
		}
		if _, err := e.svc.Save(e.ctx, hfTenant, year, histfin.SaveRequest{Lines: []histfin.LineInput{
			{AccountID: kas, Amount: domain.MustParse(amount)},
			{AccountID: modal, Amount: domain.MustParse(amount)},
		}}); err != nil {
			t.Fatalf("save %d: %v", year, err)
		}
	}
	write(2023, "100000000")
	write(2024, "250000000")
	write(2025, "400000000")

	// Mengubah 2025 tidak boleh menggeser tahun lain.
	if _, err := e.svc.Save(e.ctx, hfTenant, 2025, histfin.SaveRequest{Lines: []histfin.LineInput{
		{AccountID: kas, Amount: domain.MustParse("999000000")},
		{AccountID: modal, Amount: domain.MustParse("999000000")},
	}}); err != nil {
		t.Fatalf("ubah 2025: %v", err)
	}

	want := map[int]string{2023: "100000000", 2024: "250000000", 2025: "999000000"}
	for year, aset := range want {
		rep, err := e.svc.Neraca(e.ctx, hfTenant, year)
		if err != nil {
			t.Fatalf("neraca %d: %v", year, err)
		}
		if rep.TotalAset != aset {
			t.Errorf("total aset %d = %s, mau %s", year, rep.TotalAset, aset)
		}
		if !rep.IsBalanced {
			t.Errorf("neraca %d tidak seimbang", year)
		}
	}

	years, err := e.svc.ListYears(e.ctx, hfTenant)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(years) != 3 || years[0].FiscalYear != 2025 {
		t.Fatalf("daftar tahun salah (harus terbaru dulu): %+v", years)
	}
}

// ── 3. Tahun yang sudah punya jurnal menolak snapshot ────────────────────────

func TestIntegration_W6_TahunBerjurnalDitolak(t *testing.T) {
	e := hfSetup(t)
	year := time.Now().Year() - 1

	// Jurnal saldo awal TIDAK menghalangi: ia menyatakan posisi pembuka, bukan
	// aktivitas tahun itu.
	e.postJournal(t, hfTenant, year, "opening_balance")
	if _, err := e.svc.Create(e.ctx, hfTenant, year, nil); err != nil {
		t.Fatalf("saldo awal seharusnya tidak memblokir snapshot: %v", err)
	}
	if err := e.svc.Delete(e.ctx, hfTenant, year); err != nil {
		t.Fatalf("hapus draft: %v", err)
	}

	// Jurnal transaksi biasa MEMBLOKIR.
	e.postJournal(t, hfTenant, year, "manual")
	if _, err := e.svc.Create(e.ctx, hfTenant, year, nil); err != histfin.ErrYearHasJournals {
		t.Fatalf("create tahun berjurnal: err = %v, mau ErrYearHasJournals", err)
	}

	// Tahun depan juga ditolak.
	if _, err := e.svc.Create(e.ctx, hfTenant, time.Now().Year()+1, nil); err != histfin.ErrYearFuture {
		t.Fatalf("create tahun depan: err = %v, mau ErrYearFuture", err)
	}
}

func (e *hfEnv) postJournal(t *testing.T, tenant uint64, year int, source string) {
	t.Helper()
	err := e.db.Exec(`INSERT INTO journal_entries
		(tenant_id, date, description, source, posted_at, created_at, updated_at)
		VALUES (?,?,?,?,?,NOW(3),NOW(3))`,
		tenant, time.Date(year, 6, 30, 0, 0, 0, 0, time.UTC), "uji "+source, source, time.Now()).Error
	if err != nil {
		t.Fatalf("seed jurnal %s: %v", source, err)
	}
}

// ── 4. Status final mengunci angka ───────────────────────────────────────────

func TestIntegration_W6_FinalMengunciSampaiDibukaKembali(t *testing.T) {
	e := hfSetup(t)
	if _, err := e.svc.Create(e.ctx, hfTenant, 2022, nil); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Snapshot kosong tidak bisa difinalkan.
	if _, err := e.svc.Finalize(e.ctx, hfTenant, 2022, nil); err != histfin.ErrSnapshotEmpty {
		t.Fatalf("finalize kosong: err = %v, mau ErrSnapshotEmpty", err)
	}

	// Tidak seimbang juga ditolak.
	kas := e.accID(t, hfTenant, "1-1100")
	modal := e.accID(t, hfTenant, "3-1000")
	if _, err := e.svc.Save(e.ctx, hfTenant, 2022, histfin.SaveRequest{Lines: []histfin.LineInput{
		{AccountID: kas, Amount: domain.MustParse("100000000")},
		{AccountID: modal, Amount: domain.MustParse("90000000")},
	}}); err != nil {
		t.Fatalf("save timpang: %v", err)
	}
	if _, err := e.svc.Finalize(e.ctx, hfTenant, 2022, nil); err != histfin.ErrNotBalanced {
		t.Fatalf("finalize timpang: err = %v, mau ErrNotBalanced", err)
	}

	if _, err := e.svc.Save(e.ctx, hfTenant, 2022, histfin.SaveRequest{Lines: []histfin.LineInput{
		{AccountID: kas, Amount: domain.MustParse("100000000")},
		{AccountID: modal, Amount: domain.MustParse("100000000")},
	}}); err != nil {
		t.Fatalf("save seimbang: %v", err)
	}
	fin, err := e.svc.Finalize(e.ctx, hfTenant, 2022, nil)
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if fin.Status != histfin.StatusFinal || fin.Revision != 1 || fin.FinalizedAt == nil {
		t.Fatalf("hasil finalize salah: %+v", fin.Snapshot)
	}

	// Final = terkunci.
	if _, err := e.svc.Save(e.ctx, hfTenant, 2022, histfin.SaveRequest{Lines: []histfin.LineInput{
		{AccountID: kas, Amount: domain.MustParse("1")},
	}}); err != histfin.ErrSnapshotFinal {
		t.Fatalf("save saat final: err = %v, mau ErrSnapshotFinal", err)
	}
	if err := e.svc.Delete(e.ctx, hfTenant, 2022); err != histfin.ErrSnapshotFinal {
		t.Fatalf("delete saat final: err = %v, mau ErrSnapshotFinal", err)
	}

	// Buka kembali wajib beralasan.
	if _, err := e.svc.Reopen(e.ctx, hfTenant, 2022, "typo", nil); err != histfin.ErrReasonRequired {
		t.Fatalf("reopen tanpa alasan memadai: err = %v", err)
	}
	if _, err := e.svc.Reopen(e.ctx, hfTenant, 2022, "koreksi nilai tanah hasil appraisal ulang", nil); err != nil {
		t.Fatalf("reopen: %v", err)
	}

	// Jejaknya lengkap dan hanya berisi yang benar-benar terjadi: created,
	// saved (timpang), saved (seimbang), finalized, reopened. Percobaan yang
	// DITOLAK tidak meninggalkan audit — jejaknya harus mencerminkan keadaan
	// snapshot, bukan riwayat klik.
	audits, err := e.svc.Audits(e.ctx, hfTenant, 2022)
	if err != nil {
		t.Fatalf("audits: %v", err)
	}
	if len(audits) != 5 {
		t.Fatalf("jumlah audit = %d, mau 5: %+v", len(audits), audits)
	}
	if audits[0].Event != histfin.EventReopened || audits[0].Reason == "" {
		t.Fatalf("audit terbaru harus reopened beralasan: %+v", audits[0])
	}
	// Revisi tetap 1 setelah dibuka — revisi menghitung berapa kali disahkan.
	if audits[0].Revision != 1 {
		t.Fatalf("revisi audit reopen = %d, mau 1", audits[0].Revision)
	}
}

// ── 5. Validasi baris ────────────────────────────────────────────────────────

func TestIntegration_W6_ValidasiBaris(t *testing.T) {
	e := hfSetup(t)
	if _, err := e.svc.Create(e.ctx, hfTenant, 2021, nil); err != nil {
		t.Fatalf("create: %v", err)
	}
	kas := e.accID(t, hfTenant, "1-1100")

	// Akun ganda.
	if _, err := e.svc.Save(e.ctx, hfTenant, 2021, histfin.SaveRequest{Lines: []histfin.LineInput{
		{AccountID: kas, Amount: domain.MustParse("1000")},
		{AccountID: kas, Amount: domain.MustParse("2000")},
	}}); err != histfin.ErrDuplicateAccount {
		t.Fatalf("akun ganda: err = %v, mau ErrDuplicateAccount", err)
	}

	// Akun milik tenant lain tidak terlihat.
	asing := e.accID(t, hfOtherTenant, "1-1100")
	if _, err := e.svc.Save(e.ctx, hfTenant, 2021, histfin.SaveRequest{Lines: []histfin.LineInput{
		{AccountID: asing, Amount: domain.MustParse("1000")},
	}}); err != histfin.ErrAccountNotFound {
		t.Fatalf("akun lintas tenant: err = %v, mau ErrAccountNotFound", err)
	}

	// Ikhtisar laba rugi adalah hasil hitung, bukan angka ketikan.
	ikhtisar := e.accID(t, hfTenant, ledger.AccountIncomeSummaryCode)
	if _, err := e.svc.Save(e.ctx, hfTenant, 2021, histfin.SaveRequest{Lines: []histfin.LineInput{
		{AccountID: ikhtisar, Amount: domain.MustParse("1000")},
	}}); err != histfin.ErrComputedAccount {
		t.Fatalf("akun ikhtisar: err = %v, mau ErrComputedAccount", err)
	}

	// Nilai nol dibuang; sisanya tersimpan.
	det, err := e.svc.Save(e.ctx, hfTenant, 2021, histfin.SaveRequest{Lines: []histfin.LineInput{
		{AccountID: kas, Amount: domain.MustParse("5000")},
		{AccountID: e.accID(t, hfTenant, "3-1000"), Amount: domain.Zero},
	}})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if len(det.Lines) != 1 || det.Lines[0].AccountCode != "1-1100" {
		t.Fatalf("baris nol seharusnya dibuang: %+v", det.Lines)
	}
	// Replace-all: simpan set kosong → snapshot bersih.
	det, err = e.svc.Save(e.ctx, hfTenant, 2021, histfin.SaveRequest{})
	if err != nil {
		t.Fatalf("save kosong: %v", err)
	}
	if len(det.Lines) != 0 {
		t.Fatalf("save replace-all seharusnya mengosongkan: %+v", det.Lines)
	}
}

// ── 6. Isolasi tenant ────────────────────────────────────────────────────────

func TestIntegration_W6_IsolasiTenant(t *testing.T) {
	e := hfSetup(t)
	if _, err := e.svc.Create(e.ctx, hfTenant, 2020, nil); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := e.svc.Save(e.ctx, hfTenant, 2020, histfin.SaveRequest{Lines: []histfin.LineInput{
		{AccountID: e.accID(t, hfTenant, "1-1100"), Amount: domain.MustParse("77000000")},
		{AccountID: e.accID(t, hfTenant, "3-1000"), Amount: domain.MustParse("77000000")},
	}}); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Tenant lain tidak melihat apa pun.
	if _, err := e.svc.Get(e.ctx, hfOtherTenant, 2020); err != histfin.ErrSnapshotNotFound {
		t.Fatalf("lintas tenant: err = %v, mau ErrSnapshotNotFound", err)
	}
	years, err := e.svc.ListYears(e.ctx, hfOtherTenant)
	if err != nil {
		t.Fatalf("list tenant lain: %v", err)
	}
	if len(years) != 0 {
		t.Fatalf("tenant lain melihat %d snapshot", len(years))
	}

	// Tahun yang sama boleh dipakai tenant lain, dengan angkanya sendiri.
	if _, err := e.svc.Create(e.ctx, hfOtherTenant, 2020, nil); err != nil {
		t.Fatalf("create tenant lain: %v", err)
	}
	rep, err := e.svc.Neraca(e.ctx, hfOtherTenant, 2020)
	if err != nil {
		t.Fatalf("neraca tenant lain: %v", err)
	}
	if rep.TotalAset != "0" {
		t.Fatalf("snapshot tenant lain bocor: total aset = %s", rep.TotalAset)
	}
}

// ── 7. Satu tahun hanya satu snapshot ────────────────────────────────────────

func TestIntegration_W6_TahunGandaDitolak(t *testing.T) {
	e := hfSetup(t)
	if _, err := e.svc.Create(e.ctx, hfTenant, 2019, nil); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := e.svc.Create(e.ctx, hfTenant, 2019, nil); err != histfin.ErrSnapshotExists {
		t.Fatalf("create ganda: err = %v, mau ErrSnapshotExists", err)
	}
}
