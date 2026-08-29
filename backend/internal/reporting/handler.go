package reporting

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"esaproperti/internal/ledger"
	"esaproperti/internal/platform/auth"
	"esaproperti/internal/receivable"
)

// printNamer memberi label perusahaan/proyek untuk header cetak — dipenuhi
// oleh GORMRepository (lihat repository.go: TenantName, ProjectName).
type printNamer interface {
	TenantName(ctx context.Context, tenantID uint64) (string, error)
	ProjectName(ctx context.Context, tenantID, projectID uint64) (string, error)
}

// Handler wires reporting HTTP routes.
// READ-ONLY: tidak ada route write di sini.
type Handler struct {
	svc       *Service
	dash      *dashboardQuerier
	kpr       *kprPipelineQuerier
	printRepo printNamer
}

// NewHandler mengkonstruksi Handler production dengan GORM.
func NewHandler(db *gorm.DB) *Handler {
	repo := NewGORMRepository(db)
	qs := ledger.NewQueryService(db)
	svc := NewService(qs, repo, repo, repo)
	return &Handler{svc: svc, dash: &dashboardQuerier{db: db, repo: repo}, kpr: &kprPipelineQuerier{db: db}, printRepo: repo}
}

// SetContractFinance memasang sumber KANONIK outstanding/total_paid (sale.Service)
// ke dashboard & KPR querier (S3). Dipanggil dari wiring main.go setelah sale
// handler tersedia. Dashboard & KPR pipeline WAJIB dapat provider ini (mereka
// tak lagi menghitung outstanding sendiri).
func (h *Handler) SetContractFinance(f ContractFinanceReader) {
	h.dash.finance = f
	h.kpr.finance = f
	h.svc.WithContractFinance(f) // R4: TotalAdvance pipeline kanonik
}

// SetBudgetReader (S5) memasang sumber KANONIK total RAB aktif per proyek
// (budget.Service) — dashboard tidak menjumlahkan budget_items sendiri lagi.
func (h *Handler) SetBudgetReader(b BudgetTotalsReader) {
	h.dash.budget = b
}

// SetHouseReceivable (W-8) memasang sumber piutang harga rumah (sale.Service).
// WAJIB dipanggil dari wiring: AR Aging DAN KPI tunggakan dashboard sama-sama
// menolak jalan tanpanya, supaya tidak ada layar yang melaporkan nol piutang
// hanya karena satu baris wiring terlewat.
func (h *Handler) SetHouseReceivable(r HouseReceivableReader) {
	h.svc.WithHouseReader(r)
	h.dash.house = r
}

// SetRealizationReceivable (W-4) memasang sumber tagihan biaya realisasi
// sehingga Piutang Customer menampilkan seluruh eksposur customer, bukan hanya
// cicilan harga rumah.
func (h *Handler) SetRealizationReceivable(r RealizationReceivableReader) {
	h.svc.WithRealizationReader(r)
}

// SetLegacyReceivable (W-7) memasang sumber piutang proyek lama hasil impor.
// Belum terpasang berarti sumber `legacy` tidak muncul — bukan nol yang
// menyesatkan, melainkan memang tidak ada barisnya.
func (h *Handler) SetLegacyReceivable(r LegacyReceivableReader) {
	h.svc.WithLegacyReader(r)
}

// SetTaxReader (S7) memasang pembaca kanonik laporan pajak (tax.Service).
func (h *Handler) SetTaxReader(t TaxReader) {
	h.svc.WithTaxReader(t)
}

