package ledger

import (
	"context"
	"time"

	"esaproperti/internal/domain"
)

// ═════════════════════════════════════════════════════════════════════════════
// LedgerBalanceService (Phase 2 · S2 — docs/financial-canonical-registry.md §R-2)
//
// SATU-SATUNYA pembaca SALDO akun posted per PERAN keuangan (AccountRoleRegistry).
// Menggantikan `SUM(journal_lines...)` manual yang tersebar di reporting untuk
// kas/piutang/hutang/titipan/komisi. Saldo dihitung lewat ComputeTrialBalance
// (fungsi murni yang sama dengan neraca/trial-balance) → arah normal per tipe
// akun; nilai untuk peran = Σ Balance akun anggota peran.
//
// asOf nil = SEMUA posted (posisi berjalan; perilaku dashboard existing, nol
// perubahan angka). asOf di-set = date <= asOf (posisi historis, dipakai neraca).
// ═════════════════════════════════════════════════════════════════════════════

// LedgerBalanceService adalah read-service kanonik saldo per peran.
type LedgerBalanceService struct {
	q *QueryService
}

// NewLedgerBalanceService membuat service di atas QueryService.
func NewLedgerBalanceService(q *QueryService) *LedgerBalanceService {
	return &LedgerBalanceService{q: q}
}

// RoleBalances mengembalikan saldo (arah normal) yang dijumlahkan per peran,
// atas jurnal posted. asOf nil = tanpa batas tanggal.
// Peran yang tak punya anggota tetap ada di map bernilai nol.
func (s *LedgerBalanceService) RoleBalances(ctx context.Context, tenantID uint64, asOf *time.Time) (map[AccountRole]domain.Money, error) {
	lines, accMap, err := s.q.postedBalanceData(ctx, tenantID, asOf)
	if err != nil {
		return nil, err
	}
	stamp := time.Now()
	if asOf != nil {
		stamp = *asOf
	}
	tb := ComputeTrialBalance(lines, accMap, stamp)

	roles := []AccountRole{
		RoleCashBank, RoleReceivable, RolePayable, RoleBookingLiability,
		RoleCommissionLiability, RoleInventory, RoleVATOutput, RoleVATInput, RoleTaxLiability,
		// W-11: peran BARU, ditambahkan sebagai key tersendiri. Nilai peran lama
		// tidak berubah — RolePayable tetap 2-1000 saja (R-11).
		RoleRetentionPayable, RoleVendorAdvance,
	}
	out := make(map[AccountRole]domain.Money, len(roles))
	for _, r := range roles {
		out[r] = domain.Zero
	}
	for i := range tb.Rows {
		a := accMap[tb.Rows[i].AccountID]
		for _, r := range roles {
			if AccountInRole(a, r) {
				out[r] = out[r].Add(tb.Rows[i].Balance)
			}
		}
	}
	return out, nil
}

// AccountBalance mengembalikan saldo (arah normal) SATU akun berdasarkan
// kodenya, atas jurnal posted. Akun yang tidak ada / belum pernah dijurnal
// mengembalikan nol tanpa error — "belum ada saldo" bukan kegagalan.
//
// Ditambahkan untuk W-7: rekonsiliasi piutang lama harus membandingkan rincian
// sub-ledger terhadap saldo AKUN KONTROL tertentu (mis. 1-2000), bukan terhadap
// peran RoleReceivable yang menjumlahkan 1-2000 + 1-2100 + 1-2200. Menyamakan
// keduanya akan membuat uang muka KPR dan piutang bank ikut terhitung sebagai
// piutang customer, dan selisihnya akan terlihat "cocok" karena alasan yang salah.
//
// Ia tetap dibangun di atas ComputeTrialBalance yang sama supaya tidak ada
// definisi saldo kedua di repo ini.
func (s *LedgerBalanceService) AccountBalance(ctx context.Context, tenantID uint64, code string, asOf *time.Time) (domain.Money, error) {
	lines, accMap, err := s.q.postedBalanceData(ctx, tenantID, asOf)
	if err != nil {
		return domain.Zero, err
	}
	stamp := time.Now()
	if asOf != nil {
		stamp = *asOf
	}
	tb := ComputeTrialBalance(lines, accMap, stamp)

	out := domain.Zero
	for i := range tb.Rows {
		if a := accMap[tb.Rows[i].AccountID]; a != nil && a.Code == code {
			out = out.Add(tb.Rows[i].Balance)
		}
	}
	return out, nil
}

