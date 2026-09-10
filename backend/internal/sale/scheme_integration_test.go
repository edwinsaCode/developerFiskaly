//go:build integration

package sale_test

// Increment 3 — integration Payment Scheme (real MySQL, wiring produksi).
// Skenario:
//   A. KPR journey penuh: enforcement kontrak → snapshot → plan → jadwal (Σ
//      persis) → DP (milestone dp_paid) → gate BAST menolak pra-akad →
//      submitted→approved→akad → BAST men-debit PIUTANG BANK 1-2200 →
//      pencairan pasca-BAST mengkredit 1-2200 → saldo nol → audit events.
//   B. Akad SETELAH BAST (scheme kpr ber-gate dp_paid) → jurnal REKLAS
//      Dr 1-2200 / Cr 1-2000 sebesar outstanding.
//   C. KPR ditolak bank → konversi ke INHOUSE: jadwal lama superseded,
//      snapshot baru, state signed, event converted.
// Prasyarat: TEST_DB_DSN + DB termigrasi (≥ 000037).

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/allocation"
	"esaproperti/internal/customer"
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
	"esaproperti/internal/sale"
	"esaproperti/internal/salesorg"
	"esaproperti/internal/scheme"
)

const psTenant uint64 = 9_900_005

func psCleanup(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, tbl := range []string{
		"contract_payment_events", "payment_allocations", "credit_applications",
		"payment_schedules", "receipts", "invoices", "sale_records",
		"termin_payments", "sale_contracts",
		"documents", "document_sequences", "receipts",
		"journal_lines", "journal_entries",
		"allocation_configs", "unit_status_transitions", "units", "product_types", "project_phases", "projects",
		"financing_sources", "payment_schemes",
		"sales_persons", "sales_teams", "customers", "accounts",
	} {
		if err := db.Exec("DELETE FROM "+tbl+" WHERE tenant_id = ?", psTenant).Error; err != nil {
			t.Fatalf("cleanup %s: %v", tbl, err)
		}
	}
}

// psParties mengimplementasikan sale.PartyLookup untuk wiring test.
type psParties struct {
	customers *customer.GORMRepository
	persons   *salesorg.GORMRepository
}

func (a *psParties) CustomerExists(ctx context.Context, tenantID, id uint64) error {
	_, err := a.customers.FindByID(ctx, tenantID, id)
	return err
}
func (a *psParties) SalesPersonExists(ctx context.Context, tenantID, id uint64) error {
	_, err := a.persons.FindPersonByID(ctx, tenantID, id)
	return err
}

type psEnv struct {
	db          *gorm.DB
	svc         *sale.Service
	schemeSvc   *scheme.Service
	projectID   uint64
	customerID  uint64
	salesID     uint64
	finSourceID uint64
	schemes     map[string]*scheme.PaymentScheme // by code
}

func psSetup(t *testing.T) *psEnv {
	t.Helper()
	db := itConnect(t)
	psCleanup(t, db)
	t.Cleanup(func() { psCleanup(t, db) })
	ctx := context.Background()

	// COA lengkap (termasuk 1-2200) + default schemes — jalur seed produksi.
	if err := ledger.SeedCOA(ctx, db, psTenant); err != nil {
		t.Fatalf("seed COA: %v", err)
	}
	if err := scheme.SeedDefaultSchemes(ctx, db, psTenant); err != nil {
		t.Fatalf("seed schemes: %v", err)
	}
	if err := slSeedDocumentTypes(db, psTenant); err != nil {
		t.Fatalf("seed jenis dokumen: %v", err)
	}

	// Wiring produksi (pola sale.NewHandler).
	ledgerRepo := ledger.NewGORMRepository(db)
	posting := ledger.NewPostingService(ledgerRepo, ledgerRepo)
	allocRepo := allocation.NewGORMRepository(db)
	allocSvc := allocation.NewService(allocRepo, allocRepo, allocRepo)
	saleRepo := slWireReceipts(db, sale.NewGORMRepository(db, posting, allocSvc))
	custRepo := customer.NewGORMRepository(db)
	personRepo := salesorg.NewGORMRepository(db)
	svc := sale.NewService(saleRepo, saleRepo, saleRepo, saleRepo, saleRepo, saleRepo,
		sale.WithContractStore(saleRepo), sale.WithPaymentCommitter(saleRepo),
		sale.WithSchemeFlow(saleRepo, scheme.DefaultRegistry(), &psParties{customers: custRepo, persons: personRepo}),
		sale.WithHandoverWriter(saleRepo))
	schemeSvc := scheme.NewService(scheme.NewGORMRepository(db), scheme.DefaultRegistry())

	env := &psEnv{db: db, svc: svc, schemeSvc: schemeSvc}

	// Master data.
	res := db.Exec(`INSERT INTO projects (tenant_id, name, status) VALUES (?,?,?)`, psTenant, "Scheme Project", "selling")
	if res.Error != nil {
		t.Fatalf("seed project: %v", res.Error)
	}
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&env.projectID)

	// Konfigurasi alokasi (dibutuhkan jalur HPP actual legacy saat BAST).
	if err := db.Exec(`INSERT INTO allocation_configs (tenant_id, project_id, basis) VALUES (?,?,?)`,
		psTenant, env.projectID, "saleable_area").Error; err != nil {
		t.Fatalf("seed allocation config: %v", err)
	}

	cust, err := customer.NewService(custRepo).Create(ctx, psTenant, customer.CreateCustomerRequest{Code: "CUST-1", Name: "Budi"})
	if err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	env.customerID = cust.ID

	person := &salesorg.SalesPerson{TenantID: psTenant, Code: "SP-1", Name: "Sari", IsActive: true}
	if err := personRepo.CreatePerson(ctx, person); err != nil {
		t.Fatalf("seed sales person: %v", err)
	}
	env.salesID = person.ID

	fs, err := schemeSvc.CreateFinancingSource(ctx, psTenant, scheme.CreateFinancingSourceRequest{
		Code: "BTN", Name: "Bank BTN", Type: scheme.FinSourceKPRKomersial,
	})
	if err != nil {
		t.Fatalf("seed financing source: %v", err)
	}
	env.finSourceID = fs.ID

	all, err := schemeSvc.ListSchemes(ctx, psTenant)
	if err != nil {
		t.Fatalf("list schemes: %v", err)
	}
	env.schemes = map[string]*scheme.PaymentScheme{}
	for _, m := range all {
		env.schemes[m.Code] = m
	}
	return env
}

