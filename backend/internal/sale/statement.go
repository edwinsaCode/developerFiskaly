package sale

import (
	"context"
	"sort"
	"time"

	"esaproperti/internal/domain"
	"esaproperti/internal/receivable"
)

// ── Customer Statement (rekening koran buyer) ────────────────────────────────
//
// Statement adalah pandangan per-kontrak atas kewajiban buyer: nilai kontrak,
// jadwal cicilan, yang sudah dibayar, sisa, dan cicilan yang menunggak.
//
// TotalPaid memakai Σ termin diterima (SumTerminsByUnit) — angka kas yang benar-
// benar diterima dari buyer dan dasar RemainingBalance. Ini KONSISTEN dengan
// Service.BuyerRemainingBalance (tidak ada dua definisi "sisa" yang bertabrakan).
// Daftar cicilan menampilkan status per-installment untuk transparansi koleksi.

// StatementScheduleLine adalah satu baris cicilan pada statement.
// Status DIHITUNG DI BACKEND (bukan hanya frontend): paid | overdue | due_today | scheduled.
type StatementScheduleLine struct {
	ScheduleID        uint64 `json:"schedule_id"`
	InstallmentNumber int    `json:"installment_number"`
	Type              string `json:"type"`
	DueDate           string `json:"due_date"`
	Amount            string `json:"amount"`
	// Paid & Outstanding (T-5): pembayaran SEBAGIAN harus terlihat di rekening
	// koran pembeli — tanpa ini "cicilan 100jt menunggak" tidak bisa dibedakan
	// dari "sisa 60jt menunggak". Definisi identik dengan AR Aging.
	Paid            string  `json:"paid"`
	Outstanding     string  `json:"outstanding"`
	Status          string  `json:"status"` // paid | overdue | due_today | scheduled
	ReceivedAt      *string `json:"received_at,omitempty"`
	TerminPaymentID *uint64 `json:"termin_payment_id,omitempty"` // untuk cetak kwitansi
	Overdue         bool    `json:"overdue"`
	DaysOverdue     int     `json:"days_overdue"`
}

// CustomerStatement adalah rekening koran satu kontrak.
type CustomerStatement struct {
	ContractID   uint64 `json:"contract_id"`
	UnitID       uint64 `json:"unit_id"`
	BuyerName    string `json:"buyer_name"`
	BuyerID      string `json:"buyer_id"`
	PaymentType  string `json:"payment_type"`
	ContractDate string `json:"contract_date"`
	AsOf         string `json:"as_of"`

	ContractValue string `json:"contract_value"` // GrossAmount (tagihan ke buyer)
	DPPAmount     string `json:"dpp_amount"`
	IsPKP         bool   `json:"is_pkp"`

	// R2 additive: konteks pembiayaan untuk header Statement 360.
	BankKPR     string  `json:"bank_kpr,omitempty"`
	LoanAmount  *string `json:"loan_amount,omitempty"`
	SchemeState *string `json:"scheme_state,omitempty"`

	TotalScheduled   string `json:"total_scheduled"`   // Σ nominal cicilan
	TotalPaid        string `json:"total_paid"`        // Σ termin diterima (kas, autoritatif)
	RemainingBalance string `json:"remaining_balance"` // ContractValue − TotalPaid
	TotalOverdue     string `json:"total_overdue"`     // Σ outstanding yang sudah lewat jatuh tempo
	OverdueCount     int    `json:"overdue_count"`

	Schedules []StatementScheduleLine `json:"schedules"`

	// ── R1/R2 additive: perjalanan customer lengkap ──────────────────────────
	// Summary: SATU rumus (H-2) — harga unit, diskon, terutang.
	Summary *ContractFinancialSummary `json:"summary,omitempty"`
	// Payments: setiap penerimaan (booking fee, DP, manual, PENCAIRAN BANK)
	// dengan sumbernya — kwitansi di-link UI via GET /termins/{id}/receipt.
	Payments []StatementPaymentLine `json:"payments,omitempty"`
	// Timeline: audit trail lifecycle (pengajuan → SP3K → akad → cair → BAST).
	Timeline []*ContractPaymentEvent `json:"timeline,omitempty"`

	// Exposure (W-4): SATU angka "customer ini masih ditagih berapa" — harga
	// rumah dan biaya realisasi dijumlahkan DI SERVER. Sebelumnya kedua angka
	// hidup di dua seksi layar dan tidak pernah bertemu; kalau dua layar boleh
	// menjumlahkan sendiri, cepat atau lambat dua layar akan berbeda.
	Exposure *StatementExposure `json:"exposure,omitempty"`
}

