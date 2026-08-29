package histfin

import (
	"sort"

	"esaproperti/internal/domain"
)

// report.go adalah MESIN LAPORAN snapshot: fungsi murni, tanpa DB, tanpa HTTP.
// Semua aturan angka historis ada di sini supaya bisa diuji tanpa MySQL —
// pola yang sama dengan internal/receivable.

// ── Totals ────────────────────────────────────────────────────────────────────

// Totals adalah ringkasan satu snapshot. Semua nilai dalam arah normal akun.
type Totals struct {
	TotalAset       domain.Money `json:"total_aset"`
	TotalKewajiban  domain.Money `json:"total_kewajiban"`
	TotalEkuitas    domain.Money `json:"total_ekuitas"`
	TotalPendapatan domain.Money `json:"total_pendapatan"`
	TotalBeban      domain.Money `json:"total_beban"`
	// NetIncome = pendapatan − beban. SELALU dihitung, tidak pernah diketik.
	NetIncome domain.Money `json:"net_income"`
	// TotalKewajibanEkuitas sudah termasuk laba/rugi tahun berjalan — sisi kanan
	// neraca yang sesungguhnya dibandingkan dengan total aset.
	TotalKewajibanEkuitas domain.Money `json:"total_kewajiban_ekuitas"`
	// Difference = aset − (kewajiban + ekuitas + laba berjalan). Ditampilkan apa
	// adanya supaya admin tahu berapa yang masih kurang, bukan sekadar "tidak
	// seimbang".
	Difference domain.Money `json:"difference"`
	IsBalanced bool         `json:"is_balanced"`
	LineCount  int          `json:"line_count"`
}

// ComputeTotals menjumlahkan baris per tipe akun.
//
// Baris disimpan dalam arah normal akun, jadi penjumlahan cukup per tipe — tidak
// ada debit/kredit yang perlu dinormalisasi di sini.
func ComputeTotals(lines []SnapshotLine) Totals {
	t := Totals{
		TotalAset:       domain.Zero,
		TotalKewajiban:  domain.Zero,
		TotalEkuitas:    domain.Zero,
		TotalPendapatan: domain.Zero,
		TotalBeban:      domain.Zero,
		LineCount:       len(lines),
	}
	for _, l := range lines {
		switch domain.AccountType(l.AccountType) {
		case domain.AccountAsset:
			t.TotalAset = t.TotalAset.Add(l.Amount)
		case domain.AccountLiability:
			t.TotalKewajiban = t.TotalKewajiban.Add(l.Amount)
		case domain.AccountEquity:
			t.TotalEkuitas = t.TotalEkuitas.Add(l.Amount)
		case domain.AccountRevenue:
			t.TotalPendapatan = t.TotalPendapatan.Add(l.Amount)
		case domain.AccountExpense:
			t.TotalBeban = t.TotalBeban.Add(l.Amount)
		}
	}
	t.NetIncome = t.TotalPendapatan.Sub(t.TotalBeban)
	t.TotalKewajibanEkuitas = t.TotalKewajiban.Add(t.TotalEkuitas).Add(t.NetIncome)
	t.Difference = t.TotalAset.Sub(t.TotalKewajibanEkuitas)
	t.IsBalanced = t.Difference.IsZero()
	return t
}

// ── Payload laporan ───────────────────────────────────────────────────────────

// Line adalah satu baris laporan siap tampil. Nama field JSON-nya sengaja sama
// dengan reporting.NeracaLine/PLLine supaya format angka di frontend seragam.
type Line struct {
	Code   string `json:"code"`
	Name   string `json:"name"`
	Amount string `json:"amount"`
}

// NeracaReport adalah Neraca historis satu tahun. Berdiri sendiri: angkanya
// tidak pernah diturunkan dari tahun lain dan tidak menyentuh buku besar.
type NeracaReport struct {
	FiscalYear            int    `json:"fiscal_year"`
	Status                Status `json:"status"`
	Revision              int    `json:"revision"`
	Historical            bool   `json:"historical"` // selalu true — penanda untuk banner UI
	Aset                  []Line `json:"aset"`
	Kewajiban             []Line `json:"kewajiban"`
	Ekuitas               []Line `json:"ekuitas"`
	TotalAset             string `json:"total_aset"`
	TotalKewajiban        string `json:"total_kewajiban"`
	TotalEkuitas          string `json:"total_ekuitas"`
	LabaRugiTahunBerjalan string `json:"laba_rugi_tahun_berjalan"`
	TotalEkuitasEfektif   string `json:"total_ekuitas_efektif"`
	TotalKewajibanEkuitas string `json:"total_kewajiban_ekuitas"`
	Difference            string `json:"difference"`
	IsBalanced            bool   `json:"is_balanced"`
	Notes                 string `json:"notes,omitempty"`
}