func (e *psEnv) seedUnit(t *testing.T, code string) uint64 {
	t.Helper()
	// Status 'reserved': sejak Increment 6.1 (F-1) BAST hanya sah dari
	// reserved|ppjb — fixture mengikuti alur legal.
	// Product Catalog (fail-closed): unit_type WAJIB terdaftar di katalog.
	// Kategori 'property' + akun 4-1000 = perilaku fallback lama, jadi jurnal
	// yang dihasilkan fixture ini identik dengan sebelum hardening.
	e.db.Exec(`INSERT IGNORE INTO product_types (tenant_id, code, name, category, revenue_account_code, is_active)
		VALUES (?,?,?,?,?,TRUE)`, psTenant, "rumah", "rumah", "property", "4-1000")
	res := e.db.Exec(`INSERT INTO units (tenant_id, project_id, code, unit_type, saleable_area, land_area, list_price, status)
		VALUES (?,?,?,?,?,?,?,?)`,
		psTenant, e.projectID, code, "rumah", domain.FromInt(100), domain.FromInt(100), domain.FromInt(0), "reserved")
	if res.Error != nil {
		t.Fatalf("seed unit: %v", res.Error)
	}
	var id uint64
	e.db.Raw("SELECT LAST_INSERT_ID()").Scan(&id)
	return id
}

// unitAccountBalance = Σ(debit−credit) posted lines akun `code` ber-tag unit.
func (e *psEnv) unitAccountBalance(t *testing.T, unitID uint64, code string) string {
	t.Helper()
	var v string
	err := e.db.Raw(`SELECT COALESCE(SUM(jl.debit - jl.credit), 0) FROM journal_lines jl
		JOIN accounts a ON a.id = jl.account_id
		JOIN journal_entries je ON je.id = jl.journal_entry_id
		WHERE jl.tenant_id = ? AND jl.unit_id = ? AND a.code = ? AND je.posted_at IS NOT NULL`,
		psTenant, unitID, code).Scan(&v).Error
	if err != nil {
		t.Fatalf("balance query: %v", err)
	}
	m, _ := domain.NewMoney(v)
	return m.String()
}

func (e *psEnv) kprContract(t *testing.T, unitID uint64) *sale.SaleContract {
	t.Helper()
	c, err := e.svc.CreateContract(context.Background(), psTenant, sale.CreateContractRequest{
		UnitID:            unitID,
		BuyerName:         "Budi",
		BuyerID:           "3201...",
		ContractDate:      time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		TotalPrice:        domain.FromInt(1_000_000_000),
		PaymentSchemeID:   &e.schemes["KPR-KOM"].ID,
		FinancingSourceID: &e.finSourceID,
		CustomerID:        &e.customerID,
		SalesPersonID:     &e.salesID,
	})
	if err != nil {
		t.Fatalf("CreateContract KPR: %v", err)
	}
	return c
}

func (e *psEnv) pay(t *testing.T, contractID uint64, amount int64, src sale.PaymentSource, finSource *uint64) *sale.ReceivePaymentResult {
	t.Helper()
	cid := contractID
	res, err := e.svc.ReceivePayment(context.Background(), psTenant, sale.ReceivePaymentRequest{
		Source: src, ContractID: &cid,
		Amount: domain.FromInt(amount), Date: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
		BankAccountCode: "1-1300", FinancingSourceID: finSource,
	})
	if err != nil {
		t.Fatalf("ReceivePayment %d: %v", amount, err)
	}
	return res
}

