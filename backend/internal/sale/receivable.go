package sale

// W-8 — Piutang Harga Rumah sebagai sumber tunggal yang bisa di-tie-out dengan
// Buku Besar.
//
// BD-1 (keputusan owner, FINAL): piutang harga rumah LAHIR SAAT BAST. Jadwal
// pembayaran sebelum BAST adalah rencana penagihan, bukan piutang. Sebelum W-8
// laporan piutang membaca `payment_schedules` apa adanya — sehingga jadwal yang
// unitnya belum diserahterimakan ikut dihitung sebagai piutang, sementara unit
// yang SUDAH BAST tapi tak punya jadwal tidak muncul sama sekali. Kedua arah
// kesalahan itu terjadi bersamaan di data nyata.
//
// Yang dibaca di sini adalah peristiwa yang MENGGERAKKAN buku besar:
//
//	outstanding = gross(SaleRecord) − Σ termin_payments(counts_toward_price)
//
// Rumus itu bukan hal baru: ia sudah dipakai guard overpayment di ReceivePayment
// dan penentu nilai reklas T-3 di reclassReceivableIfBAST. W-8 memberinya nama
// (UnitOutstanding) dan menjadikannya pemakai ketiga — bukan definisi kedua.
//
// Karena `sale_records` dan `termin_payments` ditulis dalam transaksi yang SAMA
// dengan jurnalnya, sub-ledger ini tidak bisa menyimpang dari GL tanpa ada yang
// menulis jurnal di luar domain — dan itulah yang dijaga penjaga reverse (R-1).
// Tidak ada tabel sub-ledger baru: tabel baru hanya akan menjadi tempat ketiga
// yang bisa berbeda.

import (
	"context"
	"fmt"
	"time"

	"esaproperti/internal/domain"
	"esaproperti/internal/receivable"
)

// ── Seam data ─────────────────────────────────────────────────────────────────

// HouseARUnit adalah satu unit yang piutangnya SUDAH lahir (ada SaleRecord yang
// belum dibatalkan), beserta bahan penghitung sisanya.
type HouseARUnit struct {
	ContractID uint64       `gorm:"column:contract_id"`
	UnitID     uint64       `gorm:"column:unit_id"`
	UnitCode   string       `gorm:"column:unit_code"`
	BuyerName  string       `gorm:"column:buyer_name"`
	BuyerPhone string       `gorm:"column:buyer_phone"`
	BuyerEmail string       `gorm:"column:buyer_email"`
	Gross      domain.Money `gorm:"column:gross"`
	Collected  domain.Money `gorm:"column:collected"`
	// RecognitionDate (Temuan #7): tanggal Akad, dulu bast_date/BASTDate.
	RecognitionDate time.Time `gorm:"column:recognition_date"`
}

// Outstanding adalah sisa piutang unit — nilai yang HARUS sama dengan saldo akun
// kontrol piutang unit itu di buku besar.
func (u HouseARUnit) Outstanding() domain.Money {
	return u.Gross.Sub(u.Collected)
}

// HouseARSchedule adalah rencana penagihan yang memberi STRUKTUR JATUH TEMPO
// pada sisa piutang. Ia tidak menentukan BERAPA piutangnya — hanya kapan.
type HouseARSchedule struct {
	ID            uint64       `gorm:"column:id"`
	UnitID        uint64       `gorm:"column:unit_id"`
	InstallmentNo int          `gorm:"column:installment_number"`
	Type          string       `gorm:"column:type"`
	DueDate       time.Time    `gorm:"column:due_date"`
	Amount        domain.Money `gorm:"column:amount"`
	PaidAmount    domain.Money `gorm:"column:paid_amount"`
	InvoiceNumber string       `gorm:"column:invoice_number"`
}

// Remaining adalah kapasitas baris jadwal ini menampung sisa piutang.
func (s HouseARSchedule) Remaining() domain.Money {
	r := s.Amount.Sub(s.PaidAmount)
	if r.IsNeg() {
		return domain.Zero
	}
	return r
}

