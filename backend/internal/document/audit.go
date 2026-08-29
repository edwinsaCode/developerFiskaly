package document

// W-3.4 — audit mode INV-DOC-1.
//
// Acceptance criterion owner (FINAL 2026-08-07) menyebut LIMA larangan. W-3.5
// akan menegakkannya fail-closed saat posting; berkas ini menegakkannya ke
// BELAKANG — terhadap data yang sudah ada, kapan pun, tanpa mengubah apa pun.
//
// Dua penjaga itu tidak saling menggantikan. Penjaga posting hanya melihat satu
// transaksi pada satu waktu dan tidak akan pernah menemukan kerusakan yang
// masuk lewat jalur lain: migrasi tangan, perbaikan data manual, index unique
// yang tanpa sengaja hilang saat rilis. Audit inilah yang melihat SELURUH buku
// sekaligus, dan karena itu ia dijalankan ulang, bukan sekali saat backfill.
//
// Read-only, selalu. Audit yang bisa memperbaiki temuannya sendiri berhenti
// menjadi bukti independen.

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// Kode temuan — stabil, dipakai frontend dan laporan.
const (
	AuditCashWithoutDocument  = "cash_journal_without_document"
	AuditDocumentWithoutTx    = "document_without_transaction"
	AuditDoubleDocument       = "cash_movement_two_documents"
	AuditSharedDocument       = "document_two_cash_movements"
	AuditSequenceBehind       = "sequence_behind"
	defaultAuditFindingsLimit = 100
)

// AuditFinding adalah satu baris temuan. Field yang tidak relevan untuk sebuah
// kode temuan dibiarkan kosong — dipakai apa adanya oleh tabel di frontend.
type AuditFinding struct {
	JournalID  uint64 `json:"journal_id,omitempty"`
	DocumentID uint64 `json:"document_id,omitempty"`
	Number     string `json:"number,omitempty"`
	Date       string `json:"date,omitempty"`
	Source     string `json:"source,omitempty"`
	Amount     string `json:"amount,omitempty"`
	Detail     string `json:"detail"`
}

// AuditCheck adalah satu larangan beserta temuannya.
//
// `Count` adalah jumlah SEBENARNYA; `Findings` dipotong pada limit supaya
// tenant yang rusak parah tidak mengembalikan puluhan ribu baris. Memotong
// tanpa memberi tahu jumlah aslinya akan membuat laporan terbaca lebih sehat
// daripada kenyataannya.
type AuditCheck struct {
	Code      string         `json:"code"`
	Title     string         `json:"title"`
	Count     int            `json:"count"`
	Truncated bool           `json:"truncated"`
	Findings  []AuditFinding `json:"findings,omitempty"`
}

// AuditReport meringkas kondisi INV-DOC-1 satu tenant.
type AuditReport struct {
	TenantID   uint64       `json:"tenant_id"`
	CheckedAt  time.Time    `json:"checked_at"`
	CashPosted int          `json:"cash_journals_posted"`
	Documented int          `json:"cash_journals_documented"`
	Exempt     int          `json:"cash_journals_exempt"`
	Violations int          `json:"violations"`
	Clean      bool         `json:"clean"`
	Checks     []AuditCheck `json:"checks"`
}