// PLReport adalah Laba Rugi historis satu tahun (Januari–Desember tahun itu).
//
// Berbeda dengan Laba Rugi dari ledger yang kumulatif sampai `as_of`, angka di
// sini adalah kinerja SATU tahun sebagaimana dilaporkan waktu itu.
type PLReport struct {
	FiscalYear      int    `json:"fiscal_year"`
	Status          Status `json:"status"`
	Revision        int    `json:"revision"`
	Historical      bool   `json:"historical"`
	Pendapatan      []Line `json:"pendapatan"`
	Beban           []Line `json:"beban"`
	TotalPendapatan string `json:"total_pendapatan"`
	TotalBeban      string `json:"total_beban"`
	LabaRugiBersih  string `json:"laba_rugi_bersih"`
	Notes           string `json:"notes,omitempty"`
}

// BuildNeraca menyusun Neraca dari baris snapshot.
func BuildNeraca(s Snapshot, lines []SnapshotLine) NeracaReport {
	t := ComputeTotals(lines)
	return NeracaReport{
		FiscalYear:            s.FiscalYear,
		Status:                s.Status,
		Revision:              s.Revision,
		Historical:            true,
		Aset:                  linesOfType(lines, domain.AccountAsset),
		Kewajiban:             linesOfType(lines, domain.AccountLiability),
		Ekuitas:               linesOfType(lines, domain.AccountEquity),
		TotalAset:             t.TotalAset.String(),
		TotalKewajiban:        t.TotalKewajiban.String(),
		TotalEkuitas:          t.TotalEkuitas.String(),
		LabaRugiTahunBerjalan: t.NetIncome.String(),
		TotalEkuitasEfektif:   t.TotalEkuitas.Add(t.NetIncome).String(),
		TotalKewajibanEkuitas: t.TotalKewajibanEkuitas.String(),
		Difference:            t.Difference.String(),
		IsBalanced:            t.IsBalanced,
		Notes:                 s.Notes,
	}
}

// BuildLabaRugi menyusun Laba Rugi dari baris snapshot.
func BuildLabaRugi(s Snapshot, lines []SnapshotLine) PLReport {
	t := ComputeTotals(lines)
	return PLReport{
		FiscalYear:      s.FiscalYear,
		Status:          s.Status,
		Revision:        s.Revision,
		Historical:      true,
		Pendapatan:      linesOfType(lines, domain.AccountRevenue),
		Beban:           linesOfType(lines, domain.AccountExpense),
		TotalPendapatan: t.TotalPendapatan.String(),
		TotalBeban:      t.TotalBeban.String(),
		LabaRugiBersih:  t.NetIncome.String(),
		Notes:           s.Notes,
	}
}

// linesOfType menyaring dan mengurutkan baris satu tipe akun.
// Urutan: sort_order pilihan admin, lalu kode akun — deterministik supaya
// laporan yang sama selalu tampil sama.
func linesOfType(lines []SnapshotLine, t domain.AccountType) []Line {
	picked := make([]SnapshotLine, 0, len(lines))
	for _, l := range lines {
		if domain.AccountType(l.AccountType) == t {
			picked = append(picked, l)
		}
	}
	sort.SliceStable(picked, func(i, j int) bool {
		if picked[i].SortOrder != picked[j].SortOrder {
			return picked[i].SortOrder < picked[j].SortOrder
		}
		return picked[i].AccountCode < picked[j].AccountCode
	})
	out := make([]Line, 0, len(picked))
	for _, l := range picked {
		out = append(out, Line{Code: l.AccountCode, Name: l.AccountName, Amount: l.Amount.String()})
	}
	return out
}