// HouseARStore membaca bahan mentah house AR. Dipisahkan dari TerminStore karena
// ini jalur BACA laporan, bukan jalur tulis transaksi.
type HouseARStore interface {
	// ListHouseARUnits mengembalikan unit yang SUDAH BAST dan belum dibatalkan.
	// Unit tanpa SaleRecord tidak boleh muncul (BD-1).
	ListHouseARUnits(ctx context.Context, tenantID uint64) ([]HouseARUnit, error)
	// ListHouseARSchedules mengembalikan jadwal non-superseded unit-unit tsb,
	// urut jatuh tempo lalu id.
	ListHouseARSchedules(ctx context.Context, tenantID uint64, unitIDs []uint64) ([]HouseARSchedule, error)
	// ListOwnedJournalIDs mengembalikan jurnal yang dimiliki domain penjualan —
	// klaim paket ini atas mutasi akun kontrol piutang.
	ListOwnedJournalIDs(ctx context.Context, tenantID uint64) ([]uint64, error)
}

// HouseARLedgerReader adalah sisi BUKU BESAR dari rekonsiliasi. Interface, bukan
// import langsung ke ledger.LedgerBalanceService, supaya tie-out bisa diuji
// tanpa database dan supaya arah dependensinya tetap satu arah.
type HouseARLedgerReader interface {
	AccountBalance(ctx context.Context, tenantID uint64, code string, asOf *time.Time) (domain.Money, error)
	AccountMovementForJournals(ctx context.Context, tenantID uint64, code string, journalIDs []uint64, asOf *time.Time) (domain.Money, error)
	// ReceivableControlAccounts adalah daftar akun kontrol piutang menurut
	// registry peran akun — satu-satunya otoritas soal "akun mana yang berperan
	// piutang". Paket ini tidak boleh punya daftarnya sendiri: daftar kedua akan
	// menua diam-diam dan membuat akun yang menahan piutang luput dari tie-out.
	ReceivableControlAccounts() []string
}

// WithHouseARLedger memasang pembaca buku besar untuk rekonsiliasi W-8.
func WithHouseARLedger(l HouseARLedgerReader) ServiceOption {
	return func(s *Service) { s.houseARLedger = l }
}

// WithHouseARStore memasang sumber house AR (wiring produksi: GORMRepository).
func WithHouseARStore(hs HouseARStore) ServiceOption {
	return func(s *Service) { s.houseAR = hs }
}

// ErrHouseARNotConfigured dikembalikan bila pembaca dipanggil tanpa store.
// Fail-closed: laporan piutang yang diam-diam kosong lebih berbahaya daripada
// laporan yang menolak tampil.
var ErrHouseARNotConfigured = fmt.Errorf("sale: pembaca piutang harga rumah belum dikonfigurasi")

// ── Baris untuk mesin aging (INV-AR-1) ────────────────────────────────────────

// HouseARRow adalah satu baris house AR lengkap dengan akun kontrolnya.
// Akun kontrol adalah dimensi yang dibutuhkan rekonsiliasi (dan hanya itu):
// mesin aging tidak mengenalnya, jadi ia tidak ikut ke receivable.Row.
type HouseARRow struct {
	Row                receivable.Row
	ControlAccountCode string
}

// ReceivableRows adalah adapter house AR ke mesin piutang bersama (INV-AR-1).
//
// D-W8-6: yang masuk daftar "Piutang Customer" hanyalah baris yang akun
// kontrolnya piutang CUSTOMER. Sisa tagihan KPR antara akad dan pencairan duduk
// di Piutang Bank (T-3) — debiturnya bank, dan menagihkannya ke pembeli di layar
// bernama "Piutang Customer" berarti menagih orang yang salah. Baris itu tetap
// ikut pada rekonsiliasi (HouseARRows), hanya tidak di daftar penagihan.
func (s *Service) ReceivableRows(ctx context.Context, tenantID uint64) ([]receivable.Row, error) {
	all, err := s.HouseARRows(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]receivable.Row, 0, len(all))
	for _, r := range all {
		if r.ControlAccountCode != accountCodePiutang {
			continue
		}
		out = append(out, r.Row)
	}
	return out, nil
}