// StatementExposure adalah rincian eksposur piutang satu kontrak per asOf.
// Definisi outstanding & menunggak IDENTIK dengan AR Aging (satu mesin,
// receivable.BuildAging) — bukan rumus kedua yang kebetulan mirip.
type StatementExposure struct {
	HouseOutstanding       string `json:"house_outstanding"`
	HouseOverdue           string `json:"house_overdue"`
	RealizationOutstanding string `json:"realization_outstanding"`
	RealizationOverdue     string `json:"realization_overdue"`
	TotalOutstanding       string `json:"total_outstanding"`
	TotalOverdue           string `json:"total_overdue"`
	// RealizationAvailable=false berarti sumber tagihan realisasi tidak
	// terpasang/gagal dibaca. Layar WAJIB membedakannya dari "nol" — menampilkan
	// Rp0 untuk data yang tidak diketahui adalah berbohong dengan percaya diri.
	RealizationAvailable bool `json:"realization_available"`
}

// StatementPaymentLine adalah satu penerimaan dalam perjalanan customer.
type StatementPaymentLine struct {
	TerminID          uint64  `json:"termin_id"`
	Date              string  `json:"date"`
	Amount            string  `json:"amount"`
	Source            string  `json:"source"` // booking_fee|unit_termin|collection|schedule_received|kpr_disbursement
	BankAccountCode   string  `json:"bank_account_code,omitempty"`
	Reference         string  `json:"reference,omitempty"`
	FinancingSourceID *uint64 `json:"financing_source_id,omitempty"`
	// CountsTowardPrice (R4): false = pembayaran DI LUAR harga unit (booking
	// fee kebijakan baru) — UI menampilkannya terpisah, TIDAK dijumlahkan ke
	// "Total Diterima" harga.
	CountsTowardPrice bool `json:"counts_toward_price"`
}

