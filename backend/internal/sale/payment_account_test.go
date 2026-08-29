package sale_test

import (
	"context"
	"testing"
	"time"

	"esaproperti/internal/domain"
	"esaproperti/internal/sale"
)

// COA-driven payment account: ReceivePayment menolak rekening tidak valid dan
// menerima rekening bank custom yang aktif. Validasi via AccountFinder
// (ValidateCashBankAccount), bukan daftar hardcode.

func buildAccountValidationService(validateErr map[string]error) (*sale.Service, *mockTerminStore) {
	accounts := map[string]uint64{
		"1-1100": 200, "1-1300": 201, "2-1000": 210, "1-9999": 220, "1-1600": 230, "2-2000": 302, "1-2000": 301,
	}
	af := &mockAccountFinder{accounts: accounts, validateErr: validateErr}
	jw := &mockJournalWriter{}
	ur := &mockUnitReader{units: map[uint64]*sale.UnitSaleInfo{100: {ID: 100, ProjectID: 9, Status: "reserved"}}}
	ts := &mockTerminStore{}
	cp := &mockUnitCostProvider{}
	bw := &mockBASTWriter{}
	svc := sale.NewService(af, jw, ur, ts, cp, bw, sale.WithContractStore(newMockContractStore()))
	return svc, ts
}

func recvWithBank(svc *sale.Service, bank string) error {
	uid := uint64(100)
	_, err := svc.ReceivePayment(context.Background(), 1, sale.ReceivePaymentRequest{
		Source:          sale.PaymentSourceUnitTermin,
		UnitID:          &uid,
		Amount:          domain.FromInt(100_000_000),
		Date:            time.Now(),
		BankAccountCode: bank,
	})
	return err
}

// inactive account rejected
func TestPaymentAccount_InactiveRejected(t *testing.T) {
	svc, ts := buildAccountValidationService(map[string]error{"1-1300": sale.ErrPaymentAccountInactive})
	if err := recvWithBank(svc, "1-1300"); err != sale.ErrPaymentAccountInactive {
		t.Errorf("akun nonaktif harus ditolak: got %v", err)
	}
	if ts.TerminCount() != 0 {
		t.Error("tidak boleh ada termin tercatat saat akun ditolak")
	}
}

// liability account rejected
func TestPaymentAccount_LiabilityRejected(t *testing.T) {
	svc, ts := buildAccountValidationService(map[string]error{"2-1000": sale.ErrInvalidBankAccount})
	if err := recvWithBank(svc, "2-1000"); err != sale.ErrInvalidBankAccount {
		t.Errorf("akun liability harus ditolak: got %v", err)
	}
	if ts.TerminCount() != 0 {
		t.Error("tidak boleh ada termin tercatat saat akun ditolak")
	}
}

// cross tenant account rejected (load tenant-scoped → not found)
func TestPaymentAccount_CrossTenantRejected(t *testing.T) {
	svc, ts := buildAccountValidationService(map[string]error{"1-9999": sale.ErrPaymentAccountNotFound})
	if err := recvWithBank(svc, "1-9999"); err != sale.ErrPaymentAccountNotFound {
		t.Errorf("akun lintas-tenant harus ditolak: got %v", err)
	}
	if ts.TerminCount() != 0 {
		t.Error("tidak boleh ada termin tercatat saat akun ditolak")
	}
}

// custom bank account accepted
func TestPaymentAccount_CustomBankAccepted(t *testing.T) {
	// validateErr["1-1600"] = nil → dianggap akun bank aktif yang valid.
	svc, ts := buildAccountValidationService(map[string]error{"1-1600": nil})
	if err := recvWithBank(svc, "1-1600"); err != nil {
		t.Fatalf("akun bank custom aktif harus diterima: %v", err)
	}
	if ts.TerminCount() != 1 {
		t.Errorf("harus tepat 1 termin tercatat, got %d", ts.TerminCount())
	}
}