// AuditCashDocuments menjalankan seluruh pemeriksaan INV-DOC-1 untuk satu
// tenant. `limit` membatasi jumlah baris temuan per pemeriksaan (0 = default).
func AuditCashDocuments(ctx context.Context, db *gorm.DB, tenantID uint64, limit int) (*AuditReport, error) {
	if limit <= 0 {
		limit = defaultAuditFindingsLimit
	}
	tx := db.WithContext(ctx)
	rep := &AuditReport{TenantID: tenantID, CheckedAt: time.Now()}

	if err := tx.Raw(`
		SELECT COUNT(*) FROM (
			SELECT je.id FROM journal_entries je
			  JOIN journal_lines jl ON jl.journal_entry_id = je.id
			  JOIN accounts      a  ON a.id = jl.account_id AND a.tenant_id = je.tenant_id
			 WHERE je.tenant_id = ? AND je.posted_at IS NOT NULL AND a.category IN ('cash','bank')
			 GROUP BY je.id) x`, tenantID).Scan(&rep.CashPosted).Error; err != nil {
		return nil, fmt.Errorf("hitung jurnal kas: %w", err)
	}
	if err := tx.Raw(`
		SELECT COUNT(*) FROM (
			SELECT je.id FROM journal_entries je
			  JOIN journal_lines jl ON jl.journal_entry_id = je.id
			  JOIN accounts      a  ON a.id = jl.account_id AND a.tenant_id = je.tenant_id
			 WHERE je.tenant_id = ? AND je.posted_at IS NOT NULL AND je.document_id IS NOT NULL
			   AND a.category IN ('cash','bank')
			 GROUP BY je.id) x`, tenantID).Scan(&rep.Documented).Error; err != nil {
		return nil, fmt.Errorf("hitung jurnal kas berdokumen: %w", err)
	}
	if err := tx.Raw(`
		SELECT COUNT(*) FROM (
			SELECT je.id FROM journal_entries je
			  JOIN journal_lines jl ON jl.journal_entry_id = je.id
			  JOIN accounts      a  ON a.id = jl.account_id AND a.tenant_id = je.tenant_id
			 WHERE je.tenant_id = ? AND je.posted_at IS NOT NULL AND je.source = ?
			   AND a.category IN ('cash','bank')
			 GROUP BY je.id) x`, tenantID, SourceOpeningBalance).Scan(&rep.Exempt).Error; err != nil {
		return nil, fmt.Errorf("hitung jurnal saldo awal: %w", err)
	}

	for _, run := range []func(*gorm.DB, uint64, int) (AuditCheck, error){
		auditCashWithoutDocument,
		auditDocumentWithoutTx,
		auditDoubleDocument,
		auditSharedDocument,
		auditSequenceBehind,
	} {
		check, err := run(tx, tenantID, limit)
		if err != nil {
			return nil, err
		}
		rep.Checks = append(rep.Checks, check)
		rep.Violations += check.Count
	}
	rep.Clean = rep.Violations == 0
	return rep, nil
}

// ── L-1: jurnal kas terposting tanpa dokumen ────────────────────────────────

func auditCashWithoutDocument(tx *gorm.DB, tenantID uint64, limit int) (AuditCheck, error) {
	c := AuditCheck{Code: AuditCashWithoutDocument, Title: "Jurnal kas terposting tanpa dokumen"}
	var rows []struct {
		ID     uint64    `gorm:"column:id"`
		Date   time.Time `gorm:"column:date"`
		Source string    `gorm:"column:source"`
		Desc   string    `gorm:"column:description"`
		Net    string    `gorm:"column:net"`
	}
	// Saldo awal dikecualikan — posisi kas, bukan pergerakan kas (lihat W-3.3 §F.1).
	err := tx.Raw(`
		SELECT je.id, je.date, je.source, je.description,
		       SUM(jl.debit) - SUM(jl.credit) AS net
		  FROM journal_entries je
		  JOIN journal_lines   jl ON jl.journal_entry_id = je.id
		  JOIN accounts        a  ON a.id = jl.account_id AND a.tenant_id = je.tenant_id
		 WHERE je.tenant_id = ? AND je.posted_at IS NOT NULL AND je.document_id IS NULL
		   AND je.source <> ? AND a.category IN ('cash','bank')
		 GROUP BY je.id, je.date, je.source, je.description
		 ORDER BY je.id ASC`, tenantID, SourceOpeningBalance).Scan(&rows).Error
	if err != nil {
		return c, fmt.Errorf("audit %s: %w", c.Code, err)
	}
	c.Count = len(rows)
	for i, r := range rows {
		if i >= limit {
			c.Truncated = true
			break
		}
		c.Findings = append(c.Findings, AuditFinding{
			JournalID: r.ID,
			Date:      r.Date.Format("2006-01-02"),
			Source:    r.Source,
			Amount:    r.Net,
			Detail:    "tidak ada dokumen bernomor untuk pergerakan kas ini: " + r.Desc,
		})
	}
	return c, nil
}

// ── L-2: dokumen bernomor tanpa transaksi yang berhasil ─────────────────────