// Mount mendaftarkan semua route laporan.
func (h *Handler) Mount(r chi.Router) {
	r.Route("/reports", func(r chi.Router) {
		r.Get("/dashboard", h.dashboard)
		r.Get("/sales-performance", h.salesPerformance)
		r.Get("/admin-marketing-performance", h.adminMarketingPerformance)
		r.Get("/contract-workload", h.contractWorkload)
		r.Get("/balance-sheet", h.balanceSheet)
		r.Get("/balance-sheet/print", h.balanceSheetPrint)
		r.Get("/income-statement", h.incomeStatement)
		r.Get("/income-statement/print", h.incomeStatementPrint)
		r.Get("/project-pl/{projectID}", h.projectPL)
		r.Get("/project-pl/{projectID}/print", h.projectPLPrint)
		r.Get("/cash-flow", h.cashFlow)
		r.Get("/cash-flow/print", h.cashFlowPrint)
		r.Get("/sales-pipeline", h.salesPipeline)
		r.Get("/sales-pipeline/print", h.salesPipelinePrint)
		r.Get("/kpr-pipeline", h.kprPipeline)
		r.Get("/tax-liability", h.taxLiability)
		r.Get("/tax-liability/print", h.taxLiabilityPrint)
		r.Get("/ar-aging", h.arAging)
		r.Get("/ar-aging/print", h.arAgingPrint)
		r.Route("/trial-balance", func(r chi.Router) {
			r.Get("/", h.trialBalance)
			r.Get("/print", h.trialBalancePrint)
			r.Get("/{accountID}", h.generalLedger)
			r.Get("/{accountID}/print", h.generalLedgerPrint)
		})
	})
}

// ── handlers ──────────────────────────────────────────────────────────────────

