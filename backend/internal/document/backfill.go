package document

// W-3.3 — backfill dokumen kas historis.
//
// W-3.2 menutup lubangnya ke depan: sejak sekarang tidak ada jurnal kas yang
// bisa terposting tanpa dokumen. Yang tersisa adalah jurnal kas yang SUDAH
// terposting sebelum itu. Selama mereka ada, INV-DOC-1 belum bisa ditegakkan
// fail-closed (W-3.5) — enforcement yang dinyalakan di atas data yang melanggar
// hanya akan menolak laporan, bukan memperbaiki bukunya.
//
// Dua kelas pekerjaan, sengaja dibedakan:
//
//  1. **Tautkan** — jurnalnya sudah punya dokumen di baris bisnis (kwitansi
//     KWT/KWB/KWR), hanya `journal_entries.document_id` yang belum terisi.
//     Menerbitkan dokumen BARU untuk kasus ini adalah pelanggaran langsung
//     ("satu cash movement menghasilkan dua dokumen"), jadi pencarian dokumen
//     bisnis WAJIB dijalankan lebih dulu untuk setiap jurnal.
//  2. **Terbitkan retro** — tidak ada dokumen sama sekali (pengeluaran kas
//     lama, jurnal berulang, pembalik). Dokumen baru terbit dengan jenis yang
//     disimpulkan dari ARAH kas dan `source` jurnal.
//
// Tanggal terbit dokumen retro = waktu backfill dijalankan, BUKAN tanggal
// jurnalnya. Ini mengikuti aturan W-2 yang sudah terkunci (`fiscalYearFor`):
// seri tahun yang sudah ditutup tidak boleh disusupi nomor baru. Tanggal
// transaksi tetap terbaca dari jurnal yang tertaut.
//
// Default DRY-RUN. Backfill idempoten: dijalankan ulang, jurnal yang sudah
// berdokumen tidak masuk pemindaian sama sekali.

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/domain"
)

// SourceOpeningBalance — jurnal saldo awal. DIKECUALIKAN dari INV-DOC-1:
// saldo awal menetapkan POSISI kas, bukan PERGERAKAN kas. Tidak ada uang yang
// berpindah pada tanggal itu, jadi tidak ada bukti kas yang bisa jujur
// diterbitkan; memaksakan BKM justru mengarang penerimaan yang tidak pernah
// terjadi. Pengecualian yang sama harus dipakai enforcement W-3.5.
const SourceOpeningBalance = "opening_balance"

// BusinessDocumentResolver memetakan sebuah jurnal ke dokumen yang sudah terbit
// dari baris bisnisnya (mis. kwitansi). Mengembalikan 0 bila tidak ada.
//
// Disuntikkan, tidak di-hardcode: package ini tidak boleh tahu tabel modul lain
// (`receipts`, `termin_payments`). Implementasinya tinggal di modul yang memang
// memiliki tabel tersebut.
type BusinessDocumentResolver interface {
	DocumentForJournal(tx *gorm.DB, tenantID, journalID uint64) (uint64, error)
}

// BackfillFlag menandai satu jurnal yang TIDAK bisa diputuskan otomatis.
// Tidak pernah ditebak: jurnal bertanda dilaporkan untuk penanganan manual.
type BackfillFlag struct {
	JournalID uint64 `json:"journal_id"`
	Date      string `json:"date"`
	Source    string `json:"source"`
	Reason    string `json:"reason"`
}

// BackfillReport meringkas satu tenant.
type BackfillReport struct {
	TenantID uint64 `json:"tenant_id"`
	Apply    bool   `json:"apply"`
	Scanned  int    `json:"scanned"`
	Exempt   int    `json:"exempt"`
	Linked   int    `json:"linked"`
	// Issued dipetakan per kode jenis dokumen — ringkasan yang bisa dicocokkan
	// auditor dengan tabel A/B inventaris jalur kas.
	Issued  map[string]int `json:"issued"`
	Flagged int            `json:"flagged"`
	Flags   []BackfillFlag `json:"flags,omitempty"`
}

func (r *BackfillReport) IssuedTotal() int {
	var n int
	for _, v := range r.Issued {
		n += v
	}
	return n
}

