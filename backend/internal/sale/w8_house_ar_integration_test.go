//go:build integration

package sale_test

// W-8 — tie-out Piutang Harga Rumah (sub-ledger) ↔ Buku Besar, MySQL nyata.
//
// Test ini adalah bukti untuk G-1. Ia sengaja TIDAK memanggil query rekonsiliasi
// dengan data yang ditanam langsung ke tabel: setiap angka di bawah lahir dari
// jalur produksi yang sama dengan yang dipakai aplikasi (CreateContract →
// ReceivePayment → RecordBAST → ApplySchemeEvent), dengan wiring yang sama
// dengan cmd/api/main.go. Menanam baris `sale_records` sendiri akan menguji
// SQL-nya saja, bukan kebenaran akuntansinya.
//
// Yang dibuktikan:
//
//	A. Lifecycle in-house: pra-BAST bukan piutang (BD-1) → BAST melahirkan
//	   piutang DAN menggerakkan GL dengan angka yang sama → bayar sebagian →
//	   BAST ulang ditolak → pembayaran melebihi sisa ditolak tanpa sisa state →
//	   lunas → keduanya nol.
//	B. KPR: piutang duduk di akun kontrol BANK (1-2200) antara akad dan
//	   pencairan (T-3), tidak muncul di daftar Piutang Customer, tetap ikut
//	   tie-out; pencairan sebagian memindahkan sisanya ke 1-2000.
//	C. Penjaga reverse: pintu jurnal umum menolak jurnal milik penjualan (R-1),
//	   periode tertutup menolak pembalik (R-2), dan — yang paling penting —
//	   pembalikan yang MELEWATI sub-ledger benar-benar membuat tie-out GAGAL.
//	   Tanpa bagian terakhir itu, "Matched: true" tidak membuktikan apa pun.
//	D. Isolasi tenant: rekonsiliasi satu tenant tidak melihat angka tenant lain.
//
// Prasyarat: TEST_DB_DSN + DB termigrasi.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"esaproperti/internal/allocation"
	"esaproperti/internal/customer"
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
	"esaproperti/internal/platform/auth"
	"esaproperti/internal/sale"
	"esaproperti/internal/salesorg"
	"esaproperti/internal/scheme"
)

const (
	w8TenantA uint64 = 9_900_008
	w8TenantB uint64 = 9_900_009
	w8Secret         = "w8-house-ar-test-secret-32-chars"
)

var w8BankAccount = "1-1300"

// ── Env ───────────────────────────────────────────────────────────────────────

type w8Env struct {
	tenant      uint64
	db          *gorm.DB
	svc         *sale.Service
	repo        *sale.GORMRepository
	posting     *ledger.PostingService
	ledgerRepo  *ledger.GORMRepository
	projectID   uint64
	customerID  uint64
	salesID     uint64
	finSourceID uint64
	schemes     map[string]*scheme.PaymentScheme
}

func w8Cleanup(t *testing.T, db *gorm.DB, tenant uint64) {
	t.Helper()
	for _, tbl := range []string{
		"contract_payment_events", "payment_allocations", "credit_applications",
		"payment_schedules", "receipts", "invoices", "sale_records",
		"termin_payments", "sale_contracts",
		"documents", "document_sequences",
		"journal_lines", "journal_entries", "accounting_periods",
		"allocation_configs", "unit_status_transitions", "units", "product_types",
		"project_phases", "projects",
		"financing_sources", "payment_schemes",
		"sales_persons", "sales_teams", "customers", "accounts",
	} {
		if err := db.Exec("DELETE FROM "+tbl+" WHERE tenant_id = ?", tenant).Error; err != nil {
			t.Fatalf("cleanup %s: %v", tbl, err)
		}
	}
}

// w8Parties memenuhi sale.PartyLookup (pola wiring produksi).
type w8Parties struct {
	customers *customer.GORMRepository
	persons   *salesorg.GORMRepository
}

func (a *w8Parties) CustomerExists(ctx context.Context, tenantID, id uint64) error {
	_, err := a.customers.FindByID(ctx, tenantID, id)
	return err
}

func (a *w8Parties) SalesPersonExists(ctx context.Context, tenantID, id uint64) error {
	_, err := a.persons.FindPersonByID(ctx, tenantID, id)
	return err
}

