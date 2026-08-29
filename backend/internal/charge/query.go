package charge

import (
	"context"
	"fmt"
	"time"

	"esaproperti/internal/domain"
	"esaproperti/internal/receivable"
)

// ── Pembaca (semua angka lewat formula kanonik groupSummaryDB) ───────────────

// GroupSummary mengembalikan ringkasan KANONIK satu grup — dipakai receipt,
// invoice, statement, dashboard, aging, dan gate BAST (satu formula).
func (s *Service) GroupSummary(ctx context.Context, tenantID, groupID uint64) (*GroupSummary, error) {
	g, err := findGroupDB(ctx, s.db, tenantID, groupID, false)
	if err != nil {
		return nil, err
	}
	return groupSummaryDB(ctx, s.db, tenantID, g)
}

// ListGroupsByContract — seluruh grup satu kontrak (statement multi-section).
func (s *Service) ListGroupsByContract(ctx context.Context, tenantID, contractID uint64) ([]*GroupSummary, error) {
	var groups []*ChargeGroup
	if err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND sale_contract_id = ?", tenantID, contractID).
		Order("id ASC").Find(&groups).Error; err != nil {
		return nil, fmt.Errorf("daftar grup kontrak: %w", err)
	}
	return s.summarize(ctx, tenantID, groups)
}

// ListGroupsByUnit — seluruh grup satu unit (halaman tagihan unit).
func (s *Service) ListGroupsByUnit(ctx context.Context, tenantID, unitID uint64) ([]*GroupSummary, error) {
	var groups []*ChargeGroup
	if err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND unit_id = ?", tenantID, unitID).
		Order("id ASC").Find(&groups).Error; err != nil {
		return nil, fmt.Errorf("daftar grup unit: %w", err)
	}
	return s.summarize(ctx, tenantID, groups)
}

func (s *Service) summarize(ctx context.Context, tenantID uint64, groups []*ChargeGroup) ([]*GroupSummary, error) {
	out := make([]*GroupSummary, 0, len(groups))
	for _, g := range groups {
		sum, err := groupSummaryDB(ctx, s.db, tenantID, g)
		if err != nil {
			return nil, err
		}
		out = append(out, sum)
	}
	return out, nil
}

// GroupDetail — ringkasan + histori lengkap (halaman detail grup).
func (s *Service) GroupDetail(ctx context.Context, tenantID, groupID uint64) (*GroupDetail, error) {
	sum, err := s.GroupSummary(ctx, tenantID, groupID)
	if err != nil {
		return nil, err
	}
	detail := &GroupDetail{Summary: *sum}

	// Pembayaran (termin grup ini) + nomor kwitansi + status void.
	type payRow struct {
		TerminID        uint64       `gorm:"column:termin_id"`
		Amount          domain.Money `gorm:"column:amount"`
		Date            time.Time    `gorm:"column:date"`
		BankAccountCode string       `gorm:"column:bank_account_code"`
		Source          string       `gorm:"column:payment_source"`
		Description     string       `gorm:"column:description"`
		ReceiptNumber   string       `gorm:"column:receipt_number"`
		ReceiptID       uint64       `gorm:"column:receipt_id"`
		Voided          bool         `gorm:"column:voided"`
	}
	var pays []payRow
	if err := s.db.WithContext(ctx).Raw(`
		SELECT t.id AS termin_id, t.amount, t.date, t.bank_account_code,
		       t.payment_source, t.description,
		       COALESCE(r.receipt_number, '') AS receipt_number,
		       COALESCE(r.id, 0)              AS receipt_id,
		       EXISTS (SELECT 1 FROM charge_settlements cs
		               WHERE cs.tenant_id = t.tenant_id AND cs.voided_termin_id = t.id) AS voided
		FROM termin_payments t
		LEFT JOIN receipts r ON r.termin_payment_id = t.id AND r.tenant_id = t.tenant_id
		WHERE t.tenant_id = ? AND t.charge_group_id = ?
		ORDER BY t.id ASC`, tenantID, groupID).Scan(&pays).Error; err != nil {
		return nil, fmt.Errorf("daftar pembayaran grup: %w", err)
	}
	detail.Payments = make([]PaymentRow, 0, len(pays))
	for _, p := range pays {
		detail.Payments = append(detail.Payments, PaymentRow{
			TerminID:        p.TerminID,
			Amount:          p.Amount,
			Date:            p.Date,
			BankAccountCode: p.BankAccountCode,
			Source:          p.Source,
			Description:     p.Description,
			ReceiptNumber:   p.ReceiptNumber,
			ReceiptID:       p.ReceiptID,
			Voided:          p.Voided,
		})
	}

	if err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND charge_group_id = ?", tenantID, groupID).
		Order("id ASC").Find(&detail.Payouts).Error; err != nil {
		return nil, fmt.Errorf("daftar payout: %w", err)
	}
	if err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND charge_group_id = ?", tenantID, groupID).
		Order("id ASC").Find(&detail.Settlements).Error; err != nil {
		return nil, fmt.Errorf("daftar settlement: %w", err)
	}
	if err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND charge_item_id IN (SELECT id FROM charge_items WHERE tenant_id = ? AND charge_group_id = ?)",
			tenantID, tenantID, groupID).
		Order("id ASC").Find(&detail.Adjustments).Error; err != nil {
		return nil, fmt.Errorf("daftar adjustment: %w", err)
	}
	return detail, nil
}

