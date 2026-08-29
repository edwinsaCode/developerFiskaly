package billing

import (
	"context"
	"fmt"
	"regexp"
	"testing"
	"time"

	"esaproperti/internal/domain"
)

// ── Mocks ─────────────────────────────────────────────────────────────────────

type mockTermins struct {
	// [tenantID][terminID] → TerminInfo
	byID map[uint64]map[uint64]*TerminInfo
}

func newMockTermins() *mockTermins {
	return &mockTermins{byID: make(map[uint64]map[uint64]*TerminInfo)}
}

func (m *mockTermins) add(tenantID uint64, info *TerminInfo) {
	if m.byID[tenantID] == nil {
		m.byID[tenantID] = make(map[uint64]*TerminInfo)
	}
	m.byID[tenantID][info.ID] = info
}

func (m *mockTermins) LoadTerminInfo(_ context.Context, tenantID, terminID uint64) (*TerminInfo, error) {
	if t, ok := m.byID[tenantID]; ok {
		if info, ok := t[terminID]; ok {
			return info, nil
		}
	}
	return nil, ErrTerminNotFound
}

// mockReceipts meniru UNIQUE(tenant_id, termin_payment_id) + sequence numbering.
type mockReceipts struct {
	byTermin map[uint64]map[uint64]*Receipt // [tenant][termin]
	byID     map[uint64]*Receipt
	seq      map[uint64]uint64 // per-tenant
	nextID   uint64
}

func newMockReceipts() *mockReceipts {
	return &mockReceipts{
		byTermin: make(map[uint64]map[uint64]*Receipt),
		byID:     make(map[uint64]*Receipt),
		seq:      make(map[uint64]uint64),
	}
}

func (m *mockReceipts) FindReceiptByTermin(_ context.Context, tenantID, terminID uint64) (*Receipt, error) {
	if t, ok := m.byTermin[tenantID]; ok {
		if r, ok := t[terminID]; ok {
			return r, nil
		}
	}
	return nil, ErrReceiptNotFound
}

func (m *mockReceipts) FindReceiptByID(_ context.Context, tenantID, id uint64) (*Receipt, error) {
	if r, ok := m.byID[id]; ok && r.TenantID == tenantID {
		return r, nil
	}
	return nil, ErrReceiptNotFound
}

func (m *mockReceipts) CreateReceiptForTermin(ctx context.Context, tenantID, createdBy uint64, info *TerminInfo, notes string) (*Receipt, error) {
	// Idempoten: kembalikan yang sudah ada.
	if existing, err := m.FindReceiptByTermin(ctx, tenantID, info.ID); err == nil {
		return existing, nil
	}
	m.seq[tenantID]++
	m.nextID++
	rec := &Receipt{
		ID:              m.nextID,
		TenantID:        tenantID,
		TerminPaymentID: info.ID,
		UnitID:          info.UnitID,
		ReceiptNumber:   fmt.Sprintf("KWT/2026/%06d", m.seq[tenantID]),
		Amount:          info.Amount,
		BankAccountCode: info.BankAccountCode,
		ReceivedAt:      info.Date,
		Notes:           notes,
		CreatedBy:       createdBy,
	}
	if m.byTermin[tenantID] == nil {
		m.byTermin[tenantID] = make(map[uint64]*Receipt)
	}
	m.byTermin[tenantID][info.ID] = rec
	m.byID[rec.ID] = rec
	return rec, nil
}

func buildReceiptService() (*ReceiptService, *mockTermins, *mockReceipts) {
	termins := newMockTermins()
	receipts := newMockReceipts()
	svc := NewReceiptService(termins, receipts, nil)
	return svc, termins, receipts
}

// ── Tests ─────────────────────────────────────────────────────────────────────

// TestGenerateReceipt_Idempotent: pemanggilan berulang tidak membuat duplikat.
func TestGenerateReceipt_Idempotent(t *testing.T) {
	svc, termins, receipts := buildReceiptService()
	termins.add(1, &TerminInfo{ID: 10, UnitID: 100, Amount: domain.FromInt(250_000_000), BankAccountCode: "1-1300", Date: time.Now()})

	r1, err := svc.GenerateReceipt(context.Background(), 1, 7, 10, "DP")
	if err != nil {
		t.Fatalf("GenerateReceipt #1: %v", err)
	}
	r2, err := svc.GenerateReceipt(context.Background(), 1, 7, 10, "DP lagi")
	if err != nil {
		t.Fatalf("GenerateReceipt #2: %v", err)
	}

	if r1.ID != r2.ID {
		t.Errorf("idempoten gagal: ID berbeda %d vs %d", r1.ID, r2.ID)
	}
	if r1.ReceiptNumber != r2.ReceiptNumber {
		t.Errorf("idempoten gagal: nomor berbeda %s vs %s", r1.ReceiptNumber, r2.ReceiptNumber)
	}
	if got := len(receipts.byTermin[1]); got != 1 {
		t.Errorf("harus hanya 1 receipt tersimpan, got %d", got)
	}
}

// TestGenerateReceipt_AmountConsistency: nominal receipt == nominal termin.
func TestGenerateReceipt_AmountConsistency(t *testing.T) {
	svc, termins, _ := buildReceiptService()
	amt := domain.FromInt(123_456_000)
	termins.add(1, &TerminInfo{ID: 10, UnitID: 100, Amount: amt, BankAccountCode: "1-1300", Date: time.Now()})

	rec, err := svc.GenerateReceipt(context.Background(), 1, 7, 10, "")
	if err != nil {
		t.Fatalf("GenerateReceipt: %v", err)
	}
	if !rec.Amount.Equal(amt) {
		t.Errorf("amount receipt (%s) != amount termin (%s)", rec.Amount.String(), amt.String())
	}
}

// TestGenerateReceipt_NumberFormat: nomor mengikuti KWT/YYYY/NNNNNN.
func TestGenerateReceipt_NumberFormat(t *testing.T) {
	svc, termins, _ := buildReceiptService()
	termins.add(1, &TerminInfo{ID: 10, UnitID: 100, Amount: domain.FromInt(1_000_000), BankAccountCode: "1-1300", Date: time.Now()})

	rec, err := svc.GenerateReceipt(context.Background(), 1, 7, 10, "")
	if err != nil {
		t.Fatalf("GenerateReceipt: %v", err)
	}
	if !regexp.MustCompile(`^KWT/\d{4}/\d{6}$`).MatchString(rec.ReceiptNumber) {
		t.Errorf("format nomor salah: %s", rec.ReceiptNumber)
	}
}

// TestGenerateReceipt_TenantIsolation: tenant lain tidak bisa membuat kwitansi
// atas termin milik tenant A (Invariant #6).
func TestGenerateReceipt_TenantIsolation(t *testing.T) {
	svc, termins, _ := buildReceiptService()
	termins.add(1, &TerminInfo{ID: 10, UnitID: 100, Amount: domain.FromInt(250_000_000), BankAccountCode: "1-1300", Date: time.Now()})

	// Tenant 2 mencoba termin milik tenant 1 → ErrTerminNotFound.
	_, err := svc.GenerateReceipt(context.Background(), 2, 7, 10, "")
	if err == nil {
		t.Fatal("tenant 2 seharusnya tidak bisa membuat kwitansi untuk termin tenant 1")
	}
}