// w8Setup menyiapkan satu tenant lengkap dengan wiring produksi.
//
// Perhatikan dua option terakhir: WithHouseARStore + WithHouseARLedger persis
// seperti sale.NewHandler. Tie-out yang diuji dengan wiring yang berbeda dari
// produksi hanya membuktikan bahwa test-nya konsisten dengan dirinya sendiri.
func w8Setup(t *testing.T, db *gorm.DB, tenant uint64) *w8Env {
	t.Helper()
	ctx := context.Background()
	w8Cleanup(t, db, tenant)
	t.Cleanup(func() { w8Cleanup(t, db, tenant) })

	if err := ledger.SeedCOA(ctx, db, tenant); err != nil {
		t.Fatalf("seed COA: %v", err)
	}
	if err := scheme.SeedDefaultSchemes(ctx, db, tenant); err != nil {
		t.Fatalf("seed schemes: %v", err)
	}
	if err := slSeedDocumentTypes(db, tenant); err != nil {
		t.Fatalf("seed jenis dokumen: %v", err)
	}

	ledgerRepo := ledger.NewGORMRepository(db)
	posting := ledger.NewPostingService(ledgerRepo, ledgerRepo).WithPeriodChecker(ledgerRepo).WithJournalTx(ledgerRepo)
	allocRepo := allocation.NewGORMRepository(db)
	allocSvc := allocation.NewService(allocRepo, allocRepo, allocRepo)
	repo := slWireReceipts(db, sale.NewGORMRepository(db, posting, allocSvc))
	custRepo := customer.NewGORMRepository(db)
	personRepo := salesorg.NewGORMRepository(db)

	svc := sale.NewService(repo, repo, repo, repo, repo, repo,
		sale.WithContractStore(repo),
		sale.WithPaymentCommitter(repo),
		sale.WithSchemeFlow(repo, scheme.DefaultRegistry(), &w8Parties{customers: custRepo, persons: personRepo}),
		sale.WithHouseARStore(repo),
		sale.WithBillingPlanStore(repo),
		sale.WithHouseARLedger(ledger.NewLedgerBalanceService(ledger.NewQueryService(db))))

	env := &w8Env{tenant: tenant, db: db, svc: svc, repo: repo, posting: posting, ledgerRepo: ledgerRepo}

	if err := db.Exec(`INSERT INTO projects (tenant_id, name, status) VALUES (?,?,?)`,
		tenant, "W8 Project", "selling").Error; err != nil {
		t.Fatalf("seed project: %v", err)
	}
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&env.projectID)

	if err := db.Exec(`INSERT INTO allocation_configs (tenant_id, project_id, basis) VALUES (?,?,?)`,
		tenant, env.projectID, "saleable_area").Error; err != nil {
		t.Fatalf("seed allocation config: %v", err)
	}

	cust, err := customer.NewService(custRepo).Create(ctx, tenant, customer.CreateCustomerRequest{Code: "W8-CUST", Name: "Budi"})
	if err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	env.customerID = cust.ID

	person := &salesorg.SalesPerson{TenantID: tenant, Code: "W8-SP", Name: "Sari", IsActive: true}
	if err := personRepo.CreatePerson(ctx, person); err != nil {
		t.Fatalf("seed sales person: %v", err)
	}
	env.salesID = person.ID

	schemeSvc := scheme.NewService(scheme.NewGORMRepository(db), scheme.DefaultRegistry())
	fs, err := schemeSvc.CreateFinancingSource(ctx, tenant, scheme.CreateFinancingSourceRequest{
		Code: "BTN", Name: "Bank BTN", Type: scheme.FinSourceKPRKomersial,
	})
	if err != nil {
		t.Fatalf("seed financing source: %v", err)
	}
	env.finSourceID = fs.ID

	all, err := schemeSvc.ListSchemes(ctx, tenant)
	if err != nil {
		t.Fatalf("list schemes: %v", err)
	}
	env.schemes = map[string]*scheme.PaymentScheme{}
	for _, m := range all {
		env.schemes[m.Code] = m
	}
	return env
}

// ── Helper aksi (semua lewat jalur produksi) ─────────────────────────────────

func (e *w8Env) seedUnit(t *testing.T, code string) uint64 {
	t.Helper()
	e.db.Exec(`INSERT IGNORE INTO product_types (tenant_id, code, name, category, revenue_account_code, is_active)
		VALUES (?,?,?,?,?,TRUE)`, e.tenant, "rumah", "rumah", "property", "4-1000")
	if err := e.db.Exec(`INSERT INTO units (tenant_id, project_id, code, unit_type, saleable_area, land_area, list_price, status)
		VALUES (?,?,?,?,?,?,?,?)`,
		e.tenant, e.projectID, code, "rumah", domain.FromInt(100), domain.FromInt(100), domain.FromInt(0), "reserved").Error; err != nil {
		t.Fatalf("seed unit: %v", err)
	}
	var id uint64
	e.db.Raw("SELECT LAST_INSERT_ID()").Scan(&id)
	return id
}

