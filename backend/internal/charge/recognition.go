package charge

// W-5 / J-14a — pengakuan piutang biaya realisasi.
//
// Keputusan klien D-3: sisa biaya realisasi adalah PIUTANG CUSTOMER, di akun
// yang sama dengan piutang harga rumah (1-2000) — bukan mekanisme piutang kedua.
// Keputusan R-5: piutang itu lahir saat INVOICE diterbitkan, karena piutang
// wajib punya dasar dokumen yang sah.
//
// Seluruh perilaku modul ini turun dari SATU rumus:
//
//	kontribusi(item) = max(0, recognized_amount − paid)   (item open)
//	                 = 0                                   (item cancelled/dilepas)
//
//	delta = kontribusi_seharusnya − Σ delta yang sudah diposting
//	delta > 0 → Dr 1-2000 / Cr <akun titipan item>
//	delta < 0 → Dr <akun titipan item> / Cr 1-2000
//
// `max(0, …)` bukan sekadar penjaga tanda: itulah penegakan R-4 — kelebihan
// bayar TIDAK PERNAH menjadi piutang bersaldo kredit, ia jatuh ke residual dan
// menunggu disposisi admin (refund / alih item / buyer credit).
//
// Kontribusi berjalan sengaja TIDAK disimpan sebagai kolom: ia dibaca kembali
// dari Σ delta tabel audit. Dengan begitu angka yang dipakai untuk menghitung
// jurnal berikutnya adalah angka yang benar-benar terposting, bukan cache yang
// bisa menyimpang diam-diam.

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

// ── Turunan per item (dipakai ringkasan maupun mesin) ────────────────────────

// itemReceivable = kontribusi item ke akun 1-2000.
func itemReceivable(it *ChargeItem, paid domain.Money) domain.Money {
	if it.Status != ItemOpen {
		return domain.Zero
	}
	d := it.RecognizedAmount.Sub(paid)
	if d.IsNeg() {
		return domain.Zero
	}
	return d
}

// itemUnbilled = tagihan yang belum pernah masuk invoice (mis. hasil raise K-5).
func itemUnbilled(it *ChargeItem) domain.Money {
	if it.Status != ItemOpen {
		return domain.Zero
	}
	d := it.Amount.Sub(it.RecognizedAmount)
	if d.IsNeg() {
		return domain.Zero
	}
	return d
}

func itemRecognitionStatus(it *ChargeItem, paid domain.Money) RecognitionStatus {
	if it.RecognizedAt == nil || it.RecognizedAmount.IsZero() {
		return RecognitionUnbilled
	}
	if itemReceivable(it, paid).IsZero() {
		return RecognitionPaid
	}
	return RecognitionReceivable
}

// ── Pembacaan audit (kontribusi yang BENAR-BENAR sudah diposting) ────────────

// recognizedByItemDB: Σ delta per item dari tabel audit append-only.
func recognizedByItemDB(ctx context.Context, db *gorm.DB, tenantID, groupID uint64) (map[uint64]domain.Money, error) {
	type row struct {
		ItemID uint64       `gorm:"column:charge_item_id"`
		Total  domain.Money `gorm:"column:total"`
	}
	var rows []row
	if err := db.WithContext(ctx).Table("charge_receivable_recognitions").
		Select("charge_item_id, COALESCE(SUM(delta), 0) AS total").
		Where("tenant_id = ? AND charge_group_id = ?", tenantID, groupID).
		Group("charge_item_id").Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("baca pengakuan piutang: %w", err)
	}
	out := make(map[uint64]domain.Money, len(rows))
	for _, r := range rows {
		out[r.ItemID] = moneyOrZero(r.Total)
	}
	return out, nil
}

