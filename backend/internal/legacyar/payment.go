package legacyar

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/billing"
	"esaproperti/internal/document"
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

// ── Pelunasan ─────────────────────────────────────────────────────────────────

// PaymentRequest adalah satu penerimaan kas atas piutang lama.
//
// Perhatikan yang TIDAK ada di sini: unit, proyek, kontrak, jadwal. Piutang
// lama tidak punya satu pun dari itu, dan jalur pembayaran yang ada
// (sale.ReceivePayment) mensyaratkan semuanya. Memaksakan customer legacy
// memiliki unit dan kontrak palsu hanya demi lewat jalur itu akan mencemari
// data penjualan dengan entitas yang tidak pernah ada.
type PaymentRequest struct {
	Allocations []PaymentAllocation
	// CashAccountCode adalah akun kas/bank penerima. Wajib — sistem tidak
	// menebak ke mana uang masuk.
	CashAccountCode string
	PaymentDate     time.Time
	Notes           string
	IdempotencyKey  string
	ActorID         *uint64
}

// PaymentAllocation menunjuk piutang mana yang dilunasi dan berapa.
//
// Alokasi selalu EKSPLISIT. Satu setoran yang menutup tiga piutang menghasilkan
// tiga baris di sini — bukan "kurangi saldo customer sebesar X" yang membuat
// tidak ada yang tahu tagihan mana yang sebenarnya lunas.
type PaymentAllocation struct {
	ReceivableID uint64
	Amount       domain.Money
}

// PaymentResult adalah hasil pelunasan.
type PaymentResult struct {
	JournalEntryID uint64       `json:"journal_entry_id"`
	DocumentID     *uint64      `json:"document_id,omitempty"`
	DocumentNumber string       `json:"document_number,omitempty"`
	TotalAmount    domain.Money `json:"total_amount"`
	Payments       []Payment    `json:"payments"`
	Duplicate      bool         `json:"duplicate,omitempty"`
}

