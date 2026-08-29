package charge

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

// ── Query & formula kanonik (bisa dipakai di dalam maupun luar transaksi) ─────

// contractInfo minimal dari sale_contracts (raw — pola billing.ContractInfo).
type contractInfo struct {
	ID     uint64 `gorm:"column:id"`
	UnitID uint64 `gorm:"column:unit_id"`
}

func findContractDB(ctx context.Context, db *gorm.DB, tenantID, contractID uint64) (*contractInfo, error) {
	var c contractInfo
	err := db.WithContext(ctx).
		Table("sale_contracts").
		Select("id, unit_id").
		Where("id = ? AND tenant_id = ?", contractID, tenantID).
		First(&c).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrContractNotFound
		}
		return nil, fmt.Errorf("cari kontrak: %w", err)
	}
	return &c, nil
}

// unitRef memuat project/phase unit untuk baris jurnal (audit dimensi).
type unitRef struct {
	ProjectID uint64  `gorm:"column:project_id"`
	PhaseID   *uint64 `gorm:"column:phase_id"`
}

func findUnitRefDB(ctx context.Context, db *gorm.DB, tenantID, unitID uint64) (*unitRef, error) {
	var u unitRef
	err := db.WithContext(ctx).
		Table("units").
		Select("project_id, phase_id").
		Where("id = ? AND tenant_id = ?", unitID, tenantID).
		First(&u).Error
	if err != nil {
		return nil, fmt.Errorf("cari unit: %w", err)
	}
	return &u, nil
}

func findGroupDB(ctx context.Context, db *gorm.DB, tenantID, groupID uint64, lock bool) (*ChargeGroup, error) {
	q := db.WithContext(ctx).Where("id = ? AND tenant_id = ?", groupID, tenantID)
	if lock {
		q = q.Clauses(gormLockingUpdate())
	}
	var g ChargeGroup
	if err := q.First(&g).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrGroupNotFound
		}
		return nil, fmt.Errorf("cari grup: %w", err)
	}
	return &g, nil
}

func listItemsDB(ctx context.Context, db *gorm.DB, tenantID, groupID uint64, lock bool) ([]*ChargeItem, error) {
	q := db.WithContext(ctx).
		Where("tenant_id = ? AND charge_group_id = ?", tenantID, groupID).
		Order("id ASC")
	if lock {
		q = q.Clauses(gormLockingUpdate())
	}
	var items []*ChargeItem
	if err := q.Find(&items).Error; err != nil {
		return nil, fmt.Errorf("daftar item: %w", err)
	}
	return items, nil
}

// sumByItem menjumlahkan kolom amount sebuah tabel per charge_item.
type itemSum struct {
	ChargeItemID uint64       `gorm:"column:charge_item_id"`
	Total        domain.Money `gorm:"column:total"`
}

// paidByItemDB: Σ alokasi per item — type charge_item + charge_item_void (net).
// SATU-SATUNYA definisi "sudah dibayar" per item.
func paidByItemDB(ctx context.Context, db *gorm.DB, tenantID, groupID uint64) (map[uint64]domain.Money, error) {
	var rows []itemSum
	err := db.WithContext(ctx).
		Table("payment_allocations pa").
		Select("pa.charge_item_id, COALESCE(SUM(pa.amount), 0) AS total").
		Joins("JOIN charge_items ci ON ci.id = pa.charge_item_id AND ci.tenant_id = pa.tenant_id").
		Where("pa.tenant_id = ? AND ci.charge_group_id = ? AND pa.allocation_type IN ('charge_item','charge_item_void')", tenantID, groupID).
		Group("pa.charge_item_id").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("hitung alokasi item: %w", err)
	}
	out := make(map[uint64]domain.Money, len(rows))
	for _, r := range rows {
		out[r.ChargeItemID] = r.Total
	}
	return out, nil
}

func payoutByItemDB(ctx context.Context, db *gorm.DB, tenantID, groupID uint64) (map[uint64]domain.Money, error) {
	var rows []itemSum
	err := db.WithContext(ctx).
		Table("charge_payouts").
		Select("charge_item_id, COALESCE(SUM(amount), 0) AS total").
		Where("tenant_id = ? AND charge_group_id = ?", tenantID, groupID).
		Group("charge_item_id").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("hitung payout item: %w", err)
	}
	out := make(map[uint64]domain.Money, len(rows))
	for _, r := range rows {
		out[r.ChargeItemID] = r.Total
	}
	return out, nil
}