func (e *w8Env) contract(t *testing.T, unitID uint64, schemeCode string, price int64, date time.Time) *sale.SaleContract {
	t.Helper()
	m, ok := e.schemes[schemeCode]
	if !ok {
		t.Fatalf("scheme %s tidak ada", schemeCode)
	}
	req := sale.CreateContractRequest{
		UnitID:          unitID,
		BuyerName:       "Budi",
		BuyerID:         "3201...",
		ContractDate:    date,
		TotalPrice:      domain.FromInt(price),
		PaymentSchemeID: &m.ID,
		CustomerID:      &e.customerID,
		SalesPersonID:   &e.salesID,
	}
	if m.PolicyType == scheme.PolicyKPR {
		req.FinancingSourceID = &e.finSourceID
	}
	c, err := e.svc.CreateContract(context.Background(), e.tenant, req)
	if err != nil {
		t.Fatalf("CreateContract: %v", err)
	}
	// Jadwal dari plan resmi scheme: struktur jatuh tempo house AR berasal dari
	// sini, jumlahnya tetap dari GL.
	plan, err := e.svc.GetSchedulePlan(context.Background(), e.tenant, c.ID)
	if err != nil {
		t.Fatalf("GetSchedulePlan: %v", err)
	}
	if _, err := e.svc.CreatePaymentSchedule(context.Background(), e.tenant, c.ID, plan); err != nil {
		t.Fatalf("CreatePaymentSchedule: %v", err)
	}
	return c
}

func (e *w8Env) pay(t *testing.T, contractID uint64, amount int64, src sale.PaymentSource, date time.Time, finSource *uint64) *sale.ReceivePaymentResult {
	t.Helper()
	cid := contractID
	res, err := e.svc.ReceivePayment(context.Background(), e.tenant, sale.ReceivePaymentRequest{
		Source: src, ContractID: &cid,
		Amount: domain.FromInt(amount), Date: date,
		BankAccountCode: w8BankAccount, FinancingSourceID: finSource,
	})
	if err != nil {
		t.Fatalf("ReceivePayment %d: %v", amount, err)
	}
	return res
}

func (e *w8Env) bast(t *testing.T, unitID uint64, price int64, date time.Time) *sale.SaleRecord {
	t.Helper()
	rec, err := e.svc.RecordAkad(context.Background(), e.tenant, sale.RecordBASTRequest{
		UnitID: unitID, SalePrice: domain.FromInt(price), BuyerRef: "Budi", BASTDate: date,
	})
	if err != nil {
		t.Fatalf("RecordBAST: %v", err)
	}
	return rec
}

// bastWithApproval (Item 7A, UAT 2026-09-07): variant untuk kontrak KPR yang
// Akad-nya sudah terjadi SEBELUM BAST — Nilai Persetujuan KPR Bank wajib
// diisi di titik ini (lihat resolveBankApprovedAmount).
func (e *w8Env) bastWithApproval(t *testing.T, unitID uint64, price, approved int64, date time.Time) *sale.SaleRecord {
	t.Helper()
	amt := domain.FromInt(approved)
	rec, err := e.svc.RecordAkad(context.Background(), e.tenant, sale.RecordBASTRequest{
		UnitID: unitID, SalePrice: domain.FromInt(price), BuyerRef: "Budi", BASTDate: date,
		BankApprovedAmount: &amt,
	})
	if err != nil {
		t.Fatalf("RecordBAST: %v", err)
	}
	return rec
}

func (e *w8Env) accountBalance(t *testing.T, code string) string {
	t.Helper()
	bal, err := ledger.NewLedgerBalanceService(ledger.NewQueryService(e.db)).
		AccountBalance(context.Background(), e.tenant, code, nil)
	if err != nil {
		t.Fatalf("AccountBalance %s: %v", code, err)
	}
	return bal.String()
}