func (e *psEnv) event(t *testing.T, contractID uint64, ev scheme.Event, finSource *uint64) *sale.ContractPaymentEvent {
	t.Helper()
	return e.eventWithAmount(t, contractID, ev, finSource, nil)
}

// eventWithAmount (Item 7A, UAT 2026-09-07): variant yang membawa
// BankApprovedAmount — dibutuhkan untuk event `akad` pada kontrak KPR yang
// BAST-nya sudah terjadi lebih dulu (lihat reclassToFinancingIfBAST).
func (e *psEnv) eventWithAmount(t *testing.T, contractID uint64, ev scheme.Event, finSource *uint64, bankApproved *domain.Money) *sale.ContractPaymentEvent {
	t.Helper()
	out, err := e.svc.ApplySchemeEvent(context.Background(), psTenant, sale.ApplySchemeEventRequest{
		ContractID: contractID, Event: ev, FinancingSourceID: finSource, BankApprovedAmount: bankApproved,
	})
	if err != nil {
		t.Fatalf("ApplySchemeEvent %s: %v", ev, err)
	}
	return out
}

func (e *psEnv) contractState(t *testing.T, contractID uint64) string {
	t.Helper()
	var st *string
	e.db.Raw(`SELECT scheme_state FROM sale_contracts WHERE id = ? AND tenant_id = ?`, contractID, psTenant).Scan(&st)
	if st == nil {
		return ""
	}
	return *st
}

// ── Skenario A — KPR journey penuh ────────────────────────────────────────────

