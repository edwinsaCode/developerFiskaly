package legacyar

import (
	"context"
	"strings"

	"esaproperti/internal/domain"
	"esaproperti/internal/receivable"
)

// ── Adapter ke mesin piutang (W-4) ────────────────────────────────────────────

// ReceivableRows menerjemahkan piutang lama menjadi baris mesin piutang yang
// sudah ada. Inilah SATU-SATUNYA jalan piutang lama muncul di laporan umur
// piutang: tidak ada bucketing, tidak ada perhitungan telat, tidak ada
// subtotal yang dihitung di sini.
//
// INV-AR-1 dijaga dengan cara paling sederhana yang mungkin — menyerahkan
// seluruh logika umur ke receivable.BuildAging dan hanya menyediakan bahannya.
//
// Dipasang ke reporting.Service lewat WithLegacyReader di main.go, sehingga
// paket ini tidak pernah meng-import paket laporan.
func (s *Service) ReceivableRows(ctx context.Context, tenantID uint64) ([]receivable.Row, error) {
	recs, err := s.repo.ListReceivables(ctx, tenantID, ListFilter{})
	if err != nil {
		return nil, err
	}
	out := make([]receivable.Row, 0, len(recs))
	for i := range recs {
		r := recs[i]
		// Piutang yang dihapusbukukan tidak lagi menjadi eksposur. Ia tetap ada
		// di tabelnya sebagai riwayat, tapi tidak boleh ikut menua di laporan.
		if r.Status == StatusWrittenOff {
			continue
		}
		ref := ""
		if r.ExternalRef != nil {
			ref = *r.ExternalRef
		}
		out = append(out, receivable.Row{
			Source: receivable.SourceLegacy,
			RefID:  r.ID,
			// ContractID/UnitID sengaja nol: piutang ini memang tidak punya
			// kontrak maupun unit di sistem. Mengarang id palsu supaya "terlihat
			// lengkap" akan membuat tautan UI menunjuk ke entitas orang lain.
			BuyerName:  r.CustomerName,
			BuyerPhone: r.Phone,
			BuyerEmail: r.Email,
			// UnitCode dibiarkan kosong — tidak ada unitnya. Keterangan asal
			// piutang adalah Label, sesuai aturan kolom di receivable.Row.
			Label:         r.SourceLabel,
			InvoiceNumber: ref,
			DueDate:       r.EffectiveDueDate(),
			Amount:        r.OriginalAmount,
			PaidAmount:    r.PaidAmount,
			Received:      r.Status == StatusPaid,
		})
	}
	return out, nil
}

// ── Read model daftar & detail ────────────────────────────────────────────────

// ReceivableView adalah satu baris daftar piutang lama beserta sisanya.
type ReceivableView struct {
	Receivable
	Outstanding domain.Money `json:"outstanding"`
	PaymentSlot int          `json:"payment_count"`
}

// ListSummary adalah ringkasan yang menyertai daftar. Angkanya dihitung di
// server; browser tidak menjumlahkan apa pun.
type ListSummary struct {
	Count            int          `json:"count"`
	TotalOriginal    domain.Money `json:"total_original"`
	TotalPaid        domain.Money `json:"total_paid"`
	TotalOutstanding domain.Money `json:"total_outstanding"`
	OpenCount        int          `json:"open_count"`
	PaidCount        int          `json:"paid_count"`
	WrittenOffCount  int          `json:"written_off_count"`
}

// ListResult adalah balasan endpoint daftar.
type ListResult struct {
	Rows         []ReceivableView `json:"rows"`
	Summary      ListSummary      `json:"summary"`
	SourceLabels []string         `json:"source_labels"`
}

// ListReceivables mengembalikan daftar piutang lama beserta ringkasannya.
func (s *Service) ListReceivables(ctx context.Context, tenantID uint64, f ListFilter) (*ListResult, error) {
	recs, err := s.repo.ListReceivables(ctx, tenantID, f)
	if err != nil {
		return nil, err
	}
	ids := make([]uint64, 0, len(recs))
	for i := range recs {
		ids = append(ids, recs[i].ID)
	}
	payments, err := s.repo.ListPaymentsFor(ctx, tenantID, ids)
	if err != nil {
		return nil, err
	}

	out := &ListResult{
		Rows: make([]ReceivableView, 0, len(recs)),
		Summary: ListSummary{
			TotalOriginal:    domain.Zero,
			TotalPaid:        domain.Zero,
			TotalOutstanding: domain.Zero,
		},
	}
	for i := range recs {
		r := recs[i]
		v := ReceivableView{
			Receivable:  r,
			Outstanding: r.Outstanding(),
			PaymentSlot: len(payments[r.ID]),
		}
		out.Rows = append(out.Rows, v)
		out.Summary.Count++
		switch r.Status {
		case StatusOpen:
			out.Summary.OpenCount++
		case StatusPaid:
			out.Summary.PaidCount++
		case StatusWrittenOff:
			out.Summary.WrittenOffCount++
		}
		// Yang dihapusbukukan tidak menambah eksposur — konsisten dengan
		// ReceivableRows di atas dan dengan SumOutstanding di repository.
		if r.Status == StatusWrittenOff {
			continue
		}
		out.Summary.TotalOriginal = out.Summary.TotalOriginal.Add(r.OriginalAmount)
		out.Summary.TotalPaid = out.Summary.TotalPaid.Add(r.PaidAmount)
		out.Summary.TotalOutstanding = out.Summary.TotalOutstanding.Add(r.Outstanding())
	}

	labels, err := s.repo.DistinctSourceLabels(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out.SourceLabels = labels
	return out, nil
}

// ReceivableDetail adalah satu piutang lama beserta riwayat lengkapnya.
type ReceivableDetail struct {
	Receivable  Receivable   `json:"receivable"`
	Outstanding domain.Money `json:"outstanding"`
	Payments    []Payment    `json:"payments"`
	Audits      []Audit      `json:"audits"`
	Batch       *Batch       `json:"batch,omitempty"`
}

// GetReceivable mengembalikan detail satu piutang lama.
func (s *Service) GetReceivable(ctx context.Context, tenantID, id uint64) (*ReceivableDetail, error) {
	rec, err := s.repo.FindReceivable(ctx, tenantID, id, false)
	if err != nil {
		return nil, err
	}
	payments, err := s.repo.ListPayments(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	audits, err := s.repo.ListAudits(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	d := &ReceivableDetail{
		Receivable:  *rec,
		Outstanding: rec.Outstanding(),
		Payments:    payments,
		Audits:      audits,
	}
	if b, err := s.repo.FindBatch(ctx, tenantID, rec.BatchID, false); err == nil {
		d.Batch = b
	}
	return d, nil
}

// SumOutstanding adalah total sisa piutang lama tenant — dipakai rekonsiliasi.
func (s *Service) SumOutstanding(ctx context.Context, tenantID uint64) (domain.Money, error) {
	return s.repo.SumOutstanding(ctx, tenantID)
}

// normalizeSearch merapikan kata kunci pencarian.
func normalizeSearch(q string) string { return strings.TrimSpace(q) }