// ── Dashboard KPI ────────────────────────────────────────────────────────────

// PortfolioSummary — total tenant utk KPI dashboard (grup open saja).
// KPI harga rumah TIDAK berubah arti — ini angka TERPISAH.
type PortfolioSummary struct {
	OpenGroups  int          `json:"open_groups"`
	Billed      domain.Money `json:"billed"`
	Paid        domain.Money `json:"paid"`
	Outstanding domain.Money `json:"outstanding"`
	Residual    domain.Money `json:"residual"`

	// W-5 — sisa terutang portofolio terbelah menjadi yang SUDAH menjadi piutang
	// (berdokumen invoice, ada di 1-2000) dan yang BELUM ditagihkan sama sekali.
	// Angka kedua itulah pekerjaan yang menganggur: uang yang berhak ditagih tapi
	// belum pernah dimintakan.
	Receivable domain.Money `json:"receivable"`
	Unbilled   domain.Money `json:"unbilled"`
}

func (s *Service) Portfolio(ctx context.Context, tenantID uint64) (*PortfolioSummary, error) {
	var groups []*ChargeGroup
	if err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND status = ?", tenantID, string(GroupOpen)).
		Find(&groups).Error; err != nil {
		return nil, fmt.Errorf("daftar grup open: %w", err)
	}
	out := &PortfolioSummary{
		OpenGroups:  len(groups),
		Billed:      domain.Zero,
		Paid:        domain.Zero,
		Outstanding: domain.Zero,
		Residual:    domain.Zero,
		Receivable:  domain.Zero,
		Unbilled:    domain.Zero,
	}
	for _, g := range groups {
		sum, err := groupSummaryDB(ctx, s.db, tenantID, g)
		if err != nil {
			return nil, err
		}
		out.Billed = out.Billed.Add(sum.Billed)
		out.Paid = out.Paid.Add(sum.Paid)
		out.Outstanding = out.Outstanding.Add(sum.Outstanding)
		out.Residual = out.Residual.Add(sum.Residual)
		out.Receivable = out.Receivable.Add(sum.Receivable)
		out.Unbilled = out.Unbilled.Add(sum.Unbilled)
	}
	return out, nil
}

// ── Piutang realisasi (W-4: satu eksposur, satu mesin) ──────────────────────

// ReceivableRows mengembalikan seluruh tagihan biaya realisasi yang MASIH
// ditagih ke customer, dalam kosakata bersama `receivable.Row`.
//
// Ini adalah satu-satunya jalan keluar data piutang realisasi. `reporting`
// memakainya lewat interface (RealizationReceivableReader) sehingga paket
// transaksional ini TIDAK perlu meng-import paket laporan (TD-3) — sebelum W-4
// arahnya terbalik.
//
// W-5 mempersempitnya ke item yang piutangnya SUDAH DIAKUI (R-5: piutang lahir
// saat invoice terbit). Tagihan yang belum ditagihkan bukan piutang — ia tidak
// ada di buku besar, jadi ia juga tidak boleh ada di laporan umur piutang.
// Bagian itu tidak hilang dari produk: ia muncul sebagai "Belum ditagihkan" pada
// grup titipannya, lengkap dengan ajakan menerbitkan invoice.
func (s *Service) ReceivableRows(ctx context.Context, tenantID uint64) ([]receivable.Row, error) {
	return s.receivableRows(ctx, tenantID, 0)
}