func TestIntegration_Scheme_KPR_FullJourney(t *testing.T) {
	env := psSetup(t)
	ctx := context.Background()
	unitID := env.seedUnit(t, "KPR-A1")

	// Enforcement: tanpa scheme / tanpa customer → ditolak.
	if _, err := env.svc.CreateContract(ctx, psTenant, sale.CreateContractRequest{
		UnitID: unitID, BuyerName: "X", ContractDate: time.Now(), TotalPrice: domain.FromInt(1),
		PaymentType: sale.PaymentTypeTunai,
	}); err != sale.ErrPaymentSchemeRequired {
		t.Fatalf("kontrak tanpa scheme harus ErrPaymentSchemeRequired, got %v", err)
	}

	c := env.kprContract(t, unitID)
	if c.SchemeParamsSnapshot == nil || c.SchemeState == nil || *c.SchemeState != "signed" {
		t.Fatalf("kontrak harus punya snapshot + state signed, got %+v", c)
	}
	if c.PaymentType != sale.PaymentTypeKPR {
		t.Errorf("payment_type legacy harus terisi kpr (mapping terpusat), got %s", c.PaymentType)
	}

	// Snapshot immutability: ubah params master → kontrak TIDAK terpengaruh.
	newParams := scheme.Params{DPPercent: "50", FinalDueMonths: 12, BASTGate: scheme.GateAkad,
		ReceivableAccount: "1-2000", FinancingReceivableAccount: "1-2200"}
	if _, err := env.schemeSvc.UpdateScheme(ctx, psTenant, env.schemes["KPR-KOM"].ID,
		scheme.UpdateSchemeRequest{Params: &newParams}); err != nil {
		t.Fatalf("update master: %v", err)
	}

	// Plan dari SNAPSHOT (dp 10%, bukan 50% master baru): DP 100jt + final 900jt.
	plan, err := env.svc.GetSchedulePlan(ctx, psTenant, c.ID)
	if err != nil {
		t.Fatalf("GetSchedulePlan: %v", err)
	}
	if len(plan) != 2 || plan[0].Amount.String() != "100000000" || plan[1].Amount.String() != "900000000" {
		t.Fatalf("plan harus dari snapshot (DP 10%%): %+v", plan)
	}

	// Σ jadwal harus persis: kurang 1 rupiah → tolak.
	bad := []sale.ScheduleItem{{InstallmentNumber: 1, DueDate: time.Now(), Amount: domain.FromInt(999_999_999), Type: sale.ScheduleTypeFinal}}
	if _, err := env.svc.CreatePaymentSchedule(ctx, psTenant, c.ID, bad); err == nil {
		t.Fatal("Σ jadwal salah harus ditolak")
	}
	if _, err := env.svc.CreatePaymentSchedule(ctx, psTenant, c.ID, plan); err != nil {
		t.Fatalf("CreatePaymentSchedule dari plan: %v", err)
	}

	// DP 100jt → milestone dp_paid terproyeksi.
	env.pay(t, c.ID, 100_000_000, sale.PaymentSourceCollection, nil)
	if st := env.contractState(t, c.ID); st != "dp_paid" {
		t.Errorf("state = %s, want dp_paid", st)
	}

	// Gate: BAST sebelum akad → ditolak.
	if _, err := env.svc.RecordAkad(ctx, psTenant, sale.RecordBASTRequest{
		UnitID: unitID, SalePrice: domain.FromInt(1_000_000_000), BuyerRef: "Budi", BASTDate: time.Now(),
	}); err == nil {
		t.Fatal("BAST pra-akad harus ditolak gate scheme")
	}

	// Financing milestones.
	env.event(t, c.ID, scheme.EventSubmittedToBank, nil)
	env.event(t, c.ID, scheme.EventBankApproved, nil)
	akadEv := env.event(t, c.ID, scheme.EventAkad, nil)
	if akadEv.JournalEntryID != nil {
		t.Error("akad PRA-BAST tidak boleh memposting jurnal (AR belum lahir — BS-2)")
	}
	if st := env.contractState(t, c.ID); st != "akad" {
		t.Errorf("state = %s, want akad", st)
	}

	// BAST setelah akad: Nilai Persetujuan KPR Bank 900jt (== sisa) → seluruhnya
	// ke Dana Jaminan Bank 1-2200 (bukan 1-2000).
	approved900 := domain.FromInt(900_000_000)
	if _, err := env.svc.RecordAkad(ctx, psTenant, sale.RecordBASTRequest{
		UnitID: unitID, SalePrice: domain.FromInt(1_000_000_000), BuyerRef: "Budi",
		BASTDate:           time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
		BankApprovedAmount: &approved900,
	}); err != nil {
		t.Fatalf("BAST pasca-akad: %v", err)
	}
	if got := env.unitAccountBalance(t, unitID, "1-2200"); got != "900000000" {
		t.Errorf("saldo 1-2200 pasca-BAST = %s, want 900000000", got)
	}
	if got := env.unitAccountBalance(t, unitID, "1-2000"); got != "0" {
		t.Errorf("saldo 1-2000 = %s, want 0 (sisa milik bank)", got)
	}

	// Temuan #7: serah terima fisik sekarang aksi terpisah, tanpa jurnal —
	// dipanggil eksplisit di sini agar event `handed_over` tetap muncul di
	// audit trail (posisinya sama seperti dulu: setelah akad, sebelum disbursed).
	if _, err := env.svc.RecordPhysicalHandover(ctx, psTenant, sale.RecordPhysicalHandoverRequest{
		UnitID: unitID, HandoverDate: time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("RecordPhysicalHandover: %v", err)
	}

	// Pencairan bank pasca-BAST → Cr 1-2200; saldo bank-receivable menjadi nol.
	res := env.pay(t, c.ID, 900_000_000, sale.PaymentSourceKPRDisbursement, &env.finSourceID)
	if res.CreditAccount != "1-2200" {
		t.Errorf("pencairan harus mengkredit 1-2200, got %s", res.CreditAccount)
	}
	if got := env.unitAccountBalance(t, unitID, "1-2200"); got != "0" {
		t.Errorf("saldo 1-2200 setelah pencairan = %s, want 0", got)
	}
	var finTag *uint64
	env.db.Raw(`SELECT financing_source_id FROM termin_payments WHERE tenant_id=? AND unit_id=? ORDER BY id DESC LIMIT 1`,
		psTenant, unitID).Scan(&finTag)
	if finTag == nil || *finTag != env.finSourceID {
		t.Errorf("termin pencairan harus ber-tag financing_source, got %v", finTag)
	}

	// Audit trail: urutan event lengkap + event_date (tanggal kejadian bisnis).
	events, err := env.svc.ListPaymentEvents(ctx, psTenant, c.ID)
	if err != nil {
		t.Fatalf("ListPaymentEvents: %v", err)
	}
	// R1 KPR Realization: pencairan (source kpr_disbursement) saat state akad
	// OTOMATIS mencatat event `disbursed` (akad → disbursed) sebelum milestone
	// fully_paid terproyeksi — audit trail lengkap.
	want := []string{"contract_signed", "dp_paid", "submitted_to_bank", "bank_approved", "akad", "handed_over", "disbursed", "fully_paid"}
	if len(events) != len(want) {
		t.Fatalf("jumlah event = %d, want %d: %+v", len(events), len(want), eventNames(events))
	}
	for i, w := range want {
		if events[i].Event != w {
			t.Errorf("event[%d] = %s, want %s", i, events[i].Event, w)
		}
		if events[i].EventDate.IsZero() {
			t.Errorf("event[%d] %s: event_date tidak boleh kosong (hardening)", i, events[i].Event)
		}
	}
	// contract_signed memakai TANGGAL KONTRAK (bukan tanggal input); dp_paid
	// memakai TANGGAL PEMBAYARAN.
	if got := events[0].EventDate.Format("2006-01-02"); got != "2026-07-01" {
		t.Errorf("contract_signed event_date = %s, want 2026-07-01 (tanggal kontrak)", got)
	}
	if got := events[1].EventDate.Format("2006-01-02"); got != "2026-07-02" {
		t.Errorf("dp_paid event_date = %s, want 2026-07-02 (tanggal pembayaran)", got)
	}

	// Hardening snapshot: amplop menyimpan policy_type + policy_version beku.
	var snapRaw *string
	env.db.Raw(`SELECT scheme_params_snapshot FROM sale_contracts WHERE id=?`, c.ID).Scan(&snapRaw)
	snap, err := scheme.ParseTermsSnapshot(*snapRaw)
	if err != nil {
		t.Fatalf("parse snapshot: %v", err)
	}
	if snap.PolicyType != scheme.PolicyKPR || snap.PolicyVersion != 1 {
		t.Errorf("snapshot harus membekukan policy_type=kpr version=1, got %s v%d", snap.PolicyType, snap.PolicyVersion)
	}
}

func eventNames(evs []*sale.ContractPaymentEvent) []string {
	out := make([]string, len(evs))
	for i, e := range evs {
		out[i] = e.Event
	}
	return out
}

// ── Skenario B — akad SETELAH BAST → jurnal reklas ───────────────────────────

func TestIntegration_Scheme_AkadAfterBAST_Reclass(t *testing.T) {
	env := psSetup(t)
	ctx := context.Background()
	unitID := env.seedUnit(t, "KPR-B1")

	// Scheme KPR khusus ber-gate dp_paid (BAST boleh sebelum akad).
	early, err := env.schemeSvc.CreateScheme(ctx, psTenant, scheme.CreateSchemeRequest{
		Code: "KPR-EARLY", Name: "KPR BAST Awal", PolicyType: scheme.PolicyKPR,
		Params: scheme.Params{DPPercent: "10", FinalDueMonths: 6, BASTGate: scheme.GateDPPaid,
			FinancingReceivableAccount: "1-2200"},
	})
	if err != nil {
		t.Fatalf("create scheme: %v", err)
	}

	c, err := env.svc.CreateContract(ctx, psTenant, sale.CreateContractRequest{
		UnitID: unitID, BuyerName: "Budi", ContractDate: time.Now(),
		TotalPrice:      domain.FromInt(1_000_000_000),
		PaymentSchemeID: &early.ID, FinancingSourceID: &env.finSourceID,
		CustomerID: &env.customerID, SalesPersonID: &env.salesID,
	})
	if err != nil {
		t.Fatalf("CreateContract: %v", err)
	}

	// DP 100jt → BAST (gate dp_paid lolos; state pra-akad → sisa ke 1-2000 buyer).
	env.pay(t, c.ID, 100_000_000, sale.PaymentSourceCollection, nil)
	if _, err := env.svc.RecordAkad(ctx, psTenant, sale.RecordBASTRequest{
		UnitID: unitID, SalePrice: domain.FromInt(1_000_000_000), BuyerRef: "Budi", BASTDate: time.Now(),
	}); err != nil {
		t.Fatalf("BAST: %v", err)
	}
	if got := env.unitAccountBalance(t, unitID, "1-2000"); got != "900000000" {
		t.Fatalf("saldo 1-2000 pasca-BAST = %s, want 900000000 (buyer, pra-akad)", got)
	}

	// Akad SETELAH BAST, Nilai Persetujuan KPR Bank 900jt (== outstanding) →
	// REKLAS Dr 1-2200 / Cr 1-2000 sebesar min(approved, outstanding) = 900jt.
	env.event(t, c.ID, scheme.EventSubmittedToBank, nil)
	env.event(t, c.ID, scheme.EventBankApproved, nil)
	approved900 := domain.FromInt(900_000_000)
	akadEv := env.eventWithAmount(t, c.ID, scheme.EventAkad, nil, &approved900)
	if akadEv.JournalEntryID == nil {
		t.Fatal("akad pasca-BAST harus memposting jurnal reklas")
	}
	if got := env.unitAccountBalance(t, unitID, "1-2000"); got != "0" {
		t.Errorf("saldo 1-2000 setelah reklas = %s, want 0", got)
	}
	if got := env.unitAccountBalance(t, unitID, "1-2200"); got != "900000000" {
		t.Errorf("saldo 1-2200 setelah reklas = %s, want 900000000", got)
	}

	// Jurnal reklas balanced + tanpa P&L (hanya 2 akun piutang).
	var badLines int64
	env.db.Raw(`SELECT COUNT(*) FROM journal_lines jl JOIN accounts a ON a.id = jl.account_id
		WHERE jl.journal_entry_id = ? AND a.code NOT IN ('1-2000','1-2200')`, *akadEv.JournalEntryID).Scan(&badLines)
	if badLines != 0 {
		t.Errorf("reklas hanya boleh menyentuh 1-2000 & 1-2200, ada %d baris lain", badLines)
	}
}

// ── Skenario C — bank menolak → konversi scheme ───────────────────────────────

func TestIntegration_Scheme_Conversion_AfterBankRejected(t *testing.T) {
	env := psSetup(t)
	ctx := context.Background()
	unitID := env.seedUnit(t, "KPR-C1")
	c := env.kprContract(t, unitID)

	plan, _ := env.svc.GetSchedulePlan(ctx, psTenant, c.ID)
	if _, err := env.svc.CreatePaymentSchedule(ctx, psTenant, c.ID, plan); err != nil {
		t.Fatalf("schedule: %v", err)
	}

	// Konversi dari state sehat → ditolak (hanya bank_rejected).
	if _, err := env.svc.ConvertScheme(ctx, psTenant, sale.ConvertSchemeRequest{
		ContractID: c.ID, NewSchemeID: env.schemes["INHOUSE"].ID,
	}); err == nil {
		t.Fatal("konversi dari signed harus ditolak")
	}

	env.event(t, c.ID, scheme.EventSubmittedToBank, nil)
	env.event(t, c.ID, scheme.EventBankRejected, nil)

	ev, err := env.svc.ConvertScheme(ctx, psTenant, sale.ConvertSchemeRequest{
		ContractID: c.ID, NewSchemeID: env.schemes["INHOUSE"].ID, Notes: "bank menolak",
	})
	if err != nil {
		t.Fatalf("ConvertScheme: %v", err)
	}
	if ev.Event != "converted" || ev.FromState != "bank_rejected" || ev.ToState != "signed" {
		t.Errorf("event konversi salah: %+v", ev)
	}

	// Jadwal lama (belum terbayar) superseded semua.
	var active int64
	env.db.Raw(`SELECT COUNT(*) FROM payment_schedules WHERE tenant_id=? AND sale_contract_id=? AND status != 'superseded'`,
		psTenant, c.ID).Scan(&active)
	if active != 0 {
		t.Errorf("jadwal aktif tersisa %d, want 0 (semua superseded)", active)
	}

	// Kontrak ter-rebind: scheme INHOUSE + snapshot baru + financing source hilang.
	var got struct {
		PaymentSchemeID   *uint64
		FinancingSourceID *uint64
		SchemeState       *string
	}
	env.db.Raw(`SELECT payment_scheme_id, financing_source_id, scheme_state FROM sale_contracts WHERE id=?`, c.ID).Scan(&got)
	if got.PaymentSchemeID == nil || *got.PaymentSchemeID != env.schemes["INHOUSE"].ID {
		t.Errorf("payment_scheme_id = %v, want INHOUSE", got.PaymentSchemeID)
	}
	if got.FinancingSourceID != nil {
		t.Errorf("financing_source_id harus NULL setelah konversi ke inhouse")
	}

	// Jadwal baru dari plan INHOUSE (versi 2) — Σ tetap persis.
	newPlan, err := env.svc.GetSchedulePlan(ctx, psTenant, c.ID)
	if err != nil {
		t.Fatalf("plan setelah konversi: %v", err)
	}
	rows, err := env.svc.CreatePaymentSchedule(ctx, psTenant, c.ID, newPlan)
	if err != nil {
		t.Fatalf("schedule versi baru: %v", err)
	}
	if rows[0].ScheduleVersion != 2 {
		t.Errorf("schedule_version = %d, want 2", rows[0].ScheduleVersion)
	}
}

// ── R1 KPR Realization — guard pencairan + auto-state disbursed ───────────────

// Skenario klien: harga 1M (KPR-KOM), buyer bayar DP 100jt, bank cair 850jt →
// outstanding 50jt (kekurangan yang masih ditagih customer).
func TestIntegration_R1_Disbursement_GuardAndAutoState(t *testing.T) {
	env := psSetup(t)
	ctx := context.Background()
	unitID := env.seedUnit(t, "KPR-R1")
	c := env.kprContract(t, unitID)

	// Guard: pencairan SEBELUM akad → ditolak dengan pesan actionable.
	cid := c.ID
	_, err := env.svc.ReceivePayment(ctx, psTenant, sale.ReceivePaymentRequest{
		Source: sale.PaymentSourceKPRDisbursement, ContractID: &cid,
		Amount: domain.FromInt(850_000_000), Date: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
		BankAccountCode: "1-1300", FinancingSourceID: &env.finSourceID,
	})
	if !errors.Is(err, sale.ErrDisbursementRequiresAkad) {
		t.Fatalf("pencairan pra-akad harus ErrDisbursementRequiresAkad, got %v", err)
	}

	// Jalur normal: DP → pengajuan → SP3K → akad.
	env.pay(t, c.ID, 100_000_000, sale.PaymentSourceCollection, nil)
	env.event(t, c.ID, scheme.EventSubmittedToBank, nil)
	env.event(t, c.ID, scheme.EventBankApproved, nil)
	env.event(t, c.ID, scheme.EventAkad, nil)

	// Pencairan pasca-akad: SATU aksi = uang + state disbursed otomatis.
	env.pay(t, c.ID, 850_000_000, sale.PaymentSourceKPRDisbursement, &env.finSourceID)
	if st := env.contractState(t, c.ID); st != "disbursed" {
		t.Errorf("state pasca-pencairan = %s, want disbursed (auto)", st)
	}

	// Outstanding dari SATU rumus: 1M − (100jt + 850jt) = 50jt.
	sum, err := env.svc.ContractFinancialSummaryByID(ctx, psTenant, c.ID)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if sum.Outstanding.String() != "50000000" {
		t.Errorf("Outstanding = %s, want 50000000 (kekurangan customer)", sum.Outstanding)
	}

	// Pencairan kedua (parsial lanjutan) saat state sudah disbursed → tetap boleh.
	env.pay(t, c.ID, 25_000_000, sale.PaymentSourceKPRDisbursement, &env.finSourceID)
	sum2, _ := env.svc.ContractFinancialSummaryByID(ctx, psTenant, c.ID)
	if sum2.Outstanding.String() != "25000000" {
		t.Errorf("Outstanding pasca pencairan kedua = %s, want 25000000", sum2.Outstanding)
	}
}

// ── Item 7C — pencairan parsial TETAP di Dana Jaminan Bank (T-3 SUPERSEDED) ──

// T-3 (2026-08-05) dulu menganggap "bank selesai pada nilai cair": pencairan
// PERTAMA yang lebih kecil dari komitmen otomatis memindahkan sisanya ke
// Piutang Customer via reklas lump-sum. UAT 2026-09-07 (Item 7C) MEMBATALKAN
// itu — persis "bug lama" yang wajib direproduksi & diperbaiki: sisa komitmen
// bank harus TETAP di Dana Jaminan Bank (1-2200) sampai bank benar-benar
// mencairkannya (pencairan ke-2/ke-3), TIDAK PERNAH diam-diam pindah jadi
// tanggungan customer. Angka dari contoh klien (T-3, tetap dipakai sebagai
// fixture): harga rumah 500jt, DP customer 50jt, Nilai Persetujuan KPR Bank
// 450jt, pencairan pertama 430jt (kurang dari approved) lalu pencairan kedua
// 20jt (melunasi Dana Jaminan Bank persis).
func TestIntegration_T3_ShortfallIsCustomerReceivable(t *testing.T) {
	env := psSetup(t)
	ctx := context.Background()
	unitID := env.seedUnit(t, "KPR-T3")

	// Fixture = contoh baku acceptance criteria Item 7C: Harga Unit 185jt,
	// Nilai Persetujuan KPR Bank 150jt → Dana Jaminan Bank 150jt, Piutang
	// Usaha 35jt (SELISIH persetujuan vs harga, bukan hasil reklas). Tanpa DP
	// customer — 150jt (approved) < 185jt (harga) memastikan total termin
	// TIDAK PERNAH mencapai harga penuh hanya lewat pencairan bank, sehingga
	// state tetap `disbursed` (bukan otomatis maju ke `fully_paid`) sampai
	// akhir test — itulah yang membuat percobaan pencairan ke-4 (kelebihan)
	// benar-benar diuji oleh guard NOMINAL (ErrDisbursementExceedsFinancing),
	// bukan keburu ditolak oleh guard STATE (ErrDisbursementRequiresAkad).
	c, err := env.svc.CreateContract(ctx, psTenant, sale.CreateContractRequest{
		UnitID:            unitID,
		BuyerName:         "Budi",
		BuyerID:           "3201...",
		ContractDate:      time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		TotalPrice:        domain.FromInt(185_000_000),
		PaymentSchemeID:   &env.schemes["KPR-KOM"].ID,
		FinancingSourceID: &env.finSourceID,
		CustomerID:        &env.customerID,
		SalesPersonID:     &env.salesID,
	})
	if err != nil {
		t.Fatalf("CreateContract: %v", err)
	}

	env.event(t, c.ID, scheme.EventSubmittedToBank, nil)
	env.event(t, c.ID, scheme.EventBankApproved, nil)
	env.event(t, c.ID, scheme.EventAkad, nil)

	// BAST pasca-akad: Nilai Persetujuan KPR Bank 150jt → Dana Jaminan Bank;
	// sisa (185jt - 150jt = 35jt) ke Piutang Usaha — sesuai contoh klien.
	approved150 := domain.FromInt(150_000_000)
	if _, err := env.svc.RecordAkad(ctx, psTenant, sale.RecordBASTRequest{
		UnitID: unitID, SalePrice: domain.FromInt(185_000_000), BuyerRef: "Budi",
		BASTDate:           time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
		BankApprovedAmount: &approved150,
	}); err != nil {
		t.Fatalf("BAST: %v", err)
	}
	if got := env.unitAccountBalance(t, unitID, "1-2200"); got != "150000000" {
		t.Fatalf("saldo 1-2200 pasca-BAST = %s, want 150000000 (Dana Jaminan Bank)", got)
	}
	if got := env.unitAccountBalance(t, unitID, "1-2000"); got != "35000000" {
		t.Fatalf("saldo 1-2000 pasca-BAST = %s, want 35000000 (selisih approved vs harga)", got)
	}

	// Pencairan bank #1: 50jt.
	res := env.pay(t, c.ID, 50_000_000, sale.PaymentSourceKPRDisbursement, &env.finSourceID)
	if res.CreditAccount != "1-2200" {
		t.Errorf("pencairan bank #1 harus mengkredit 1-2200, got %s", res.CreditAccount)
	}
	if got := env.unitAccountBalance(t, unitID, "1-2200"); got != "100000000" {
		t.Errorf("Dana Jaminan Bank pasca-pencairan #1 = %s, want 100000000", got)
	}
	if got := env.unitAccountBalance(t, unitID, "1-2000"); got != "35000000" {
		t.Errorf("Piutang Usaha pasca-pencairan #1 = %s, want 35000000 (tidak tersentuh)", got)
	}

	// Milestone `disbursed` tercatat, tapi TANPA jurnal reklas (7C: reklas
	// lump-sum lama dicabut — satu-satunya jurnal adalah penerimaan kas itu
	// sendiri, dikreditkan langsung ke 1-2200).
	events, err := env.svc.ListPaymentEvents(ctx, psTenant, c.ID)
	if err != nil {
		t.Fatalf("ListPaymentEvents: %v", err)
	}
	var disbursedEv *sale.ContractPaymentEvent
	for _, e := range events {
		if e.Event == string(scheme.EventDisbursed) {
			disbursedEv = e
		}
	}
	if disbursedEv == nil {
		t.Fatalf("event disbursed tidak tercatat: %v", eventNames(events))
	}
	if disbursedEv.JournalEntryID != nil {
		t.Error("milestone disbursed TIDAK boleh lagi membawa jurnal reklas (7C: dicabut)")
	}

	// Pencairan bank #2: 60jt — INTI Item 7C: pencairan KEDUA tetap mengurangi
	// Dana Jaminan Bank, bukan diam-diam dialihkan ke Piutang Usaha (bug T-3
	// lama yang wajib direproduksi & diperbaiki).
	res2 := env.pay(t, c.ID, 60_000_000, sale.PaymentSourceKPRDisbursement, &env.finSourceID)
	if res2.CreditAccount != "1-2200" {
		t.Errorf("pencairan bank #2 harus mengkredit 1-2200, got %s", res2.CreditAccount)
	}
	if got := env.unitAccountBalance(t, unitID, "1-2200"); got != "40000000" {
		t.Errorf("Dana Jaminan Bank pasca-pencairan #2 = %s, want 40000000", got)
	}
	if got := env.unitAccountBalance(t, unitID, "1-2000"); got != "35000000" {
		t.Errorf("Piutang Usaha pasca-pencairan #2 = %s, want 35000000 (tidak tersentuh)", got)
	}

	// Pencairan bank #3: 40jt — MELUNASI Dana Jaminan Bank persis; Piutang
	// Usaha (35jt) tidak pernah tersentuh oleh pencairan bank sama sekali.
	res3 := env.pay(t, c.ID, 40_000_000, sale.PaymentSourceKPRDisbursement, &env.finSourceID)
	if res3.CreditAccount != "1-2200" {
		t.Errorf("pencairan bank #3 harus mengkredit 1-2200, got %s", res3.CreditAccount)
	}
	if got := env.unitAccountBalance(t, unitID, "1-2200"); got != "0" {
		t.Errorf("Dana Jaminan Bank akhir = %s, want 0 (lunas)", got)
	}
	if got := env.unitAccountBalance(t, unitID, "1-2000"); got != "35000000" {
		t.Errorf("Piutang Usaha akhir = %s, want 35000000 (utuh — bukan tanggungan bank)", got)
	}

	// Pencairan bank #4 (kelebihan, 1jt) setelah Dana Jaminan Bank lunas HARUS
	// ditolak berbasis NOMINAL — Dana Jaminan Bank tidak boleh dicairkan
	// melebihi sisa (7C: batas keras), state tetap `disbursed` di titik ini
	// sehingga guard urutan-flow tidak lebih dulu memblokir percobaan ini.
	if _, err := env.svc.ReceivePayment(ctx, psTenant, sale.ReceivePaymentRequest{
		Source: sale.PaymentSourceKPRDisbursement, ContractID: &c.ID,
		Amount: domain.FromInt(1_000_000), Date: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
		BankAccountCode: "1-1300", FinancingSourceID: &env.finSourceID,
	}); !errors.Is(err, sale.ErrDisbursementExceedsFinancing) {
		t.Errorf("pencairan melebihi sisa Dana Jaminan Bank harus ErrDisbursementExceedsFinancing, got %v", err)
	}
}
