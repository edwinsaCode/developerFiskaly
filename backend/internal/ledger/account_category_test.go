package ledger_test

import (
	"testing"

	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

func payAcc(code string, t domain.AccountType, active bool, cat ledger.AccountCategory) *ledger.Account {
	return &ledger.Account{Code: code, Type: t, IsActive: active, Category: cat}
}

// inactive account rejected
func TestValidatePaymentAccount_InactiveRejected(t *testing.T) {
	err := ledger.ValidatePaymentAccount(payAcc("1-1300", domain.AccountAsset, false, ledger.CategoryBank))
	if err != ledger.ErrAccountInactive {
		t.Errorf("akun nonaktif harus ditolak: got %v, want ErrAccountInactive", err)
	}
}

// liability account rejected
func TestValidatePaymentAccount_LiabilityRejected(t *testing.T) {
	err := ledger.ValidatePaymentAccount(payAcc("2-1000", domain.AccountLiability, true, ""))
	if err != ledger.ErrNotCashBankAccount {
		t.Errorf("akun liability harus ditolak: got %v, want ErrNotCashBankAccount", err)
	}
}

// asset tapi bukan kas/bank (other_asset) ditolak
func TestValidatePaymentAccount_OtherAssetRejected(t *testing.T) {
	err := ledger.ValidatePaymentAccount(payAcc("1-2000", domain.AccountAsset, true, ledger.CategoryOtherAsset))
	if err != ledger.ErrNotCashBankAccount {
		t.Errorf("aset non-kas/bank harus ditolak: got %v, want ErrNotCashBankAccount", err)
	}
}

// custom bank account accepted (kode di luar daftar bawaan)
func TestValidatePaymentAccount_CustomBankAccepted(t *testing.T) {
	if err := ledger.ValidatePaymentAccount(payAcc("1-1600", domain.AccountAsset, true, ledger.CategoryBank)); err != nil {
		t.Errorf("akun bank custom aktif harus diterima: got %v", err)
	}
}

func TestValidatePaymentAccount_CashAccepted(t *testing.T) {
	if err := ledger.ValidatePaymentAccount(payAcc("1-1100", domain.AccountAsset, true, ledger.CategoryCash)); err != nil {
		t.Errorf("akun kas aktif harus diterima: got %v", err)
	}
}

func TestValidatePaymentAccount_NilRejected(t *testing.T) {
	if err := ledger.ValidatePaymentAccount(nil); err != ledger.ErrAccountNotFound {
		t.Errorf("akun nil harus ErrAccountNotFound: got %v", err)
	}
}