// ReceivableRowsByContract mempersempit ReceivableRows ke satu kontrak — dipakai
// blok eksposur pada Customer Statement. Filternya di SQL, bukan di Go: satu
// statement tidak boleh menarik seluruh piutang tenant untuk membuang 99%-nya.
func (s *Service) ReceivableRowsByContract(ctx context.Context, tenantID, contractID uint64) ([]receivable.Row, error) {
	if contractID == 0 {
		return nil, nil
	}
	return s.receivableRows(ctx, tenantID, contractID)
}

func (s *Service) receivableRows(ctx context.Context, tenantID, contractID uint64) ([]receivable.Row, error) {
	type agingRow struct {
		ContractID uint64       `gorm:"column:contract_id"`
		UnitID     uint64       `gorm:"column:unit_id"`
		BuyerName  string       `gorm:"column:buyer_name"`
		BuyerPhone string       `gorm:"column:buyer_phone"`
		BuyerEmail string       `gorm:"column:buyer_email"`
		UnitCode   string       `gorm:"column:unit_code"`
		GroupLabel string       `gorm:"column:group_label"`
		ItemLabel  string       `gorm:"column:item_label"`
		Kind       string       `gorm:"column:kind"`
		ItemID     uint64       `gorm:"column:item_id"`
		InvoiceNo  string       `gorm:"column:invoice_no"`
		DueDate    time.Time    `gorm:"column:due_date"`
		Amount     domain.Money `gorm:"column:amount"`
		Paid       domain.Money `gorm:"column:paid"`
	}
	// Paid dihitung di SQL yang sama lewat subquery, bukan N+1 query per item:
	// laporan piutang dibuka setiap hari dan jumlah itemnya tumbuh terus.
	// Formulanya identik dengan groupSummaryDB (charge_item + charge_item_void).
	//
	// Nominalnya adalah recognized_amount — nilai yang benar-benar di-invoice dan
	// diposting ke 1-2000, bukan ci.amount yang bisa saja sudah naik (K-5) tanpa
	// dokumen baru. Jatuh temponya memakai jatuh tempo item bila ada, jatuh tempo
	// invoice bila tidak; keduanya kosong mustahil karena pengakuan selalu
	// menstempel salah satunya.
	var rows []agingRow
	if err := s.db.WithContext(ctx).Raw(`
		SELECT cg.sale_contract_id AS contract_id, cg.unit_id, cg.label AS group_label, cg.kind,
		       ci.id AS item_id, ci.label AS item_label,
		       COALESCE(ci.due_date, ci.recognized_due_date) AS due_date,
		       ci.recognized_amount AS amount,
		       COALESCE(sc.buyer_name, '') AS buyer_name,
		       COALESCE(cu.phone, '')      AS buyer_phone,
		       COALESCE(cu.email, '')      AS buyer_email,
		       COALESCE(u.code, '')        AS unit_code,
		       COALESCE(inv.invoice_number, '') AS invoice_no,
		       COALESCE((
		           SELECT SUM(pa.amount) FROM payment_allocations pa
		           WHERE pa.tenant_id = ci.tenant_id AND pa.charge_item_id = ci.id
		             AND pa.allocation_type IN ('charge_item','charge_item_void')
		       ), 0) AS paid
		FROM charge_items ci
		JOIN charge_groups cg ON cg.id = ci.charge_group_id AND cg.tenant_id = ci.tenant_id
		LEFT JOIN sale_contracts sc ON sc.id = cg.sale_contract_id AND sc.tenant_id = cg.tenant_id
		LEFT JOIN customers cu ON cu.id = sc.customer_id AND cu.tenant_id = sc.tenant_id
		LEFT JOIN units u ON u.id = cg.unit_id AND u.tenant_id = cg.tenant_id
		LEFT JOIN invoices inv ON inv.id = ci.recognized_invoice_id AND inv.tenant_id = ci.tenant_id
		WHERE ci.tenant_id = ? AND ci.status = 'open' AND cg.status = 'open'
		  AND ci.recognized_at IS NOT NULL
		  AND ci.recognized_amount > 0
		  AND COALESCE(ci.due_date, ci.recognized_due_date) IS NOT NULL
		  AND (? = 0 OR cg.sale_contract_id = ?)
		ORDER BY due_date ASC, ci.id ASC`, tenantID, contractID, contractID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("baris piutang realisasi: %w", err)
	}

	out := make([]receivable.Row, 0, len(rows))
	for _, r := range rows {
		paid := moneyOrZero(r.Paid)
		// W-13: piutang produk tambahan BUKAN piutang biaya realisasi. Keduanya
		// keluar dari tabel yang sama, tapi artinya berbeda bagi penagih —
		// realisasi berarti ada pihak ketiga yang menunggu dibayarkan, addon
		// berarti perusahaan menjual sesuatu dan belum menerima uangnya.
		src := receivable.SourceRealization
		if GroupKind(r.Kind) == KindAddon {
			src = receivable.SourceAddon
		}
		out = append(out, receivable.Row{
			Source:     src,
			ContractID: r.ContractID,
			UnitID:     r.UnitID,
			RefID:      r.ItemID,
			BuyerName:  r.BuyerName,
			BuyerPhone: r.BuyerPhone,
			BuyerEmail: r.BuyerEmail,
			UnitCode:   r.UnitCode,
			// Label menyebut grup DAN item: "PDAM" saja tidak cukup ketika satu
			// unit punya beberapa grup tagihan.
			Label:         fmt.Sprintf("%s / %s", r.GroupLabel, r.ItemLabel),
			InvoiceNumber: r.InvoiceNo,
			DueDate:       r.DueDate,
			Amount:        r.Amount,
			PaidAmount:    paid,
			Received:      !paid.LessThan(r.Amount),
		})
	}
	return out, nil
}

