package reporting

// R1 KPR Realization — pipeline KPR: satu layar semua kontrak KPR & posisinya
// (pengajuan → SP3K → akad → cair → lunas). Read-only, derived:
//   - state dari sale_contracts.scheme_state (proyeksi audit scheme flow)
//   - dana cair = Σ termin_payments source 'kpr_disbursement' (kanonik #18)
//   - outstanding & total_paid = sale.ContractOutstandingPaid (kanonik #7/#8, S3)
//   - umur state = hari sejak event terakhir yang mengubah ke state tsb.

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// dec mem-parse string DECIMAL SQL → decimal (Invariant #2: tanpa float).
func dec(s string) decimal.Decimal {
	d, err := decimal.NewFromString(nz(s))
	if err != nil {
		return decimal.Zero
	}
	return d
}

type KPRPipelineRow struct {
	ContractID    uint64 `json:"contract_id"`
	UnitID        uint64 `json:"unit_id"`
	UnitCode      string `json:"unit_code"`
	ProjectName   string `json:"project_name"`
	BuyerName     string `json:"buyer_name"`
	BankName      string `json:"bank_name,omitempty"`
	SchemeState   string `json:"scheme_state"` // signed|dp_paid|submitted_to_bank|bank_approved|akad|disbursed|…
	GrossAmount   string `json:"gross_amount"`
	LoanAmount    string `json:"loan_amount,omitempty"` // plafon pengajuan (opsional)
	DisbursedTotal string `json:"disbursed_total"`      // Σ pencairan bank
	TotalPaid     string `json:"total_paid"`
	Outstanding   string `json:"outstanding"`
	StateSince    *time.Time `json:"state_since,omitempty"` // event terakhir yang mengubah state
	StateAgeDays  int        `json:"state_age_days"`
}

type KPRPipelineReport struct {
	AsOf time.Time        `json:"as_of"`
	Rows []KPRPipelineRow `json:"rows"`
	// Counts per tahap (untuk header board & dashboard).
	CountPreparation int `json:"count_preparation"` // signed|dp_paid (belum diajukan)
	CountSubmitted   int `json:"count_submitted"`
	CountApproved    int `json:"count_approved"` // SP3K
	CountAkad        int `json:"count_akad"`
	CountDisbursed   int `json:"count_disbursed"` // cair, outstanding > 0
	CountSettled     int `json:"count_settled"`   // outstanding <= 0
	TotalDisbursed   string `json:"total_disbursed"`
	TotalOutstanding string `json:"total_outstanding"` // Σ outstanding kontrak KPR belum lunas
}

type kprPipelineQuerier struct {
	db      *gorm.DB
	finance ContractFinanceReader // S3: outstanding/total_paid kanonik
}

func (q *kprPipelineQuerier) GetKPRPipeline(ctx context.Context, tenantID uint64, now time.Time) (*KPRPipelineReport, error) {
	var rows []struct {
		ContractID     uint64
		UnitID         uint64
		UnitCode       string
		ProjectName    string
		BuyerName      string
		BankName       string
		SchemeState    string
		GrossAmount    string
		LoanAmount     string
		DisbursedTotal string
		TotalPaid      string
		StateSince     *time.Time
	}
	err := q.db.WithContext(ctx).Raw(`
		SELECT
		  sc.id            AS contract_id,
		  sc.unit_id       AS unit_id,
		  u.code           AS unit_code,
		  p.name           AS project_name,
		  sc.buyer_name    AS buyer_name,
		  COALESCE(fs.name, sc.bank_kpr, '') AS bank_name,
		  COALESCE(sc.scheme_state, 'signed') AS scheme_state,
		  sc.gross_amount  AS gross_amount,
		  COALESCE(sc.loan_amount, 0) AS loan_amount,
		  COALESCE(tp.disbursed, 0)   AS disbursed_total,
		  '0'                         AS total_paid,
		  ev.last_change              AS state_since
		FROM sale_contracts sc
		JOIN units u    ON u.id = sc.unit_id AND u.tenant_id = sc.tenant_id
		JOIN projects p ON p.id = u.project_id
		LEFT JOIN financing_sources fs ON fs.id = sc.financing_source_id AND fs.tenant_id = sc.tenant_id
		LEFT JOIN (
		  SELECT unit_id,
		    SUM(CASE WHEN payment_source = 'kpr_disbursement' THEN amount ELSE 0 END) AS disbursed
		  FROM termin_payments WHERE tenant_id = ?
		  GROUP BY unit_id
		) tp ON tp.unit_id = sc.unit_id
		LEFT JOIN (
		  SELECT sale_contract_id, MAX(event_date) AS last_change
		  FROM contract_payment_events
		  WHERE tenant_id = ? AND to_state <> from_state AND to_state <> ''
		  GROUP BY sale_contract_id
		) ev ON ev.sale_contract_id = sc.id
		WHERE sc.tenant_id = ?
		  AND sc.payment_type = 'kpr'
		  AND (sc.scheme_state IS NULL OR sc.scheme_state NOT IN ('cancelled','converted'))
		ORDER BY sc.id DESC`,
		tenantID, tenantID, tenantID).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("kpr pipeline: %w", err)
	}

	rep := &KPRPipelineReport{AsOf: now, Rows: make([]KPRPipelineRow, 0, len(rows))}
	totalDisb, totalOut := decimal.Zero, decimal.Zero
	for _, r := range rows {
		// S3: outstanding & total_paid dari sumber KANONIK (ContractFinancialSummary),
		// bukan `gross − Σtermin` yang dihitung querier sendiri. Definisi tunggal.
		outM, paidM, ferr := q.finance.ContractOutstandingPaid(ctx, tenantID, r.ContractID)
		if ferr != nil {
			return nil, fmt.Errorf("kpr outstanding kontrak %d: %w", r.ContractID, ferr)
		}
		r.TotalPaid = paidM.String()
		outstanding := outM.Decimal()
		age := 0
		if r.StateSince != nil {
			age = int(now.Sub(*r.StateSince).Hours() / 24)
			if age < 0 {
				age = 0
			}
		}
		rep.Rows = append(rep.Rows, KPRPipelineRow{
			ContractID: r.ContractID, UnitID: r.UnitID, UnitCode: r.UnitCode,
			ProjectName: r.ProjectName, BuyerName: r.BuyerName, BankName: r.BankName,
			SchemeState: r.SchemeState, GrossAmount: nz(r.GrossAmount),
			LoanAmount: nz(r.LoanAmount), DisbursedTotal: nz(r.DisbursedTotal),
			TotalPaid: nz(r.TotalPaid), Outstanding: outstanding.String(),
			StateSince: r.StateSince, StateAgeDays: age,
		})

		settled := !outstanding.IsPositive()
		switch {
		case settled:
			rep.CountSettled++
		case r.SchemeState == "submitted_to_bank":
			rep.CountSubmitted++
		case r.SchemeState == "bank_approved":
			rep.CountApproved++
		case r.SchemeState == "akad":
			rep.CountAkad++
		case r.SchemeState == "disbursed":
			rep.CountDisbursed++
		default: // signed | dp_paid | bank_rejected
			rep.CountPreparation++
		}
		totalDisb = totalDisb.Add(dec(r.DisbursedTotal))
		if !settled {
			totalOut = totalOut.Add(outstanding)
		}
	}
	rep.TotalDisbursed = totalDisb.String()
	rep.TotalOutstanding = totalOut.String()
	return rep, nil
}