// recognitionsByJournalDB mengembalikan baris pengakuan yang lahir dari SATU
// jurnal — dipakai void untuk membalik persis porsi piutang yang dulu dikredit,
// bukan menebaknya ulang dari keadaan sekarang.
func recognitionsByJournalDB(ctx context.Context, db *gorm.DB, tenantID, journalID uint64) ([]*ChargeReceivableRecognition, error) {
	var rows []*ChargeReceivableRecognition
	if err := db.WithContext(ctx).
		Where("tenant_id = ? AND journal_entry_id = ?", tenantID, journalID).
		Order("id ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("baca pengakuan jurnal: %w", err)
	}
	return rows, nil
}

// ── Mesin pengakuan ──────────────────────────────────────────────────────────

// recognitionOpts menentukan MENGAPA sinkronisasi dijalankan. Predikatnya
// sengaja hanya dua saklar: pengakuan baru (invoice/BAST) dan pelepasan penuh
// (pembatalan penjualan). Selebihnya adalah konsekuensi otomatis dari perubahan
// nilai tagihan atau status item — tidak ada jalur khusus per operasi.
type recognitionOpts struct {
	Reason    RecognitionReason
	Date      time.Time
	Desc      string
	InvoiceID *uint64
	DueDate   *time.Time // jatuh tempo invoice — dipakai saat Recognize
	CreatedBy *uint64
	Recognize bool // item open diakui sebesar tagihan berjalannya
	Release   bool // seluruh pengakuan dilepas (Dr titipan / Cr piutang)
}

// syncRecognitionInTx menyamakan kembali piutang grup dengan keadaannya yang
// seharusnya, menerbitkan SATU jurnal penyesuaian bila perlu. Mengembalikan
// total delta yang diposting (nol = tidak ada jurnal).
func (s *Service) syncRecognitionInTx(ctx context.Context, tx *gorm.DB, tenantID uint64, g *ChargeGroup, opt recognitionOpts) (domain.Money, error) {
	// Addon bukan titipan biaya realisasi — ia produk tambahan yang jalur
	// piutangnya ikut harga rumah. Tidak pernah diakui di sini.
	if g.Kind != KindRealization {
		return domain.Zero, nil
	}
	items, err := listItemsDB(ctx, tx, tenantID, g.ID, true)
	if err != nil {
		return domain.Zero, err
	}
	paid, err := paidByItemDB(ctx, tx, tenantID, g.ID)
	if err != nil {
		return domain.Zero, err
	}
	posted, err := recognizedByItemDB(ctx, tx, tenantID, g.ID)
	if err != nil {
		return domain.Zero, err
	}

	type change struct {
		item          *ChargeItem
		newRecognized domain.Money
		delta         domain.Money
		stamp         bool // item baru pertama kali diakui
	}
	changes := make([]change, 0, len(items))
	perAcc := map[string]domain.Money{}
	total := domain.Zero

	for _, it := range items {
		newRec := it.RecognizedAmount
		switch {
		case opt.Release:
			newRec = domain.Zero
		case opt.Recognize && it.Status == ItemOpen:
			newRec = it.Amount
		case newRec.GreaterThan(it.Amount):
			// True-up menurunkan tagihan di bawah nilai yang sudah di-invoice:
			// klaimnya ikut turun, tidak boleh menyisakan piutang fiktif.
			newRec = it.Amount
		}
		target := domain.Zero
		if it.Status == ItemOpen {
			if d := newRec.Sub(moneyOrZero(paid[it.ID])); d.GreaterThan(domain.Zero) {
				target = d
			}
		}
		delta := target.Sub(moneyOrZero(posted[it.ID]))
		stamp := opt.Recognize && it.Status == ItemOpen
		if delta.IsZero() && newRec.Sub(it.RecognizedAmount).IsZero() && !stamp {
			continue
		}
		changes = append(changes, change{item: it, newRecognized: newRec, delta: delta, stamp: stamp})
		if !delta.IsZero() {
			acc := itemDepositCode(it)
			perAcc[acc] = moneyOrZero(perAcc[acc]).Add(delta)
			total = total.Add(delta)
		}
	}
	if len(changes) == 0 {
		return domain.Zero, nil
	}

	// Jurnal penyesuaian — hanya bila ada rupiah yang benar-benar berpindah.
	var journalID *uint64
	if id, err := s.postRecognitionJournalTx(ctx, tx, tenantID, g, perAcc, total, opt); err != nil {
		return domain.Zero, err
	} else if id != 0 {
		journalID = &id
	}

	for _, c := range changes {
		updates := map[string]any{}
		if !c.newRecognized.Sub(c.item.RecognizedAmount).IsZero() {
			updates["recognized_amount"] = c.newRecognized
		}
		if c.stamp {
			if c.item.RecognizedAt == nil {
				at := opt.Date
				updates["recognized_at"] = at
			}
			if due := recognitionDueDate(c.item, opt.DueDate); due != nil {
				updates["recognized_due_date"] = *due
			}
			if opt.InvoiceID != nil {
				updates["recognized_invoice_id"] = *opt.InvoiceID
			}
		}
		if len(updates) > 0 {
			if err := tx.WithContext(ctx).Model(&ChargeItem{}).
				Where("id = ? AND tenant_id = ?", c.item.ID, tenantID).
				Updates(updates).Error; err != nil {
				return domain.Zero, fmt.Errorf("perbarui pengakuan item: %w", err)
			}
		}
		if c.delta.IsZero() {
			continue
		}
		if err := recordRecognitionInTx(ctx, tx, tenantID, g, c.item, c.delta, opt.Reason, journalID, opt.InvoiceID, opt.CreatedBy); err != nil {
			return domain.Zero, err
		}
	}
	return total, nil
}