// AccountMovementBySource memecah saldo (arah normal) SATU akun menurut
// ASAL EKONOMI-nya, atas jurnal posted. Kuncinya adalah `journal_entries.source`
// apa adanya; source kosong dikelompokkan sebagai "".
//
// Satu pengecualian yang disengaja: jurnal PEMBALIK dihitung pada source jurnal
// yang dibalikkannya, bukan pada "reversal". Alasannya bukan kerapian melainkan
// kebenaran — buku besar ini append-only, jadi satu-satunya cara sah mengoreksi
// saldo awal yang salah adalah membalikkannya. Kalau pembalik itu jatuh ke
// kelompok lain, koreksi yang benar justru membuat porsi saldo awal tampak
// terlalu besar SELAMANYA, dan rekonsiliasi piutang lama melaporkan selisih yang
// tidak pernah bisa ditutup. Pembalik adalah pencabutan mutasi asalnya, jadi ia
// harus mengurangi kelompok yang sama.
//
// Σ nilai peta ini SAMA PERSIS dengan AccountBalance untuk akun yang sama —
// keduanya menjumlahkan baris posted yang sama dengan arah normal yang sama.
// Yang berbeda hanya pengelompokannya.
//
// Ditambahkan untuk W-7. Rekonsiliasi piutang lama perlu memisahkan bagian
// saldo 1-2000 yang berasal dari SALDO AWAL dari bagian yang berasal dari
// operasi berjalan (BAST, penagihan, pelunasan). Memisahkannya lewat source
// jurnal jauh lebih kuat daripada meminta paket sale/charge menghitung ulang
// "piutang yang sudah diakui": buku besarnya sendiri yang menjawab, sehingga
// tidak ada definisi piutang kedua yang bisa menyimpang.
func (s *LedgerBalanceService) AccountMovementBySource(ctx context.Context, tenantID uint64, code string, asOf *time.Time) (map[string]domain.Money, error) {
	var acc Account
	err := s.q.db.WithContext(ctx).
		Where("tenant_id = ? AND code = ?", tenantID, code).
		First(&acc).Error
	if err != nil {
		// Akun yang belum ada bukan kegagalan — belum ada mutasinya.
		return map[string]domain.Money{}, nil
	}

	type row struct {
		Source string
		Debit  domain.Money
		Credit domain.Money
	}
	// `orig` menghadirkan jurnal yang dibalikkan supaya pembalik mewarisi
	// source-nya. Satu tingkat sudah cukup: pembalik hanya boleh dibuat atas
	// jurnal yang belum pernah dibalikkan (ErrJournalAlreadyReversed), sehingga
	// rantai pembalik-atas-pembalik tidak terbentuk.
	const effectiveSource = "COALESCE(NULLIF(orig.source, ''), je.source, '')"
	q := s.q.db.WithContext(ctx).
		Table("journal_lines AS jl").
		Select(effectiveSource+" AS source, COALESCE(SUM(jl.debit),0) AS debit, COALESCE(SUM(jl.credit),0) AS credit").
		Joins("JOIN journal_entries je ON je.id = jl.journal_entry_id").
		Joins("LEFT JOIN journal_entries orig ON orig.id = je.reverses_id AND orig.tenant_id = je.tenant_id").
		Where("jl.tenant_id = ? AND jl.account_id = ? AND je.posted_at IS NOT NULL", tenantID, acc.ID)
	if asOf != nil {
		q = q.Where("je.date <= ?", *asOf)
	}
	var rows []row
	if err := q.Group(effectiveSource).Scan(&rows).Error; err != nil {
		return nil, err
	}

	out := make(map[string]domain.Money, len(rows))
	for _, r := range rows {
		net := r.Debit.Sub(r.Credit)
		if acc.NormalBalance == domain.NormalBalanceCredit {
			net = r.Credit.Sub(r.Debit)
		}
		out[r.Source] = out[r.Source].Add(net)
	}
	return out, nil
}

