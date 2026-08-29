package ap

import (
	"context"
	"time"

	"esaproperti/internal/cost"
	"esaproperti/internal/domain"
)

// Pembacaan tagihan.
//
// Tidak ada kolom "sisa tagihan" dan tidak ada kolom "status pembayaran" di
// mana pun (D-12). Keduanya DITURUNKAN di sini dari sub-ledger alokasi. Kolom
// cache adalah cara termudah melahirkan dua angka yang tidak sepakat — satu di
// daftar, satu di detail — dan yang salah tidak pernah memunculkan error.

// PaymentStatus adalah status pembayaran TURUNAN sebuah tagihan.
type PaymentStatus string

const (
	// PayStatusUnrecognized: tagihan belum diposting. Bukan "belum dibayar" —
	// kewajibannya memang belum ada di buku (INV-AP-9).
	PayStatusUnrecognized PaymentStatus = "unrecognized"
	PayStatusUnpaid       PaymentStatus = "unpaid"
	PayStatusPartial      PaymentStatus = "partial"
	PayStatusPaid         PaymentStatus = "paid"
	PayStatusReversed     PaymentStatus = "reversed"
)

// InvoiceView adalah tagihan sebagaimana dibaca layar: nominal tersimpan +
// angka turunan + baris biayanya.
type InvoiceView struct {
	*Invoice
	VendorName    string            `json:"vendor_name"`
	VendorIsPKP   bool              `json:"vendor_is_pkp"`
	PaidAmount    string            `json:"paid_amount"`
	Outstanding   string            `json:"outstanding"`
	PaymentStatus PaymentStatus     `json:"payment_status"`
	Lines         []*cost.CostEntry `json:"lines,omitempty"`
}

// GetInvoice mengembalikan satu tagihan beserta baris biaya dan angka turunannya.
func (s *Service) GetInvoice(ctx context.Context, tenantID, id uint64) (*InvoiceView, error) {
	inv, err := s.repo.FindInvoice(ctx, tenantID, id, false)
	if err != nil {
		return nil, err
	}
	view, err := s.viewOf(ctx, tenantID, inv)
	if err != nil {
		return nil, err
	}
	lines, err := s.repo.ListCostEntriesByInvoice(ctx, tenantID, inv.ID)
	if err != nil {
		return nil, err
	}
	view.Lines = lines
	return view, nil
}