func (e *w8Env) count(t *testing.T, table string) int64 {
	t.Helper()
	var n int64
	if err := e.db.Table(table).Where("tenant_id = ?", e.tenant).Count(&n).Error; err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func (e *w8Env) recon(t *testing.T) *sale.HouseARReconciliation {
	t.Helper()
	rec, err := e.svc.ReconcileHouseAR(context.Background(), e.tenant, nil)
	if err != nil {
		t.Fatalf("ReconcileHouseAR: %v", err)
	}
	return rec
}

// customerARTotal = Σ sisa baris yang masuk mesin aging sebagai Piutang Customer.
func (e *w8Env) customerARTotal(t *testing.T) string {
	t.Helper()
	rows, err := e.svc.ReceivableRows(context.Background(), e.tenant)
	if err != nil {
		t.Fatalf("ReceivableRows: %v", err)
	}
	total := domain.Zero
	for _, r := range rows {
		if r.Received {
			continue
		}
		total = total.Add(r.Amount.Sub(r.PaidAmount))
	}
	return total.String()
}

// assertTieOut adalah inti G-1: sub-ledger dan buku besar dibandingkan per akun
// kontrol, bukan sebagai satu angka besar, lalu totalnya dicocokkan dengan
// nilai yang diharapkan skenario.
func (e *w8Env) assertTieOut(t *testing.T, step string, wantTotal string, wantLayers map[string]string) {
	t.Helper()
	rec := e.recon(t)
	for _, l := range rec.Layers {
		if !l.Matched {
			t.Errorf("%s: lapis %s TIDAK cocok — sub-ledger %s vs GL(penjualan) %s (selisih %s)",
				step, l.ControlAccountCode, l.Subledger, l.LedgerHouse, l.Difference)
		}
	}
	if !rec.Matched {
		t.Errorf("%s: rekonsiliasi tidak matched (selisih total %s)", step, rec.Difference)
	}
	if got := rec.SubledgerTotal.String(); got != wantTotal {
		t.Errorf("%s: sub-ledger total = %s, want %s", step, got, wantTotal)
	}
	if got := rec.LedgerHouseTot.String(); got != wantTotal {
		t.Errorf("%s: GL (mutasi milik penjualan) = %s, want %s", step, got, wantTotal)
	}
	for code, want := range wantLayers {
		got := "0"
		for _, l := range rec.Layers {
			if l.ControlAccountCode == code {
				got = l.LedgerHouse.String()
			}
		}
		if got != want {
			t.Errorf("%s: GL lapis %s = %s, want %s", step, code, got, want)
		}
	}
}

// ── A. Lifecycle in-house ─────────────────────────────────────────────────────

func TestIntegration_W8_HouseAR_Lifecycle_TieOut(t *testing.T) {
	db := itConnect(t)
	env := w8Setup(t, db, w8TenantA)
	ctx := context.Background()
	unitID := env.seedUnit(t, "W8-A1")

	const price = 500_000_000
	c := env.contract(t, unitID, "INHOUSE", price, time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))

	// ── 1. Pra-BAST: jadwal ADA, piutang BELUM (BD-1) ────────────────────────
	if n := env.count(t, "payment_schedules"); n == 0 {
		t.Fatal("prasyarat: jadwal harus terbentuk supaya perbedaan jadwal-vs-piutang teruji")
	}
	rows, err := env.svc.HouseARRows(ctx, env.tenant)
	if err != nil {
		t.Fatalf("HouseARRows pra-BAST: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("pra-BAST tidak boleh ada baris house AR, got %d baris", len(rows))
	}
	env.assertTieOut(t, "pra-BAST", "0", map[string]string{"1-2000": "0"})

	// DP 100jt (20% INHOUSE) — kas masuk sebagai KEWAJIBAN, bukan pelunasan
	// piutang (Invariant #7). Ini justru menegaskan BD-1: uang sudah diterima,
	// piutang tetap belum lahir.
	env.pay(t, c.ID, 100_000_000, sale.PaymentSourceCollection, time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC), nil)
	if got := env.accountBalance(t, "2-2000"); got != "100000000" {
		t.Errorf("uang muka pra-BAST = %s, want 100000000", got)
	}
	env.assertTieOut(t, "pra-BAST + DP", "0", map[string]string{"1-2000": "0"})
	if got := env.customerARTotal(t); got != "0" {
		t.Errorf("piutang customer pra-BAST = %s, want 0 (jadwal bukan piutang)", got)
	}

	// ── 2. BAST: piutang lahir, GL bergerak dengan angka yang sama ───────────
	env.bast(t, unitID, price, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	env.assertTieOut(t, "pasca-BAST", "400000000", map[string]string{"1-2000": "400000000"})
	if got := env.customerARTotal(t); got != "400000000" {
		t.Errorf("piutang customer pasca-BAST = %s, want 400000000", got)
	}
	if got := env.accountBalance(t, "2-2000"); got != "0" {
		t.Errorf("uang muka pasca-BAST = %s, want 0 (sudah diakui pendapatan)", got)
	}

	// ── 3. Bayar sebagian: keduanya turun sama ───────────────────────────────
	env.pay(t, c.ID, 150_000_000, sale.PaymentSourceCollection, time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC), nil)
	env.assertTieOut(t, "bayar sebagian", "250000000", map[string]string{"1-2000": "250000000"})

	// ── 4. BAST ulang ditolak, angka tak bergeser (idempotensi) ──────────────
	if _, err := env.svc.RecordAkad(ctx, env.tenant, sale.RecordBASTRequest{
		UnitID: unitID, SalePrice: domain.FromInt(price), BuyerRef: "Budi",
		BASTDate: time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC),
	}); err == nil {
		t.Fatal("BAST kedua harus ditolak — piutang tidak boleh lahir dua kali")
	}
	env.assertTieOut(t, "BAST ulang ditolak", "250000000", map[string]string{"1-2000": "250000000"})

	// ── 5. Transaksi gagal tidak meninggalkan sisa ───────────────────────────
	// Pembayaran melebihi sisa piutang pasca-BAST ditolak; yang diperiksa bukan
	// pesan errornya melainkan bahwa TIDAK ADA jurnal/termin/kwitansi yang
	// tertinggal — rollback parsial akan langsung memisahkan GL dari sub-ledger.
	terminBefore := env.count(t, "termin_payments")
	journalBefore := env.count(t, "journal_entries")
	receiptBefore := env.count(t, "receipts")
	if _, err := env.svc.ReceivePayment(ctx, env.tenant, sale.ReceivePaymentRequest{
		Source: sale.PaymentSourceCollection, ContractID: &c.ID,
		Amount: domain.FromInt(300_000_000), Date: time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC),
		BankAccountCode: w8BankAccount,
	}); err == nil {
		t.Fatal("pembayaran melebihi sisa piutang pasca-BAST harus ditolak")
	}
	if got := env.count(t, "termin_payments"); got != terminBefore {
		t.Errorf("termin_payments = %d setelah transaksi gagal, want %d", got, terminBefore)
	}
	if got := env.count(t, "journal_entries"); got != journalBefore {
		t.Errorf("journal_entries = %d setelah transaksi gagal, want %d", got, journalBefore)
	}
	if got := env.count(t, "receipts"); got != receiptBefore {
		t.Errorf("receipts = %d setelah transaksi gagal, want %d", got, receiptBefore)
	}
	env.assertTieOut(t, "pasca transaksi gagal", "250000000", map[string]string{"1-2000": "250000000"})

	// ── 6. Lunas: keduanya nol ───────────────────────────────────────────────
	env.pay(t, c.ID, 250_000_000, sale.PaymentSourceCollection, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), nil)
	env.assertTieOut(t, "lunas", "0", map[string]string{"1-2000": "0"})
	if got := env.customerARTotal(t); got != "0" {
		t.Errorf("piutang customer setelah lunas = %s, want 0", got)
	}
	if got := env.accountBalance(t, "1-2000"); got != "0" {
		t.Errorf("saldo GL 1-2000 setelah lunas = %s, want 0", got)
	}

	// Unit lunas tetap muncul sebagai baris `Received` — basis collection rate.
	all, err := env.svc.ReceivableRows(ctx, env.tenant)
	if err != nil {
		t.Fatalf("ReceivableRows: %v", err)
	}
	if len(all) != 1 || !all[0].Received || all[0].Amount.String() != "500000000" {
		t.Errorf("unit lunas harus jadi satu baris Received senilai harga unit, got %+v", all)
	}
}