// returnedActions adalah aksi settlement yang benar-benar MENGURANGI dana yang
// dipegang grup. void_payment sengaja di luar daftar: pembatalan pembayaran
// sudah ternetralkan lewat baris alokasi mirror negatif, jadi menghitungnya lagi
// di sini akan mengurangi dana dua kali.
var returnedActions = []string{
	string(ActionRefund), string(ActionTransferHouse), string(ActionTransferGroup),
}

// returnedDB: Σ settlement refund + transfer (dana keluar dari titipan grup).
func returnedDB(ctx context.Context, db *gorm.DB, tenantID, groupID uint64) (domain.Money, error) {
	type result struct {
		Total domain.Money `gorm:"column:total"`
	}
	var res result
	err := db.WithContext(ctx).
		Table("charge_settlements").
		Select("COALESCE(SUM(amount), 0) AS total").
		Where("tenant_id = ? AND charge_group_id = ? AND action IN ?", tenantID, groupID, returnedActions).
		Scan(&res).Error
	if err != nil {
		return domain.Zero, fmt.Errorf("hitung settlement: %w", err)
	}
	return res.Total, nil
}

// returnedByAccountDB memecah returnedDB menurut akun kewajiban, dari rincian
// per akun settlement (charge_settlement_lines).
func returnedByAccountDB(ctx context.Context, db *gorm.DB, tenantID, groupID uint64) (map[string]domain.Money, error) {
	type row struct {
		Account string       `gorm:"column:deposit_account_code"`
		Total   domain.Money `gorm:"column:total"`
	}
	var rows []row
	err := db.WithContext(ctx).
		Table("charge_settlement_lines csl").
		Select("csl.deposit_account_code, COALESCE(SUM(csl.amount), 0) AS total").
		Joins("JOIN charge_settlements cs ON cs.id = csl.charge_settlement_id AND cs.tenant_id = csl.tenant_id").
		Where("csl.tenant_id = ? AND cs.charge_group_id = ? AND cs.action IN ?", tenantID, groupID, returnedActions).
		Group("csl.deposit_account_code").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("hitung settlement per akun: %w", err)
	}
	out := make(map[string]domain.Money, len(rows))
	for _, r := range rows {
		out[r.Account] = r.Total
	}
	return out, nil
}