// GetCustomerStatement menyusun rekening koran untuk satu kontrak per tanggal asOf.
// Tenant-scoped via store (Invariant #6). Menunggak dihitung dari due_date vs asOf
// (deterministik) — tidak bergantung apakah job MarkOverdue sudah berjalan.
func (s *Service) GetCustomerStatement(ctx context.Context, tenantID, contractID uint64, asOf time.Time) (*CustomerStatement, error) {
	if err := s.requireContractStore(); err != nil {
		return nil, err
	}

	contract, err := s.contracts.FindContractByID(ctx, tenantID, contractID)
	if err != nil {
		return nil, err
	}

	schedules, err := s.contracts.ListSchedulesByContract(ctx, tenantID, contractID)
	if err != nil {
		return nil, err
	}

	// Urutkan deterministik: nomor cicilan menaik (ties: jatuh tempo lalu ID).
	sort.SliceStable(schedules, func(i, j int) bool {
		a, b := schedules[i], schedules[j]
		if a.InstallmentNumber != b.InstallmentNumber {
			return a.InstallmentNumber < b.InstallmentNumber
		}
		if !a.DueDate.Equal(b.DueDate) {
			return a.DueDate.Before(b.DueDate)
		}
		return a.ID < b.ID
	})

	collected, err := s.store.SumTerminsByUnit(ctx, tenantID, contract.UnitID)
	if err != nil {
		return nil, err
	}

	stmt := &CustomerStatement{
		ContractID:    contract.ID,
		UnitID:        contract.UnitID,
		BuyerName:     contract.BuyerName,
		BuyerID:       contract.BuyerID,
		PaymentType:   string(contract.PaymentType),
		ContractDate:  contract.ContractDate.Format("2006-01-02"),
		AsOf:          asOf.Format("2006-01-02"),
		ContractValue: contract.GrossAmount.String(),
		DPPAmount:     contract.DPPAmount.String(),
		IsPKP:         contract.IsPKP,
		SchemeState:   contract.SchemeState,
		Schedules:     make([]StatementScheduleLine, 0, len(schedules)),
	}
	if contract.BankKPR != nil {
		stmt.BankKPR = *contract.BankKPR
	}
	if contract.LoanAmount != nil {
		la := contract.LoanAmount.String()
		stmt.LoanAmount = &la
	}

	// R1/R2 additive: summary + rincian pembayaran + timeline (best-effort —
	// kegagalan bagian opsional tidak menggagalkan statement inti).
	if sum, serr := s.summarizeContract(ctx, tenantID, contract); serr == nil {
		stmt.Summary = sum
	}
	if termins, terr := s.store.ListTerminsByUnit(ctx, tenantID, contract.UnitID); terr == nil {
		stmt.Payments = make([]StatementPaymentLine, 0, len(termins))
		for _, tp := range termins {
			// RULE KLIEN 2026-07-29: statement kontrak = murni pembayaran
			// RUMAH. Penerimaan di luar harga (booking fee = Pendapatan
			// Booking) TIDAK ditampilkan di sini — ia hidup di Laba Rugi.
			// Fee pra-R4 (flag TRUE, historis bagian harga) tetap tampil.
			if !tp.CountsTowardPrice {
				continue
			}
			stmt.Payments = append(stmt.Payments, StatementPaymentLine{
				TerminID:          tp.ID,
				Date:              tp.Date.Format("2006-01-02"),
				Amount:            tp.Amount.String(),
				Source:            string(tp.PaymentSource),
				BankAccountCode:   tp.BankAccountCode,
				Reference:         tp.Description,
				FinancingSourceID: tp.FinancingSourceID,
				CountsTowardPrice: tp.CountsTowardPrice,
			})
		}
	}
	if s.schemeEnabled() {
		if evs, everr := s.ListPaymentEvents(ctx, tenantID, contractID); everr == nil {
			stmt.Timeline = evs
		}
	}

	var totalScheduled, totalOverdue domain.Money
	for _, sch := range schedules {
		totalScheduled = totalScheduled.Add(sch.Amount)

		st := scheduleStatusAt(sch.Amount, sch.PaidAmount,
			sch.Status == ScheduleStatusReceived, sch.DueDate, asOf)
		effectivePaid, outstanding := st.Paid, st.Outstanding
		days, overdue, effectiveStatus := st.DaysOverdue, st.Overdue, st.Status

		var receivedAt *string
		if sch.ReceivedAt != nil {
			s := sch.ReceivedAt.Format("2006-01-02")
			receivedAt = &s
		}

		line := StatementScheduleLine{
			ScheduleID:        sch.ID,
			InstallmentNumber: sch.InstallmentNumber,
			Type:              string(sch.Type),
			DueDate:           sch.DueDate.Format("2006-01-02"),
			Amount:            sch.Amount.String(),
			Paid:              effectivePaid.String(),
			Outstanding:       outstanding.String(),
			Status:            effectiveStatus,
			ReceivedAt:        receivedAt,
			TerminPaymentID:   sch.TerminPaymentID,
			Overdue:           overdue,
			DaysOverdue:       days,
		}
		// Umur tunggakan hanya bermakna untuk cicilan yang MASIH menunggak —
		// cicilan lunas tidak boleh tampil "telat 41 hari" (sama dengan aging).
		if !overdue {
			line.DaysOverdue = 0
		}
		stmt.Schedules = append(stmt.Schedules, line)

		if overdue {
			totalOverdue = totalOverdue.Add(outstanding)
			stmt.OverdueCount++
		}
	}

	remaining := contract.GrossAmount.Sub(collected)

	stmt.TotalScheduled = totalScheduled.String()
	stmt.TotalPaid = collected.String()
	stmt.RemainingBalance = remaining.String()
	stmt.TotalOverdue = totalOverdue.String()

	// ── Eksposur tunggal (W-4) ───────────────────────────────────────────────
	// Sisi harga rumah memakai TotalOverdue & sisa cicilan yang BARU saja
	// dihitung di loop di atas — tidak dihitung ulang, supaya tidak ada
	// kesempatan bagi dua angka untuk menyimpang.
	var houseOutstanding domain.Money
	for _, line := range stmt.Schedules {
		if o, err := domain.NewMoney(line.Outstanding); err == nil {
			houseOutstanding = houseOutstanding.Add(o)
		}
	}
	exp := &StatementExposure{
		HouseOutstanding: houseOutstanding.String(),
		HouseOverdue:     totalOverdue.String(),
		TotalOutstanding: houseOutstanding.String(),
		TotalOverdue:     totalOverdue.String(),
	}
	if s.realization != nil {
		rows, rerr := s.realization.ReceivableRowsByContract(ctx, tenantID, contractID)
		if rerr == nil {
			var ro, rov domain.Money
			for _, r := range rows {
				paid := r.PaidAmount
				if r.Received {
					paid = r.Amount
				}
				out := r.Amount.Sub(paid)
				if out.IsZero() || out.IsNeg() {
					continue
				}
				ro = ro.Add(out)
				if receivable.DaysBetween(r.DueDate, asOf) > 0 {
					rov = rov.Add(out)
				}
			}
			exp.RealizationAvailable = true
			exp.RealizationOutstanding = ro.String()
			exp.RealizationOverdue = rov.String()
			exp.TotalOutstanding = houseOutstanding.Add(ro).String()
			exp.TotalOverdue = totalOverdue.Add(rov).String()
		}
	}
	stmt.Exposure = exp

	return stmt, nil
}

// statementDaysOverdue mengembalikan jumlah hari penuh dueDate sampai asOf
// (positif = lewat jatuh tempo).
//
// W-4: satu-satunya definisi "umur tunggakan" ada di receivable.DaysBetween
// (INV-AR-1). Salinan kedua di file ini dulu identik — tapi dua salinan identik
// hanya berarti belum ada yang mengubah salah satunya.
func statementDaysOverdue(dueDate, asOf time.Time) int {
	return receivable.DaysBetween(dueDate, asOf)
}