// HouseARRows menyusun SELURUH baris house AR, termasuk yang akun kontrolnya
// bukan piutang customer. Inilah himpunan yang di-tie-out dengan buku besar.
//
// Alokasi (D-W8-5): sisa piutang menurut buku besar dibagikan ke jadwal yang
// masih bersisa, urut jatuh tempo terawal. Jadwal hanya menyumbang TANGGAL;
// jumlahnya selalu berasal dari GL. Sisa yang tidak tertampung jadwal — termasuk
// unit yang tidak punya jadwal sama sekali — menjadi satu baris yang jatuh tempo
// pada TANGGAL BAST: piutang yang sudah diserahterimakan tanpa rencana
// penagihan tidak boleh diam-diam dianggap belum jatuh tempo selamanya.
//
// Dengan konstruksi ini Σ outstanding baris == outstanding GL, selalu, tanpa
// pembulatan (Invariant #3).
func (s *Service) HouseARRows(ctx context.Context, tenantID uint64) ([]HouseARRow, error) {
	if s.houseAR == nil {
		return nil, ErrHouseARNotConfigured
	}
	units, err := s.houseAR.ListHouseARUnits(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("baca unit piutang rumah: %w", err)
	}
	if len(units) == 0 {
		return []HouseARRow{}, nil
	}

	unitIDs := make([]uint64, 0, len(units))
	for _, u := range units {
		unitIDs = append(unitIDs, u.UnitID)
	}
	scheds, err := s.houseAR.ListHouseARSchedules(ctx, tenantID, unitIDs)
	if err != nil {
		return nil, fmt.Errorf("baca jadwal piutang rumah: %w", err)
	}
	byUnit := make(map[uint64][]HouseARSchedule, len(units))
	for _, sc := range scheds {
		byUnit[sc.UnitID] = append(byUnit[sc.UnitID], sc)
	}

	out := make([]HouseARRow, 0, len(units))
	for _, u := range units {
		finCode, finAmt, defCode, cerr := s.houseControlSplit(ctx, tenantID, u.UnitID)
		if cerr != nil {
			return nil, cerr
		}
		out = append(out, s.rowsForUnit(u, byUnit[u.UnitID], finCode, finAmt, defCode)...)
	}
	return out, nil
}

// controlPortion adalah sepotong nominal yang jatuh ke satu akun kontrol
// tertentu — satu baris jadwal/sisa bisa pecah jadi dua controlPortion kalau
// ia melewati batas antara Dana Jaminan Bank dan Piutang Usaha.
type controlPortion struct {
	Code string
	Amt  domain.Money
}