// ── B. KPR — akun kontrol berpindah, tie-out tetap ───────────────────────────

func TestIntegration_W8_HouseAR_KPR_ControlAccountSplit(t *testing.T) {
	db := itConnect(t)
	env := w8Setup(t, db, w8TenantA)
	ctx := context.Background()
	unitID := env.seedUnit(t, "W8-B1")

	const price = 1_000_000_000
	c := env.contract(t, unitID, "KPR-KOM", price, time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))

	env.pay(t, c.ID, 100_000_000, sale.PaymentSourceCollection, time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC), nil)
	for _, ev := range []scheme.Event{scheme.EventSubmittedToBank, scheme.EventBankApproved, scheme.EventAkad} {
		if _, err := env.svc.ApplySchemeEvent(ctx, env.tenant, sale.ApplySchemeEventRequest{
			ContractID: c.ID, Event: ev,
		}); err != nil {
			t.Fatalf("ApplySchemeEvent %s: %v", ev, err)
		}
	}

	// BAST pasca-akad: Nilai Persetujuan KPR Bank 900jt (== sisa) → seluruhnya
	// piutang BANK, bukan customer.
	env.bastWithApproval(t, unitID, price, 900_000_000, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	env.assertTieOut(t, "BAST pasca-akad", "900000000",
		map[string]string{"1-2200": "900000000", "1-2000": "0"})
	if got := env.customerARTotal(t); got != "0" {
		t.Errorf("piutang CUSTOMER saat debitur adalah bank = %s, want 0 (D-W8-6)", got)
	}

	// Pencairan SEBAGIAN (Item 7C, UAT 2026-09-07 — T-3 lama DICABUT): sisa
	// komitmen bank TETAP di Dana Jaminan Bank, TIDAK diam-diam pindah ke
	// piutang customer. Dua akun kontrol bergerak berlawanan adalah PERSIS
	// bug lama yang wajib direproduksi & dicegah di sini.
	env.pay(t, c.ID, 800_000_000, sale.PaymentSourceKPRDisbursement,
		time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC), &env.finSourceID)
	env.assertTieOut(t, "pencairan sebagian", "100000000",
		map[string]string{"1-2200": "100000000", "1-2000": "0"})
	if got := env.customerARTotal(t); got != "0" {
		t.Errorf("kekurangan pasca-pencairan #1 = %s, want 0 (Item 7C: tetap tanggungan bank, bukan customer)", got)
	}

	// Pencairan bank KEDUA melunasi sisa Dana Jaminan Bank persis — TIDAK ada
	// pelunasan oleh customer di jalur ini.
	env.pay(t, c.ID, 100_000_000, sale.PaymentSourceKPRDisbursement,
		time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), &env.finSourceID)
	env.assertTieOut(t, "Dana Jaminan Bank lunas", "0",
		map[string]string{"1-2200": "0", "1-2000": "0"})
}