// recognitionDueDate memilih jatuh tempo piutang: jatuh tempo item bila ada,
// jatuh tempo invoice bila tidak. Aging harus punya tanggal — item titipan
// sering dibuat tanpa jatuh tempo sendiri, dan tanpa fallback ini piutangnya
// akan ada di buku besar tapi hilang dari laporan umur piutang.
func recognitionDueDate(it *ChargeItem, invoiceDue *time.Time) *time.Time {
	if it.DueDate != nil {
		return it.DueDate
	}
	return invoiceDue
}

// postRecognitionJournalTx menerbitkan jurnal penyesuaian piutang. Sisi titipan
// pecah per akun (satu grup boleh memuat Notaris/BPHTB/PDAM dengan akun berbeda),
// sisi piutang selalu satu baris 1-2000 sebesar NET-nya — karena piutang customer
// memang satu, apa pun jenis biaya yang menyusunnya.
func (s *Service) postRecognitionJournalTx(ctx context.Context, tx *gorm.DB, tenantID uint64,
	g *ChargeGroup, perAcc map[string]domain.Money, total domain.Money, opt recognitionOpts) (uint64, error) {

	parts := sortedDepositAmounts(perAcc)
	if len(parts) == 0 {
		return 0, nil
	}
	u, err := findUnitRefDB(ctx, tx, tenantID, g.UnitID)
	if err != nil {
		return 0, err
	}
	pid, uid := u.ProjectID, g.UnitID

	lines := make([]ledger.LineInput, 0, len(parts)+1)
	for _, p := range parts {
		accID, err := depositAccountIDByCode(ctx, tx, tenantID, p.AccountCode)
		if err != nil {
			return 0, err
		}
		l := ledger.LineInput{AccountID: accID, ProjectID: &pid, PhaseID: u.PhaseID, UnitID: &uid,
			Description: "Titipan Realisasi (kewajiban — bukan pendapatan)"}
		if p.Amount.IsNeg() {
			l.Debit = domain.Zero.Sub(p.Amount)
		} else {
			l.Credit = p.Amount
		}
		lines = append(lines, l)
	}
	if !total.IsZero() {
		arID, err := accountIDByCode(ctx, tx, tenantID, AccountCodePiutangCustomer)
		if err != nil {
			return 0, err
		}
		l := ledger.LineInput{AccountID: arID, ProjectID: &pid, PhaseID: u.PhaseID, UnitID: &uid,
			Description: opt.Desc}
		if total.IsNeg() {
			l.Credit = domain.Zero.Sub(total)
		} else {
			l.Debit = total
		}
		lines = append(lines, l)
	}
	// Bukan jurnal kas — dokumennya adalah invoice (atau aksi yang memicunya),
	// jadi INV-DOC-1 tidak berlaku di sini.
	return postJournalTx(ctx, tx, tenantID, opt.Date, opt.Desc, opt.CreatedBy, lines)
}