// AccountMovementForJournals mengembalikan mutasi (arah normal) SATU akun yang
// berasal dari sekumpulan jurnal tertentu — dan dari PEMBALIK jurnal-jurnal itu.
//
// Ditambahkan untuk W-8. Rekonsiliasi piutang harga rumah harus menjawab
// "berapa bagian saldo 1-2000 yang lahir dari penjualan rumah?", dan memecah
// per `source` tidak cukup: seluruh jurnal domain penjualan MAUPUN pengakuan
// biaya realisasi sama-sama tercatat `source = "system"`. Yang membedakan
// keduanya bukan label melainkan KEPEMILIKAN DATA — jurnal yang id-nya tersimpan
// di `sale_records` / `termin_payments` / `contract_payment_events` adalah milik
// domain penjualan, dan hanya pemiliknya yang boleh mengklaimnya.
//
// Menghitungnya sebagai sisa (saldo − saldo awal − realisasi) akan membuat
// jurnal manual yang menyentuh 1-2000 diam-diam terhitung sebagai piutang rumah,
// sehingga tie-out justru COCOK persis pada kasus yang seharusnya ia tangkap.
//
// journalIDs kosong mengembalikan nol tanpa query. Daftar dipotong per 1000 id
// supaya tenant dengan puluhan ribu termin tidak menabrak batas paket query.
func (s *LedgerBalanceService) AccountMovementForJournals(ctx context.Context, tenantID uint64, code string, journalIDs []uint64, asOf *time.Time) (domain.Money, error) {
	if len(journalIDs) == 0 {
		return domain.Zero, nil
	}
	var acc Account
	err := s.q.db.WithContext(ctx).
		Where("tenant_id = ? AND code = ?", tenantID, code).
		First(&acc).Error
	if err != nil {
		// Akun yang belum ada bukan kegagalan — belum ada mutasinya.
		return domain.Zero, nil
	}

	total := domain.Zero
	const chunk = 1000
	for start := 0; start < len(journalIDs); start += chunk {
		end := start + chunk
		if end > len(journalIDs) {
			end = len(journalIDs)
		}
		ids := journalIDs[start:end]

		var r struct {
			Debit  domain.Money
			Credit domain.Money
		}
		q := s.q.db.WithContext(ctx).
			Table("journal_lines AS jl").
			Select("COALESCE(SUM(jl.debit),0) AS debit, COALESCE(SUM(jl.credit),0) AS credit").
			Joins("JOIN journal_entries je ON je.id = jl.journal_entry_id").
			Where("jl.tenant_id = ? AND jl.account_id = ? AND je.posted_at IS NOT NULL", tenantID, acc.ID).
			// Pembalik ikut: membalikkan jurnal BAST adalah satu-satunya cara sah
			// mencabutnya (buku besar append-only), dan pencabutan itu tetap milik
			// domain yang sama. Bila pembalik tidak ikut terhitung, saldo GL turun
			// sementara klaim sub-ledger tidak — dan selisihnya abadi.
			Where("(je.id IN ? OR je.reverses_id IN ?)", ids, ids)
		if asOf != nil {
			q = q.Where("je.date <= ?", *asOf)
		}
		if err := q.Scan(&r).Error; err != nil {
			return domain.Zero, err
		}
		net := r.Debit.Sub(r.Credit)
		if acc.NormalBalance == domain.NormalBalanceCredit {
			net = r.Credit.Sub(r.Debit)
		}
		total = total.Add(net)
	}
	return total, nil
}

// ReceivableControlAccounts mengembalikan kode akun kontrol piutang menurut
// registry peran (RoleReceivable). Dipakai rekonsiliasi W-8 agar paket sub-ledger
// tidak menyimpan daftar akun piutangnya sendiri.
func (s *LedgerBalanceService) ReceivableControlAccounts() []string {
	return RoleCodeList(RoleReceivable)
}

// RoleBalance adalah pintas untuk satu peran.
func (s *LedgerBalanceService) RoleBalance(ctx context.Context, tenantID uint64, role AccountRole, asOf *time.Time) (domain.Money, error) {
	all, err := s.RoleBalances(ctx, tenantID, asOf)
	if err != nil {
		return domain.Zero, err
	}
	return all[role], nil
}
