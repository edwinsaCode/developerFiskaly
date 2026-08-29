package ledger

import (
	"context"
	"fmt"
	"strings"
	"time"

	"esaproperti/internal/domain"
)

// ── Tutup Buku Tahunan (Year-End Closing) ─────────────────────────────────────
//
// ClosingService memposting entri penutup akhir tahun mengikuti siklus akuntansi
// standar, melalui akun perantara Ikhtisar Laba Rugi (Income Summary):
//
//	Entry A — tutup nominal (temporary) ke Ikhtisar Laba Rugi (3-3000):
//	    Dr  semua Pendapatan (4-xxxx)     sebesar saldonya
//	    Cr  semua Beban       (5-xxxx)     sebesar saldonya
//	    Cr/Dr Ikhtisar Laba Rugi (3-3000)  selisihnya (laba=Cr, rugi=Dr)
//
//	Entry B — tutup Ikhtisar Laba Rugi ke Laba Ditahan (3-2000):
//	    laba: Dr 3-3000 / Cr 3-2000
//	    rugi: Dr 3-2000 / Cr 3-3000
//
// Bersifat ADITIF terhadap engine — memakai PostingService yang ada (tetap
// balanced, immutable, append-only). Koreksi via jurnal pembalik biasa lalu
// tutup ulang. Source jurnal = "closing" untuk jejak audit.

// Kode akun penutup.
const (
	AccountIncomeSummaryCode    = "3-3000" // Ikhtisar Laba Rugi / Laba Rugi Tahun Berjalan
	AccountRetainedEarningsCode = "3-2000" // Laba Ditahan
	JournalSourceClosing        = "closing"
)

// ClosingAccountLine adalah satu akun nominal yang ditutup (untuk preview).
type ClosingAccountLine struct {
	Code   string `json:"code"`
	Name   string `json:"name"`
	Amount string `json:"amount"`
}

// ClosingPreview adalah ringkasan rencana tutup buku TANPA memposting apa pun.
type ClosingPreview struct {
	Year            int                  `json:"year"`
	AsOf            string               `json:"as_of"`
	AlreadyClosed   bool                 `json:"already_closed"`
	CanClose        bool                 `json:"can_close"`
	TotalRevenue    string               `json:"total_revenue"`
	TotalExpense    string               `json:"total_expense"`
	NetIncome       string               `json:"net_income"` // positif = laba; negatif = rugi
	RevenueAccounts []ClosingAccountLine `json:"revenue_accounts"`
	ExpenseAccounts []ClosingAccountLine `json:"expense_accounts"`
}

// ClosingResult adalah hasil eksekusi tutup buku.
type ClosingResult struct {
	Year                      int    `json:"year"`
	AsOf                      string `json:"as_of"`
	TotalRevenue              string `json:"total_revenue"`
	TotalExpense              string `json:"total_expense"`
	NetIncome                 string `json:"net_income"`
	IncomeSummaryJournalID    uint64 `json:"income_summary_journal_id"`
	RetainedEarningsJournalID uint64 `json:"retained_earnings_journal_id,omitempty"`
}

// ClosingReader menyediakan data yang dibutuhkan untuk merencanakan tutup buku.
// *QueryService memenuhi interface ini.
type ClosingReader interface {
	TrialBalance(ctx context.Context, tenantID uint64, asOf time.Time) (*TrialBalance, error)
	ListAccounts(ctx context.Context, tenantID uint64) ([]*Account, error)
	ListJournalSummaries(ctx context.Context, tenantID uint64, filter JournalFilter) ([]*JournalSummary, error)
}

// ClosingService mengeksekusi tutup buku tahunan.
type ClosingService struct {
	posting *PostingService
	reader  ClosingReader
}

// NewClosingService mengonstruksi ClosingService.
//
// posting sebaiknya TANPA PeriodChecker: entri penutup bertanggal akhir tahun dan
// merupakan langkah finalisasi — boleh diposting meski periode bulanan sudah
// dikunci. Normal manual journal tetap dijaga oleh PeriodChecker terpisah.
func NewClosingService(posting *PostingService, reader ClosingReader) *ClosingService {
	return &ClosingService{posting: posting, reader: reader}
}

// closingPlan adalah rencana entri penutup (hasil perhitungan murni).
type closingPlan struct {
	totalRevenue domain.Money
	totalExpense domain.Money
	netIncome    domain.Money
	revenueLines []ClosingAccountLine
	expenseLines []ClosingAccountLine
	entryA       []LineInput // tutup pendapatan & beban ke 3-3000
	entryB       []LineInput // tutup 3-3000 ke 3-2000 (kosong jika netIncome == 0)
}