// cashJournalRow adalah satu jurnal kas terposting yang belum berdokumen,
// beserta agregat baris kas/bank-nya.
type cashJournalRow struct {
	ID          uint64    `gorm:"column:id"`
	Date        time.Time `gorm:"column:date"`
	Source      string    `gorm:"column:source"`
	CreatedBy   *uint64   `gorm:"column:created_by"`
	IsReversing bool      `gorm:"column:is_reversing"`
	ReversesID  *uint64   `gorm:"column:reverses_id"`
	Debit       string    `gorm:"column:cash_debit"`
	Credit      string    `gorm:"column:cash_credit"`
	CashLines   int       `gorm:"column:cash_lines"`
}

// scanCashJournals mengambil jurnal kas terposting yang belum berdokumen.
//
// Diurut menurut `id` — urutan PEMBUATAN, bukan tanggal transaksi. Dua alasan:
// nomor retro jadi terbit searah dengan lahirnya jurnal, dan jurnal pembalik
// dijamin diproses SESUDAH jurnal aslinya sehingga dokumen JR selalu punya
// dokumen asal untuk ditunjuk.
func scanCashJournals(ctx context.Context, db *gorm.DB, tenantID uint64) ([]cashJournalRow, error) {
	var rows []cashJournalRow
	err := db.WithContext(ctx).Raw(`
		SELECT je.id, je.date, je.source, je.created_by, je.is_reversing, je.reverses_id,
		       SUM(jl.debit)  AS cash_debit,
		       SUM(jl.credit) AS cash_credit,
		       COUNT(*)       AS cash_lines
		  FROM journal_entries je
		  JOIN journal_lines   jl ON jl.journal_entry_id = je.id
		  JOIN accounts        a  ON a.id = jl.account_id AND a.tenant_id = je.tenant_id
		 WHERE je.tenant_id = ?
		   AND je.posted_at IS NOT NULL
		   AND je.document_id IS NULL
		   AND a.category IN ('cash','bank')
		 GROUP BY je.id, je.date, je.source, je.created_by, je.is_reversing, je.reverses_id
		 ORDER BY je.id ASC`, tenantID).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("pindai jurnal kas tenant %d: %w", tenantID, err)
	}
	return rows, nil
}

// classifyCashJournal (MURNI) menyimpulkan jenis dokumen dan nominalnya dari
// arah kas dan asal jurnal.
//
// Untuk data lama, ARAH adalah satu-satunya fakta yang selalu tersedia —
// `source` sering hanya "system" karena dulu tidak diisi modul pemanggil.
// Karena itu arah yang menentukan, dan `source` hanya mempertajam sisi keluar
// ke jenis yang kepemilikan ekonomis dananya bukan uang perusahaan.
func classifyCashJournal(r cashJournalRow) (typeCode string, amount domain.Money, err error) {
	dr, err := domain.NewMoney(r.Debit)
	if err != nil {
		return "", domain.Zero, fmt.Errorf("debit kas jurnal %d tidak terbaca: %w", r.ID, err)
	}
	cr, err := domain.NewMoney(r.Credit)
	if err != nil {
		return "", domain.Zero, fmt.Errorf("kredit kas jurnal %d tidak terbaca: %w", r.ID, err)
	}
	net := dr.Sub(cr)

	switch {
	case net.IsZero():
		// Kas masuk = kas keluar. Perpindahan antar kas/bank (setor tunai ke
		// bank): posisi kas perusahaan tidak berubah, jadi ini memo transfer
		// internal — bukan bukti kas masuk maupun keluar.
		if r.CashLines < 2 || dr.IsZero() {
			return "", domain.Zero, fmt.Errorf("jurnal kas bernilai nol / hanya satu sisi — tidak bisa disimpulkan")
		}
		amount = dr
	case net.IsNeg():
		amount = net.Neg()
	default:
		amount = net
	}

	// Pembalik: jenisnya JR apa pun arahnya — yang dibuktikan bukan pergerakan
	// kas baru, melainkan pembatalan pergerakan sebelumnya (D-W3-5).
	if r.IsReversing {
		return TypeJournalReversal, amount, nil
	}

	switch {
	case net.IsZero():
		return TypeInternalTransfer, amount, nil
	case net.IsNeg():
		return cashOutTypeFor(r.Source), amount, nil
	default:
		return TypeCashIn, amount, nil
	}
}