// recordRecognitionInTx menulis SATU baris audit pengakuan (append-only).
func recordRecognitionInTx(ctx context.Context, tx *gorm.DB, tenantID uint64, g *ChargeGroup,
	it *ChargeItem, delta domain.Money, reason RecognitionReason, journalID, invoiceID, createdBy *uint64) error {

	row := &ChargeReceivableRecognition{
		TenantID:       tenantID,
		ChargeGroupID:  g.ID,
		ChargeItemID:   it.ID,
		UnitID:         g.UnitID,
		Delta:          delta,
		Reason:         reason,
		DepositAccount: itemDepositCode(it),
		ReceivableAcct: AccountCodePiutangCustomer,
		JournalEntryID: journalID,
		InvoiceID:      invoiceID,
		CreatedBy:      createdBy,
	}
	if err := tx.WithContext(ctx).Create(row).Error; err != nil {
		return fmt.Errorf("catat pengakuan piutang: %w", err)
	}
	return nil
}

// ── Routing pembayaran (piutang lebih dulu, sisanya titipan) ─────────────────

// receivableSplit memecah alokasi sebuah pembayaran menjadi porsi yang melunasi
// PIUTANG (item yang sudah di-invoice) dan porsi yang menjadi TITIPAN (item yang
// belum). Urutannya bukan pilihan: rupiah pertama selalu melunasi tagihan yang
// sudah terbit dokumennya, sisanya baru menjadi uang muka atas tagihan yang
// belum ditagihkan.
//
// Mengembalikan porsi piutang per item, totalnya, dan pecahan titipan per akun.
type receivableSplit struct {
	PerItem  map[uint64]domain.Money // itemID → porsi yang mengurangi piutang
	AR       domain.Money            // Σ PerItem
	Deposits []DepositAmount         // sisanya, per akun kewajiban
}

func splitAgainstReceivable(allocs []AllocationInput, items []*ChargeItem, posted map[uint64]domain.Money) receivableSplit {
	byID := make(map[uint64]*ChargeItem, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}
	out := receivableSplit{PerItem: map[uint64]domain.Money{}, AR: domain.Zero}
	perAcc := map[string]domain.Money{}
	for _, a := range allocs {
		it, ok := byID[a.ChargeItemID]
		if !ok {
			continue
		}
		ar := moneyOrZero(posted[a.ChargeItemID])
		if ar.GreaterThan(a.Amount) {
			ar = a.Amount
		}
		if ar.GreaterThan(domain.Zero) {
			out.PerItem[a.ChargeItemID] = ar
			out.AR = out.AR.Add(ar)
		}
		if rest := a.Amount.Sub(ar); rest.GreaterThan(domain.Zero) {
			acc := itemDepositCode(it)
			perAcc[acc] = moneyOrZero(perAcc[acc]).Add(rest)
		}
	}
	out.Deposits = sortedDepositAmounts(perAcc)
	return out
}

