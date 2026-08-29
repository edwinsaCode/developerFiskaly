package reporting

// P2b — Contract Workload: drill-down PER KONTRAK untuk Sales & Admin
// Marketing. GetSalesPerformance / GetAdminMarketingPerformance (paralel di
// sales_performance.go / admin_marketing_performance.go) hanya agregat
// per-orang — tidak cukup untuk pertanyaan management "Admin Marketing A
// sedang menangani proyek apa dan kontrak mana, sudah sampai tahap apa?".
// SATU query di sini melayani KEDUA tab (frontend filter per person_id),
// dibangun di atas sumber kanonik yang sama (sale.PortfolioFinancials) agar
// nilai kontrak/collected tidak pernah dihitung ulang secara berbeda.

import (
	"context"
	"database/sql"
	"fmt"
)

type ContractWorkloadRow struct {
	ContractID             uint64  `json:"contract_id"`
	UnitCode               string  `json:"unit_code"`
	ProjectID              uint64  `json:"project_id"`
	ProjectName            string  `json:"project_name"`
	BuyerName              string  `json:"buyer_name"`
	SalesPersonID          *uint64 `json:"sales_person_id"`
	AdminMarketingPersonID *uint64 `json:"admin_marketing_person_id"`
	PaymentType            string  `json:"payment_type"` // kpr|tunai
	// SchemeState — tahap kontrak (signed|dp_paid|submitted_to_bank|
	// bank_approved|akad|disbursed|fully_paid|handed_over), sama dgn nilai
	// yang dipakai KPRPipelineBoard — bukan label baru.
	SchemeState string `json:"scheme_state"`
	HandedOver  bool   `json:"handed_over"` // BAST sudah terbit (sale_records, non-cancelled)

	ContractValue string `json:"contract_value"` // NetContract kanonik (R4)
	Collected     string `json:"collected"`      // TotalPaid kanonik
}

type ContractWorkloadReport struct {
	Rows []ContractWorkloadRow `json:"rows"`
}

func (q *dashboardQuerier) GetContractWorkload(ctx context.Context, tenantID uint64) (*ContractWorkloadReport, error) {
	rep := &ContractWorkloadReport{}

	var meta []struct {
		ContractID  uint64
		UnitCode    string
		ProjectID   uint64
		ProjectName string
		BuyerName   string
		PaymentType string
		SchemeState sql.NullString
		HandedOver  bool
	}
	err := q.db.WithContext(ctx).Raw(`
		SELECT sc.id AS contract_id, u.code AS unit_code, p.id AS project_id, p.name AS project_name,
		  sc.buyer_name, sc.payment_type, sc.scheme_state,
		  (sr.id IS NOT NULL) AS handed_over
		FROM sale_contracts sc
		JOIN units u ON u.id = sc.unit_id AND u.tenant_id = sc.tenant_id
		JOIN projects p ON p.id = u.project_id AND p.tenant_id = sc.tenant_id
		LEFT JOIN sale_records sr
		  ON sr.unit_id = sc.unit_id AND sr.tenant_id = sc.tenant_id AND sr.cancelled_at IS NULL
		WHERE sc.tenant_id = ? AND (sc.scheme_state IS NULL OR sc.scheme_state <> 'cancelled')
		ORDER BY sc.contract_date DESC`,
		tenantID).Scan(&meta).Error
	if err != nil {
		return nil, fmt.Errorf("contract workload: %w", err)
	}

	portfolio, err := q.finance.PortfolioFinancials(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("contract workload portfolio: %w", err)
	}
	byContract := make(map[uint64]int, len(portfolio))
	for i, pr := range portfolio {
		byContract[pr.ContractID] = i
	}

	rows := make([]ContractWorkloadRow, 0, len(meta))
	for _, m := range meta {
		row := ContractWorkloadRow{
			ContractID:  m.ContractID,
			UnitCode:    m.UnitCode,
			ProjectID:   m.ProjectID,
			ProjectName: m.ProjectName,
			BuyerName:   m.BuyerName,
			PaymentType: m.PaymentType,
			SchemeState: "signed", // kontrak baru tanpa scheme_state = tahap awal
			HandedOver:  m.HandedOver,
		}
		if m.SchemeState.Valid && m.SchemeState.String != "" {
			row.SchemeState = m.SchemeState.String
		}
		if i, ok := byContract[m.ContractID]; ok {
			pr := portfolio[i]
			row.SalesPersonID = pr.SalesPersonID
			row.AdminMarketingPersonID = pr.AdminMarketingPersonID
			row.ContractValue = pr.NetContract.String()
			row.Collected = pr.TotalPaid.String()
		} else {
			row.ContractValue = "0"
			row.Collected = "0"
		}
		rows = append(rows, row)
	}
	rep.Rows = rows
	return rep, nil
}