// ── C. Penjaga reverse (R-1 / R-2) ───────────────────────────────────────────

func TestIntegration_W8_ReverseGuards(t *testing.T) {
	db := itConnect(t)
	env := w8Setup(t, db, w8TenantA)
	ctx := context.Background()
	unitID := env.seedUnit(t, "W8-C1")

	const price = 500_000_000
	c := env.contract(t, unitID, "INHOUSE", price, time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	env.pay(t, c.ID, 100_000_000, sale.PaymentSourceCollection, time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC), nil)
	env.bast(t, unitID, price, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	pay := env.pay(t, c.ID, 150_000_000, sale.PaymentSourceCollection, time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC), nil)
	env.assertTieOut(t, "sebelum uji reverse", "250000000", map[string]string{"1-2000": "250000000"})

	// Jurnal penerimaan termin di atas — milik domain penjualan.
	var terminJournalID uint64
	if err := db.Raw(`SELECT journal_entry_id FROM termin_payments WHERE tenant_id = ? AND id = ?`,
		env.tenant, pay.TerminID).Scan(&terminJournalID).Error; err != nil || terminJournalID == 0 {
		t.Fatalf("ambil jurnal termin: id=%d err=%v", terminJournalID, err)
	}

	// Jurnal manual (tanpa sub-ledger) sebagai pembanding. Sengaja TIDAK
	// menyentuh kas: jurnal kas wajib bawa dokumen bernomor (INV-DOC-1), dan
	// yang diuji di sini kepemilikan sub-ledger, bukan aturan dokumen.
	bebanID, err := env.repo.FindAccountIDByCode(ctx, env.tenant, "5-4000")
	if err != nil {
		t.Fatalf("cari akun beban: %v", err)
	}
	hutangID, err := env.repo.FindAccountIDByCode(ctx, env.tenant, "2-1000")
	if err != nil {
		t.Fatalf("cari akun hutang usaha: %v", err)
	}
	manualJournal := func(date time.Time, desc string, amount int64) *ledger.JournalEntry {
		t.Helper()
		je, err := env.posting.Create(ctx, ledger.CreateJournalRequest{
			TenantID: env.tenant, Date: date, Description: desc,
			Lines: []ledger.LineInput{
				{AccountID: bebanID, Debit: domain.FromInt(amount), Description: desc},
				{AccountID: hutangID, Credit: domain.FromInt(amount), Description: desc},
			},
		})
		if err != nil {
			t.Fatalf("buat jurnal manual: %v", err)
		}
		if _, err := env.posting.Post(ctx, env.tenant, je.ID); err != nil {
			t.Fatalf("posting jurnal manual: %v", err)
		}
		return je
	}
	manual := manualJournal(time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC), "Beban administrasi (jurnal manual)", 1_000_000)

	// Router produksi: middleware auth + handler ledger + seam kepemilikan.
	r := chi.NewRouter()
	r.Use(auth.Middleware(w8Secret))
	lh := ledger.NewHandler(db)
	lh.AddJournalOwnership(env.repo)
	lh.Mount(r)
	token, err := auth.Generate(w8Secret, env.tenant, 1, "owner", time.Hour)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	reverse := func(journalID uint64, date string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"date": date})
		req := httptest.NewRequest(http.MethodPost,
			fmt.Sprintf("/journals/%d/reverse", journalID), bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	// R-1: jurnal milik penjualan ditolak dari pintu umum.
	t.Run("R-1 jurnal ber-sub-ledger ditolak", func(t *testing.T) {
		w := reverse(terminJournalID, "2026-06-25")
		if w.Code != http.StatusConflict {
			t.Fatalf("reverse jurnal termin = %d %s, want 409", w.Code, w.Body.String())
		}
		if body := w.Body.String(); !bytes.Contains([]byte(body), []byte("penjualan")) {
			t.Errorf("pesan harus menyebut domain pemiliknya, got %s", body)
		}
		env.assertTieOut(t, "setelah reverse ditolak", "250000000", map[string]string{"1-2000": "250000000"})
	})

	// Jurnal BAST juga dilindungi — bukan hanya penerimaan kas.
	t.Run("R-1 jurnal BAST ditolak", func(t *testing.T) {
		var revenueJournalID uint64
		if err := db.Raw(`SELECT revenue_journal_id FROM sale_records WHERE tenant_id = ? AND unit_id = ?`,
			env.tenant, unitID).Scan(&revenueJournalID).Error; err != nil || revenueJournalID == 0 {
			t.Fatalf("ambil jurnal BAST: id=%d err=%v", revenueJournalID, err)
		}
		if w := reverse(revenueJournalID, "2026-06-25"); w.Code != http.StatusConflict {
			t.Fatalf("reverse jurnal BAST = %d %s, want 409", w.Code, w.Body.String())
		}
	})

	// Jurnal manual TIDAK ikut terkunci — penjaga ini menutup satu lubang, bukan
	// mematikan fitur pembalikan.
	t.Run("jurnal manual tetap boleh dibalik", func(t *testing.T) {
		if w := reverse(manual.ID, "2026-06-25"); w.Code != http.StatusCreated {
			t.Fatalf("reverse jurnal manual = %d %s, want 201", w.Code, w.Body.String())
		}
	})

	// R-2: periode tertutup menolak pembalik, lewat pintu umum maupun langsung.
	t.Run("R-2 periode tertutup", func(t *testing.T) {
		if _, err := env.ledgerRepo.UpsertPeriodClose(ctx, env.tenant, 2026, 8, 1); err != nil {
			t.Fatalf("tutup periode: %v", err)
		}
		manual2 := manualJournal(time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC), "Beban administrasi kedua", 2_000_000)
		if _, err := env.posting.Reverse(ctx, env.tenant, manual2.ID, time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)); err != ledger.ErrPeriodClosed {
			t.Fatalf("Reverse ke periode tertutup = %v, want ErrPeriodClosed", err)
		}
		if w := reverse(manual2.ID, "2026-08-05"); w.Code != http.StatusConflict {
			t.Fatalf("reverse HTTP ke periode tertutup = %d %s, want 409", w.Code, w.Body.String())
		}
		// Periode berjalan tetap bisa: koreksi tidak diblokir, hanya diarahkan.
		if _, err := env.posting.Reverse(ctx, env.tenant, manual2.ID, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)); err != nil {
			t.Fatalf("Reverse ke periode terbuka: %v", err)
		}
	})

	// Bukti bahwa tie-out punya gigi: pembalikan yang melewati sub-ledger HARUS
	// membuat rekonsiliasi gagal. Dijalankan paling akhir karena sengaja
	// merusak keadaan tenant test.
	t.Run("tie-out mendeteksi GL yang bergerak tanpa sub-ledger", func(t *testing.T) {
		if _, err := env.posting.Reverse(ctx, env.tenant, terminJournalID,
			time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)); err != nil {
			t.Fatalf("reverse langsung (simulasi lubang): %v", err)
		}
		rec := env.recon(t)
		if rec.Matched {
			t.Fatal("rekonsiliasi masih matched padahal GL dibalik tanpa sub-ledger — tie-out tidak berfungsi")
		}
		if rec.Difference.String() != "-150000000" {
			t.Errorf("selisih = %s, want -150000000 (sub-ledger − GL)", rec.Difference)
		}
	})
}

