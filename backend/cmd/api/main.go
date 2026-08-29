package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"gorm.io/gorm"

	"esaproperti/internal/allocation"
	"esaproperti/internal/ap"
	"esaproperti/internal/approval"
	"esaproperti/internal/billing"
	"esaproperti/internal/budget"
	"esaproperti/internal/cancellation"
	"esaproperti/internal/charge"
	"esaproperti/internal/closing"
	"esaproperti/internal/commission"
	"esaproperti/internal/cost"
	"esaproperti/internal/crm"
	"esaproperti/internal/customer"
	"esaproperti/internal/document"
	"esaproperti/internal/domain"
	"esaproperti/internal/fixedasset"
	"esaproperti/internal/histfin"
	"esaproperti/internal/land"
	"esaproperti/internal/ledger"
	"esaproperti/internal/legacyar"
	"esaproperti/internal/notary"
	"esaproperti/internal/platform/auth"
	"esaproperti/internal/platform/config"
	"esaproperti/internal/platform/db"
	"esaproperti/internal/project"
	"esaproperti/internal/reporting"
	"esaproperti/internal/sale"
	"esaproperti/internal/salesorg"
	"esaproperti/internal/scheme"
	"esaproperti/internal/tax"
	"esaproperti/internal/tenant"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	gdb, err := db.Connect(cfg.DBDSN, cfg.IsDevelopment())
	if err != nil {
		log.Fatalf("database: %v", err)
	}

	// Dependency wiring.
	tenantRepo := tenant.NewGORMRepository(gdb)
	tenantSvc := tenant.NewService(tenantRepo, tenantRepo, cfg.JWTSecret, 24*time.Hour)
	tenantSvc.SetCOASeeder(&coaSeederAdapter{db: gdb}) // tenant baru otomatis dapat COA
	tenantHandler := tenant.NewHandler(tenantSvc)
	ledgerHandler := ledger.NewHandler(gdb)
	projectHandler := project.NewHandler(gdb)
	// Increment 6 D2: gate transisi hold/blocked lewat Generic Approval Workflow
	// (opt-in). Tanpa workflow `unit_transition` aktif → transisi langsung.
	projectHandler.Svc().SetApprovalGate(&unitTransitionGateAdapter{svc: approval.NewService(approval.NewGORMRepository(gdb))})
	costHandler := cost.NewHandler(gdb)
	allocationHandler := allocation.NewHandler(gdb)
	saleHandler := sale.NewHandler(gdb)
	taxHandler := tax.NewHandler(gdb)
	budgetHandler := budget.NewHandler(gdb)
	reportingHandler := reporting.NewHandler(gdb)
	// S3 (SSOT): reporting membaca outstanding/total_paid dari sumber KANONIK
	// (sale.ContractFinancialSummary), bukan menghitung `gross − Σtermin` sendiri.
	// Product Catalog (H-1/H-2/M-1): kebijakan produk per unit_type — kategori
	// (ikut HPP atau tidak) + akun pendapatan — di-resolve fail-closed dari
	// master product_types. WAJIB terpasang: tanpa seam ini BAST akan memakai
	// jalur legacy (semua unit dianggap properti, akun 4-1000).
	saleHandler.Svc().SetProductPolicyResolver(project.NewGORMRepository(gdb))

	reportingHandler.SetContractFinance(saleHandler.Svc())
	reportingHandler.SetBudgetReader(budgetHandler.Svc()) // S5: total RAB kanonik
	reportingHandler.SetTaxReader(taxHandler.Svc())       // S7: laporan pajak kanonik
	customerHandler := customer.NewHandler(gdb)
	salesorgHandler := salesorg.NewHandler(gdb)
	schemeHandler := scheme.NewHandler(gdb)
	approvalHandler := approval.NewHandler(gdb)
	billingHandler, billingSvc := billing.NewHandler(gdb)
	saleHandler.SetInvoiceUpdater(billingSvc)                                             // auto-update invoice when schedule received
	saleHandler.SetReceiptGenerator(&receiptGenAdapter{svc: billingHandler.ReceiptSvc()}) // auto-kwitansi saat collection payment
	// Hardening Receipt/Invoice: ringkasan finansial kontrak dari SATU rumus
	// (sale.ContractFinancialSummary) untuk receipt DAN invoice.
	summaryAdapter := &contractSummaryAdapter{svc: saleHandler.Svc()}
	billingSvc.SetContractSummaryProvider(summaryAdapter)
	billingHandler.ReceiptSvc().SetContractSummaryProvider(summaryAdapter)
	// R1 KPR Realization: auto-invoice kekurangan (kebijakan tenant, default off).
	billingSvc.SetTenantPolicyReader(billing.NewGORMRepository(gdb))
	saleHandler.Svc().SetShortfallInvoicer(billingSvc)
	saleHandler.Svc().SetUnitTransitioner(projectHandler.Svc()) // D3: kontrak baru → proyeksi unit ppjb (best-effort)
	closingHandler := closing.NewHandler(gdb)
	// P0-4 D2: BAST pasca-completion memakai act_HPP finalized dari true-up.
	saleHandler.Svc().SetFinalizedHPPSource(&finalizedHPPAdapter{svc: closingHandler.Svc()})
	cancellationHandler := cancellation.NewHandler(gdb)
	commissionHandler := commission.NewHandler(gdb)
	crmHandler := crm.NewHandler(gdb)
	notaryHandler := notary.NewHandler(gdb)         // UAT Batch 2 §3
	documentHandler := document.NewHandler(gdb)     // W-2 — Document Domain
	histfinHandler := histfin.NewHandler(gdb)       // W-6 — Snapshot Keuangan Historis (non-ledger)
	landHandler := land.NewHandler(gdb)             // LT-3..LT-5 — Kelebihan Tanah: pool, reservasi, Akad (kelebihan-tanah-final-architecture-2026-08.md)
	fixedAssetHandler := fixedasset.NewHandler(gdb) // Fixed Asset Register + Penyusutan (straight-line)
	// Toggle "Jenis Pembelian: Fixed Asset" di /expenses — SATU pintu masuk
	// pengeluaran yang sudah ada (W-10), bukan endpoint baru.
	costHandler.SetFixedAssetService(fixedAssetHandler.Service())

	// ── Billing Batch 2 — Charge Group (Titipan Realisasi & Addon) ───────────
	chargeHandler := charge.NewHandler(gdb)
	chargeSvc := chargeHandler.Svc()
	// Kwitansi KWR atomik dalam transaksi pembayaran (seri sendiri, derived
	// dari payment_source — pola KWB).
	chargeSvc.SetReceiptTxGenerator(&chargeReceiptAdapter{svc: billingHandler.ReceiptSvc()})
	// W-13 — item produk tambahan menunjuk katalog produk (master yang sama
	// dengan yang dipakai unit), sehingga akun pendapatannya datang dari master,
	// bukan dari yang mengetik. Tanpa wiring ini pembuatan item addon DITOLAK —
	// fail-closed, karena ini jalur uang.
	chargeSvc.SetProductPolicyResolver(project.NewGORMRepository(gdb))
	// Transfer sisa titipan → harga rumah lewat pintu pembayaran sale yang ADA.
	chargeSvc.SetHouseTransferApplier(saleHandler.Svc())
	// Invoice REALISASI (nominal dari formula kanonik grup, billing mempersist).
	chargeSvc.SetInvoiceIssuer(&chargeInvoiceAdapter{svc: billingSvc})
	// Kwitansi KWR menampilkan 4 angka grup dari formula kanonik charge.
	billingHandler.ReceiptSvc().SetChargeSummaryProvider(&chargeSummaryAdapter{svc: chargeSvc})
	// W-5 / D-3 — gate BAST biaya realisasi DICABUT. Sebagai gantinya, transaksi
	// BAST menerbitkan invoice realisasi yang belum terbit sehingga sisanya
	// menjadi Piutang Customer (1-2000) yang sama dengan piutang rumah.
	saleHandler.SetRealizationRecognizer(chargeSvc)
	// W-4 — SATU eksposur piutang customer. Tagihan biaya realisasi masuk ke
	// laporan Piutang Customer dan ke blok eksposur pada Customer Statement,
	// lewat mesin aging yang sama (receivable.BuildAging). Tanpa dua pemasangan
	// di bawah, kedua angka kembali hidup di dua layar yang tak pernah
	// berjumlah — persis cacat yang W-4 tutup.
	// W-5 — pembatalan penjualan ikut membalik piutang biaya realisasi unit.
	cancellationHandler.Svc().SetRealizationReleaser(chargeSvc)
	reportingHandler.SetRealizationReceivable(chargeSvc)
	saleHandler.Svc().SetRealizationExposure(chargeSvc)
	// T-1 (keputusan klien 2026-08-05): transfer titipan = transfer INTERNAL —
	// dokumennya Memo Transfer Internal (MTI), bukan kwitansi. W-2: charge
	// meminta nomornya langsung ke mesin penomoran dokumen; adapter di sini
	// sudah tidak ada lagi karena hanya menyamarkan aturan "satu engine".

	// ── W-7 — Piutang Proyek Lama (Legacy AR) ────────────────────────────────
	// Piutang dari proyek yang selesai SEBELUM buku ini dibuka. Ia tidak punya
	// unit, kontrak, maupun jadwal di sistem — dan tidak dipaksa punya.
	legacyARHandler := legacyar.NewHandler(gdb)
	// Rekonsiliasi memecah saldo akun kontrol menurut source jurnal, sehingga
	// porsi SALDO AWAL bisa dibandingkan dengan rincian piutang lama tanpa
	// tercampur mutasi penjualan berjalan.
	legacyARHandler.Svc().WithBalanceReader(ledger.NewLedgerBalanceService(ledger.NewQueryService(gdb)))
	// SATU daftar piutang (INV-AR-1). `legacy` adalah sumber ketiga pada mesin
	// aging yang sama, bukan halaman aging tersendiri: seorang customer berutang
	// satu jumlah, walau tagihannya lahir dari proses yang berbeda.
	reportingHandler.SetLegacyReceivable(legacyARHandler.Svc())

	// ── W-11 — Hutang Usaha (Accounts Payable) ───────────────────────────────
	// Tagihan vendor. Baris biayanya mendarat di `cost_entries` yang sama dengan
	// jalur biaya kas, sehingga realisasi RAB tetap punya satu sumber.
	apHandler := ap.NewHandler(gdb)

	// ── W-8 — Piutang Harga Rumah lahir saat BAST (BD-1) ─────────────────────
	// Pemilik piutang harga rumah adalah paket yang MENULIS peristiwanya.
	// Sebelum ini laporan membaca `payment_schedules` langsung, sehingga jadwal
	// unit yang belum diserahterimakan terhitung sebagai piutang sementara unit
	// ber-BAST tanpa jadwal tidak terhitung sama sekali — dua kesalahan yang
	// berjalan bersamaan dan tak satu pun terlihat dari layar mana pun.
	reportingHandler.SetHouseReceivable(saleHandler.Svc())

	// R-1: pintu pembalikan jurnal umum menolak jurnal yang punya sub-ledger.
	// Pembatalan penjualan tetap membalik lewat PostingService yang sama — yang
	// ditutup hanyalah jalan yang menggerakkan buku besar tanpa sub-ledgernya.
	ledgerHandler.AddJournalOwnership(saleHandler.JournalOwnership())

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)

	r.Get("/health", healthHandler(gdb))

	r.Route("/api/v1", func(r chi.Router) {
		// ── Public routes (no JWT) ───────────────────────────────────────────
		tenantHandler.MountPublic(r)

		// ── Protected routes (JWT required for everything below) ─────────────
		r.Group(func(r chi.Router) {
			r.Use(auth.Middleware(cfg.JWTSecret))
			// W-12 — batas baca per-role. Satu tempat, fail-closed: role
			// marketing hanya menyentuh daftar-izin di auth/scope.go, role
			// lain lewat tanpa perubahan. Harus tepat setelah Middleware
			// supaya tidak ada rute terlindung yang melewatinya.
			r.Use(auth.Scope("/api/v1"))

			tenantHandler.MountProtected(r)

			// Ledger routes — all require a valid JWT; write routes also
			// require role owner|accountant (enforced inside Mount via RequireWrite).
			r.Route("/ledger", ledgerHandler.Mount)
			projectHandler.Mount(r)
			projectHandler.MountProgress(r, gdb) // R3: progress fisik (append-only, non-ledger)
			costHandler.Mount(r)
			allocationHandler.Mount(r)
			saleHandler.Mount(r)
			taxHandler.Mount(r)
			budgetHandler.Mount(r)
			reportingHandler.Mount(r)
			customerHandler.Mount(r)
			salesorgHandler.Mount(r)
			schemeHandler.Mount(r)
			approvalHandler.Mount(r)
			billingHandler.Mount(r)
			closingHandler.Mount(r)
			cancellationHandler.Mount(r)
			commissionHandler.Mount(r)
			crmHandler.Mount(r)
			notaryHandler.Mount(r)     // UAT Batch 2 §3 — Titipan Notaris
			chargeHandler.Mount(r)     // Billing Batch 2 — Charge Group
			documentHandler.Mount(r)   // W-2 — Document Domain (penomoran dokumen)
			histfinHandler.Mount(r)    // W-6 — Snapshot Keuangan Historis (tidak menyentuh ledger)
			legacyARHandler.Mount(r)   // W-7 — Piutang Proyek Lama (impor + pelunasan)
			apHandler.Mount(r)         // W-11 — Hutang Usaha (vendor + tagihan)
			landHandler.Mount(r)       // LT-3 — Kelebihan Tanah: pool inventory
			fixedAssetHandler.Mount(r) // Fixed Asset Register + Penyusutan
		})
	})

	addr := cfg.BindAddr + ":" + cfg.Port
	log.Printf("esaProperti API listening on %s [%s]", addr, cfg.AppEnv)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatalf("server: %v", err)
	}
}