// receivableCreditLines menyusun baris kredit sebuah penerimaan: satu baris
// 1-2000 (bila ada porsi piutang) + baris titipan per akun.
func receivableCreditLines(ctx context.Context, tx *gorm.DB, tenantID uint64, split receivableSplit,
	u *unitRef, unitID uint64, desc string) ([]ledger.LineInput, error) {

	lines := make([]ledger.LineInput, 0, len(split.Deposits)+1)
	if split.AR.GreaterThan(domain.Zero) {
		arID, err := accountIDByCode(ctx, tx, tenantID, AccountCodePiutangCustomer)
		if err != nil {
			return nil, err
		}
		pid := u.ProjectID
		lines = append(lines, ledger.LineInput{AccountID: arID, Credit: split.AR,
			ProjectID: &pid, PhaseID: u.PhaseID, UnitID: &unitID,
			Description: "Pelunasan piutang biaya realisasi"})
	}
	// Pembayaran yang seluruhnya melunasi piutang tidak menyentuh titipan sama
	// sekali — dan itu keadaan yang benar, bukan "akun titipan hilang".
	if len(split.Deposits) == 0 {
		return lines, nil
	}
	dep, err := depositLines(ctx, tx, tenantID, split.Deposits, false, u, unitID, desc)
	if err != nil {
		return nil, err
	}
	return append(lines, dep...), nil
}

// primaryCreditCode memilih akun DOMINAN sisi kredit sebuah penerimaan untuk
// kolom ringkasan satu-nilai pada termin. Piutang ikut dipertimbangkan: sejak
// W-5 sebagian besar pembayaran realisasi mengkredit 1-2000, dan menuliskan akun
// titipan di sana akan menyesatkan pembaca. Kebenaran akuntansinya tetap ada di
// baris jurnal, tidak pernah di kolom ini.
func primaryCreditCode(split receivableSplit) string {
	parts := split.Deposits
	if split.AR.GreaterThan(domain.Zero) {
		parts = append(append([]DepositAmount{}, parts...),
			DepositAmount{AccountCode: AccountCodePiutangCustomer, Amount: split.AR})
	}
	return primaryDepositCode(parts)
}

// recordPaymentRecognitionInTx mencatat pengurangan piutang oleh sebuah
// pembayaran. Tidak menerbitkan jurnal sendiri: pengurangannya SUDAH menjadi
// baris kredit 1-2000 di dalam jurnal kas pembayaran itu. Satu peristiwa, satu
// jurnal — bukan jurnal kas plus jurnal penyesuaian.
func recordPaymentRecognitionInTx(ctx context.Context, tx *gorm.DB, tenantID uint64, g *ChargeGroup,
	items []*ChargeItem, split receivableSplit, reason RecognitionReason, journalID uint64, createdBy *uint64) error {

	byID := make(map[uint64]*ChargeItem, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}
	for _, it := range items {
		amt, ok := split.PerItem[it.ID]
		if !ok || amt.IsZero() {
			continue
		}
		jid := journalID
		if err := recordRecognitionInTx(ctx, tx, tenantID, g, byID[it.ID],
			domain.Zero.Sub(amt), reason, &jid, nil, createdBy); err != nil {
			return err
		}
	}
	return nil
}

// ── Pencabutan pengakuan (pembatalan penjualan pasca-BAST) ───────────────────

// ReverseRecognitionInTx membalik SELURUH piutang biaya realisasi sebuah unit —
// dipanggil dari transaksi pembatalan penjualan. Dana yang sudah diterima tidak
// disentuh: disposisinya tetap keputusan admin (R-4), bukan efek samping
// pembatalan.
func (s *Service) ReverseRecognitionInTx(ctx context.Context, tx *gorm.DB, tenantID, unitID uint64, createdBy *uint64) error {
	var groups []*ChargeGroup
	if err := tx.WithContext(ctx).Clauses(gormLockingUpdate()).
		Where("tenant_id = ? AND unit_id = ? AND kind = ?", tenantID, unitID, string(KindRealization)).
		Find(&groups).Error; err != nil {
		return fmt.Errorf("muat grup realisasi unit: %w", err)
	}
	for _, g := range groups {
		if _, err := s.syncRecognitionInTx(ctx, tx, tenantID, g, recognitionOpts{
			Reason:    ReasonRecSaleCancelled,
			Date:      time.Now(),
			Desc:      fmt.Sprintf("Pembalikan piutang biaya realisasi grup #%d (penjualan dibatalkan)", g.ID),
			CreatedBy: createdBy,
			Release:   true,
		}); err != nil {
			return err
		}
	}
	return nil
}