// ListInvoices mengembalikan daftar tagihan beserta angka turunannya.
func (s *Service) ListInvoices(ctx context.Context, tenantID uint64, f InvoiceFilter) ([]*InvoiceView, error) {
	invs, err := s.repo.ListInvoices(ctx, tenantID, f)
	if err != nil {
		return nil, err
	}
	out := make([]*InvoiceView, 0, len(invs))
	for _, inv := range invs {
		v, err := s.viewOf(ctx, tenantID, inv)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Service) viewOf(ctx context.Context, tenantID uint64, inv *Invoice) (*InvoiceView, error) {
	paid, err := s.repo.SumAllocations(ctx, tenantID, inv.ID)
	if err != nil {
		return nil, err
	}
	outstanding := inv.PayableAmount.Sub(paid)
	v := &InvoiceView{
		Invoice:       inv,
		PaidAmount:    paid.String(),
		Outstanding:   outstanding.String(),
		PaymentStatus: derivePaymentStatus(inv, paid, outstanding),
	}
	if vendor, err := s.repo.FindVendor(ctx, tenantID, inv.VendorID); err == nil {
		v.VendorName = vendor.Name
		v.VendorIsPKP = vendor.IsPKP
	}
	return v, nil
}

// ── Pembayaran ────────────────────────────────────────────────────────────────

// PaymentView adalah pembayaran sebagaimana dibaca layar.
//
// Alokasinya ikut dibawa karena tanpa itu satu baris pembayaran tidak bisa
// menjawab pertanyaan yang paling sering diajukan tentangnya: uang ini melunasi
// tagihan yang mana.
type PaymentView struct {
	*Payment
	VendorName  string           `json:"vendor_name"`
	Allocations []AllocationView `json:"allocations,omitempty"`
	IsReversed  bool             `json:"is_reversed"`
}

// GetPayment mengembalikan satu pembayaran beserta alokasinya.
func (s *Service) GetPayment(ctx context.Context, tenantID, id uint64) (*PaymentView, error) {
	pay, err := s.repo.FindPayment(ctx, tenantID, id, false)
	if err != nil {
		return nil, err
	}
	return s.paymentViewOf(ctx, tenantID, pay, true)
}

// ListPayments mengembalikan daftar pembayaran.
//
// Alokasinya TIDAK ikut dimuat di daftar: satu query per baris untuk data yang
// tidak ditampilkan di daftar adalah biaya yang dibayar tanpa imbalan.
func (s *Service) ListPayments(ctx context.Context, tenantID uint64, f PaymentFilter) ([]*PaymentView, error) {
	pays, err := s.repo.ListPayments(ctx, tenantID, f)
	if err != nil {
		return nil, err
	}
	out := make([]*PaymentView, 0, len(pays))
	for _, p := range pays {
		v, err := s.paymentViewOf(ctx, tenantID, p, false)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Service) paymentViewOf(ctx context.Context, tenantID uint64, p *Payment, withAllocations bool) (*PaymentView, error) {
	v := &PaymentView{Payment: p, IsReversed: p.ReversedAt != nil}
	if vendor, err := s.repo.FindVendor(ctx, tenantID, p.VendorID); err == nil {
		v.VendorName = vendor.Name
	}
	if !withAllocations {
		return v, nil
	}
	allocs, err := s.repo.ListAllocationsByPayment(ctx, tenantID, p.ID)
	if err != nil {
		return nil, err
	}
	for _, a := range allocs {
		av := AllocationView{InvoiceID: a.InvoiceID, Amount: a.Amount.String()}
		if inv, err := s.repo.FindInvoice(ctx, tenantID, a.InvoiceID, false); err == nil {
			av.InvoiceNumber = inv.InvoiceNumber
			av.DueDate = inv.DueDate.Format("2006-01-02")
		}
		v.Allocations = append(v.Allocations, av)
	}
	return v, nil
}

// InvoicePaymentRow adalah satu baris riwayat pembayaran sebuah tagihan.
//
// `reversed_at` dibawa apa adanya, tidak disaring: alokasi yang dibalik tetap
// bagian dari riwayat tagihan ini. Menyembunyikannya membuat layar detail
// menjawab "tidak pernah dibayar" untuk tagihan yang sebenarnya pernah dibayar
// lalu dibatalkan — dua keadaan yang sangat berbeda saat ada pertanyaan.
type InvoicePaymentRow struct {
	AllocationID   uint64     `json:"allocation_id"`
	PaymentID      *uint64    `json:"payment_id,omitempty"`
	PaymentDate    string     `json:"payment_date,omitempty"`
	DocumentNumber string     `json:"document_number,omitempty"`
	CashAccount    string     `json:"cash_account_code,omitempty"`
	Type           string     `json:"allocation_type"`
	Amount         string     `json:"amount"`
	ReversedAt     *time.Time `json:"reversed_at,omitempty"`
}

// ListInvoicePayments mengembalikan riwayat pembayaran satu tagihan.
func (s *Service) ListInvoicePayments(ctx context.Context, tenantID, invoiceID uint64) ([]*InvoicePaymentRow, error) {
	allocs, err := s.repo.ListAllocationsByInvoice(ctx, tenantID, invoiceID)
	if err != nil {
		return nil, err
	}
	out := make([]*InvoicePaymentRow, 0, len(allocs))
	for _, a := range allocs {
		row := &InvoicePaymentRow{
			AllocationID: a.ID,
			PaymentID:    a.PaymentID,
			Type:         string(a.Type),
			Amount:       a.Amount.String(),
			ReversedAt:   a.ReversedAt,
		}
		if a.PaymentID != nil {
			if p, err := s.repo.FindPayment(ctx, tenantID, *a.PaymentID, false); err == nil {
				row.PaymentDate = p.PaymentDate.Format("2006-01-02")
				row.DocumentNumber = p.DocumentNumber
				row.CashAccount = p.CashAccountCode
			}
		}
		out = append(out, row)
	}
	return out, nil
}

// derivePaymentStatus menurunkan status pembayaran dari sub-ledger.
//
// Tagihan draft SELALU `unrecognized`, apa pun isi sub-ledgernya — kewajibannya
// belum lahir, jadi menyebutnya "belum dibayar" akan menempatkannya di daftar
// tagihan yang harus dibayar padahal ia belum menjadi kewajiban siapa pun.
func derivePaymentStatus(inv *Invoice, paid, outstanding domain.Money) PaymentStatus {
	switch inv.Status {
	case InvoiceDraft:
		return PayStatusUnrecognized
	case InvoiceReversed:
		return PayStatusReversed
	}
	switch {
	case paid.IsZero():
		return PayStatusUnpaid
	case outstanding.GreaterThan(domain.Zero):
		return PayStatusPartial
	default:
		return PayStatusPaid
	}
}