// receiptGenAdapter mengadaptasi billing.ReceiptService ke sale.ReceiptGenerator
// (mengembalikan nomor + ID kwitansi) tanpa membuat sale import billing.
// contractSummaryAdapter mengadaptasi sale.Service → billing.ContractSummaryProvider
// tanpa membuat billing import sale (pola adapter wiring yang sama).
type contractSummaryAdapter struct {
	svc *sale.Service
}

func (a *contractSummaryAdapter) SummaryByContractID(ctx context.Context, tenantID, contractID uint64) (*billing.ContractSummary, error) {
	sum, err := a.svc.ContractFinancialSummaryByID(ctx, tenantID, contractID)
	if err != nil {
		return nil, err
	}
	return mapContractSummary(sum), nil
}

func (a *contractSummaryAdapter) SummaryByUnitID(ctx context.Context, tenantID, unitID uint64) (*billing.ContractSummary, error) {
	sum, err := a.svc.ContractFinancialSummaryByUnit(ctx, tenantID, unitID)
	if err != nil {
		return nil, err
	}
	return mapContractSummary(sum), nil
}

func mapContractSummary(sum *sale.ContractFinancialSummary) *billing.ContractSummary {
	return &billing.ContractSummary{
		UnitPrice:       sum.UnitPrice,
		PriceIsSnapshot: sum.PriceIsSnapshot,
		Discount:        sum.Discount,
		NetContract:     sum.NetContract,
		TotalPaid:       sum.TotalPaid,
		Outstanding:     sum.Outstanding,
	}
}

