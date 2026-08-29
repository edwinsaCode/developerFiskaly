package reporting

// P2 — Admin Marketing Performance: paralel dari sales_performance.go, TAPI
// untuk peran Admin Marketing (P1: independen dari SalesPersonID, disimpan di
// kolom admin_marketing_person_id — bukan tabel kedua). Admin Marketing
// menangani dokumen/KPR/admin — TIDAK pernah menerima komisi (invariant
// komisi tetap eksklusif ke SalesPersonID, lihat internal/commission).

import (
	"context"
	"fmt"

	"esaproperti/internal/domain"
)

type AdminMarketingPerformanceRow struct {
	AdminMarketingPersonID uint64 `json:"admin_marketing_person_id"`
	Name                   string `json:"name"`
	IsActive               bool   `json:"is_active"`

	ContractsHandled int    `json:"contracts_handled"`
	ProjectsHandled  int    `json:"projects_handled"`
	ProjectNames     string `json:"project_names"` // CSV label, bukan SoT — hanya visibilitas
	KPRCount         int    `json:"kpr_count"`
	CashCount        int    `json:"cash_count"`
	BASTCount        int    `json:"bast_count"`

	ContractValue string `json:"contract_value"` // Σ NetContract kontrak aktif (kanonik R4)
	Collected     string `json:"collected"`      // Σ TotalPaid kanonik
}

type AdminMarketingPerformanceReport struct {
	Rows []AdminMarketingPerformanceRow `json:"rows"`
	// Cakupan penugasan: berapa kontrak aktif sudah/belum punya Admin Marketing.
	TotalContractsAssigned   int `json:"total_contracts_assigned"`
	TotalContractsUnassigned int `json:"total_contracts_unassigned"`
}

func (q *dashboardQuerier) GetAdminMarketingPerformance(ctx context.Context, tenantID uint64) (*AdminMarketingPerformanceReport, error) {
	rep := &AdminMarketingPerformanceReport{}

	var rows []AdminMarketingPerformanceRow
	err := q.db.WithContext(ctx).Raw(`
		SELECT sp.id AS admin_marketing_person_id, sp.name, sp.is_active,
		  COALESCE(c.total, 0)          AS contracts_handled,
		  COALESCE(c.projects, 0)       AS projects_handled,
		  COALESCE(c.project_names, '') AS project_names,
		  COALESCE(c.kpr, 0)            AS kpr_count,
		  COALESCE(c.cash, 0)           AS cash_count,
		  COALESCE(sr.cnt, 0)           AS bast_count
		FROM sales_persons sp
		LEFT JOIN (
		  SELECT sc.admin_marketing_person_id,
		         COUNT(*) AS total,
		         SUM(sc.payment_type = 'kpr')   AS kpr,
		         SUM(sc.payment_type = 'tunai') AS cash,
		         COUNT(DISTINCT u.project_id)   AS projects,
		         GROUP_CONCAT(DISTINCT p.name ORDER BY p.name SEPARATOR ', ') AS project_names
		  FROM sale_contracts sc
		  JOIN units u ON u.id = sc.unit_id AND u.tenant_id = sc.tenant_id
		  JOIN projects p ON p.id = u.project_id AND p.tenant_id = sc.tenant_id
		  WHERE sc.tenant_id = ? AND sc.admin_marketing_person_id IS NOT NULL
		    AND (sc.scheme_state IS NULL OR sc.scheme_state <> 'cancelled')
		  GROUP BY sc.admin_marketing_person_id
		) c ON c.admin_marketing_person_id = sp.id
		LEFT JOIN (
		  SELECT sc2.admin_marketing_person_id, COUNT(*) AS cnt
		  FROM sale_records sr2
		  JOIN sale_contracts sc2 ON sc2.unit_id = sr2.unit_id AND sc2.tenant_id = sr2.tenant_id
		  WHERE sr2.tenant_id = ? AND sr2.cancelled_at IS NULL AND sc2.admin_marketing_person_id IS NOT NULL
		  GROUP BY sc2.admin_marketing_person_id
		) sr ON sr.admin_marketing_person_id = sp.id
		WHERE sp.tenant_id = ?
		ORDER BY COALESCE(c.total, 0) DESC, sp.name`,
		tenantID, tenantID, tenantID).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("admin marketing performance: %w", err)
	}

	portfolio, err := q.finance.PortfolioFinancials(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("admin marketing performance portfolio: %w", err)
	}
	type amAgg struct{ value, collected domain.Money }
	byAM := map[uint64]*amAgg{}
	for _, pr := range portfolio {
		if pr.AdminMarketingPersonID == nil {
			continue
		}
		a := byAM[*pr.AdminMarketingPersonID]
		if a == nil {
			a = &amAgg{value: domain.Zero, collected: domain.Zero}
			byAM[*pr.AdminMarketingPersonID] = a
		}
		a.value = a.value.Add(pr.NetContract)
		a.collected = a.collected.Add(pr.TotalPaid)
	}

	for i := range rows {
		value, collected := domain.Zero, domain.Zero
		if a := byAM[rows[i].AdminMarketingPersonID]; a != nil {
			value, collected = a.value, a.collected
		}
		rows[i].ContractValue = value.String()
		rows[i].Collected = collected.String()
	}
	rep.Rows = rows

	var t struct {
		Assigned   int
		Unassigned int
	}
	if err := q.db.WithContext(ctx).Raw(`
		SELECT
		  SUM(admin_marketing_person_id IS NOT NULL) AS assigned,
		  SUM(admin_marketing_person_id IS NULL)     AS unassigned
		FROM sale_contracts
		WHERE tenant_id = ? AND (scheme_state IS NULL OR scheme_state <> 'cancelled')`,
		tenantID).Scan(&t).Error; err != nil {
		return nil, fmt.Errorf("admin marketing coverage: %w", err)
	}
	rep.TotalContractsAssigned, rep.TotalContractsUnassigned = t.Assigned, t.Unassigned
	return rep, nil
}