// ReceivePayment mencatat penerimaan kas atas satu atau beberapa piutang lama.
//
// Satu jurnal, satu dokumen (BKM), beberapa baris alokasi:
//
//	Dr Kas/Bank              total
//	    Cr Piutang Customer      total   (per akun kontrol tiap piutang)
//
// Dokumennya BKM lewat Document Domain yang sudah ada — bukan jenis dokumen
// baru. Penerimaan kas dari piutang lama adalah penerimaan kas biasa; membuat
// jenis dokumen tersendiri hanya akan menambah seri nomor yang harus dirawat
// tanpa menjawab pertanyaan yang belum terjawab.
func (s *Service) ReceivePayment(ctx context.Context, tenantID uint64, req PaymentRequest) (*PaymentResult, error) {
	if len(req.Allocations) == 0 {
		return nil, fmt.Errorf("tidak ada piutang yang dipilih")
	}
	if strings.TrimSpace(req.CashAccountCode) == "" {
		return nil, ErrCashAccount
	}
	date := req.PaymentDate
	if date.IsZero() {
		date = time.Now()
	}

	// Idempotensi diperiksa lebih dulu di luar transaksi supaya klik ganda
	// mengembalikan hasil yang sama, bukan 500 dari unique key.
	if req.IdempotencyKey != "" {
		if prev, err := s.repo.FindPaymentByIdempotencyKey(ctx, tenantID, req.IdempotencyKey); err != nil {
			return nil, err
		} else if prev != nil {
			return s.resultOfJournal(ctx, tenantID, prev.JournalEntryID, true)
		}
	}

	var out PaymentResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		r := s.repo.WithTx(tx)

		cash, err := accountByCode(ctx, tx, tenantID, req.CashAccountCode)
		if err != nil {
			return err
		}
		// Akun penerima harus benar-benar kas/bank menurut kategori COA (S3) —
		// satu-satunya definisi "akun kas" di repo ini.
		if !ledger.AccountInRole(cash, ledger.RoleCashBank) {
			return fmt.Errorf("%w: %s (%s)", ErrCashAccount, cash.Code, cash.Name)
		}

		total := domain.Zero
		lines := []ledger.LineInput{}
		type pending struct {
			rec    *Receivable
			amount domain.Money
		}
		var items []pending
		// Akumulasi per akun kontrol: dua piutang di akun kontrol yang sama
		// menghasilkan SATU baris kredit, bukan dua baris kembar.
		creditByAccount := map[string]domain.Money{}
		var creditOrder []string

		for _, a := range req.Allocations {
			if !a.Amount.GreaterThan(domain.Zero) {
				return ErrAmountNotPositive
			}
			// Kunci baris piutang. Tanpa ini dua pembayaran serentak bisa
			// sama-sama membaca sisa yang sama dan keduanya lolos pemeriksaan
			// lebih bayar — keduanya benar sendiri-sendiri, salah bersama.
			rec, err := r.FindReceivable(ctx, tenantID, a.ReceivableID, true)
			if err != nil {
				return err
			}
			switch rec.Status {
			case StatusPaid:
				return fmt.Errorf("%w: %s", ErrAlreadySettled, rec.CustomerName)
			case StatusWrittenOff:
				return fmt.Errorf("%w: %s", ErrWrittenOff, rec.CustomerName)
			}

			// Sisa dihitung dari SUB-LEDGER, bukan dari kolom cache. Kalau
			// keduanya pernah menyimpang, yang benar adalah baris pelunasannya.
			paid, err := r.SumPayments(ctx, tenantID, rec.ID)
			if err != nil {
				return err
			}
			outstanding := rec.OriginalAmount.Sub(paid)
			if a.Amount.GreaterThan(outstanding) {
				// Lebih bayar DITOLAK. Piutang lama adalah sisa tagihan yang
				// nyata; kelebihannya bukan uang muka atas apa pun, dan
				// menyimpannya sebagai saldo kredit berarti mengarang kewajiban
				// yang belum diputuskan perlakuannya.
				return fmt.Errorf("%w: %s sisa %s, dibayar %s",
					ErrOverpayment, rec.CustomerName, outstanding.String(), a.Amount.String())
			}

			total = total.Add(a.Amount)
			if _, seen := creditByAccount[rec.ControlAccountCode]; !seen {
				creditOrder = append(creditOrder, rec.ControlAccountCode)
				creditByAccount[rec.ControlAccountCode] = domain.Zero
			}
			creditByAccount[rec.ControlAccountCode] = creditByAccount[rec.ControlAccountCode].Add(a.Amount)
			items = append(items, pending{rec: rec, amount: a.Amount})
		}

		desc := paymentDescription(items[0].rec.CustomerName, len(items), req.Notes)

		lines = append(lines, ledger.LineInput{
			AccountID:   cash.ID,
			Debit:       total,
			Description: "Penerimaan piutang proyek lama",
		})
		for _, code := range creditOrder {
			ctrl, err := accountByCode(ctx, tx, tenantID, code)
			if err != nil {
				return err
			}
			lines = append(lines, ledger.LineInput{
				AccountID:   ctrl.ID,
				Credit:      creditByAccount[code],
				Description: "Pelunasan piutang proyek lama",
			})
		}

		txLedger := ledger.NewGORMRepository(tx)
		posting := ledger.NewPostingService(txLedger, txLedger).WithPeriodChecker(txLedger)
		entry, err := posting.CreateAndPost(ctx, ledger.CreateJournalRequest{
			TenantID:    tenantID,
			Date:        date,
			Description: desc,
			Source:      SourceLegacyPayment,
			CreatedBy:   req.ActorID,
			Lines:       lines,
			Document:    ledger.DocumentSpec{TypeCode: ledger.DocCashIn},
		})
		if err != nil {
			return err
		}

		doc, err := document.FindByJournal(tx.WithContext(ctx), tenantID, entry.ID)
		if err != nil {
			return err
		}
		var docID *uint64
		var docNo string
		if doc != nil {
			id := doc.ID
			docID, docNo = &id, doc.Number
		}

		for i, it := range items {
			p := &Payment{
				TenantID:           tenantID,
				LegacyReceivableID: it.rec.ID,
				Amount:             it.amount,
				PaymentDate:        truncDate(date),
				CashAccountCode:    cash.Code,
				ControlAccountCode: it.rec.ControlAccountCode,
				JournalEntryID:     entry.ID,
				DocumentID:         docID,
				DocumentNumber:     docNo,
				Notes:              trunc(strings.TrimSpace(req.Notes), 500),
				CreatedBy:          req.ActorID,
			}
			// Kunci idempotensi menempel pada SATU baris saja (unique key per
			// tenant). Barisnya cukup untuk menemukan jurnalnya, dan dari jurnal
			// itu seluruh alokasi lain ikut terbawa.
			if i == 0 && req.IdempotencyKey != "" {
				key := req.IdempotencyKey
				p.IdempotencyKey = &key
			}
			if err := r.CreatePayment(ctx, p); err != nil {
				if isDuplicateKey(err) {
					return fmt.Errorf("pembayaran dengan kunci idempotensi yang sama sedang diproses")
				}
				return err
			}

			// Kwitansi (requirement #4/#5): SATU kwitansi per baris pembayaran,
			// lewat mesin kwitansi yang sudah ada (billing) — bukan mesin kedua.
			// Dibuat DI DALAM transaksi yang sama dengan jurnal & sub-ledger di
			// atas, jadi kwitansi tidak pernah lahir tanpa pembayarannya (atau
			// sebaliknya). "Terutang" di kwitansi selalu dihitung ulang saat
			// dicetak (billing.LoadReceiptPrintData), bukan dibekukan di sini.
			if s.receipts != nil {
				var actor uint64
				if req.ActorID != nil {
					actor = *req.ActorID
				}
				receipt, rerr := s.receipts.CreateLegacyReceiptInTx(ctx, tx, tenantID, actor, billing.LegacyReceiptInput{
					LegacyReceivablePaymentID: p.ID,
					Amount:                    it.amount,
					BankAccountCode:           cash.Code,
					Date:                      date,
					Notes:                     trunc(strings.TrimSpace(req.Notes), 500),
				})
				if rerr != nil {
					return rerr
				}
				// Snapshot balik ke baris pembayaran (pola DocumentID/DocumentNumber
				// di atas) supaya halaman detail bisa menawarkan cetak kwitansi
				// langsung dari riwayat pembayaran, tanpa join dan tanpa pindah ke
				// Buku Dokumen.
				if receipt != nil {
					if err := r.SavePaymentReceipt(ctx, tenantID, p.ID, receipt.ID, receipt.ReceiptNumber); err != nil {
						return err
					}
					p.ReceiptID = &receipt.ID
					p.ReceiptNumber = receipt.ReceiptNumber
				}
			}

			// Guard #1 (pola FE-2): kolom cache TIDAK PERNAH ditulis tanpa baris
			// sub-ledgernya di transaksi yang sama.
			paid, err := r.SumPayments(ctx, tenantID, it.rec.ID)
			if err != nil {
				return err
			}
			it.rec.PaidAmount = paid
			if !it.rec.OriginalAmount.Sub(paid).GreaterThan(domain.Zero) {
				it.rec.Status = StatusPaid
			}
			if err := r.SaveReceivable(ctx, it.rec); err != nil {
				return err
			}
			if err := r.AppendAudit(ctx, &Audit{
				TenantID:           tenantID,
				LegacyReceivableID: it.rec.ID,
				Event:              EventPaid,
				Amount:             it.amount,
				OutstandingAfter:   it.rec.OriginalAmount.Sub(paid),
				Detail: fmt.Sprintf("Penerimaan %s ke %s%s — jurnal #%d",
					it.amount.String(), cash.Code, docSuffix(docNo), entry.ID),
				ActorID: req.ActorID,
			}); err != nil {
				return err
			}
			out.Payments = append(out.Payments, *p)
		}

		out.JournalEntryID = entry.ID
		out.DocumentID = docID
		out.DocumentNumber = docNo
		out.TotalAmount = total
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// SourceLegacyPayment menandai jurnal yang lahir dari pelunasan piutang lama.
// Dipakai audit dan laporan untuk memisahkan penerimaan legacy dari penerimaan
// penjualan berjalan tanpa perlu menelusuri baris jurnalnya.
const SourceLegacyPayment = "legacy_ar_payment"

// VoidPayment membalik satu pelunasan.
//
// Jurnal asal TIDAK diubah dan baris pelunasannya TIDAK dihapus (Invariant #5).
// Yang terbentuk adalah jurnal pembalik bernomor sendiri dan baris pelunasan
// NEGATIF yang mencerminkannya — sehingga riwayat "pernah dibayar, lalu
// dibatalkan" tetap terbaca, bukan menguap seolah tidak pernah terjadi.
func (s *Service) VoidPayment(ctx context.Context, tenantID, paymentID uint64, reason string, actorID *uint64) (*PaymentResult, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, fmt.Errorf("alasan pembatalan wajib diisi")
	}

	var out PaymentResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		r := s.repo.WithTx(tx)

		p, err := r.FindPayment(ctx, tenantID, paymentID, true)
		if err != nil {
			return err
		}
		if p.IsVoid() {
			return fmt.Errorf("%w: baris ini sendiri adalah pembatalan", ErrPaymentVoided)
		}
		if voided, err := r.VoidExists(ctx, tenantID, p.ID); err != nil {
			return err
		} else if voided {
			return ErrPaymentVoided
		}

		rec, err := r.FindReceivable(ctx, tenantID, p.LegacyReceivableID, true)
		if err != nil {
			return err
		}
		cash, err := accountByCode(ctx, tx, tenantID, p.CashAccountCode)
		if err != nil {
			return err
		}
		ctrl, err := accountByCode(ctx, tx, tenantID, p.ControlAccountCode)
		if err != nil {
			return err
		}

		desc := fmt.Sprintf("Pembalik pelunasan piutang lama #%d (%s) — %s",
			p.ID, rec.CustomerName, reason)

		var reverses uint64
		if p.DocumentID != nil {
			reverses = *p.DocumentID
		}

		txLedger := ledger.NewGORMRepository(tx)
		posting := ledger.NewPostingService(txLedger, txLedger).WithPeriodChecker(txLedger)
		entry, err := posting.CreateAndPost(ctx, ledger.CreateJournalRequest{
			TenantID:    tenantID,
			Date:        time.Now(),
			Description: desc,
			Source:      SourceLegacyPayment,
			CreatedBy:   actorID,
			Lines: []ledger.LineInput{
				{AccountID: ctrl.ID, Debit: p.Amount, Description: "Piutang proyek lama hidup kembali (void)"},
				{AccountID: cash.ID, Credit: p.Amount, Description: "Pengembalian kas (void)"},
			},
			Document: ledger.DocumentSpec{
				TypeCode:           ledger.DocJournalReversal,
				ReversesDocumentID: reverses,
			},
		})
		if err != nil {
			return err
		}

		doc, err := document.FindByJournal(tx.WithContext(ctx), tenantID, entry.ID)
		if err != nil {
			return err
		}
		var docID *uint64
		var docNo string
		if doc != nil {
			id := doc.ID
			docID, docNo = &id, doc.Number
		}

		orig := p.ID
		mirror := &Payment{
			TenantID:           tenantID,
			LegacyReceivableID: rec.ID,
			Amount:             p.Amount.Neg(),
			PaymentDate:        truncDate(time.Now()),
			CashAccountCode:    p.CashAccountCode,
			ControlAccountCode: p.ControlAccountCode,
			JournalEntryID:     entry.ID,
			DocumentID:         docID,
			DocumentNumber:     docNo,
			VoidsPaymentID:     &orig,
			Notes:              trunc(reason, 500),
			CreatedBy:          actorID,
		}
		if err := r.CreatePayment(ctx, mirror); err != nil {
			if isDuplicateKey(err) {
				return ErrPaymentVoided
			}
			return err
		}

		paid, err := r.SumPayments(ctx, tenantID, rec.ID)
		if err != nil {
			return err
		}
		rec.PaidAmount = paid
		if rec.OriginalAmount.Sub(paid).GreaterThan(domain.Zero) {
			rec.Status = StatusOpen
		}
		if err := r.SaveReceivable(ctx, rec); err != nil {
			return err
		}
		if err := r.AppendAudit(ctx, &Audit{
			TenantID:           tenantID,
			LegacyReceivableID: rec.ID,
			Event:              EventPaymentVoid,
			Amount:             p.Amount.Neg(),
			OutstandingAfter:   rec.OriginalAmount.Sub(paid),
			Detail:             fmt.Sprintf("Pembatalan pelunasan #%d%s — %s", p.ID, docSuffix(docNo), reason),
			ActorID:            actorID,
		}); err != nil {
			return err
		}

		out.JournalEntryID = entry.ID
		out.DocumentID = docID
		out.DocumentNumber = docNo
		out.TotalAmount = p.Amount.Neg()
		out.Payments = []Payment{*mirror}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// resultOfJournal menyusun ulang hasil dari jurnal yang sudah ada — dipakai
// jawaban idempotensi.
func (s *Service) resultOfJournal(ctx context.Context, tenantID, journalID uint64, duplicate bool) (*PaymentResult, error) {
	var rows []Payment
	err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND journal_entry_id = ?", tenantID, journalID).
		Order("id ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := &PaymentResult{JournalEntryID: journalID, Payments: rows, Duplicate: duplicate, TotalAmount: domain.Zero}
	for _, p := range rows {
		out.TotalAmount = out.TotalAmount.Add(p.Amount)
		if out.DocumentID == nil && p.DocumentID != nil {
			out.DocumentID = p.DocumentID
			out.DocumentNumber = p.DocumentNumber
		}
	}
	return out, nil
}

func paymentDescription(firstName string, n int, notes string) string {
	base := fmt.Sprintf("Penerimaan piutang proyek lama — %s", firstName)
	if n > 1 {
		base = fmt.Sprintf("%s dan %d tagihan lain", base, n-1)
	}
	if notes = strings.TrimSpace(notes); notes != "" {
		base = base + " (" + notes + ")"
	}
	return trunc(base, 500)
}

func docSuffix(no string) string {
	if no == "" {
		return ""
	}
	return " (" + no + ")"
}