type receiptGenAdapter struct {
	svc *billing.ReceiptService
}

func (a *receiptGenAdapter) GenerateReceipt(ctx context.Context, tenantID, createdBy, terminID uint64, notes string) (string, uint64, error) {
	rec, err := a.svc.GenerateReceipt(ctx, tenantID, createdBy, terminID, notes)
	if err != nil {
		return "", 0, err
	}
	return rec.ReceiptNumber, rec.ID, nil
}

// GenerateReceiptInTx mengimplementasikan sale.ReceiptTxGenerator agar kwitansi
// dibuat DI DALAM transaksi commit pembayaran (FE-2, guard #2 — atomik).
func (a *receiptGenAdapter) GenerateReceiptInTx(ctx context.Context, tx *gorm.DB, tenantID, createdBy, terminID, unitID uint64, amount domain.Money, bankAccountCode string, date time.Time, notes string) (string, uint64, error) {
	rec, err := a.svc.GenerateReceiptInTx(ctx, tx, tenantID, createdBy, terminID, unitID, amount, bankAccountCode, date, notes)
	if err != nil {
		return "", 0, err
	}
	return rec.ReceiptNumber, rec.ID, nil
}

// ── Billing Batch 2 adapters (charge ↔ billing tanpa import silang) ──────────

// chargeReceiptAdapter mengadaptasi billing.ReceiptService → charge.ReceiptTxGenerator.
type chargeReceiptAdapter struct {
	svc *billing.ReceiptService
}