// ── D. Isolasi tenant ─────────────────────────────────────────────────────────

func TestIntegration_W8_HouseAR_TenantIsolation(t *testing.T) {
	db := itConnect(t)
	a := w8Setup(t, db, w8TenantA)
	b := w8Setup(t, db, w8TenantB)

	unitA := a.seedUnit(t, "W8-D1")
	cA := a.contract(t, unitA, "INHOUSE", 500_000_000, time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	a.pay(t, cA.ID, 100_000_000, sale.PaymentSourceCollection, time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC), nil)
	a.bast(t, unitA, 500_000_000, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))

	unitB := b.seedUnit(t, "W8-D1") // kode unit SAMA — kalau scope bocor, angkanya bercampur
	cB := b.contract(t, unitB, "INHOUSE", 800_000_000, time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	b.pay(t, cB.ID, 160_000_000, sale.PaymentSourceCollection, time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC), nil)
	b.bast(t, unitB, 800_000_000, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))

	a.assertTieOut(t, "tenant A", "400000000", map[string]string{"1-2000": "400000000"})
	b.assertTieOut(t, "tenant B", "640000000", map[string]string{"1-2000": "640000000"})

	rowsA, err := a.svc.HouseARRows(context.Background(), a.tenant)
	if err != nil {
		t.Fatalf("HouseARRows A: %v", err)
	}
	for _, r := range rowsA {
		if r.Row.UnitID == unitB {
			t.Fatalf("baris tenant B bocor ke tenant A: %+v", r)
		}
	}
	if got := b.customerARTotal(t); got != "640000000" {
		t.Errorf("piutang customer tenant B = %s, want 640000000", got)
	}
}

