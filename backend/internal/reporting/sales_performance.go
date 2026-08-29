package reporting

// PS-3 — Sales Performance (blueprint §4 READ-MODEL + §8): funnel & kinerja per
// salesperson, derived dari leads/bookings/contracts/BAST/commissions/ledger.
// Tidak ada saldo tersimpan.

import (
	"context"
	"fmt"

	"esaproperti/internal/domain"
)

type SalesPerformanceRow struct {
	SalesPersonID uint64 `json:"sales_person_id"`
	Name          string `json:"name"`
	IsActive      bool   `json:"is_active"`

	Leads          int `json:"leads"`
	LeadsConverted int `json:"leads_converted"`
	BookingsActive int `json:"bookings_active"`
	BookingsTotal  int `json:"bookings_total"`
	Contracts      int `json:"contracts"`
	BASTCount      int `json:"bast_count"`

	ContractValue string `json:"contract_value"` // Σ NetContract kontrak aktif (kanonik R4)
	BASTValue     string `json:"bast_value"`     // Σ harga jual BAST non-cancelled (nilai BAST — dokumen)
	Collected     string `json:"collected"`      // Σ TotalPaid kanonik (counts_toward_price=TRUE)

	CommissionEarned string `json:"commission_earned"` // Σ komisi non-cancelled/clawed
	CommissionPaid   string `json:"commission_paid"`
}

type SalesPerformanceReport struct {
	Rows []SalesPerformanceRow `json:"rows"`
	// Totals funnel perusahaan (termasuk yang tanpa atribusi sales).
	TotalLeads     int `json:"total_leads"`
	TotalBookings  int `json:"total_bookings"`
	TotalContracts int `json:"total_contracts"`
	TotalBAST      int `json:"total_bast"`
}

func (q *dashboardQuerier) GetSalesPerformance(ctx context.Context, tenantID uint64) (*SalesPerformanceReport, error) {
	rep := &SalesPerformanceReport{}

	// R4: contract_value & collected per salesperson dari baris portfolio
	// KANONIK (sale.PortfolioFinancials) — bukan SQL Σ gross / Σ termin sendiri.
	// bast_value tetap dari sale_records (nilai BAST = angka DOKUMEN, dilabeli
	// demikian di registry #9 — bukan "revenue").
	var rows []SalesPerformanceRow
	err := q.db.WithContext(ctx).Raw(`
		SELECT sp.id AS sales_person_id, sp.name, sp.is_active,
		  COALESCE(l.total, 0)      AS leads,
		  COALESCE(l.converted, 0)  AS leads_converted,
		  COALESCE(b.active, 0)     AS bookings_active,
		  COALESCE(b.total, 0)      AS bookings_total,
		  COALESCE(c.total, 0)      AS contracts,
		  COALESCE(sr.cnt, 0)       AS bast_count,
		  COALESCE(sr.value, 0)     AS bast_value,
		  COALESCE(cm.earned, 0)    AS commission_earned,
		  COALESCE(cm.paid, 0)      AS commission_paid
		FROM sales_persons sp
		LEFT JOIN (
		  SELECT sales_person_id, COUNT(*) AS total,
		         SUM(status = 'converted') AS converted
		  FROM leads WHERE tenant_id = ? GROUP BY sales_person_id
		) l ON l.sales_person_id = sp.id
		LEFT JOIN (
		  SELECT sales_person_id, COUNT(*) AS total,
		         SUM(status = 'active') AS active
		  FROM bookings WHERE tenant_id = ? GROUP BY sales_person_id
		) b ON b.sales_person_id = sp.id
		LEFT JOIN (
		  SELECT sales_person_id, COUNT(*) AS total
		  FROM sale_contracts
		  WHERE tenant_id = ? AND (scheme_state IS NULL OR scheme_state <> 'cancelled')
		  GROUP BY sales_person_id
		) c ON c.sales_person_id = sp.id
		LEFT JOIN (
		  SELECT sc.sales_person_id, COUNT(*) AS cnt, SUM(sr2.sale_price) AS value
		  FROM sale_records sr2
		  JOIN sale_contracts sc ON sc.unit_id = sr2.unit_id AND sc.tenant_id = sr2.tenant_id
		  WHERE sr2.tenant_id = ? AND sr2.cancelled_at IS NULL
		  GROUP BY sc.sales_person_id
		) sr ON sr.sales_person_id = sp.id
		LEFT JOIN (
		  SELECT sales_person_id,
		         SUM(CASE WHEN status NOT IN ('cancelled','clawed_back') THEN amount ELSE 0 END) AS earned,
		         SUM(CASE WHEN status = 'paid' THEN amount ELSE 0 END) AS paid
		  FROM commissions WHERE tenant_id = ? GROUP BY sales_person_id
		) cm ON cm.sales_person_id = sp.id
		WHERE sp.tenant_id = ?
		ORDER BY COALESCE(sr.value, 0) DESC, sp.name`,
		tenantID, tenantID, tenantID, tenantID, tenantID, tenantID).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("sales performance: %w", err)
	}

	portfolio, err := q.finance.PortfolioFinancials(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("sales performance portfolio: %w", err)
	}
	type spAgg struct{ value, collected domain.Money }
	bySP := map[uint64]*spAgg{}
	for _, pr := range portfolio {
		if pr.SalesPersonID == nil {
			continue
		}
		a := bySP[*pr.SalesPersonID]
		if a == nil {
			a = &spAgg{value: domain.Zero, collected: domain.Zero}
			bySP[*pr.SalesPersonID] = a
		}
		a.value = a.value.Add(pr.NetContract)
		a.collected = a.collected.Add(pr.TotalPaid)
	}

	for i := range rows {
		value, collected := domain.Zero, domain.Zero
		if a := bySP[rows[i].SalesPersonID]; a != nil {
			value, collected = a.value, a.collected
		}
		rows[i].ContractValue = value.String()
		rows[i].Collected = collected.String()
		rows[i].BASTValue = nz(rows[i].BASTValue)
		rows[i].CommissionEarned = nz(rows[i].CommissionEarned)
		rows[i].CommissionPaid = nz(rows[i].CommissionPaid)
	}
	rep.Rows = rows

	// Totals funnel perusahaan.
	var t struct {
		Leads     int
		Bookings  int
		Contracts int
		Bast      int
	}
	if err := q.db.WithContext(ctx).Raw(`
		SELECT
		  (SELECT COUNT(*) FROM leads WHERE tenant_id = ?) AS leads,
		  (SELECT COUNT(*) FROM bookings WHERE tenant_id = ?) AS bookings,
		  (SELECT COUNT(*) FROM sale_contracts WHERE tenant_id = ?
		     AND (scheme_state IS NULL OR scheme_state <> 'cancelled')) AS contracts,
		  (SELECT COUNT(*) FROM sale_records WHERE tenant_id = ? AND cancelled_at IS NULL) AS bast`,
		tenantID, tenantID, tenantID, tenantID).Scan(&t).Error; err != nil {
		return nil, fmt.Errorf("sales totals: %w", err)
	}
	rep.TotalLeads, rep.TotalBookings, rep.TotalContracts, rep.TotalBAST =
		t.Leads, t.Bookings, t.Contracts, t.Bast
	return rep, nil
}