func (a *chargeReceiptAdapter) GenerateReceiptInTx(ctx context.Context, tx *gorm.DB, tenantID, createdBy, terminID, unitID uint64, amount domain.Money, bankAccountCode string, date time.Time, notes string) (string, uint64, error) {
	rec, err := a.svc.GenerateReceiptInTx(ctx, tx, tenantID, createdBy, terminID, unitID, amount, bankAccountCode, date, notes)
	if err != nil {
		return "", 0, err
	}
	return rec.ReceiptNumber, rec.ID, nil
}

// chargeInvoiceAdapter mengadaptasi billing.Service → charge.InvoiceIssuer.
type chargeInvoiceAdapter struct {
	svc *billing.Service
}

// W-5: penerbitan invoice realisasi kini berjalan DI DALAM transaksi pemanggil —
// invoice dan jurnal pengakuan piutangnya harus lahir atau gagal bersama.
func (a *chargeInvoiceAdapter) IssueChargeInvoiceInTx(ctx context.Context, tx *gorm.DB, tenantID, contractID, chargeGroupID, createdBy uint64, outstanding domain.Money, dueDate time.Time, notes string) (uint64, string, time.Time, error) {
	inv, err := a.svc.GenerateChargeGroupInvoiceInTx(ctx, tx, tenantID, contractID, chargeGroupID, createdBy, outstanding, dueDate, notes)
	if err != nil {
		return 0, "", time.Time{}, err
	}
	return inv.ID, inv.InvoiceNumber, inv.DueDate, nil
}

