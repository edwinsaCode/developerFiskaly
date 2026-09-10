package ledger

import (
	"context"
	"fmt"

	"esaproperti/internal/domain"
)

// ═════════════════════════════════════════════════════════════════════════════
// ActualCost — pembaca KANONIK "biaya aktual / realisasi" dari ledger
// (Phase 2 · S5 — docs/financial-canonical-registry.md §13, keputusan PO #2).
//
// SATU definisi netting untuk seluruh sistem:
//
//	ActualCost = Σ debit (jurnal non-reversal) − Σ kredit (jurnal source='reversal')
//
// posted-only, kode akun dari AccountRoleRegistry / taxonomy domain. Kredit
// NON-reversal (mis. relief HPP saat BAST) TIDAK mengurangi — relief hanya
// reklasifikasi ke HPP, bukan "un-spending" (keputusan PO). Konsumen:
// cost.AccumulatedBy*, allocation.queryCosts, budget realisasi,
// closing.actualCapitalizedCost, dashboard actual_cost — SEMUA lewat sini.
// ═════════════════════════════════════════════════════════════════════════════

// ActualCostScope membatasi cakupan baca (nol nilai = tanpa filter tersebut).
type ActualCostScope struct {
	ProjectID       uint64  // 0 = semua proyek
	PhaseID         *uint64 // nil = semua fase
	UnitID          *uint64 // non-nil = hanya baris ber-tag unit ini
	ProjectWideOnly bool    // true = hanya baris unit_id IS NULL

	// ExcludeSource, bila diisi, mengecualikan SELURUH jurnal ber-source ini
	// dari pembacaan (bukan hanya dari netting reversal). "" = tanpa
	// pengecualian. Dipakai budget.GetRealisasiByProject untuk mengecualikan
	// jurnal kapitalisasi RAB (source "rab_capitalization") dari realisasi
	// Construction/Hard — lihat komentar di sana untuk alasan lengkapnya.
	ExcludeSource string
}

// ActualCostByCode mengembalikan biaya aktual (netting kanonik) per kode akun.
func (q *QueryService) ActualCostByCode(ctx context.Context, tenantID uint64, codes []string, sc ActualCostScope) (map[string]domain.Money, error) {
	if len(codes) == 0 {
		return map[string]domain.Money{}, nil
	}
	qq := q.db.WithContext(ctx).
		Table("journal_lines jl").
		Select("a.code AS code, "+actualCostNettingExpr+" AS total").
		Joins("JOIN journal_entries je ON je.id = jl.journal_entry_id").
		Joins("JOIN accounts a ON a.id = jl.account_id AND a.tenant_id = jl.tenant_id").
		Where("jl.tenant_id = ? AND je.posted_at IS NOT NULL", tenantID).
		Where("a.code IN ?", codes).
		Group("a.code")

	if sc.ProjectID != 0 {
		qq = qq.Where("jl.project_id = ?", sc.ProjectID)
	}
	if sc.PhaseID != nil {
		qq = qq.Where("jl.phase_id = ?", *sc.PhaseID)
	}
	if sc.UnitID != nil {
		qq = qq.Where("jl.unit_id = ?", *sc.UnitID)
	}
	if sc.ProjectWideOnly {
		qq = qq.Where("jl.unit_id IS NULL")
	}
	if sc.ExcludeSource != "" {
		qq = qq.Where("je.source <> ?", sc.ExcludeSource)
	}

	var rows []struct {
		Code  string
		Total domain.Money
	}
	if err := qq.Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("ledger: ActualCostByCode: %w", err)
	}
	out := make(map[string]domain.Money, len(rows))
	for _, r := range rows {
		out[r.Code] = r.Total
	}
	return out, nil
}

// ActualCostTotalsAllProjects: total biaya aktual (netting kanonik, kode
// terpilih) per project_id dalam SATU query — dipakai dashboard per-proyek.
func (q *QueryService) ActualCostTotalsAllProjects(ctx context.Context, tenantID uint64, codes []string) (map[uint64]domain.Money, error) {
	if len(codes) == 0 {
		return map[uint64]domain.Money{}, nil
	}
	var rows []struct {
		ProjectID uint64
		Total     domain.Money
	}
	err := q.db.WithContext(ctx).
		Table("journal_lines jl").
		Select("jl.project_id AS project_id, "+actualCostNettingExpr+" AS total").
		Joins("JOIN journal_entries je ON je.id = jl.journal_entry_id").
		Joins("JOIN accounts a ON a.id = jl.account_id AND a.tenant_id = jl.tenant_id").
		Where("jl.tenant_id = ? AND je.posted_at IS NOT NULL AND jl.project_id IS NOT NULL", tenantID).
		Where("a.code IN ?", codes).
		Group("jl.project_id").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("ledger: ActualCostTotalsAllProjects: %w", err)
	}
	out := make(map[uint64]domain.Money, len(rows))
	for _, r := range rows {
		out[r.ProjectID] = r.Total
	}
	return out, nil
}

// actualCostNettingExpr adalah SATU-SATUNYA ekspresi netting biaya aktual.
// Jangan tulis ulang di SQL mana pun — konsumsi lewat ActualCostByCode /
// ActualCostTotalsAllProjects.
const actualCostNettingExpr = `COALESCE(SUM(CASE WHEN je.source <> 'reversal' THEN jl.debit ELSE 0 END), 0)
	- COALESCE(SUM(CASE WHEN je.source = 'reversal' THEN jl.credit ELSE 0 END), 0)`