func (h *Handler) balanceSheet(w http.ResponseWriter, r *http.Request) {
	tenantID, err := reportingTenantID(r)
	if err != nil {
		writeReportError(w, http.StatusUnauthorized, err.Error())
		return
	}
	asOf := parseDate(r.URL.Query().Get("as_of"), time.Now())

	neraca, err := h.svc.GetNeraca(r.Context(), tenantID, asOf)
	if err != nil {
		writeReportError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if r.URL.Query().Get("format") == "csv" {
		var rows [][]string
		for _, line := range neraca.Aset {
			rows = append(rows, []string{"aset", line.Code, line.Name, line.Amount})
		}
		rows = append(rows, []string{"aset_total", "", "Total Aset", neraca.TotalAset})
		for _, line := range neraca.Kewajiban {
			rows = append(rows, []string{"kewajiban", line.Code, line.Name, line.Amount})
		}
		rows = append(rows, []string{"ekuitas_total", "", "Laba/Rugi Tahun Berjalan", neraca.LabaRugiTahunBerjalan})
		for _, line := range neraca.Ekuitas {
			rows = append(rows, []string{"ekuitas", line.Code, line.Name, line.Amount})
		}
		rows = append(rows, []string{"ke_total", "", "Total Kewajiban+Ekuitas", neraca.TotalKewajibanEkuitas})
		writeCSV(w, []string{"seksi", "kode", "nama", "jumlah"}, rows)
		return
	}
	writeReportJSON(w, http.StatusOK, neraca)
}

func (h *Handler) incomeStatement(w http.ResponseWriter, r *http.Request) {
	tenantID, err := reportingTenantID(r)
	if err != nil {
		writeReportError(w, http.StatusUnauthorized, err.Error())
		return
	}
	asOf := parseDate(r.URL.Query().Get("as_of"), time.Now())

	rpt, err := h.svc.GetKonsolidasiPL(r.Context(), tenantID, asOf)
	if err != nil {
		writeReportError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if r.URL.Query().Get("format") == "csv" {
		writeCSV(w, []string{"seksi", "kode", "nama", "jumlah"}, plToCSVRows(rpt))
		return
	}
	writeReportJSON(w, http.StatusOK, rpt)
}

func (h *Handler) projectPL(w http.ResponseWriter, r *http.Request) {
	tenantID, err := reportingTenantID(r)
	if err != nil {
		writeReportError(w, http.StatusUnauthorized, err.Error())
		return
	}
	projectID, err := strconv.ParseUint(chi.URLParam(r, "projectID"), 10, 64)
	if err != nil {
		writeReportError(w, http.StatusBadRequest, "projectID tidak valid")
		return
	}
	asOf := parseDate(r.URL.Query().Get("as_of"), time.Now())

	rpt, err := h.svc.GetProjectPL(r.Context(), tenantID, projectID, asOf)
	if err != nil {
		writeReportError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if r.URL.Query().Get("format") == "csv" {
		writeCSV(w, []string{"seksi", "kode", "nama", "jumlah"}, plToCSVRows(rpt))
		return
	}
	writeReportJSON(w, http.StatusOK, rpt)
}

func (h *Handler) cashFlow(w http.ResponseWriter, r *http.Request) {
	tenantID, err := reportingTenantID(r)
	if err != nil {
		writeReportError(w, http.StatusUnauthorized, err.Error())
		return
	}
	from := parseDate(r.URL.Query().Get("from"), time.Now().AddDate(0, -1, 0))
	to := parseDate(r.URL.Query().Get("to"), time.Now())

	rpt, err := h.svc.GetArusKas(r.Context(), tenantID, from, to)
	if err != nil {
		writeReportError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if r.URL.Query().Get("format") == "csv" {
		var rows [][]string
		for _, l := range rpt.Operasi.Lines {
			rows = append(rows, []string{"operasi", l.Description, l.Amount})
		}
		rows = append(rows, []string{"operasi_net", "Net Arus Kas Operasi", rpt.Operasi.Net})
		for _, l := range rpt.Investasi.Lines {
			rows = append(rows, []string{"investasi", l.Description, l.Amount})
		}
		rows = append(rows, []string{"investasi_net", "Net Arus Kas Investasi", rpt.Investasi.Net})
		for _, l := range rpt.Pendanaan.Lines {
			rows = append(rows, []string{"pendanaan", l.Description, l.Amount})
		}
		rows = append(rows, []string{"pendanaan_net", "Net Arus Kas Pendanaan", rpt.Pendanaan.Net})
		rows = append(rows, []string{"net_change", "Perubahan Kas Bersih", rpt.NetChange})
		writeCSV(w, []string{"kategori", "deskripsi", "jumlah"}, rows)
		return
	}
	writeReportJSON(w, http.StatusOK, rpt)
}

func (h *Handler) salesPipeline(w http.ResponseWriter, r *http.Request) {
	tenantID, err := reportingTenantID(r)
	if err != nil {
		writeReportError(w, http.StatusUnauthorized, err.Error())
		return
	}

	rpt, err := h.svc.GetSalesPipeline(r.Context(), tenantID)
	if err != nil {
		writeReportError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if r.URL.Query().Get("format") == "csv" {
		rows := [][]string{
			{"available", strconv.Itoa(rpt.Available.Count), rpt.Available.TotalListPrice, ""},
			{"reserved", strconv.Itoa(rpt.Reserved.Count), rpt.Reserved.TotalListPrice, rpt.Reserved.TotalAdvance},
			{"sold", strconv.Itoa(rpt.Sold.Count), "", rpt.Sold.TotalContractValue},
			{"total", strconv.Itoa(rpt.TotalUnits), rpt.ProjectedRevenue, ""},
		}
		writeCSV(w, []string{"status", "jumlah_unit", "total_list_price", "nilai_kontrak"}, rows)
		return
	}
	writeReportJSON(w, http.StatusOK, rpt)
}

func (h *Handler) taxLiability(w http.ResponseWriter, r *http.Request) {
	tenantID, err := reportingTenantID(r)
	if err != nil {
		writeReportError(w, http.StatusUnauthorized, err.Error())
		return
	}
	from := parseDate(r.URL.Query().Get("from"), time.Now().AddDate(-1, 0, 0))
	to := parseDate(r.URL.Query().Get("to"), time.Now())

	rpt, err := h.svc.GetTaxLiability(r.Context(), tenantID, from, to)
	if err != nil {
		writeReportError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if r.URL.Query().Get("format") == "csv" {
		var rows [][]string
		for _, item := range rpt.Items {
			unitID := ""
			if item.UnitID != nil {
				unitID = strconv.FormatUint(*item.UnitID, 10)
			}
			rows = append(rows, []string{
				strconv.FormatUint(item.ID, 10),
				unitID,
				item.RateCode,
				item.TransferValue,
				item.Rate,
				item.TaxAmount,
				item.Status,
				item.AccrualDate,
			})
		}
		rows = append(rows, []string{"", "", "", "", "Total Kewajiban", rpt.TotalObligation, "", ""})
		rows = append(rows, []string{"", "", "", "", "Total Dibayar", rpt.TotalPaid, "", ""})
		rows = append(rows, []string{"", "", "", "", "Belum Dibayar", rpt.TotalOutstanding, "", ""})
		writeCSV(w, []string{"id", "unit_id", "rate_code", "transfer_value", "rate", "tax_amount", "status", "accrual_date"}, rows)
		return
	}
	writeReportJSON(w, http.StatusOK, rpt)
}

func (h *Handler) arAging(w http.ResponseWriter, r *http.Request) {
	tenantID, err := reportingTenantID(r)
	if err != nil {
		writeReportError(w, http.StatusUnauthorized, err.Error())
		return
	}
	asOf := parseDate(r.URL.Query().Get("as_of"), time.Now())

	// W-4: satu laporan, dua sumber. `source` menyaring; nilai yang tidak
	// dikenal DITOLAK, bukan diam-diam diperlakukan "semua" — filter yang salah
	// ketik lalu menampilkan seluruh piutang lebih berbahaya daripada error.
	var src receivable.Source
	if raw := r.URL.Query().Get("source"); raw != "" && raw != "all" {
		src = receivable.Source(raw)
		if !src.Valid() {
			writeReportError(w, http.StatusBadRequest,
				"source tidak dikenal: pakai house, realization, legacy, atau all")
			return
		}
	}

	rpt, err := h.svc.GetARAging(r.Context(), tenantID, asOf, src)
	if err != nil {
		writeReportError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if r.URL.Query().Get("format") == "csv" {
		var rows [][]string
		for _, row := range rpt.Rows {
			rows = append(rows, []string{
				sourceLabel(row.Source),
				row.BuyerName, row.UnitCode, row.InvoiceNumber, row.DueDate,
				row.Amount, row.Paid, row.Outstanding,
				strconv.Itoa(row.DaysOverdue), string(row.Bucket), string(row.Status),
			})
		}
		rows = append(rows, []string{"", "", "", "", "Total Piutang", "", "", rpt.TotalPiutang, "", "", ""})
		rows = append(rows, []string{"", "", "", "", "Belum Jatuh Tempo", "", "", rpt.CurrentDue, "", "", ""})
		rows = append(rows, []string{"", "", "", "", "Jatuh Tempo", "", "", rpt.Overdue, "", "", ""})
		rows = append(rows, []string{"", "", "", "", "Jatuh Tempo Minggu Ini", "", "", rpt.DueThisWeek, "", "", ""})
		rows = append(rows, []string{"", "", "", "", "Collection Rate (%)", "", "", rpt.CollectionRate, "", "", ""})
		writeCSV(w, []string{"sumber", "buyer", "unit", "invoice", "jatuh_tempo", "nominal", "dibayar", "outstanding", "hari_telat", "bucket", "status"}, rows)
		return
	}
	writeReportJSON(w, http.StatusOK, rpt)
}

func (h *Handler) trialBalance(w http.ResponseWriter, r *http.Request) {
	tenantID, err := reportingTenantID(r)
	if err != nil {
		writeReportError(w, http.StatusUnauthorized, err.Error())
		return
	}
	asOf := parseDate(r.URL.Query().Get("as_of"), time.Now())

	tb, err := h.svc.GetTrialBalance(r.Context(), tenantID, asOf)
	if err != nil {
		writeReportError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if r.URL.Query().Get("format") == "csv" {
		var rows [][]string
		for _, row := range tb.Rows {
			rows = append(rows, []string{row.AccountCode, row.AccountName, string(row.AccountType), row.TotalDebit.String(), row.TotalCredit.String(), row.Balance.String()})
		}
		rows = append(rows, []string{"", "", "TOTAL", tb.TotalDebit.String(), tb.TotalCredit.String(), ""})
		writeCSV(w, []string{"kode", "nama", "tipe", "total_debit", "total_kredit", "saldo"}, rows)
		return
	}
	writeReportJSON(w, http.StatusOK, tb)
}

func (h *Handler) generalLedger(w http.ResponseWriter, r *http.Request) {
	tenantID, err := reportingTenantID(r)
	if err != nil {
		writeReportError(w, http.StatusUnauthorized, err.Error())
		return
	}
	accountID, err := strconv.ParseUint(chi.URLParam(r, "accountID"), 10, 64)
	if err != nil {
		writeReportError(w, http.StatusBadRequest, "accountID tidak valid")
		return
	}

	filter := ledger.LedgerFilter{AccountID: &accountID}
	if s := r.URL.Query().Get("from"); s != "" {
		t := parseDate(s, time.Time{})
		if !t.IsZero() {
			filter.DateFrom = &t
		}
	}
	if s := r.URL.Query().Get("to"); s != "" {
		t := parseDate(s, time.Time{})
		if !t.IsZero() {
			filter.DateTo = &t
		}
	}
	if s := r.URL.Query().Get("project_id"); s != "" {
		pid, err2 := strconv.ParseUint(s, 10, 64)
		if err2 == nil {
			filter.ProjectID = &pid
		}
	}

	entries, err := h.svc.GetGeneralLedger(r.Context(), tenantID, filter)
	if err != nil {
		writeReportError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if r.URL.Query().Get("format") == "csv" {
		var rows [][]string
		for _, e := range entries {
			rows = append(rows, []string{
				e.Date.Format("2006-01-02"), e.Reference, e.Description,
				e.Debit.String(), e.Credit.String(), e.Balance.String(),
			})
		}
		writeCSV(w, []string{"tanggal", "referensi", "keterangan", "debit", "kredit", "saldo"}, rows)
		return
	}
	writeReportJSON(w, http.StatusOK, entries)
}

// ── print handlers (HTML A4 siap-cetak, bukan JSON) ──────────────────────────
//
// Pola sama dengan internal/billing/print.go: tidak ada library PDF — browser
// yang mencetak ke PDF lewat window.print(). Handler ini mengambil data lewat
// service yang SAMA dengan endpoint JSON (satu sumber angka), lalu merender
// lewat internal/reporting/print.go.

func (h *Handler) writePrintHTML(w http.ResponseWriter, render func(io.Writer) error) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = render(w)
}

func (h *Handler) balanceSheetPrint(w http.ResponseWriter, r *http.Request) {
	tenantID, err := reportingTenantID(r)
	if err != nil {
		writeReportError(w, http.StatusUnauthorized, err.Error())
		return
	}
	asOf := parseDate(r.URL.Query().Get("as_of"), time.Now())
	rpt, err := h.svc.GetNeraca(r.Context(), tenantID, asOf)
	if err != nil {
		writeReportError(w, http.StatusInternalServerError, err.Error())
		return
	}
	company, _ := h.printRepo.TenantName(r.Context(), tenantID)
	h.writePrintHTML(w, func(w io.Writer) error { return RenderNeracaPrint(w, company, rpt) })
}

func (h *Handler) incomeStatementPrint(w http.ResponseWriter, r *http.Request) {
	tenantID, err := reportingTenantID(r)
	if err != nil {
		writeReportError(w, http.StatusUnauthorized, err.Error())
		return
	}
	asOf := parseDate(r.URL.Query().Get("as_of"), time.Now())
	rpt, err := h.svc.GetKonsolidasiPL(r.Context(), tenantID, asOf)
	if err != nil {
		writeReportError(w, http.StatusInternalServerError, err.Error())
		return
	}
	company, _ := h.printRepo.TenantName(r.Context(), tenantID)
	h.writePrintHTML(w, func(w io.Writer) error { return RenderPLPrint(w, company, rpt) })
}

func (h *Handler) projectPLPrint(w http.ResponseWriter, r *http.Request) {
	tenantID, err := reportingTenantID(r)
	if err != nil {
		writeReportError(w, http.StatusUnauthorized, err.Error())
		return
	}
	projectID, err := strconv.ParseUint(chi.URLParam(r, "projectID"), 10, 64)
	if err != nil {
		writeReportError(w, http.StatusBadRequest, "projectID tidak valid")
		return
	}
	asOf := parseDate(r.URL.Query().Get("as_of"), time.Now())
	rpt, err := h.svc.GetProjectPL(r.Context(), tenantID, projectID, asOf)
	if err != nil {
		writeReportError(w, http.StatusInternalServerError, err.Error())
		return
	}
	company, _ := h.printRepo.TenantName(r.Context(), tenantID)
	projectName, _ := h.printRepo.ProjectName(r.Context(), tenantID, projectID)
	if projectName == "" {
		projectName = fmt.Sprintf("#%d", projectID)
	}
	h.writePrintHTML(w, func(w io.Writer) error { return RenderProjectPLPrint(w, company, projectName, rpt) })
}

func (h *Handler) cashFlowPrint(w http.ResponseWriter, r *http.Request) {
	tenantID, err := reportingTenantID(r)
	if err != nil {
		writeReportError(w, http.StatusUnauthorized, err.Error())
		return
	}
	from := parseDate(r.URL.Query().Get("from"), time.Now().AddDate(0, -1, 0))
	to := parseDate(r.URL.Query().Get("to"), time.Now())
	rpt, err := h.svc.GetArusKas(r.Context(), tenantID, from, to)
	if err != nil {
		writeReportError(w, http.StatusInternalServerError, err.Error())
		return
	}
	company, _ := h.printRepo.TenantName(r.Context(), tenantID)
	h.writePrintHTML(w, func(w io.Writer) error { return RenderArusKasPrint(w, company, rpt) })
}

func (h *Handler) trialBalancePrint(w http.ResponseWriter, r *http.Request) {
	tenantID, err := reportingTenantID(r)
	if err != nil {
		writeReportError(w, http.StatusUnauthorized, err.Error())
		return
	}
	asOf := parseDate(r.URL.Query().Get("as_of"), time.Now())
	tb, err := h.svc.GetTrialBalance(r.Context(), tenantID, asOf)
	if err != nil {
		writeReportError(w, http.StatusInternalServerError, err.Error())
		return
	}
	company, _ := h.printRepo.TenantName(r.Context(), tenantID)
	h.writePrintHTML(w, func(w io.Writer) error { return RenderTrialBalancePrint(w, company, tb) })
}

func (h *Handler) taxLiabilityPrint(w http.ResponseWriter, r *http.Request) {
	tenantID, err := reportingTenantID(r)
	if err != nil {
		writeReportError(w, http.StatusUnauthorized, err.Error())
		return
	}
	from := parseDate(r.URL.Query().Get("from"), time.Now().AddDate(-1, 0, 0))
	to := parseDate(r.URL.Query().Get("to"), time.Now())
	rpt, err := h.svc.GetTaxLiability(r.Context(), tenantID, from, to)
	if err != nil {
		writeReportError(w, http.StatusInternalServerError, err.Error())
		return
	}
	company, _ := h.printRepo.TenantName(r.Context(), tenantID)
	h.writePrintHTML(w, func(w io.Writer) error { return RenderTaxLiabilityPrint(w, company, rpt) })
}

func (h *Handler) salesPipelinePrint(w http.ResponseWriter, r *http.Request) {
	tenantID, err := reportingTenantID(r)
	if err != nil {
		writeReportError(w, http.StatusUnauthorized, err.Error())
		return
	}
	rpt, err := h.svc.GetSalesPipeline(r.Context(), tenantID)
	if err != nil {
		writeReportError(w, http.StatusInternalServerError, err.Error())
		return
	}
	company, _ := h.printRepo.TenantName(r.Context(), tenantID)
	h.writePrintHTML(w, func(w io.Writer) error { return RenderPipelinePrint(w, company, rpt) })
}

func (h *Handler) generalLedgerPrint(w http.ResponseWriter, r *http.Request) {
	tenantID, err := reportingTenantID(r)
	if err != nil {
		writeReportError(w, http.StatusUnauthorized, err.Error())
		return
	}
	accountID, err := strconv.ParseUint(chi.URLParam(r, "accountID"), 10, 64)
	if err != nil {
		writeReportError(w, http.StatusBadRequest, "accountID tidak valid")
		return
	}

	filter := ledger.LedgerFilter{AccountID: &accountID}
	from, to := time.Time{}, time.Time{}
	if s := r.URL.Query().Get("from"); s != "" {
		if t := parseDate(s, time.Time{}); !t.IsZero() {
			filter.DateFrom = &t
			from = t
		}
	}
	if s := r.URL.Query().Get("to"); s != "" {
		if t := parseDate(s, time.Time{}); !t.IsZero() {
			filter.DateTo = &t
			to = t
		}
	}
	if to.IsZero() {
		to = time.Now()
	}
	if s := r.URL.Query().Get("project_id"); s != "" {
		pid, err2 := strconv.ParseUint(s, 10, 64)
		if err2 == nil {
			filter.ProjectID = &pid
		}
	}

	entries, err := h.svc.GetGeneralLedger(r.Context(), tenantID, filter)
	if err != nil {
		writeReportError(w, http.StatusInternalServerError, err.Error())
		return
	}

	accountCode, accountName := "", ""
	if accs, err2 := h.svc.GetListAccounts(r.Context(), tenantID); err2 == nil {
		for _, a := range accs {
			if a.ID == accountID {
				accountCode, accountName = a.Code, a.Name
				break
			}
		}
	}

	company, _ := h.printRepo.TenantName(r.Context(), tenantID)
	h.writePrintHTML(w, func(w io.Writer) error {
		return RenderGeneralLedgerPrint(w, company, accountCode, accountName, entries, from, to)
	})
}

func (h *Handler) arAgingPrint(w http.ResponseWriter, r *http.Request) {
	tenantID, err := reportingTenantID(r)
	if err != nil {
		writeReportError(w, http.StatusUnauthorized, err.Error())
		return
	}
	asOf := parseDate(r.URL.Query().Get("as_of"), time.Now())

	var src receivable.Source
	if raw := r.URL.Query().Get("source"); raw != "" && raw != "all" {
		src = receivable.Source(raw)
		if !src.Valid() {
			writeReportError(w, http.StatusBadRequest,
				"source tidak dikenal: pakai house, realization, legacy, atau all")
			return
		}
	}

	rpt, err := h.svc.GetARAging(r.Context(), tenantID, asOf, src)
	if err != nil {
		writeReportError(w, http.StatusInternalServerError, err.Error())
		return
	}
	company, _ := h.printRepo.TenantName(r.Context(), tenantID)
	h.writePrintHTML(w, func(w io.Writer) error { return RenderARAgingPrint(w, company, rpt) })
}

// ── helpers ───────────────────────────────────────────────────────────────────

func reportingTenantID(r *http.Request) (uint64, error) {
	id, ok := auth.TenantIDFrom(r.Context())
	if !ok {
		return 0, errors.New("missing authentication context")
	}
	return id, nil
}

func writeReportJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeReportError(w http.ResponseWriter, status int, msg string) {
	writeReportJSON(w, status, map[string]string{"error": msg})
}

func writeCSV(w http.ResponseWriter, header []string, rows [][]string) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment")
	w.WriteHeader(http.StatusOK)
	cw := csv.NewWriter(w)
	_ = cw.Write(header)
	_ = cw.WriteAll(rows)
	cw.Flush()
}

func parseDate(s string, fallback time.Time) time.Time {
	if s == "" {
		return fallback
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return fallback
	}
	return t
}

func plToCSVRows(rpt *PLReport) [][]string {
	var rows [][]string
	for _, l := range rpt.Pendapatan {
		rows = append(rows, []string{"pendapatan", l.Code, l.Name, l.Amount})
	}
	rows = append(rows, []string{"pendapatan_total", "", "Total Pendapatan", rpt.TotalPendapatan})
	for _, l := range rpt.HPP {
		rows = append(rows, []string{"hpp", l.Code, l.Name, l.Amount})
	}
	rows = append(rows, []string{"hpp_total", "", "Total HPP", rpt.TotalHPP})
	rows = append(rows, []string{"laba_kotor", "", "Laba Kotor", rpt.LabaKotor})
	for _, l := range rpt.BebanOperasional {
		rows = append(rows, []string{"beban_operasional", l.Code, l.Name, l.Amount})
	}
	rows = append(rows, []string{"beban_operasional_total", "", "Total Beban Operasional", rpt.TotalBebanOperasional})
	rows = append(rows, []string{"laba_operasional", "", "Laba Operasional", rpt.LabaOperasional})
	for _, l := range rpt.PendapatanLuarUsaha {
		rows = append(rows, []string{"pendapatan_luar_usaha", l.Code, l.Name, l.Amount})
	}
	rows = append(rows, []string{"pendapatan_luar_usaha_total", "", "Total Pendapatan Luar Usaha", rpt.TotalPendapatanLuarUsaha})
	for _, l := range rpt.BebanLuarUsaha {
		rows = append(rows, []string{"beban_luar_usaha", l.Code, l.Name, l.Amount})
	}
	rows = append(rows, []string{"beban_luar_usaha_total", "", "Total Beban Luar Usaha", rpt.TotalBebanLuarUsaha})
	rows = append(rows, []string{"laba_sebelum_pajak", "", "Laba Bersih Sebelum Pajak", rpt.LabaBersihSebelumPajak})
	for _, l := range rpt.BebanPajak {
		rows = append(rows, []string{"beban_pajak", l.Code, l.Name, l.Amount})
	}
	rows = append(rows, []string{"beban_pajak_total", "", "Total Beban Pajak", rpt.TotalBebanPajak})
	rows = append(rows, []string{"laba_rugi", "", "Laba/Rugi Bersih", rpt.LabaRugiBersih})
	return rows
}

// Pastikan GORMRepository memenuhi semua interface yang dibutuhkan service.
var _ PLReader = (*GORMRepository)(nil)
var _ PipelineReader = (*GORMRepository)(nil)
var _ CashFlowReader = (*GORMRepository)(nil)

// kprPipeline — R1: pipeline KPR (board + agregat).
func (h *Handler) kprPipeline(w http.ResponseWriter, r *http.Request) {
	tenantID, err := reportingTenantID(r)
	if err != nil {
		writeReportError(w, http.StatusUnauthorized, err.Error())
		return
	}
	rep, err := h.kpr.GetKPRPipeline(r.Context(), tenantID, time.Now())
	if err != nil {
		writeReportError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeReportJSON(w, http.StatusOK, rep)
}

// dashboard — PS-2 Dashboard Owner V2: satu payload komposit.
func (h *Handler) dashboard(w http.ResponseWriter, r *http.Request) {
	tenantID, err := reportingTenantID(r)
	if err != nil {
		writeReportError(w, http.StatusUnauthorized, err.Error())
		return
	}
	rep, err := h.dash.GetDashboard(r.Context(), tenantID, time.Now())
	if err != nil {
		writeReportError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeReportJSON(w, http.StatusOK, rep)
}

// salesPerformance — PS-3: funnel & kinerja per salesperson (read-model).
func (h *Handler) salesPerformance(w http.ResponseWriter, r *http.Request) {
	tenantID, err := reportingTenantID(r)
	if err != nil {
		writeReportError(w, http.StatusUnauthorized, err.Error())
		return
	}
	rep, err := h.dash.GetSalesPerformance(r.Context(), tenantID)
	if err != nil {
		writeReportError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeReportJSON(w, http.StatusOK, rep)
}

// adminMarketingPerformance — P2: kinerja/beban kerja per Admin Marketing
// (independen dari sales-performance; Admin Marketing tidak menerima komisi).
func (h *Handler) adminMarketingPerformance(w http.ResponseWriter, r *http.Request) {
	tenantID, err := reportingTenantID(r)
	if err != nil {
		writeReportError(w, http.StatusUnauthorized, err.Error())
		return
	}
	rep, err := h.dash.GetAdminMarketingPerformance(r.Context(), tenantID)
	if err != nil {
		writeReportError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeReportJSON(w, http.StatusOK, rep)
}

// contractWorkload — P2b: drill-down per-kontrak, dipakai FE untuk expand
// baris Sales maupun Admin Marketing (filter by person_id di frontend; hanya
// satu daftar aktif per tenant, biasanya kecil).
func (h *Handler) contractWorkload(w http.ResponseWriter, r *http.Request) {
	tenantID, err := reportingTenantID(r)
	if err != nil {
		writeReportError(w, http.StatusUnauthorized, err.Error())
		return
	}
	rep, err := h.dash.GetContractWorkload(r.Context(), tenantID)
	if err != nil {
		writeReportError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeReportJSON(w, http.StatusOK, rep)
}

// sourceLabel menerjemahkan sumber eksposur ke label CSV berbahasa Indonesia.
// CSV dibuka akuntan di Excel, bukan dibaca program — slug tidak membantu siapa pun.
func sourceLabel(s receivable.Source) string {
	switch s {
	case receivable.SourceRealization:
		return "Biaya Realisasi"
	case receivable.SourceHouse:
		return "Harga Rumah"
	case receivable.SourceAddon:
		return "Produk Tambahan"
	case receivable.SourceLegacy:
		return "Piutang Proyek Lama"
	default:
		return string(s)
	}
}