func (a *chargeInvoiceAdapter) FindLiveChargeInvoiceInTx(ctx context.Context, tx *gorm.DB, tenantID, contractID, chargeGroupID uint64) (uint64, string, time.Time, error) {
	return a.svc.FindLiveChargeInvoiceInTx(ctx, tx, tenantID, contractID, chargeGroupID)
}

func (a *chargeInvoiceAdapter) SettleChargeInvoiceIfPaid(ctx context.Context, tenantID, contractID, chargeGroupID uint64, outstanding domain.Money) {
	a.svc.SettleChargeInvoiceIfPaid(ctx, tenantID, contractID, chargeGroupID, outstanding)
}

// chargeSummaryAdapter mengadaptasi charge.Service → billing.ChargeSummaryProvider
// (kwitansi KWR membaca 4 angka dari formula kanonik grup).
type chargeSummaryAdapter struct {
	svc *charge.Service
}

func (a *chargeSummaryAdapter) SummaryByGroupID(ctx context.Context, tenantID, groupID uint64) (*billing.ChargeGroupSummary, error) {
	sum, err := a.svc.GroupSummary(ctx, tenantID, groupID)
	if err != nil {
		return nil, err
	}
	return &billing.ChargeGroupSummary{
		Label:       sum.Label,
		Billed:      sum.Billed,
		Paid:        sum.Paid,
		Outstanding: sum.Outstanding,
	}, nil
}