// AgingReport menyusun laporan umur piutang REALISASI saja.
//
// W-4: ia kini hanyalah ReceivableRows + mesin aging bersama — bukan salinan
// kedua. Eksposur penuh (rumah + realisasi) ada di GET /reports/ar-aging.
func (s *Service) AgingReport(ctx context.Context, tenantID uint64, asOf time.Time) (*receivable.AgingReport, error) {
	rows, err := s.ReceivableRows(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	rpt := receivable.BuildAging(rows, asOf)
	return &rpt, nil
}

// ── Memo Transfer Internal (T-1) ─────────────────────────────────────────────

// TransferMemo mengembalikan dokumen memo satu settlement transfer.
//
// Hanya untuk aksi transfer (transfer_house / transfer_group): refund adalah kas
// keluar (punya jejak jurnal kas sendiri) dan void adalah pembalik — keduanya
// bukan perpindahan internal, jadi tidak ber-memo.
func (s *Service) TransferMemo(ctx context.Context, tenantID, settlementID uint64) (*TransferMemo, error) {
	var st ChargeSettlement
	if err := s.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", settlementID, tenantID).
		First(&st).Error; err != nil {
		return nil, ErrSettlementNotFound
	}
	if st.Action != ActionTransferHouse && st.Action != ActionTransferGroup {
		return nil, ErrNotATransfer
	}
	if st.MemoNumber == nil || *st.MemoNumber == "" {
		// Transfer pra-000060 — dokumen belum ada. Jujur katakan, jangan karang
		// nomor baru untuk dokumen historis.
		return nil, ErrMemoNotIssued
	}

	src, err := findGroupDB(ctx, s.db, tenantID, st.ChargeGroupID, false)
	if err != nil {
		return nil, err
	}
	memo := &TransferMemo{
		SettlementID:     st.ID,
		MemoNumber:       *st.MemoNumber,
		Action:           st.Action,
		Date:             st.Date,
		Amount:           st.Amount,
		SourceGroupID:    src.ID,
		SourceGroupLabel: src.Label,
		SaleContractID:   src.SaleContractID,
		TargetGroupID:    st.TargetGroupID,
		TargetTerminID:   st.TargetTerminID,
		JournalEntryID:   st.JournalEntryID,
		Notes:            st.Notes,
		CreatedBy:        st.CreatedBy,
	}

	// Rujukan jurnal. Transfer antar grup memposting jurnalnya sendiri sehingga
	// journal_entry_id tersimpan di baris settlement. Transfer ke harga rumah
	// lewat pintu pembayaran (sale.ApplyDepositTransfer) — jurnalnya melekat
	// pada termin hasil transfer, jadi diturunkan dari sana. Memo TIDAK boleh
	// terbit tanpa rujukan jurnal: dokumen tanpa jejak jurnal = audit buntu.
	if memo.JournalEntryID == nil && st.TargetTerminID != nil {
		var je []uint64
		if err := s.db.WithContext(ctx).Raw(
			`SELECT journal_entry_id FROM termin_payments WHERE id = ? AND tenant_id = ?`,
			*st.TargetTerminID, tenantID).Scan(&je).Error; err == nil && len(je) > 0 && je[0] != 0 {
			memo.JournalEntryID = &je[0]
		}
	}

	// Identitas pembeli & unit sumber (dokumen — bukan angka keuangan).
	var head struct {
		BuyerName string `gorm:"column:buyer_name"`
		UnitCode  string `gorm:"column:unit_code"`
	}
	if err := s.db.WithContext(ctx).Raw(`
		SELECT sc.buyer_name AS buyer_name, u.code AS unit_code
		FROM sale_contracts sc
		JOIN units u ON u.id = sc.unit_id AND u.tenant_id = sc.tenant_id
		WHERE sc.id = ? AND sc.tenant_id = ?`, src.SaleContractID, tenantID).
		Scan(&head).Error; err == nil {
		memo.BuyerName = head.BuyerName
		memo.SourceUnitCode = head.UnitCode
	}
	var company []string
	if err := s.db.WithContext(ctx).Raw(
		`SELECT name FROM tenants WHERE id = ?`, tenantID).Scan(&company).Error; err == nil && len(company) > 0 {
		memo.CompanyName = company[0]
	}

	switch st.Action {
	case ActionTransferHouse:
		memo.TargetLabel = fmt.Sprintf("Pembayaran harga rumah unit %s", memo.SourceUnitCode)
	case ActionTransferGroup:
		if st.TargetGroupID != nil {
			if tgt, terr := findGroupDB(ctx, s.db, tenantID, *st.TargetGroupID, false); terr == nil {
				memo.TargetGroupLabel = tgt.Label
				var tu []string
				if err := s.db.WithContext(ctx).Raw(
					`SELECT code FROM units WHERE id = ? AND tenant_id = ?`, tgt.UnitID, tenantID).
					Scan(&tu).Error; err == nil && len(tu) > 0 {
					memo.TargetUnitCode = tu[0]
				}
				memo.TargetLabel = fmt.Sprintf("Grup tagihan #%d %s (unit %s)", tgt.ID, tgt.Label, memo.TargetUnitCode)
			}
		}
	}
	return memo, nil
}

// boolPolicyValue menormalkan nilai boolean untuk tabel audit (string apa
// adanya, agar tabel audit bisa menampung nilai bertipe lain kelak).
func boolPolicyValue(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// ── Riwayat kebijakan tenant (audit R-A) ────────────────────────────────────
//
// W-5 mencabut gate BAST realisasi (keputusan klien D-3): BAST tidak pernah lagi
// menuntut biaya realisasi lunas — sisanya menjadi Piutang Customer. Kebijakan
// require_realization_settled beserta pembaca/penulisnya ikut dihapus; RIWAYATNYA
// tidak — tabel audit append-only, dan pencabutannya sendiri tercatat di sana
// (migrasi 000067).

// ListPolicyChanges mengembalikan riwayat perubahan kebijakan tenant (terbaru
// dulu). Read-only — tabel audit tidak pernah di-update atau dihapus.
func (s *Service) ListPolicyChanges(ctx context.Context, tenantID uint64, limit int) ([]*TenantPolicyChange, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows []*TenantPolicyChange
	if err := s.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("riwayat kebijakan: %w", err)
	}
	return rows, nil
}