// rowsForUnit memecah sisa piutang satu unit menjadi baris ber-jatuh-tempo,
// SEKALIGUS membelahnya per akun kontrol: finCode/finAmt (Item 7A/7C) adalah
// sisa yang MASIH di Dana Jaminan Bank — tidak lagi otomatis dianggap pindah
// ke defCode begitu state maju ke `disbursed` (T-3 lama, sudah dicabut).
// Konsumsi finAmt bersifat FIFO mengikuti urutan jadwal (jatuh tempo
// terawal): tie-out hanya butuh Σ per akun kontrol cocok dengan GL, bukan
// klaim baris jadwal MANA persis yang "milik bank" — split itu memang lahir
// di level kontrak (Akad), bukan per-cicilan.
func (s *Service) rowsForUnit(u HouseARUnit, scheds []HouseARSchedule, finCode string, finAmt domain.Money, defCode string) []HouseARRow {
	outstanding := u.Outstanding()
	if outstanding.IsNeg() {
		outstanding = domain.Zero
	}

	// Unit lunas: tidak ada baris tabel, tetapi kontribusinya ke collection rate
	// tetap harus terhitung — kalau tidak, tingkat penagihan naik palsu setiap
	// kali seseorang melunasi. Akun kontrolnya tak berpengaruh (Received=true
	// dikecualikan dari total sub-ledger).
	if outstanding.IsZero() {
		return []HouseARRow{{
			ControlAccountCode: defCode,
			Row: receivable.Row{
				Source:     receivable.SourceHouse,
				ContractID: u.ContractID,
				UnitID:     u.UnitID,
				BuyerName:  u.BuyerName,
				UnitCode:   u.UnitCode,
				Label:      "Harga unit",
				BuyerPhone: u.BuyerPhone,
				BuyerEmail: u.BuyerEmail,
				DueDate:    u.RecognitionDate,
				Amount:     u.Gross,
				PaidAmount: u.Gross,
				Received:   true,
			},
		}}
	}

	finBudget := finAmt
	if finBudget.IsNeg() {
		finBudget = domain.Zero
	}
	if finBudget.GreaterThan(outstanding) {
		finBudget = outstanding
	}
	splitPortion := func(amt domain.Money) []controlPortion {
		if finBudget.IsZero() {
			return []controlPortion{{defCode, amt}}
		}
		finPart := amt
		if finPart.GreaterThan(finBudget) {
			finPart = finBudget
		}
		finBudget = finBudget.Sub(finPart)
		defPart := amt.Sub(finPart)
		out := make([]controlPortion, 0, 2)
		if !finPart.IsZero() {
			out = append(out, controlPortion{finCode, finPart})
		}
		if !defPart.IsZero() {
			out = append(out, controlPortion{defCode, defPart})
		}
		return out
	}

	rows := make([]HouseARRow, 0, len(scheds)+1)
	remaining := outstanding
	for _, sc := range scheds {
		if remaining.IsZero() {
			break
		}
		cap := sc.Remaining()
		if cap.IsZero() {
			continue
		}
		take := cap
		if take.GreaterThan(remaining) {
			take = remaining
		}
		remaining = remaining.Sub(take)
		for _, part := range splitPortion(take) {
			rows = append(rows, HouseARRow{
				ControlAccountCode: part.Code,
				Row: receivable.Row{
					Source:        receivable.SourceHouse,
					ContractID:    u.ContractID,
					UnitID:        u.UnitID,
					RefID:         sc.ID,
					BuyerName:     u.BuyerName,
					UnitCode:      u.UnitCode,
					Label:         houseScheduleLabel(sc.Type, sc.InstallmentNo),
					InvoiceNumber: sc.InvoiceNumber,
					BuyerPhone:    u.BuyerPhone,
					BuyerEmail:    u.BuyerEmail,
					DueDate:       sc.DueDate,
					Amount:        part.Amt,
				},
			})
		}
	}

	if !remaining.IsZero() {
		for _, part := range splitPortion(remaining) {
			rows = append(rows, HouseARRow{
				ControlAccountCode: part.Code,
				Row: receivable.Row{
					Source:     receivable.SourceHouse,
					ContractID: u.ContractID,
					UnitID:     u.UnitID,
					BuyerName:  u.BuyerName,
					UnitCode:   u.UnitCode,
					Label:      "Sisa harga unit (tanpa jadwal)",
					BuyerPhone: u.BuyerPhone,
					BuyerEmail: u.BuyerEmail,
					DueDate:    u.RecognitionDate,
					Amount:     part.Amt,
				},
			})
		}
	}

	// Yang sudah dibayar dititipkan pada baris pertama supaya basis collection
	// rate (Σ Amount) tetap sama dengan harga unit: Σ Amount = sisa + terbayar.
	// Menaruhnya di baris terpisah akan memunculkan baris lunas palsu di tabel.
	// Amount − PaidAmount baris itu tidak berubah, jadi tidak menggeser total
	// per akun kontrol di atas.
	if len(rows) > 0 && !u.Collected.IsZero() {
		rows[0].Row.Amount = rows[0].Row.Amount.Add(u.Collected)
		rows[0].Row.PaidAmount = u.Collected
	}
	return rows
}