// coaSeederAdapter mengadaptasi ledger.SeedCOA ke tenant.COASeeder.
// Sekaligus menyemai Payment Scheme default (Increment 3) dan Tax Rule default
// (Increment 4) — tenant baru langsung bisa membuat kontrak ber-scheme dan
// akrual pajak tanpa konfigurasi manual.
type coaSeederAdapter struct{ db *gorm.DB }

func (a *coaSeederAdapter) SeedCOA(ctx context.Context, tenantID uint64) error {
	if err := ledger.SeedCOA(ctx, a.db, tenantID); err != nil {
		return err
	}
	if err := scheme.SeedDefaultSchemes(ctx, a.db, tenantID); err != nil {
		return err
	}
	if err := project.SeedDefaultProductTypes(ctx, a.db, tenantID); err != nil { // UAT Batch 2 §2
		return err
	}
	if err := charge.SeedDefaultChargeTypes(ctx, a.db, tenantID); err != nil { // W-1
		return err
	}
	// W-2: tanpa master jenis dokumen, tenant baru tidak bisa menerbitkan
	// kwitansi/invoice sama sekali (resolver fail-closed). Harus ikut di sini.
	if err := document.SeedDefaultDocumentTypes(ctx, a.db, tenantID); err != nil {
		return err
	}
	// W-10: tanpa master jenis pengeluaran, tenant baru tidak bisa mencatat
	// satu pun biaya operasional (resolver fail-closed).
	if err := cost.SeedDefaultExpenseTypes(ctx, a.db, tenantID); err != nil {
		return err
	}
	// Fixed Asset: tanpa master kategori, tenant baru tidak bisa mengakuisisi
	// satu pun aset tetap dari toggle di /expenses (resolver fail-closed).
	if err := fixedasset.SeedDefaultCategories(ctx, a.db, tenantID); err != nil {
		return err
	}
	return tax.SeedDefaultRates(ctx, a.db, tenantID)
}

// unitTransitionGateAdapter menjembatani approval.Service ke project.ApprovalGate
// (Increment 6 D2). Menerjemahkan approval.ErrApprovalRequired ke sentinel project
// agar package project tidak bergantung pada package approval.
// finalizedHPPAdapter menjembatani closing.Service ke sale.FinalizedHPPSource
// (P0-4 D2) + menerjemahkan error closing ke sentinel sale (sale tidak
// meng-import closing).
type finalizedHPPAdapter struct{ svc *closing.Service }

func (a *finalizedHPPAdapter) FinalizedUnitHPP(ctx context.Context, tenantID, projectID, unitID uint64) (domain.UnitCostBreakdown, bool, error) {
	bd, ok, err := a.svc.FinalizedUnitHPP(ctx, tenantID, projectID, unitID)
	if errors.Is(err, closing.ErrTrueupNotPosted) {
		return bd, ok, sale.ErrTrueupNotPostedForBAST
	}
	return bd, ok, err
}

type unitTransitionGateAdapter struct{ svc *approval.Service }

func (a *unitTransitionGateAdapter) RequireApproved(ctx context.Context, tenantID, unitID uint64) error {
	err := a.svc.RequireApproved(ctx, tenantID, approval.TargetUnitTransition, unitID)
	if errors.Is(err, approval.ErrApprovalRequired) {
		return project.ErrApprovalRequired
	}
	return err
}

type healthResponse struct {
	Status  string `json:"status"`
	DB      string `json:"db"`
	Service string `json:"service"`
}

func healthHandler(gdb *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dbStatus := "ok"
		sqlDB, err := gdb.DB()
		if err != nil || sqlDB.Ping() != nil {
			dbStatus = "error"
		}

		status := "ok"
		code := http.StatusOK
		if dbStatus != "ok" {
			status = "degraded"
			code = http.StatusServiceUnavailable
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(healthResponse{
			Status:  status,
			DB:      dbStatus,
			Service: "esaproperti",
		})
	}
}