// planClosing menghitung entri penutup dari baris trial balance — pure function.
// accByCode harus memuat akun 3-3000 dan 3-2000.
func planClosing(rows []TrialBalanceRow, accByCode map[string]*Account) (*closingPlan, error) {
	incomeSummary := accByCode[AccountIncomeSummaryCode]
	retained := accByCode[AccountRetainedEarningsCode]
	if incomeSummary == nil || retained == nil {
		return nil, ErrClosingAccountsMissing
	}

	p := &closingPlan{
		revenueLines: []ClosingAccountLine{},
		expenseLines: []ClosingAccountLine{},
	}

	for _, row := range rows {
		if row.Balance.IsZero() {
			continue
		}
		switch {
		case strings.HasPrefix(row.AccountCode, "4-"):
			// Pendapatan (normal kredit) → di-debit sebesar saldo untuk menutup.
			p.entryA = append(p.entryA, LineInput{
				AccountID:   row.AccountID,
				Debit:       row.Balance,
				Description: "tutup " + row.AccountName + " ke Ikhtisar Laba Rugi",
			})
			p.totalRevenue = p.totalRevenue.Add(row.Balance)
			p.revenueLines = append(p.revenueLines, ClosingAccountLine{row.AccountCode, row.AccountName, row.Balance.String()})
		case strings.HasPrefix(row.AccountCode, "5-"):
			// Beban (normal debit) → di-kredit sebesar saldo untuk menutup.
			p.entryA = append(p.entryA, LineInput{
				AccountID:   row.AccountID,
				Credit:      row.Balance,
				Description: "tutup " + row.AccountName + " ke Ikhtisar Laba Rugi",
			})
			p.totalExpense = p.totalExpense.Add(row.Balance)
			p.expenseLines = append(p.expenseLines, ClosingAccountLine{row.AccountCode, row.AccountName, row.Balance.String()})
		}
	}

	if len(p.revenueLines) == 0 && len(p.expenseLines) == 0 {
		return nil, ErrNothingToClose
	}

	p.netIncome = p.totalRevenue.Sub(p.totalExpense)

	// Baris penyeimbang Ikhtisar Laba Rugi pada Entry A.
	switch {
	case p.netIncome.GreaterThan(domain.Zero): // laba → kredit 3-3000
		p.entryA = append(p.entryA, LineInput{
			AccountID:   incomeSummary.ID,
			Credit:      p.netIncome,
			Description: "laba bersih ke Ikhtisar Laba Rugi",
		})
	case p.netIncome.IsNeg(): // rugi → debit 3-3000
		loss := p.netIncome.Neg()
		p.entryA = append(p.entryA, LineInput{
			AccountID:   incomeSummary.ID,
			Debit:       loss,
			Description: "rugi bersih ke Ikhtisar Laba Rugi",
		})
	}
	// netIncome == 0 → Entry A sudah balanced (Σ pendapatan == Σ beban), tanpa baris 3-3000.

	// Entry B — pindahkan Ikhtisar Laba Rugi ke Laba Ditahan.
	switch {
	case p.netIncome.GreaterThan(domain.Zero):
		p.entryB = []LineInput{
			{AccountID: incomeSummary.ID, Debit: p.netIncome, Description: "tutup Ikhtisar Laba Rugi"},
			{AccountID: retained.ID, Credit: p.netIncome, Description: "laba bersih ke Laba Ditahan"},
		}
	case p.netIncome.IsNeg():
		loss := p.netIncome.Neg()
		p.entryB = []LineInput{
			{AccountID: retained.ID, Debit: loss, Description: "rugi bersih membebani Laba Ditahan"},
			{AccountID: incomeSummary.ID, Credit: loss, Description: "tutup Ikhtisar Laba Rugi"},
		}
	}
	// netIncome == 0 → tidak ada Entry B.

	return p, nil
}

// yearBounds mengembalikan 1 Jan dan 31 Des (UTC) untuk sebuah tahun.
func yearBounds(year int) (jan1, dec31 time.Time) {
	jan1 = time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	dec31 = time.Date(year, 12, 31, 0, 0, 0, 0, time.UTC)
	return
}