// houseControlSplit menjawab BERAPA sisa piutang unit ini yang masih di Dana
// Jaminan Bank (finCode/finAmt) dan berapa di Piutang Usaha (defCode) — dari
// SUMBER YANG SAMA yang menentukan ke mana pencairan KPR mengkredit
// (schemeCreditAccountForPayment source=kpr_disbursement) dan ke mana Akad
// membelah piutang (schemeAkadSplitAccounts/financingOutstanding).
//
// Item 7C (UAT 2026-09-07): sisa Dana Jaminan Bank TIDAK LAGI otomatis
// dianggap pindah ke Piutang Usaha begitu scheme_state maju ke `disbursed`
// (itu bug T-3 lama). Reporting sebelumnya memakai resolusi berbasis STATE
// generik (ResolveReceivableAccount) untuk memilih SATU akun kontrol per
// unit — benar di rezim T-3 lump-sum, tapi salah sekarang: state bisa sudah
// `disbursed` sementara Dana Jaminan Bank masih menyisakan saldo nyata di GL
// (pencairan bertahap). Fungsi ini menghitung split NYATA dari GL
// (financingOutstanding), bukan dari state.
func (s *Service) houseControlSplit(ctx context.Context, tenantID, unitID uint64) (finCode string, finAmt domain.Money, defCode string, err error) {
	defCode = accountCodePiutang
	if s.contracts == nil {
		return "", domain.Zero, defCode, nil
	}
	c, cerr := s.contracts.FindContractByUnitID(ctx, tenantID, unitID)
	if cerr != nil {
		// Unit ber-BAST tanpa kontrak: piutangnya tetap ada di 1-2000 (jalur
		// BAST langsung tanpa kontrak). Bukan alasan menggagalkan laporan.
		return "", domain.Zero, defCode, nil
	}
	candidateFinCode, candidateDefCode := s.schemeAkadSplitAccounts(ctx, c)
	if candidateFinCode == "" || c.LoanAmount == nil {
		// Kontrak tanpa financing (atau belum pernah diisi Nilai Persetujuan
		// KPR Bank) — seluruhnya di akun default seperti semula.
		return "", domain.Zero, defCode, nil
	}
	remaining, ferr := s.financingOutstanding(ctx, tenantID, c, candidateFinCode)
	if ferr != nil {
		return "", domain.Zero, defCode, ferr
	}
	if candidateDefCode != "" {
		defCode = candidateDefCode
	}
	return candidateFinCode, remaining, defCode, nil
}

// HouseControlSplit (Gap 2, UAT 2026-09-08) mengekspor houseControlSplit untuk
// konsumen DI LUAR package ini (billing kwitansi, via adapter wiring) yang
// tidak boleh mengimpor package sale langsung (aturan impor CLAUDE.md). SUMBER
// SAMA dengan HouseARRows — bukan rumus kedua: Terutang kwitansi WAJIB tie-out
// dengan Piutang Usaha/Dana Jaminan Bank yang sama dilihat laporan aging.
func (s *Service) HouseControlSplit(ctx context.Context, tenantID, unitID uint64) (financingCode string, financingRemaining domain.Money, defaultReceivableCode string, err error) {
	return s.houseControlSplit(ctx, tenantID, unitID)
}

// houseScheduleLabel memberi nama baris cicilan. Sama dengan label yang dipakai
// laporan sebelum W-8 supaya operator tidak perlu belajar istilah baru.
func houseScheduleLabel(schedType string, no int) string {
	switch schedType {
	case "dp":
		return "Uang Muka (DP)"
	case "final":
		return "Pelunasan"
	}
	if no > 0 {
		return fmt.Sprintf("Termin %d", no)
	}
	return "Cicilan"
}

// ── Rekonsiliasi GL ↔ sub-ledger (INV-AR-4) ───────────────────────────────────

// HouseARLayer adalah satu lapis rekonsiliasi: satu akun kontrol, sisi
// sub-ledger dan sisi buku besar berdampingan.
type HouseARLayer struct {
	ControlAccountCode string       `json:"control_account_code"`
	ControlAccountName string       `json:"control_account_name"`
	Subledger          domain.Money `json:"subledger"`
	UnitCount          int          `json:"unit_count"`
}

// HouseARSummary adalah sisi sub-ledger dari rekonsiliasi, dipecah per akun
// kontrol — pola "kupas lapis" yang sama dengan W-7. Membandingkan dua angka
// besar akan menyembunyikan dua kesalahan yang kebetulan saling menutup.
type HouseARSummary struct {
	Total  domain.Money   `json:"total"`
	Layers []HouseARLayer `json:"layers"`
}

