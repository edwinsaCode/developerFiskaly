package charge

import (
	"errors"
	"testing"

	"esaproperti/internal/domain"
)

func m(t *testing.T, s string) domain.Money {
	t.Helper()
	v, err := domain.NewMoney(s)
	if err != nil {
		t.Fatalf("NewMoney(%s): %v", s, err)
	}
	return v
}

func fixtureItems(t *testing.T) []*ChargeItem {
	return []*ChargeItem{
		{ID: 1, Label: "Notaris", Amount: m(t, "10000000"), Status: ItemOpen},
		{ID: 2, Label: "PDAM", Amount: m(t, "5000000"), Status: ItemOpen},
		{ID: 3, Label: "Listrik", Amount: m(t, "5000000"), Status: ItemOpen},
		{ID: 4, Label: "Batal", Amount: m(t, "1000000"), Status: ItemCancelled},
	}
}

// K-3: alokasi manual admin — contoh A klien (Notaris 10jt + PDAM 5jt = 15jt).
func TestValidateAllocations_FlexibleAdminChoice(t *testing.T) {
	items := fixtureItems(t)
	paid := map[uint64]domain.Money{}

	allocs := []AllocationInput{
		{ChargeItemID: 1, Amount: m(t, "10000000")},
		{ChargeItemID: 2, Amount: m(t, "5000000")},
	}
	if err := validateAllocations(allocs, m(t, "15000000"), items, paid); err != nil {
		t.Fatalf("contoh A klien harus valid: %v", err)
	}

	// Contoh B klien: Notaris 10jt + PDAM 3jt + Listrik 2jt.
	allocsB := []AllocationInput{
		{ChargeItemID: 1, Amount: m(t, "10000000")},
		{ChargeItemID: 2, Amount: m(t, "3000000")},
		{ChargeItemID: 3, Amount: m(t, "2000000")},
	}
	if err := validateAllocations(allocsB, m(t, "15000000"), items, paid); err != nil {
		t.Fatalf("contoh B klien harus valid: %v", err)
	}
}

// Invariant #3: Σ alokasi WAJIB == nominal pembayaran, persis.
func TestValidateAllocations_SumMustEqualAmount(t *testing.T) {
	items := fixtureItems(t)
	paid := map[uint64]domain.Money{}
	allocs := []AllocationInput{
		{ChargeItemID: 1, Amount: m(t, "10000000")},
		{ChargeItemID: 2, Amount: m(t, "4000000")},
	}
	err := validateAllocations(allocs, m(t, "15000000"), items, paid)
	if !errors.Is(err, ErrAllocationMismatch) {
		t.Fatalf("Σ 14jt vs bayar 15jt harus ErrAllocationMismatch, dapat: %v", err)
	}
}

// Alokasi tidak boleh melebihi sisa tagihan item (memperhitungkan yang sudah dibayar).
func TestValidateAllocations_CannotExceedItemOutstanding(t *testing.T) {
	items := fixtureItems(t)
	paid := map[uint64]domain.Money{2: m(t, "3000000")} // PDAM sisa 2jt
	allocs := []AllocationInput{{ChargeItemID: 2, Amount: m(t, "2500000")}}
	err := validateAllocations(allocs, m(t, "2500000"), items, paid)
	if !errors.Is(err, ErrAllocationExceeds) {
		t.Fatalf("alokasi 2.5jt > sisa 2jt harus ErrAllocationExceeds, dapat: %v", err)
	}
	// Pas di batas sisa → valid.
	allocsOK := []AllocationInput{{ChargeItemID: 2, Amount: m(t, "2000000")}}
	if err := validateAllocations(allocsOK, m(t, "2000000"), items, paid); err != nil {
		t.Fatalf("alokasi pas sisa harus valid: %v", err)
	}
}

func TestValidateAllocations_Guards(t *testing.T) {
	items := fixtureItems(t)
	paid := map[uint64]domain.Money{}

	// K-3: TANPA alokasi = ditolak (tidak ada auto-allocation).
	if err := validateAllocations(nil, m(t, "1000000"), items, paid); !errors.Is(err, ErrAllocationsRequired) {
		t.Fatalf("tanpa alokasi harus ErrAllocationsRequired, dapat: %v", err)
	}
	// Item duplikat.
	dup := []AllocationInput{
		{ChargeItemID: 1, Amount: m(t, "1000000")},
		{ChargeItemID: 1, Amount: m(t, "1000000")},
	}
	if err := validateAllocations(dup, m(t, "2000000"), items, paid); !errors.Is(err, ErrAllocationDuplicate) {
		t.Fatalf("item duplikat harus ErrAllocationDuplicate, dapat: %v", err)
	}
	// Item di luar grup.
	outside := []AllocationInput{{ChargeItemID: 99, Amount: m(t, "1000000")}}
	if err := validateAllocations(outside, m(t, "1000000"), items, paid); !errors.Is(err, ErrItemNotInGroup) {
		t.Fatalf("item asing harus ErrItemNotInGroup, dapat: %v", err)
	}
	// Item cancelled.
	cancelled := []AllocationInput{{ChargeItemID: 4, Amount: m(t, "1000000")}}
	if err := validateAllocations(cancelled, m(t, "1000000"), items, paid); !errors.Is(err, ErrItemNotOpen) {
		t.Fatalf("item cancelled harus ErrItemNotOpen, dapat: %v", err)
	}
	// Nominal pecahan.
	frac := []AllocationInput{{ChargeItemID: 1, Amount: m(t, "1000000.50")}}
	if err := validateAllocations(frac, m(t, "1000000.50"), items, paid); !errors.Is(err, ErrAmountInvalid) {
		t.Fatalf("nominal pecahan harus ErrAmountInvalid, dapat: %v", err)
	}
	// Nominal nol.
	zero := []AllocationInput{{ChargeItemID: 1, Amount: domain.Zero}}
	if err := validateAllocations(zero, domain.Zero, items, paid); !errors.Is(err, ErrAmountInvalid) {
		t.Fatalf("nominal nol harus ErrAmountInvalid, dapat: %v", err)
	}
}

func TestValidateItems(t *testing.T) {
	if err := validateItems(nil); !errors.Is(err, ErrItemsRequired) {
		t.Fatalf("tanpa item harus ErrItemsRequired, dapat: %v", err)
	}
	if err := validateItems([]NewItemInput{{Label: "", Amount: m(t, "1000")}}); !errors.Is(err, ErrLabelRequired) {
		t.Fatalf("label kosong harus ErrLabelRequired, dapat: %v", err)
	}
	if err := validateItems([]NewItemInput{{Label: "PDAM", Amount: m(t, "-5")}}); !errors.Is(err, ErrAmountInvalid) {
		t.Fatalf("amount negatif harus ErrAmountInvalid, dapat: %v", err)
	}
	if err := validateItems([]NewItemInput{{Label: "PDAM", Amount: m(t, "5000000")}}); err != nil {
		t.Fatalf("item sah harus valid: %v", err)
	}
}