// groupSummaryDB menghitung ringkasan KANONIK satu grup (formula §GroupSummary).
func groupSummaryDB(ctx context.Context, db *gorm.DB, tenantID uint64, g *ChargeGroup) (*GroupSummary, error) {
	items, err := listItemsDB(ctx, db, tenantID, g.ID, false)
	if err != nil {
		return nil, err
	}
	paidMap, err := paidByItemDB(ctx, db, tenantID, g.ID)
	if err != nil {
		return nil, err
	}
	payoutMap, err := payoutByItemDB(ctx, db, tenantID, g.ID)
	if err != nil {
		return nil, err
	}
	returned, err := returnedDB(ctx, db, tenantID, g.ID)
	if err != nil {
		return nil, err
	}
	returnedByAcc, err := returnedByAccountDB(ctx, db, tenantID, g.ID)
	if err != nil {
		return nil, err
	}

	sum := &GroupSummary{
		GroupID:        g.ID,
		SaleContractID: g.SaleContractID,
		UnitID:         g.UnitID,
		Kind:           g.Kind,
		Label:          g.Label,
		Status:         g.Status,
		Billed:         domain.Zero,
		Paid:           domain.Zero,
		Outstanding:    domain.Zero,
		Payout:         domain.Zero,
		Returned:       returned,
		Residual:       domain.Zero,
		Receivable:     domain.Zero,
		Unbilled:       domain.Zero,
		Items:          make([]ItemSummary, 0, len(items)),
	}
	// Residual per akun: rumus yang SAMA (paid − payout − returned), hanya
	// dikelompokkan menurut akun snapshot item. Akun yang hanya muncul di
	// settlement (mis. item-nya sudah dibatalkan) tetap ikut terhitung.
	perAcc := map[string]domain.Money{}
	addAcc := func(code string, delta domain.Money) {
		perAcc[code] = moneyOrZero(perAcc[code]).Add(delta)
	}
	for acc, amt := range returnedByAcc {
		addAcc(acc, domain.Zero.Sub(amt))
	}

	for _, it := range items {
		paid := moneyOrZero(paidMap[it.ID])
		payout := moneyOrZero(payoutMap[it.ID])
		acc := itemDepositCode(it)
		is := ItemSummary{
			ItemID:            it.ID,
			Label:             it.Label,
			ChargeTypeCode:    strPtrValue(it.ChargeTypeCode),
			ProductCode:       strPtrValue(it.ProductCode),
			DepositAccount:    acc,
			Status:            it.Status,
			Amount:            it.Amount,
			OriginalAmount:    it.OriginalAmount,
			Paid:              paid,
			Outstanding:       it.Amount.Sub(paid),
			Payout:            payout,
			DueDate:           it.DueDate,
			RecognizedAmount:  it.RecognizedAmount,
			RecognizedAt:      it.RecognizedAt,
			Receivable:        itemReceivable(it, paid),
			Unbilled:          itemUnbilled(it),
			RecognitionStatus: itemRecognitionStatus(it, paid),
		}
		sum.Items = append(sum.Items, is)
		// Item cancelled: tidak menambah billed (dan by-guard tak punya uang).
		if it.Status == ItemOpen {
			sum.Billed = sum.Billed.Add(it.Amount)
			sum.Receivable = sum.Receivable.Add(is.Receivable)
			sum.Unbilled = sum.Unbilled.Add(is.Unbilled)
		}
		if it.RecognizedAt != nil {
			sum.Recognized = true
		}
		sum.Paid = sum.Paid.Add(paid)
		sum.Payout = sum.Payout.Add(payout)
		addAcc(acc, paid.Sub(payout))
	}
	sum.Outstanding = sum.Billed.Sub(sum.Paid)
	sum.Residual = sum.Paid.Sub(sum.Payout).Sub(sum.Returned)
	sum.DepositResiduals = sortedDepositAmounts(perAcc)
	if sum.Recognized {
		no, err := recognizedInvoiceNumberDB(ctx, db, tenantID, items)
		if err != nil {
			return nil, err
		}
		sum.InvoiceNumber = no
	}
	return sum, nil
}

// recognizedInvoiceNumberDB membaca nomor invoice TERAKHIR yang mengakui piutang
// grup. Sumbernya adalah recognized_invoice_id yang dibekukan di item — bukan
// "invoice hidup terakhir milik grup" — karena yang ingin dijawab layar adalah
// "dokumen mana yang melahirkan piutang ini", dan itu invoice yang benar-benar
// tercetak pada jurnal pengakuannya.
//
// Query hanya dijalankan bila grup memang sudah pernah diakui, jadi grup draft
// (mayoritas di layar daftar) tidak membayar apa pun untuknya.
func recognizedInvoiceNumberDB(ctx context.Context, db *gorm.DB, tenantID uint64, items []*ChargeItem) (string, error) {
	ids := make([]uint64, 0, len(items))
	for _, it := range items {
		if it.RecognizedInvoiceID != nil && *it.RecognizedInvoiceID != 0 {
			ids = append(ids, *it.RecognizedInvoiceID)
		}
	}
	if len(ids) == 0 {
		return "", nil
	}
	var no string
	err := db.WithContext(ctx).Raw(`
		SELECT invoice_number FROM invoices
		WHERE tenant_id = ? AND id IN (?)
		ORDER BY id DESC LIMIT 1`, tenantID, ids).Scan(&no).Error
	if err != nil {
		return "", fmt.Errorf("nomor invoice pengakuan: %w", err)
	}
	return no, nil
}

// sortedDepositAmounts mengurutkan peta akun→nominal menjadi daftar deterministik
// (kode akun menaik) dan membuang akun bernilai nol.
func sortedDepositAmounts(m map[string]domain.Money) []DepositAmount {
	out := make([]DepositAmount, 0, len(m))
	for acc, amt := range m {
		if amt.IsZero() {
			continue
		}
		out = append(out, DepositAmount{AccountCode: acc, Amount: amt})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].AccountCode < out[b].AccountCode })
	return out
}