// ── E. Jadwal Penagihan pra-BAST (P6) ────────────────────────────────────────
//
// BD-1 mengeluarkan jadwal pra-BAST dari piutang. Yang dibuktikan di sini bukan
// hanya "layar barunya berisi", melainkan bahwa kedua himpunan itu KOMPLEMEN:
// satu kontrak tidak boleh muncul di dua-duanya, dan tidak boleh hilang dari
// dua-duanya. Sebuah kontrak yang lenyap dari keduanya berarti tagihan yang
// tidak pernah ditagih siapa pun.
func TestIntegration_W8_BillingPlan_PreBAST(t *testing.T) {
	db := itConnect(t)
	env := w8Setup(t, db, w8TenantA)
	ctx := context.Background()
	asOf := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)

	unitPre := env.seedUnit(t, "W8-E1")  // tetap pra-BAST
	unitPost := env.seedUnit(t, "W8-E2") // di-BAST di tengah test

	cPre := env.contract(t, unitPre, "INHOUSE", 500_000_000, time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	cPost := env.contract(t, unitPost, "INHOUSE", 400_000_000, time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	env.pay(t, cPre.ID, 100_000_000, sale.PaymentSourceCollection, time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC), nil)
	// DP unit kedua dilunasi supaya gate BAST scheme terpenuhi (80jt = 20%).
	env.pay(t, cPost.ID, 80_000_000, sale.PaymentSourceCollection, time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC), nil)

	plan, err := env.svc.BillingPlanFor(ctx, env.tenant, asOf)
	if err != nil {
		t.Fatalf("BillingPlanFor: %v", err)
	}
	if plan.UnitCount != 2 {
		t.Fatalf("unit pra-BAST = %d, want 2", plan.UnitCount)
	}
	// Dijadwalkan = seluruh nilai kontrak keduanya; sisanya dikurangi DP 100jt.
	if plan.TotalScheduled != "900000000" {
		t.Errorf("total dijadwalkan = %s, want 900000000", plan.TotalScheduled)
	}
	if plan.TotalPaid != "180000000" || plan.TotalOutstanding != "720000000" {
		t.Errorf("terbayar/sisa = %s/%s, want 180000000/720000000", plan.TotalPaid, plan.TotalOutstanding)
	}
	// Inti BD-1: yang terjadwal ini BELUM piutang.
	if got := env.customerARTotal(t); got != "0" {
		t.Errorf("piutang customer pra-BAST = %s, want 0", got)
	}
	env.assertTieOut(t, "pra-BAST", "0", nil)

	// Status per cicilan memakai kosakata yang sama dengan statement, dan cicilan
	// yang sudah dibayar tidak boleh tampil menunggak.
	var pre *sale.BillingPlanUnit
	for i := range plan.Units {
		if plan.Units[i].UnitID == unitPre {
			pre = &plan.Units[i]
		}
	}
	if pre == nil {
		t.Fatal("unit pra-BAST tidak muncul di Jadwal Penagihan")
	}
	if pre.Outstanding != "400000000" || pre.Paid != "100000000" {
		t.Errorf("unit pra-BAST: sisa/terbayar = %s/%s, want 400000000/100000000", pre.Outstanding, pre.Paid)
	}
	for _, l := range pre.Schedules {
		switch l.Status {
		case "paid", "overdue", "due_today", "scheduled":
		default:
			t.Errorf("status cicilan %q di luar kosakata backend", l.Status)
		}
		if l.Status == "paid" && l.DaysOverdue != 0 {
			t.Errorf("cicilan lunas tampil telat %d hari", l.DaysOverdue)
		}
	}

	// BAST unit kedua: ia harus PINDAH — keluar dari jadwal penagihan, masuk ke
	// piutang. Bukan muncul di dua tempat, bukan hilang dari dua-duanya.
	env.bast(t, unitPost, 400_000_000, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))

	plan2, err := env.svc.BillingPlanFor(ctx, env.tenant, asOf)
	if err != nil {
		t.Fatalf("BillingPlanFor pasca-BAST: %v", err)
	}
	if plan2.UnitCount != 1 {
		t.Fatalf("unit pra-BAST pasca-BAST = %d, want 1", plan2.UnitCount)
	}
	for _, u := range plan2.Units {
		if u.UnitID == unitPost {
			t.Fatal("unit yang sudah BAST masih muncul di Jadwal Penagihan (dihitung dua kali)")
		}
	}
	if plan2.TotalScheduled != "500000000" || plan2.TotalOutstanding != "400000000" {
		t.Errorf("pasca-BAST: dijadwalkan/sisa = %s/%s, want 500000000/400000000",
			plan2.TotalScheduled, plan2.TotalOutstanding)
	}
	if got := env.customerARTotal(t); got != "320000000" {
		t.Errorf("piutang customer pasca-BAST = %s, want 320000000", got)
	}
	env.assertTieOut(t, "pasca-BAST unit kedua", "320000000", map[string]string{"1-2000": "320000000"})

	// Isolasi tenant pada pembaca baru.
	other := w8Setup(t, db, w8TenantB)
	planB, err := other.svc.BillingPlanFor(ctx, other.tenant, asOf)
	if err != nil {
		t.Fatalf("BillingPlanFor tenant B: %v", err)
	}
	if planB.UnitCount != 0 || planB.TotalScheduled != "0" {
		t.Errorf("tenant B melihat jadwal tenant A: %d unit, %s", planB.UnitCount, planB.TotalScheduled)
	}
}