// HouseARSubledger merangkum house AR per akun kontrol.
func (s *Service) HouseARSubledger(ctx context.Context, tenantID uint64) (*HouseARSummary, error) {
	rows, err := s.HouseARRows(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	order := make([]string, 0, 2)
	totals := map[string]domain.Money{}
	units := map[string]map[uint64]struct{}{}
	total := domain.Zero

	for _, r := range rows {
		out := r.Row.Amount.Sub(r.Row.PaidAmount)
		if r.Row.Received || out.IsZero() || out.IsNeg() {
			continue
		}
		code := r.ControlAccountCode
		if _, seen := totals[code]; !seen {
			order = append(order, code)
			totals[code] = domain.Zero
			units[code] = map[uint64]struct{}{}
		}
		totals[code] = totals[code].Add(out)
		units[code][r.Row.UnitID] = struct{}{}
		total = total.Add(out)
	}

	sum := &HouseARSummary{Total: total, Layers: make([]HouseARLayer, 0, len(order))}
	for _, code := range order {
		sum.Layers = append(sum.Layers, HouseARLayer{
			ControlAccountCode: code,
			Subledger:          totals[code],
			UnitCount:          len(units[code]),
		})
	}
	return sum, nil
}

// ── Tie-out (INV-AR-4) ────────────────────────────────────────────────────────

// ErrHouseARLedgerNotConfigured dikembalikan bila rekonsiliasi dipanggil tanpa
// pembaca buku besar. Fail-closed dengan alasan yang sama seperti store: alat
// kontrol yang diam-diam melaporkan "cocok" lebih buruk daripada tidak ada.
var ErrHouseARLedgerNotConfigured = fmt.Errorf("sale: pembaca buku besar untuk rekonsiliasi piutang rumah belum dikonfigurasi")

// HouseARReconLayer adalah satu akun kontrol yang dikupas lapis demi lapis.
type HouseARReconLayer struct {
	ControlAccountCode string `json:"control_account_code"`
	UnitCount          int    `json:"unit_count"`

	// Subledger = Σ outstanding baris house AR pada akun ini.
	Subledger domain.Money `json:"subledger"`
	// LedgerHouse = mutasi akun ini yang berasal dari jurnal MILIK domain
	// penjualan (BAST, termin, reklas, beserta pembaliknya). Inilah pembanding
	// yang sah — bukan saldo akun seluruhnya.
	LedgerHouse domain.Money `json:"ledger_house"`
	// Difference = Subledger − LedgerHouse. Nol adalah satu-satunya nilai yang
	// benar (INV-AR-4).
	Difference domain.Money `json:"difference"`
	Matched    bool         `json:"matched"`

	// LedgerTotal = saldo akun kontrol seluruhnya, dan LedgerOther sisanya.
	// Keduanya BUKAN bagian dari tie-out; mereka ada supaya orang yang membuka
	// neraca tidak menyimpulkan laporan ini salah ketika 1-2000 memang memuat
	// saldo awal piutang lama dan tagihan biaya realisasi.
	LedgerTotal domain.Money `json:"ledger_total"`
	LedgerOther domain.Money `json:"ledger_other"`
}

// HouseARReconciliation adalah hasil tie-out GL ↔ sub-ledger piutang rumah.
type HouseARReconciliation struct {
	AsOfDate string              `json:"as_of_date"`
	Layers   []HouseARReconLayer `json:"layers"`

	SubledgerTotal  domain.Money `json:"subledger_total"`
	LedgerHouseTot  domain.Money `json:"ledger_house_total"`
	Difference      domain.Money `json:"difference"`
	Matched         bool         `json:"matched"`
	OwnedJournalCnt int          `json:"owned_journal_count"`
	Note            string       `json:"note,omitempty"`
}

// ReconcileHouseAR membandingkan sub-ledger piutang harga rumah dengan buku
// besar, per akun kontrol (D-W8-6), dengan pola "kupas lapis" yang sama dengan
// W-7:
//
//	saldo akun kontrol per cutoff
//	 − mutasi milik domain penjualan   ← dibandingkan dengan Σ outstanding rumah
//	 = sisa (saldo awal piutang lama, tagihan biaya realisasi, jurnal manual)
//
// Perbandingannya berbasis nominal per akun, bukan satu angka besar: dua
// kesalahan yang kebetulan saling menutup akan lolos dari perbandingan tunggal.
//
// Akun kontrol yang dilihat adalah gabungan akun yang muncul di sub-ledger DAN
// akun piutang yang dikenal alur penjualan — supaya saldo yang tertinggal di
// sebuah akun tanpa satu pun baris sub-ledger (persis pola kesalahan yang harus
// ditangkap) tetap terlihat, bukan hilang karena tak ada yang mencarinya.
func (s *Service) ReconcileHouseAR(ctx context.Context, tenantID uint64, asOf *time.Time) (*HouseARReconciliation, error) {
	if s.houseAR == nil {
		return nil, ErrHouseARNotConfigured
	}
	if s.houseARLedger == nil {
		return nil, ErrHouseARLedgerNotConfigured
	}

	sub, err := s.HouseARSubledger(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	owned, err := s.houseAR.ListOwnedJournalIDs(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("baca jurnal milik penjualan: %w", err)
	}

	subByCode := make(map[string]HouseARLayer, len(sub.Layers))
	codes := make([]string, 0, len(sub.Layers)+2)
	for _, l := range sub.Layers {
		subByCode[l.ControlAccountCode] = l
		codes = append(codes, l.ControlAccountCode)
	}
	for _, c := range s.houseARLedger.ReceivableControlAccounts() {
		if _, seen := subByCode[c]; !seen {
			codes = append(codes, c)
		}
	}

	out := &HouseARReconciliation{
		Layers:          make([]HouseARReconLayer, 0, len(codes)),
		SubledgerTotal:  domain.Zero,
		LedgerHouseTot:  domain.Zero,
		OwnedJournalCnt: len(owned),
	}
	if asOf != nil {
		out.AsOfDate = asOf.Format("2006-01-02")
	}

	for _, code := range codes {
		l := HouseARReconLayer{
			ControlAccountCode: code,
			Subledger:          subByCode[code].Subledger,
			UnitCount:          subByCode[code].UnitCount,
		}
		if l.Subledger.IsZero() {
			l.Subledger = domain.Zero
		}
		total, err := s.houseARLedger.AccountBalance(ctx, tenantID, code, asOf)
		if err != nil {
			return nil, fmt.Errorf("saldo akun kontrol %s: %w", code, err)
		}
		house, err := s.houseARLedger.AccountMovementForJournals(ctx, tenantID, code, owned, asOf)
		if err != nil {
			return nil, fmt.Errorf("mutasi penjualan pada %s: %w", code, err)
		}
		l.LedgerTotal = total
		l.LedgerHouse = house
		l.LedgerOther = total.Sub(house)
		l.Difference = l.Subledger.Sub(house)
		l.Matched = l.Difference.IsZero()

		// Akun yang sepenuhnya kosong di kedua sisi tidak perlu ditampilkan —
		// baris nol hanya membuat layar rekonsiliasi sulit dibaca.
		if l.Subledger.IsZero() && l.LedgerTotal.IsZero() && l.LedgerHouse.IsZero() {
			continue
		}
		out.SubledgerTotal = out.SubledgerTotal.Add(l.Subledger)
		out.LedgerHouseTot = out.LedgerHouseTot.Add(house)
		out.Layers = append(out.Layers, l)
	}

	out.Difference = out.SubledgerTotal.Sub(out.LedgerHouseTot)
	out.Matched = true
	for _, l := range out.Layers {
		if !l.Matched {
			out.Matched = false
			break
		}
	}
	// Total yang nol TIDAK boleh menutupi lapis yang tidak cocok: selisih +X di
	// satu akun dan −X di akun lain berarti piutang duduk di akun yang salah.
	if out.Matched && !out.Difference.IsZero() {
		out.Matched = false
	}
	return out, nil
}