// plan memuat data dan menghitung rencana tutup buku untuk satu tahun.
func (s *ClosingService) plan(ctx context.Context, tenantID uint64, year int) (asOf time.Time, alreadyClosed bool, p *closingPlan, err error) {
	jan1, dec31 := yearBounds(year)

	// Sudah ditutup? — ada jurnal source="closing" pada tahun tsb.
	existing, err := s.reader.ListJournalSummaries(ctx, tenantID, JournalFilter{
		DateFrom: &jan1, DateTo: &dec31, Source: JournalSourceClosing,
	})
	if err != nil {
		return dec31, false, nil, fmt.Errorf("cek tutup buku existing: %w", err)
	}
	alreadyClosed = len(existing) > 0

	tb, err := s.reader.TrialBalance(ctx, tenantID, dec31)
	if err != nil {
		return dec31, alreadyClosed, nil, fmt.Errorf("trial balance: %w", err)
	}
	accounts, err := s.reader.ListAccounts(ctx, tenantID)
	if err != nil {
		return dec31, alreadyClosed, nil, fmt.Errorf("list accounts: %w", err)
	}
	accByCode := make(map[string]*Account, len(accounts))
	for _, a := range accounts {
		accByCode[a.Code] = a
	}

	p, err = planClosing(tb.Rows, accByCode)
	return dec31, alreadyClosed, p, err
}

// PreviewYearClose mengembalikan ringkasan rencana tutup buku tanpa memposting.
func (s *ClosingService) PreviewYearClose(ctx context.Context, tenantID uint64, year int) (*ClosingPreview, error) {
	asOf, alreadyClosed, p, err := s.plan(ctx, tenantID, year)
	preview := &ClosingPreview{
		Year:            year,
		AsOf:            asOf.Format("2006-01-02"),
		AlreadyClosed:   alreadyClosed,
		TotalRevenue:    domain.Zero.String(),
		TotalExpense:    domain.Zero.String(),
		NetIncome:       domain.Zero.String(),
		RevenueAccounts: []ClosingAccountLine{},
		ExpenseAccounts: []ClosingAccountLine{},
	}
	if err != nil {
		// Tidak ada yang ditutup bukan error fatal untuk preview.
		if err == ErrNothingToClose {
			preview.CanClose = false
			return preview, nil
		}
		return nil, err
	}
	preview.TotalRevenue = p.totalRevenue.String()
	preview.TotalExpense = p.totalExpense.String()
	preview.NetIncome = p.netIncome.String()
	preview.RevenueAccounts = p.revenueLines
	preview.ExpenseAccounts = p.expenseLines
	preview.CanClose = !alreadyClosed
	return preview, nil
}

// CloseFiscalYear memposting entri penutup tahun. Idempoten via guard
// ErrYearAlreadyClosed (menolak jika entri penutup sudah ada untuk tahun tsb).
func (s *ClosingService) CloseFiscalYear(ctx context.Context, tenantID uint64, year int, userID uint64) (*ClosingResult, error) {
	asOf, alreadyClosed, p, err := s.plan(ctx, tenantID, year)
	if err != nil {
		return nil, err
	}
	if alreadyClosed {
		return nil, ErrYearAlreadyClosed
	}

	ref := fmt.Sprintf("CLOSING/%d", year)
	uid := userID

	// Entry A — tutup pendapatan & beban ke Ikhtisar Laba Rugi.
	entryA, err := s.posting.Create(ctx, CreateJournalRequest{
		TenantID:    tenantID,
		Date:        asOf,
		Description: fmt.Sprintf("Tutup buku %d — pendapatan & beban ke Ikhtisar Laba Rugi", year),
		Reference:   ref,
		Lines:       p.entryA,
		Source:      JournalSourceClosing,
		CreatedBy:   &uid,
	})
	if err != nil {
		return nil, fmt.Errorf("buat entri penutup A: %w", err)
	}
	if _, err := s.posting.Post(ctx, tenantID, entryA.ID); err != nil {
		return nil, fmt.Errorf("posting entri penutup A: %w", err)
	}

	result := &ClosingResult{
		Year:                   year,
		AsOf:                   asOf.Format("2006-01-02"),
		TotalRevenue:           p.totalRevenue.String(),
		TotalExpense:           p.totalExpense.String(),
		NetIncome:              p.netIncome.String(),
		IncomeSummaryJournalID: entryA.ID,
	}

	// Entry B — tutup Ikhtisar Laba Rugi ke Laba Ditahan (skip jika netIncome == 0).
	if len(p.entryB) > 0 {
		entryB, err := s.posting.Create(ctx, CreateJournalRequest{
			TenantID:    tenantID,
			Date:        asOf,
			Description: fmt.Sprintf("Tutup buku %d — Ikhtisar Laba Rugi ke Laba Ditahan", year),
			Reference:   ref,
			Lines:       p.entryB,
			Source:      JournalSourceClosing,
			CreatedBy:   &uid,
		})
		if err != nil {
			return nil, fmt.Errorf("buat entri penutup B (entri A sudah diposting — balikkan lalu tutup ulang): %w", err)
		}
		if _, err := s.posting.Post(ctx, tenantID, entryB.ID); err != nil {
			return nil, fmt.Errorf("posting entri penutup B: %w", err)
		}
		result.RetainedEarningsJournalID = entryB.ID
	}

	return result, nil
}
