package ledger

import (
	"esaproperti/internal/domain"
)

// seedCashCodes / seedBankCodes adalah akun kas/bank bawaan (selaras migrasi 000024).
var seedCashCodes = map[string]bool{"1-1100": true, "1-1200": true}
var seedBankCodes = map[string]bool{"1-1300": true, "1-1400": true, "1-1500": true}

// AccountCategory mengklasifikasikan akun untuk pemilihan rekening pembayaran.
// Hanya akun aset yang punya kategori bermakna; non-aset bernilai "".
type AccountCategory string

const (
	CategoryCash       AccountCategory = "cash"
	CategoryBank       AccountCategory = "bank"
	CategoryOtherAsset AccountCategory = "other_asset"
)

// IsCashBank melaporkan apakah kategori boleh dipakai sebagai tujuan pembayaran.
func (c AccountCategory) IsCashBank() bool { return c == CategoryCash || c == CategoryBank }

// IsValid memvalidasi nilai kategori yang diterima dari input user.
func (c AccountCategory) IsValid() bool {
	switch c {
	case CategoryCash, CategoryBank, CategoryOtherAsset, "":
		return true
	default:
		return false
	}
}

// ValidatePaymentAccount memastikan akun layak sebagai tujuan kas/bank:
// bertipe aset, aktif, dan berkategori cash/bank (COA-driven, bukan hardcode).
func ValidatePaymentAccount(a *Account) error {
	if a == nil {
		return ErrAccountNotFound
	}
	if a.Type != domain.AccountAsset {
		return ErrNotCashBankAccount
	}
	if !a.IsActive {
		return ErrAccountInactive
	}
	if !a.Category.IsCashBank() {
		return ErrNotCashBankAccount
	}
	return nil
}

// DefaultCategoryFor menentukan kategori default saat user tidak menyetelnya.
// Aset → other_asset; non-aset → "".
func DefaultCategoryFor(t domain.AccountType) AccountCategory {
	if t == domain.AccountAsset {
		return CategoryOtherAsset
	}
	return ""
}

// seedCategoryFor menentukan kategori untuk akun bawaan saat seeding COA:
// 1-11xx = cash, 1-13xx/14xx/15xx = bank, aset lain = other_asset, non-aset = "".
func seedCategoryFor(code string, t domain.AccountType) AccountCategory {
	if t != domain.AccountAsset {
		return ""
	}
	switch {
	case seedCashCodes[code]:
		return CategoryCash
	case seedBankCodes[code]:
		return CategoryBank
	default:
		return CategoryOtherAsset
	}
}
