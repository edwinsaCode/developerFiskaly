package cost

import (
	"errors"
	"testing"
	"time"

	"esaproperti/internal/domain"
)

func TestExpenseDTO_Proyek_JenisPengeluaranHanyaUntukOperasional(t *testing.T) {
	id, pid := uint64(3), uint64(1)
	base := expenseDTO{
		Scope: expenseScopeProyek, PaymentMethod: "bank", BankAccountCode: "1-1300",
		ProjectID: &pid, ExpenseTypeID: &id, CostTier: string(domain.CostTierOverhead),
	}

	op := base
	op.Category = string(domain.CostCategoryOperational)
	req, err := op.toRequest(domain.FromInt(1000), time.Now())
	if err != nil {
		t.Fatalf("operational + jenis harus diterima: %v", err)
	}
	if req.ExpenseTypeID == nil || *req.ExpenseTypeID != id || req.Category != domain.CostCategoryOperational {
		t.Errorf("jenis & kategori harus diteruskan apa adanya, dapat %+v", req)
	}

	soft := base
	soft.Category = string(domain.CostCategorySoft)
	if _, err := soft.toRequest(domain.FromInt(1000), time.Now()); !errors.Is(err, ErrExpenseTypeNotForProjectCost) {
		t.Fatalf("soft + jenis harus ditolak, dapat %v", err)
	}
}

func TestExpenseScopeOf(t *testing.T) {
	id := uint64(1)
	cases := []struct {
		typeID *uint64
		cat    domain.CostCategory
		want   string
	}{
		{&id, domain.CostCategoryOther, "operasional"},
		{&id, domain.CostCategoryOperational, "proyek"},
		{nil, domain.CostCategoryOperational, "proyek"},
		{nil, domain.CostCategoryHard, "proyek"},
	}
	for _, c := range cases {
		if got := expenseScopeOf(c.typeID, c.cat); got != c.want {
			t.Errorf("expenseScopeOf(%v, %s) = %s, harusnya %s", c.typeID != nil, c.cat, got, c.want)
		}
	}
}