// ── R-3: jurnal susulan sekali jalan ────────────────────────────────────────

// CatchUpReport merangkum hasil satu putaran jurnal susulan.
type CatchUpReport struct {
	GroupsScanned   int
	GroupsRecognize int
	Posted          domain.Money
	Skipped         []string // alasan per grup yang dilewati (dibaca manusia)
}

// CatchUpRecognition menerbitkan pengakuan piutang untuk grup yang invoice-nya
// SUDAH terbit sebelum W-5 (keputusan R-3). Sebelum W-5 invoice realisasi terbit
// tanpa jurnal apa pun, jadi tanpa langkah ini piutang lama tidak akan pernah
// ada di buku besar — sementara piutang baru sudah ada. Dua aturan berjalan
// bersamaan adalah keadaan yang tidak boleh dibiarkan hidup.
//
// Idempoten karena rumusnya, bukan karena penanda terpisah: setelah putaran
// pertama Σ delta item sudah sama dengan kontribusi seharusnya, sehingga putaran
// berikutnya menghitung delta nol dan tidak menerbitkan jurnal apa pun.
//
// apply=false menjalankan seluruh perhitungan lalu MEMBATALKAN transaksinya —
// laporan dry-run yang dihitung dari jalur yang sama persis dengan jalur nyata,
// bukan dari perkiraan terpisah yang bisa berbeda.
func (s *Service) CatchUpRecognition(ctx context.Context, tenantID uint64, apply bool, actor *uint64) (*CatchUpReport, error) {
	if s.invoices == nil {
		return nil, fmt.Errorf("wiring tidak lengkap: jurnal susulan tanpa pembaca invoice")
	}
	rep := &CatchUpReport{Posted: domain.Zero}
	errStop := fmt.Errorf("dry-run")
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var groups []*ChargeGroup
		if err := tx.WithContext(ctx).Clauses(gormLockingUpdate()).
			Where("tenant_id = ? AND kind = ? AND status = ?",
				tenantID, string(KindRealization), string(GroupOpen)).
			Order("id ASC").Find(&groups).Error; err != nil {
			return fmt.Errorf("muat grup realisasi: %w", err)
		}
		for _, g := range groups {
			rep.GroupsScanned++
			invID, invNo, due, err := s.invoices.FindLiveChargeInvoiceInTx(ctx, tx, tenantID, g.SaleContractID, g.ID)
			if err != nil {
				return err
			}
			if invID == 0 {
				// Belum pernah ditagihkan → memang belum piutang (R-5).
				rep.Skipped = append(rep.Skipped,
					fmt.Sprintf("grup #%d %s: belum ada invoice hidup", g.ID, g.Label))
				continue
			}
			posted, err := s.syncRecognitionInTx(ctx, tx, tenantID, g, recognitionOpts{
				Reason:    ReasonRecCatchUp,
				Date:      time.Now(),
				Desc:      fmt.Sprintf("Jurnal susulan piutang biaya realisasi — invoice %s (grup #%d)", invNo, g.ID),
				InvoiceID: &invID,
				DueDate:   &due,
				CreatedBy: actor,
				Recognize: true,
			})
			if err != nil {
				return err
			}
			if posted.IsZero() {
				continue
			}
			rep.GroupsRecognize++
			rep.Posted = rep.Posted.Add(posted)
		}
		if !apply {
			return errStop
		}
		return nil
	})
	if err != nil && err != errStop {
		return nil, err
	}
	return rep, nil
}