func auditDocumentWithoutTx(tx *gorm.DB, tenantID uint64, limit int) (AuditCheck, error) {
	c := AuditCheck{Code: AuditDocumentWithoutTx, Title: "Dokumen kas bernomor tanpa transaksi"}
	var rows []struct {
		ID     uint64 `gorm:"column:id"`
		Number string `gorm:"column:number"`
		Code   string `gorm:"column:document_type_code"`
		Reason string `gorm:"column:reason"`
	}
	// Satu query, satu baris per dokumen. Kerusakannya bisa berlapis (dokumen
	// yatim YANG JUGA menunjuk jurnal hantu); melaporkannya dua kali membuat
	// angka pelanggaran terbaca lebih besar dari jumlah dokumen yang rusak,
	// dan angka yang dilebih-lebihkan sama menyesatkannya dengan yang dikurangi.
	//
	// `holder` adalah jurnal yang memegang dokumen ini — 1:1 karena uk_je_document.
	// `src` adalah jurnal asal, hanya untuk dokumen yang lahir dari jurnal.
	err := tx.Raw(`
		SELECT d.id, d.number, d.document_type_code,
		       CASE
		         WHEN holder.id IS NOT NULL
		           THEN 'dipegang jurnal yang belum terposting'
		         WHEN d.source_table = ? AND src.id IS NULL
		           THEN 'menunjuk jurnal yang tidak ada'
		         WHEN d.source_table = ? AND src.posted_at IS NULL
		           THEN 'menunjuk jurnal yang belum terposting'
		         ELSE 'nomor terbit tetapi tidak ada jurnal kas yang memegangnya'
		       END AS reason
		  FROM documents d
		  LEFT JOIN journal_entries holder
		         ON holder.tenant_id = d.tenant_id AND holder.document_id = d.id
		  LEFT JOIN journal_entries src
		         ON src.tenant_id = d.tenant_id AND src.id = d.source_id
		        AND d.source_table = ?
		 WHERE d.tenant_id = ? AND d.document_type_code IN (?)
		   AND (holder.id IS NULL OR holder.posted_at IS NULL)
		 ORDER BY d.id ASC`,
		SourceJournal, SourceJournal, SourceJournal, tenantID, cashDocumentTypes()).Scan(&rows).Error
	if err != nil {
		return c, fmt.Errorf("audit %s: %w", c.Code, err)
	}
	c.Count = len(rows)
	for i, r := range rows {
		if i >= limit {
			c.Truncated = true
			break
		}
		c.Findings = append(c.Findings, AuditFinding{
			DocumentID: r.ID, Number: r.Number, Source: r.Code, Detail: r.Reason,
		})
	}
	return c, nil
}

// ── L-3: satu pergerakan kas menghasilkan dua dokumen ───────────────────────

func auditDoubleDocument(tx *gorm.DB, tenantID uint64, limit int) (AuditCheck, error) {
	c := AuditCheck{Code: AuditDoubleDocument, Title: "Satu pergerakan kas, dua dokumen"}
	var rows []struct {
		JournalID uint64 `gorm:"column:journal_id"`
		Held      string `gorm:"column:held"`
		Extra     string `gorm:"column:extra"`
	}
	// Bentuk yang lolos dari kedua unique index: jurnal memegang dokumen baris
	// bisnis (kwitansi), TAPI ada juga dokumen yang lahir dari jurnal itu
	// sendiri. Masing-masing sah menurut indexnya; berdua mereka melanggar.
	err := tx.Raw(`
		SELECT je.id AS journal_id, held.number AS held, extra.number AS extra
		  FROM journal_entries je
		  JOIN documents held  ON held.id = je.document_id AND held.tenant_id = je.tenant_id
		  JOIN documents extra ON extra.tenant_id = je.tenant_id
		                      AND extra.source_table = ? AND extra.source_id = je.id
		                      AND extra.id <> held.id
		 WHERE je.tenant_id = ?
		 ORDER BY je.id ASC`, SourceJournal, tenantID).Scan(&rows).Error
	if err != nil {
		return c, fmt.Errorf("audit %s: %w", c.Code, err)
	}
	c.Count = len(rows)
	for i, r := range rows {
		if i >= limit {
			c.Truncated = true
			break
		}
		c.Findings = append(c.Findings, AuditFinding{
			JournalID: r.JournalID, Number: r.Held,
			Detail: "jurnal memegang " + r.Held + " tetapi " + r.Extra + " juga terbit dari jurnal yang sama",
		})
	}
	return c, nil
}

// ── L-4: satu dokumen dipakai dua pergerakan kas ────────────────────────────

func auditSharedDocument(tx *gorm.DB, tenantID uint64, limit int) (AuditCheck, error) {
	c := AuditCheck{Code: AuditSharedDocument, Title: "Satu dokumen dipakai dua pergerakan kas"}
	var rows []struct {
		DocumentID uint64 `gorm:"column:document_id"`
		Number     string `gorm:"column:number"`
		N          int    `gorm:"column:n"`
	}
	// Dijaga uk_je_document di level schema. Tetap diperiksa: index bisa hilang
	// pada rilis yang salah, dan audit yang hanya memeriksa apa yang sudah
	// dijamin index tidak pernah menemukan apa pun.
	err := tx.Raw(`
		SELECT je.document_id, MAX(d.number) AS number, COUNT(*) AS n
		  FROM journal_entries je
		  LEFT JOIN documents d ON d.id = je.document_id AND d.tenant_id = je.tenant_id
		 WHERE je.tenant_id = ? AND je.document_id IS NOT NULL
		 GROUP BY je.document_id HAVING COUNT(*) > 1
		 ORDER BY je.document_id ASC`, tenantID).Scan(&rows).Error
	if err != nil {
		return c, fmt.Errorf("audit %s: %w", c.Code, err)
	}
	c.Count = len(rows)
	for i, r := range rows {
		if i >= limit {
			c.Truncated = true
			break
		}
		c.Findings = append(c.Findings, AuditFinding{
			DocumentID: r.DocumentID, Number: r.Number,
			Detail: fmt.Sprintf("dipakai %d jurnal", r.N),
		})
	}
	return c, nil
}