// cashOutTypeFor memilih seri kas keluar menurut KEPEMILIKAN EKONOMIS dana —
// prinsip yang sama dengan D-W3-6 pada jalur yang hidup.
func cashOutTypeFor(source string) string {
	switch source {
	case "cancellation":
		// Pembatalan mengembalikan uang CUSTOMER.
		return TypeCustomerRefund
	case "notary", "charge":
		// Titipan pihak ketiga yang direalisasikan (notaris, PDAM, listrik).
		return TypeThirdPartyPayout
	default:
		return TypeCashOut
	}
}

// BackfillCashDocuments menjalankan backfill satu tenant.
//
// `apply=false` (default pemanggil) hanya memindai dan melaporkan rencana —
// tidak ada nomor yang terpakai, tidak ada baris yang berubah.
//
// Satu transaksi per jurnal, bukan satu transaksi untuk seluruh tenant:
// atomisitas yang dijamin INV-DOC-1 adalah "dokumen + tautan lahir bersama",
// dan itu berlaku per pergerakan kas. Backfill yang berhenti di tengah
// meninggalkan sebagian pekerjaan yang sudah benar, lalu tinggal dijalankan
// ulang untuk sisanya.
func BackfillCashDocuments(ctx context.Context, db *gorm.DB, tenantID uint64, apply bool,
	resolver BusinessDocumentResolver) (*BackfillReport, error) {
	if resolver == nil {
		// Tanpa resolver, jurnal yang kwitansinya sudah ada akan dianggap tidak
		// berdokumen dan menerima dokumen retro KEDUA. Fail-closed.
		return nil, fmt.Errorf("backfill butuh BusinessDocumentResolver: tanpa itu jurnal berkwitansi akan berdokumen ganda")
	}
	rep := &BackfillReport{TenantID: tenantID, Apply: apply, Issued: map[string]int{}}

	rows, err := scanCashJournals(ctx, db, tenantID)
	if err != nil {
		return nil, err
	}
	rep.Scanned = len(rows)

	for _, row := range rows {
		if row.Source == SourceOpeningBalance {
			rep.Exempt++
			continue
		}

		docID, err := resolver.DocumentForJournal(db.WithContext(ctx), tenantID, row.ID)
		if err != nil {
			rep.flag(row, "cari dokumen bisnis: "+err.Error())
			continue
		}
		if docID != 0 {
			if apply {
				if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
					return LinkJournal(tx, tenantID, docID, row.ID)
				}); err != nil {
					rep.flag(row, "tautkan dokumen bisnis: "+err.Error())
					continue
				}
			}
			rep.Linked++
			continue
		}

		typeCode, amount, err := classifyCashJournal(row)
		if err != nil {
			rep.flag(row, err.Error())
			continue
		}
		if !apply {
			rep.Issued[typeCode]++
			continue
		}
		if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return issueRetroDocument(tx, tenantID, row, typeCode, amount)
		}); err != nil {
			rep.flag(row, "terbitkan dokumen retro: "+err.Error())
			continue
		}
		rep.Issued[typeCode]++
	}
	return rep, nil
}

// issueRetroDocument menerbitkan satu dokumen retro + tautannya dalam satu
// transaksi. Untuk jurnal pembalik, dokumen asal ditunjuk bila jurnal aslinya
// sudah berdokumen — kalau belum (mis. aslinya jurnal non-kas), JR tetap terbit
// tanpa tautan balik, sesuai kontrak IssueReversal.
func issueRetroDocument(tx *gorm.DB, tenantID uint64, row cashJournalRow, typeCode string, amount domain.Money) error {
	issuedAt := time.Now()
	if typeCode != TypeJournalReversal {
		_, err := IssueForJournal(tx, tenantID, typeCode, issuedAt, row.ID, amount, row.CreatedBy)
		return err
	}

	var original uint64
	if row.ReversesID != nil {
		doc, err := FindByJournal(tx, tenantID, *row.ReversesID)
		if err != nil {
			return err
		}
		if doc != nil {
			original = doc.ID
		}
	}
	_, err := IssueReversal(tx, tenantID, issuedAt, row.ID, original, amount, row.CreatedBy)
	return err
}

func (r *BackfillReport) flag(row cashJournalRow, reason string) {
	r.Flagged++
	r.Flags = append(r.Flags, BackfillFlag{
		JournalID: row.ID,
		Date:      row.Date.Format("2006-01-02"),
		Source:    row.Source,
		Reason:    reason,
	})
}
