package domain

// ContractPortfolioRow adalah value object ringkasan keuangan SATU kontrak
// aktif untuk agregasi lintas-modul (dashboard, sales performance, pipeline).
// Nilai dihitung oleh sale.Service dari rumus kanonik ContractFinancialSummary
// (registry #7/#8) — konsumen TIDAK menghitung ulang, hanya menjumlah.
type ContractPortfolioRow struct {
	ContractID    uint64
	UnitID        uint64
	ProjectID     uint64
	SalesPersonID *uint64
	// AdminMarketingPersonID (P1/P2 — Sales ≠ Admin Marketing): independen dari
	// SalesPersonID, dipakai reporting utk agregat kinerja Admin Marketing.
	AdminMarketingPersonID *uint64
	NetContract   Money // nilai kontrak ditagih (GrossAmount)
	TotalPaid     Money // Σ termin counts_toward_price=TRUE (kanonik #8)
	Outstanding   Money // NetContract − TotalPaid (kanonik #7)
}