// ── L-5: seri tertinggal di belakang dokumen yang sudah terbit ─────────────

func auditSequenceBehind(tx *gorm.DB, tenantID uint64, limit int) (AuditCheck, error) {
	c := AuditCheck{Code: AuditSequenceBehind, Title: "Counter seri tertinggal di belakang dokumen terbit"}
	var rows []struct {
		Code    string `gorm:"column:document_type_code"`
		Year    uint16 `gorm:"column:fiscal_year"`
		LastVal uint64 `gorm:"column:last_val"`
		MaxNo   uint64 `gorm:"column:max_no"`
	}
	// Yang diperiksa BUKAN lubang di seri.
	//
	// Lubang bukan pelanggaran, dan memperlakukannya sebagai pelanggaran akan
	// menghasilkan alarm palsu pada setiap tenant yang punya data pra-W-2:
	// penomoran lama memakai SATU counter bersama untuk KWT dan KWB, sehingga
	// masing-masing seri memang terbaca berlubang setelah diregistrasi ke
	// Document Engine dengan nomor aslinya dipertahankan. Nomor yang dialokasikan
	// transaksi gagal juga tidak meninggalkan lubang — `last_val` ikut rollback
	// karena Allocate wajib berjalan di transaksi pemanggil. Larangan "nomor
	// terbakar oleh transaksi yang gagal" karena itu ditegakkan oleh L-2
	// (dokumen bernomor tanpa transaksi), bukan dengan menghitung lubang.
	//
	// Yang berbahaya justru arah sebaliknya: counter TERTINGGAL di belakang
	// nomor tertinggi yang sudah terbit. Itu berarti penerbitan berikutnya
	// mengarah ke nomor yang sudah dipakai — satu nomor untuk dua bukti kas.
	// Allocate menyembuhkannya sendiri lewat pembacaan lantai seri, tetapi
	// kondisinya tetap harus terlihat: ia hanya bisa muncul dari penulisan
	// langsung ke database.
	err := tx.Raw(`
		SELECT s.document_type_code, s.fiscal_year, s.last_val,
		       COALESCE((SELECT MAX(d.sequence_no) FROM documents d
		                  WHERE d.tenant_id = s.tenant_id
		                    AND d.document_type_code = s.document_type_code
		                    AND d.fiscal_year = s.fiscal_year), 0) AS max_no
		  FROM document_sequences s
		 WHERE s.tenant_id = ?
		 ORDER BY s.document_type_code, s.fiscal_year`, tenantID).Scan(&rows).Error
	if err != nil {
		return c, fmt.Errorf("audit %s: %w", c.Code, err)
	}
	for _, r := range rows {
		if r.LastVal >= r.MaxNo {
			continue
		}
		c.Count++
		if len(c.Findings) >= limit {
			c.Truncated = true
			continue
		}
		c.Findings = append(c.Findings, AuditFinding{
			Source: r.Code,
			Detail: fmt.Sprintf("seri %s tahun %d: counter di %d padahal nomor tertinggi yang terbit %d",
				r.Code, r.Year, r.LastVal, r.MaxNo),
		})
	}
	return c, nil
}

// cashDocumentTypes: jenis dokumen yang WAJIB memegang jurnal kas. MTI dan INV
// tidak termasuk — memo transfer internal dan tagihan memang bukan bukti
// pergerakan kas masuk/keluar perusahaan.
func cashDocumentTypes() []string {
	return []string{
		TypeHousePayment, TypeBooking, TypeRealization, TypeKPRDisbursement,
		TypeCashIn, TypeCashOut, TypeThirdPartyPayout, TypeCustomerRefund,
		TypeJournalReversal,
	}
}

// Audit menjalankan pemeriksaan INV-DOC-1 lewat Service (dipakai handler).
func (s *Service) Audit(ctx context.Context, tenantID uint64, limit int) (*AuditReport, error) {
	return AuditCashDocuments(ctx, s.db, tenantID, limit)
}