func strPtrValue(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// ── Akun titipan per jenis biaya (koreksi arsitektur 2026-08-06) ────────────
//
// Akun kewajiban titipan adalah atribut JENIS BIAYA, bukan properti grup. Satu
// grup boleh memuat Notaris, BPHTB, PDAM, dan Listrik dengan akun yang berbeda
// sekaligus. Yang menjaga agar jurnal tidak pernah ambigu bukan "satu grup satu
// akun", melainkan: setiap rupiah selalu bisa ditelusuri ke akun asalnya —
//   - uang masuk & payout menempel pada ITEM, dan item membawa snapshot akunnya;
//   - refund/transfer/void menempel pada GRUP, dan pecahannya per akun disimpan
//     eksplisit di charge_settlement_lines.
//
// itemDepositCode membaca snapshot item. Kosong hanya mungkin untuk baris
// pra-000063; jatuh ke akun default — persis akun yang dulu dipakai, karena
// histori tidak pernah ditulis ulang (Invariant #5).
func itemDepositCode(it *ChargeItem) string {
	if code := strings.TrimSpace(it.DepositAccountCode); code != "" {
		return code
	}
	return AccountCodeTitipanRealisasi
}

// depositAccountIDByCode me-resolve satu kode akun titipan menjadi ID.
func depositAccountIDByCode(ctx context.Context, tx *gorm.DB, tenantID uint64, code string) (uint64, error) {
	if strings.TrimSpace(code) == "" {
		code = AccountCodeTitipanRealisasi
	}
	id, err := accountIDByCode(ctx, tx, tenantID, code)
	if err != nil || id == 0 {
		return 0, fmt.Errorf("%w (akun %s)", ErrDepositAccountMissing, code)
	}
	return id, nil
}

// depositLines mengubah pecahan per akun menjadi baris jurnal — debit bila
// dr=true (pelepasan kewajiban), kredit bila tidak (penambahan kewajiban).
// Fail-closed: akun yang tidak ada di COA menggagalkan seluruh transaksi.
func depositLines(ctx context.Context, tx *gorm.DB, tenantID uint64, parts []DepositAmount, dr bool, ref *unitRef, unitID uint64, desc string) ([]ledger.LineInput, error) {
	lines := make([]ledger.LineInput, 0, len(parts))
	for _, p := range parts {
		if p.Amount.IsZero() {
			continue
		}
		id, err := depositAccountIDByCode(ctx, tx, tenantID, p.AccountCode)
		if err != nil {
			return nil, err
		}
		pid, uid := ref.ProjectID, unitID
		line := ledger.LineInput{
			AccountID: id, ProjectID: &pid, PhaseID: ref.PhaseID, UnitID: &uid, Description: desc,
		}
		if dr {
			line.Debit = p.Amount
		} else {
			line.Credit = p.Amount
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return nil, ErrDepositAccountMissing
	}
	return lines, nil
}

// depositSplitOfAllocations mengelompokkan alokasi per item menjadi pecahan per
// akun kewajiban. Inilah jembatan antara keputusan admin (alokasi ke item, K-3)
// dan akuntansinya (akun mana yang bertambah/berkurang) — tidak ada penebakan:
// Σ pecahan == Σ alokasi, dan alokasi sendiri sudah dipaksa sama persis dengan
// nominal pembayaran.
func depositSplitOfAllocations(allocs []AllocationInput, items []*ChargeItem) []DepositAmount {
	byID := make(map[uint64]*ChargeItem, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}
	perAcc := map[string]domain.Money{}
	for _, a := range allocs {
		code := AccountCodeTitipanRealisasi
		if it, ok := byID[a.ChargeItemID]; ok {
			code = itemDepositCode(it)
		}
		perAcc[code] = moneyOrZero(perAcc[code]).Add(a.Amount)
	}
	return sortedDepositAmounts(perAcc)
}

// splitByResidual memecah satu nominal level-grup (refund / transfer) menjadi
// pecahan per akun kewajiban, PROPORSIONAL terhadap residual masing-masing akun
// dan largest-remainder — Σ pecahan == amount PERSIS (Invariant #3).
//
// Kenapa proporsional dan bukan pilihan admin: refund/transfer bergerak di level
// grup, sementara kelebihan dana tidak melekat pada satu jenis biaya tertentu.
// Selama satu grup memakai satu akun (keadaan seluruh data hari ini) hasilnya
// identik dengan perilaku lama. Bila kelak admin ingin menentukan sendiri asal
// dananya, pintunya adalah alokasi per item seperti K-3 — bukan mengubah bentuk
// arsitektur ini.
//
// Akun ber-residual negatif (payout melampaui titipan, K-5) tidak ikut menanggung
// pengembalian: menariknya lebih dalam hanya memperbesar kewajiban yang sudah
// terlanjur minus.
func splitByResidual(residuals []DepositAmount, amount domain.Money) ([]DepositAmount, error) {
	positive := make([]DepositAmount, 0, len(residuals))
	total := domain.Zero
	for _, r := range residuals {
		if r.Amount.IsNeg() || r.Amount.IsZero() {
			continue
		}
		positive = append(positive, r)
		total = total.Add(r.Amount)
	}
	if len(positive) == 0 || amount.GreaterThan(total) {
		return nil, fmt.Errorf("%w (sisa %s, diminta %s)", ErrExceedsResidual, total, amount)
	}
	if len(positive) == 1 {
		return []DepositAmount{{AccountCode: positive[0].AccountCode, Amount: amount}}, nil
	}
	weights := make([]decimal.Decimal, len(positive))
	for i, p := range positive {
		weights[i] = p.Amount.Decimal()
	}
	parts := amount.Allocate(weights)
	out := make([]DepositAmount, 0, len(parts))
	for i, part := range parts {
		if part.IsZero() {
			continue
		}
		out = append(out, DepositAmount{AccountCode: positive[i].AccountCode, Amount: part})
	}
	return out, nil
}

// primaryDepositCode memilih akun DOMINAN dari sebuah pecahan — nominal terbesar,
// seri terkecil bila seri. Hanya untuk kolom ringkasan satu-nilai pada termin
// (credit_account_code / bank_account_code): kebenaran akuntansinya ada di baris
// jurnal dan di charge_settlement_lines, tidak pernah di kolom ini.
func primaryDepositCode(parts []DepositAmount) string {
	best := ""
	var bestAmt domain.Money
	for _, p := range parts {
		if best == "" || p.Amount.GreaterThan(bestAmt) {
			best, bestAmt = p.AccountCode, p.Amount
		}
	}
	if best == "" {
		return AccountCodeTitipanRealisasi
	}
	return best
}

// saveSettlementLines menuliskan rincian per akun sebuah settlement.
func saveSettlementLines(ctx context.Context, tx *gorm.DB, tenantID, settlementID uint64, parts []DepositAmount) error {
	for _, p := range parts {
		row := &ChargeSettlementLine{
			TenantID:           tenantID,
			ChargeSettlementID: settlementID,
			DepositAccountCode: p.AccountCode,
			Amount:             p.Amount,
		}
		if err := tx.WithContext(ctx).Create(row).Error; err != nil {
			return fmt.Errorf("simpan rincian settlement akun %s: %w", p.AccountCode, err)
		}
	}
	return nil
}

// settlementLinesDB membaca kembali pecahan per akun sebuah settlement — dipakai
// saat melanjutkan transfer yang tertinggal, agar pecahannya PERSIS sama dengan
// yang sudah direservasi (menghitung ulang dari residual akan meleset, karena
// residualnya sudah berubah oleh langkah pertama).
func settlementLinesDB(ctx context.Context, db *gorm.DB, tenantID, settlementID uint64) ([]DepositAmount, error) {
	var rows []ChargeSettlementLine
	if err := db.WithContext(ctx).
		Where("tenant_id = ? AND charge_settlement_id = ?", tenantID, settlementID).
		Order("deposit_account_code ASC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("baca rincian settlement: %w", err)
	}
	out := make([]DepositAmount, 0, len(rows))
	for _, r := range rows {
		out = append(out, DepositAmount{AccountCode: r.DepositAccountCode, Amount: r.Amount})
	}
	return out, nil
}

func moneyOrZero(m domain.Money) domain.Money {
	if m.Decimal().IsZero() {
		return domain.Zero
	}
	return m
}

// ── Helper jurnal & akun (pola notary — posting service di dalam tx) ─────────

func accountIDByCode(ctx context.Context, tx *gorm.DB, tenantID uint64, code string) (uint64, error) {
	var id uint64
	if err := tx.WithContext(ctx).Raw(
		`SELECT id FROM accounts WHERE tenant_id = ? AND code = ? AND is_active = 1`,
		tenantID, code).Scan(&id).Error; err != nil {
		return 0, err
	}
	return id, nil
}

// cashBankAccountID memvalidasi akun kas/bank COA-driven (otoritas tunggal:
// ledger.ValidatePaymentAccount) lalu mengembalikan ID-nya.
func cashBankAccountID(ctx context.Context, tx *gorm.DB, tenantID uint64, code string) (uint64, error) {
	var acc ledger.Account
	if err := tx.WithContext(ctx).
		Where("tenant_id = ? AND code = ?", tenantID, code).
		First(&acc).Error; err != nil {
		return 0, fmt.Errorf("akun bank %s tidak ditemukan", code)
	}
	if err := ledger.ValidatePaymentAccount(&acc); err != nil {
		return 0, err
	}
	return acc.ID, nil
}

// postJournalTx membuat + memposting satu jurnal balanced DI DALAM tx.
// Untuk jurnal yang TIDAK menyentuh kas (transfer titipan antar grup, dsb).
func postJournalTx(ctx context.Context, tx *gorm.DB, tenantID uint64, date time.Time, desc string, createdBy *uint64, lines []ledger.LineInput) (uint64, error) {
	return postCashJournalTx(ctx, tx, tenantID, date, desc, createdBy, lines, ledger.DocumentSpec{})
}

// draftJournalTx membuat jurnal balanced sebagai DRAFT — belum diposting.
//
// Dipakai jalur yang dokumennya baru bisa lahir SESUDAH jurnalnya ada (kwitansi
// KWR butuh termin, termin butuh id jurnal). W-3.5 menolak jurnal kas yang
// selesai diposting tanpa dokumen, jadi urutannya dibalik: draft → bukti →
// tautkan → post. Selama masih draft, kas belum bergerak sama sekali.
func draftJournalTx(ctx context.Context, tx *gorm.DB, tenantID uint64, date time.Time, desc string,
	createdBy *uint64, lines []ledger.LineInput) (uint64, error) {
	txLedger := ledger.NewGORMRepository(tx)
	posting := ledger.NewPostingService(txLedger, txLedger).WithPeriodChecker(txLedger)
	entry, err := posting.Create(ctx, ledger.CreateJournalRequest{
		TenantID:    tenantID,
		Date:        date,
		Description: desc,
		CreatedBy:   createdBy,
		Lines:       lines,
	})
	if err != nil {
		return 0, fmt.Errorf("buat draft jurnal: %w", err)
	}
	return entry.ID, nil
}

// postDraftTx memposting draft yang dokumennya sudah tertaut. Spec kosong:
// buktinya sudah terpasang, PostDraft tinggal memverifikasinya (W-3.5).
func postDraftTx(ctx context.Context, tx *gorm.DB, tenantID, journalID uint64) error {
	txLedger := ledger.NewGORMRepository(tx)
	posting := ledger.NewPostingService(txLedger, txLedger).WithPeriodChecker(txLedger)
	if _, err := posting.PostDraft(ctx, tenantID, journalID, ledger.DocumentSpec{}); err != nil {
		return fmt.Errorf("posting jurnal: %w", err)
	}
	return nil
}

// postCashJournalTx sama dengan postJournalTx, ditambah penerbitan dokumen kas
// di transaksi yang SAMA (W-3.2). Jenis dokumen datang dari pemanggil karena
// hanya jalur bisnisnya yang tahu uang siapa yang bergerak: titipan pihak
// ketiga (BTP) dan pengembalian ke customer (RFC) terlihat identik dari sisi
// debit/kredit.
func postCashJournalTx(ctx context.Context, tx *gorm.DB, tenantID uint64, date time.Time, desc string,
	createdBy *uint64, lines []ledger.LineInput, spec ledger.DocumentSpec) (uint64, error) {
	txLedger := ledger.NewGORMRepository(tx)
	posting := ledger.NewPostingService(txLedger, txLedger).WithPeriodChecker(txLedger)
	entry, err := posting.CreateAndPost(ctx, ledger.CreateJournalRequest{
		TenantID:    tenantID,
		Date:        date,
		Description: desc,
		CreatedBy:   createdBy,
		Lines:       lines,
		Document:    spec,
	})
	if err != nil {
		return 0, fmt.Errorf("posting jurnal: %w", err)
	}
	return entry.ID, nil
}
