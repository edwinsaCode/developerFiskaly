package project

// UAT Batch 2 §2 — Product Catalog: master jenis produk developer per tenant
// (rumah, ruko, kavling, kelebihan tanah, PDAM, ...). `units.unit_type` = KODE
// product type; akun pendapatan BAST di-resolve dari mapping ini (COA-driven,
// editable) — TIDAK ada hardcode "rumah"/"4-1000" tersebar.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

// reservedRevenueAccountCodes — akun pendapatan yang punya peran keuangan
// KHUSUS di luar "jual produk katalog" (mis. 4-2000 Pendapatan Lain-lain =
// Pendapatan Luar Usaha, 4-2100 Pendapatan Booking = milik alur booking-fee
// sendiri). Memetakan produk katalog ke akun ini akan membuat pendapatan
// penjualannya salah klasifikasi di Laba Rugi (masuk Luar Usaha, bukan
// Pendapatan inti) — dicegah di sini, bukan cuma "akun revenue aktif".
func reservedRevenueAccountCodes() map[string]bool {
	out := map[string]bool{}
	for _, c := range ledger.RoleCodeList(ledger.RoleOtherIncome) {
		out[c] = true
	}
	for _, c := range ledger.RoleCodeList(ledger.RoleBookingRevenue) {
		out[c] = true
	}
	return out
}

// ProductCategory memisahkan produk properti (ikut HPP/alokasi) dari produk
// non-properti (barang/jasa sederhana, tanpa pool HPP).
//
// H-1: definisi + ATURAN partisipasi (ikut HPP / ikut progress) sekarang tinggal
// di domain (domain.ProductCategory) sebagai satu-satunya sumber kebenaran.
// Alias ini hanya menjaga nama lama tetap terbaca di package project.
type ProductCategory = domain.ProductCategory

const (
	ProductCategoryProperty    = domain.ProductCategoryProperty
	ProductCategoryLand        = domain.ProductCategoryLand
	ProductCategoryNonProperty = domain.ProductCategoryNonProperty
)

// Default mapping akun katalog — SATU tempat (dulu "4-1000" tersebar di
// resolver, service, dan sale.resolveAccounts → L-2).
const (
	DefaultPropertyRevenueAccount    = "4-1000" // Pendapatan Penjualan Unit
	DefaultLandRevenueAccount        = "4-1100" // Pendapatan Kelebihan Tanah (LT-8)
	DefaultNonPropertyRevenueAccount = "4-2000" // Pendapatan Lain-lain
)

// ProductType adalah master satu jenis produk tenant.
type ProductType struct {
	ID                 uint64          `gorm:"primaryKey;autoIncrement"                 json:"id"`
	TenantID           uint64          `gorm:"not null;index"                           json:"-"`
	Code               string          `gorm:"not null;size:50"                         json:"code"`
	Name               string          `gorm:"not null;size:200"                        json:"name"`
	Category           ProductCategory `gorm:"not null;size:20;default:'property'"      json:"category"`
	RevenueAccountCode string          `gorm:"not null;size:20;default:'4-1000'"        json:"revenue_account_code"`
	// TaxCategory (rule klien UAT #3, migration 000092): NULL = produk ini
	// tidak menentukan sendiri tarif PPh Final — proyek (projects.tax_category)
	// tetap sumber penentu (jalur legacy, kode "rumah"). Diisi hanya oleh
	// produk yang MEMANG membedakan subsidi/komersial sebagai jenisnya sendiri
	// (rumah_subsidi/rumah_komersial) — satu proyek boleh menjual keduanya.
	TaxCategory *domain.TaxCategory `gorm:"size:20"                                   json:"tax_category,omitempty"`
	IsActive    bool                `gorm:"not null;default:true"                    json:"is_active"`
	CreatedAt   time.Time           `                                                json:"created_at"`
	UpdatedAt   time.Time           `                                                json:"updated_at"`
}

func (ProductType) TableName() string { return "product_types" }

var (
	ErrProductTypeNotFound  = errors.New("product type tidak ditemukan")
	ErrProductTypeInvalid   = errors.New("code dan name product type wajib diisi; category property|land|non_property")
	ErrProductTypeDuplicate = errors.New("kode product type sudah ada")
	ErrUnitTypeUnknown      = errors.New("unit_type tidak terdaftar di master product type aktif")
	// W-13: produk non-properti (kelebihan tanah, PDAM, listrik) dijual sebagai
	// PRODUK TAMBAHAN pada penjualan unit, bukan sebagai unit tersendiri.
	ErrUnitTypeNotProperty = errors.New("unit_type bukan produk properti: jual sebagai produk tambahan pada unit, bukan sebagai unit sendiri")
	// ErrProductTypeInactive: produk dinonaktifkan — tidak boleh dipakai untuk
	// transaksi baru (fail-closed, bukan fallback ke produk lain).
	ErrProductTypeInactive = errors.New("product type nonaktif — tidak bisa dipakai untuk transaksi baru")
	// ErrRevenueAccountInvalid (H-2): mapping akun pendapatan produk tidak sah.
	ErrRevenueAccountInvalid = errors.New("akun pendapatan produk tidak sah")
)

// ── Store (GORMRepository) ───────────────────────────────────────────────────

func (r *GORMRepository) ListProductTypes(ctx context.Context, tenantID uint64) ([]*ProductType, error) {
	var out []*ProductType
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("category, code").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("ListProductTypes: %w", err)
	}
	return out, nil
}

func (r *GORMRepository) FindProductTypeByCode(ctx context.Context, tenantID uint64, code string) (*ProductType, error) {
	var pt ProductType
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND code = ?", tenantID, code).
		First(&pt).Error
	if err != nil {
		return nil, ErrProductTypeNotFound
	}
	return &pt, nil
}

func (r *GORMRepository) CreateProductType(ctx context.Context, pt *ProductType) error {
	if err := r.db.WithContext(ctx).Create(pt).Error; err != nil {
		if isDuplicateKeyError(err) {
			return ErrProductTypeDuplicate
		}
		return fmt.Errorf("CreateProductType: %w", err)
	}
	return nil
}

func (r *GORMRepository) UpdateProductType(ctx context.Context, tenantID, id uint64, updates map[string]interface{}) (*ProductType, error) {
	res := r.db.WithContext(ctx).Model(&ProductType{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(updates)
	if res.Error != nil {
		return nil, fmt.Errorf("UpdateProductType: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, ErrProductTypeNotFound
	}
	var pt ProductType
	if err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).First(&pt).Error; err != nil {
		return nil, ErrProductTypeNotFound
	}
	return &pt, nil
}

// ── Resolver KANONIK produk (H-1/H-2/M-1) ────────────────────────────────────
//
// FAIL-CLOSED. Tidak ada lagi fallback diam-diam "4-1000": kode yang tidak
// terdaftar, produk nonaktif, mapping akun kosong, atau error DB SEMUANYA
// mengembalikan error sehingga caller (RecordBAST) membatalkan sebelum jurnal
// dibuat. Baris histori pra-katalog sudah dijadikan baris katalog eksplisit
// oleh migration 000058 — fallback tidak lagi diperlukan.

// ResolveProductPolicy mengembalikan kebijakan produk untuk satu kode unit_type.
func (r *GORMRepository) ResolveProductPolicy(ctx context.Context, tenantID uint64, unitType string) (domain.ProductPolicy, error) {
	code := strings.TrimSpace(unitType)
	if code == "" {
		return domain.ProductPolicy{}, fmt.Errorf("%w: unit_type kosong", ErrUnitTypeUnknown)
	}
	var pt ProductType
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND code = ?", tenantID, code).
		First(&pt).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.ProductPolicy{}, fmt.Errorf("%w: %q", ErrUnitTypeUnknown, code)
	}
	if err != nil {
		// M-1: error DB TIDAK boleh menjadi fallback akun — ini jalur uang.
		return domain.ProductPolicy{}, fmt.Errorf("ResolveProductPolicy(%q): %w", code, err)
	}
	return productPolicyOf(&pt)
}

// ResolveUnitProductPolicy mengembalikan kebijakan produk sebuah UNIT (join
// units → product_types). Dipakai guard biaya direct (cost) dan pembaca lain
// yang hanya memegang unit_id.
func (r *GORMRepository) ResolveUnitProductPolicy(ctx context.Context, tenantID, unitID uint64) (domain.ProductPolicy, error) {
	var row struct {
		UnitType string `gorm:"column:unit_type"`
	}
	err := r.db.WithContext(ctx).
		Table("units").Select("unit_type").
		Where("tenant_id = ? AND id = ?", tenantID, unitID).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.ProductPolicy{}, ErrUnitNotFound
	}
	if err != nil {
		return domain.ProductPolicy{}, fmt.Errorf("ResolveUnitProductPolicy(unit=%d): %w", unitID, err)
	}
	return r.ResolveProductPolicy(ctx, tenantID, row.UnitType)
}

// normalizeProductCode menyamakan cara pencocokan kode produk dengan collation
// MySQL (utf8mb4_unicode_ci — case-insensitive) agar map lookup di Go dan
// pencarian di DB tidak pernah berbeda kesimpulan.
func normalizeProductCode(code string) string {
	return strings.ToLower(strings.TrimSpace(code))
}

// productPolicyOf memvalidasi baris master lalu memetakannya ke value object.
func productPolicyOf(pt *ProductType) (domain.ProductPolicy, error) {
	if !pt.IsActive {
		return domain.ProductPolicy{}, fmt.Errorf("%w: %q", ErrProductTypeInactive, pt.Code)
	}
	if !pt.Category.Valid() {
		return domain.ProductPolicy{}, fmt.Errorf("%w: %q kategori %q", ErrProductTypeInvalid, pt.Code, pt.Category)
	}
	if strings.TrimSpace(pt.RevenueAccountCode) == "" {
		return domain.ProductPolicy{}, fmt.Errorf("%w: produk %q belum punya akun pendapatan", ErrRevenueAccountInvalid, pt.Code)
	}
	if pt.TaxCategory != nil && !pt.TaxCategory.Valid() {
		return domain.ProductPolicy{}, fmt.Errorf("%w: %q tax_category %q", ErrProductTypeInvalid, pt.Code, *pt.TaxCategory)
	}
	return domain.ProductPolicy{
		Code:               pt.Code,
		Name:               pt.Name,
		Category:           pt.Category,
		RevenueAccountCode: strings.TrimSpace(pt.RevenueAccountCode),
		TaxCategory:        pt.TaxCategory,
	}, nil
}

// ── Validasi akun pendapatan (H-2) ───────────────────────────────────────────

// accountFacts adalah proyeksi minimal COA yang diperlukan validasi mapping.
type accountFacts struct {
	Type     string `gorm:"column:type"`
	IsActive bool   `gorm:"column:is_active"`
}

// ValidateRevenueAccount memastikan kode akun BOLEH dipakai sebagai akun
// pendapatan produk: ada di COA tenant, aktif, dan bertipe revenue.
// Aset/Liabilitas/Ekuitas/Beban ditolak — mapping salah harus gagal SEBELUM
// ada jurnal, bukan menghasilkan jurnal balanced yang salah semantik.
func (r *GORMRepository) ValidateRevenueAccount(ctx context.Context, tenantID uint64, code string) error {
	c := strings.TrimSpace(code)
	if c == "" {
		return fmt.Errorf("%w: kode akun kosong", ErrRevenueAccountInvalid)
	}
	var f accountFacts
	err := r.db.WithContext(ctx).
		Table("accounts").Select("type, is_active").
		Where("tenant_id = ? AND code = ?", tenantID, c).
		Take(&f).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("%w: akun %q tidak ada di COA", ErrRevenueAccountInvalid, c)
	}
	if err != nil {
		return fmt.Errorf("ValidateRevenueAccount(%q): %w", c, err)
	}
	if !f.IsActive {
		return fmt.Errorf("%w: akun %q nonaktif", ErrRevenueAccountInvalid, c)
	}
	if domain.AccountType(f.Type) != domain.AccountRevenue {
		return fmt.Errorf("%w: akun %q bertipe %s — hanya akun pendapatan (revenue) yang boleh dipetakan",
			ErrRevenueAccountInvalid, c, f.Type)
	}
	if reservedRevenueAccountCodes()[c] {
		return fmt.Errorf("%w: akun %q dipakai khusus (Pendapatan Luar Usaha/Booking) — pilih akun pendapatan katalog lain",
			ErrRevenueAccountInvalid, c)
	}
	return nil
}

// ── Seed ─────────────────────────────────────────────────────────────────────

// defaultProductTypes: katalog default (cermin seed migration 000057, dipangkas
// migration 000062, kelebihan_tanah dipindah ke kategori `land` oleh migration
// 000083 — LT-8). Mapping akun default EDITABLE per tenant (COA-driven).
//
// Keputusan bisnis FINAL klien 2026-08-06 — produk yang DIJUAL hanya tiga:
// Rumah, Ruko, Kelebihan Tanah. Dua kode sengaja TIDAK ada di sini lagi:
//
//	kavling — "Tidak ada Kavling." Bukan produk yang dijual perusahaan ini.
//	pdam    — PDAM bukan produk ber-pendapatan, melainkan JENIS BIAYA REALISASI
//	          (titipan). Rumahnya sekarang di charge.defaultChargeTypes → 2-2400.
//
// Keduanya TIDAK dihapus dari DB tenant lama, hanya dinonaktifkan (000062) —
// data historis harus tetap bisa dibaca. Tenant BARU tidak pernah menerimanya.
//
// Kelebihan Tanah kini `land`, bukan `non_property` (LT-8,
// docs/kelebihan-tanah-final-architecture-2026-08.md §G): produk ini dijual
// lewat jalur standalone land_stock/land_sales (LT-3+), bukan lagi lewat jalur
// addon lama — resolveAddonProduct (internal/charge/addon.go) menolak kategori
// ini secara fail-closed via ParticipatesInHPP(). Tenant BARU sejak migration
// 000083 langsung mendapat baris yang sudah benar; tidak ada lagi jalur addon
// aktif untuk produk ini sejak awal.
// TODO(tax-advisor): akun & perlakuan PPN utk kelebihan tanah.
//
// rumah_subsidi/rumah_komersial (migration 000092, rule klien UAT #3): SUBSIDI
// dan KOMERSIAL adalah klasifikasi PRODUK, bukan sekadar skema KPR — HPP/akun
// pendapatan bisa sama, tapi tarif PPh Final (1% vs 2,5%) mengikuti PRODUK unit
// tsb, bukan hanya proyeknya (satu proyek boleh menjual keduanya). Kode legacy
// "rumah" (TaxCategory nil) TETAP ada dan TIDAK berubah perilaku — unit lama
// yang masih memakainya terus mengikuti tax_category PROYEK seperti sebelumnya.
func taxCategoryPtr(c domain.TaxCategory) *domain.TaxCategory { return &c }

var defaultProductTypes = []ProductType{
	{Code: "rumah", Name: "Rumah", Category: ProductCategoryProperty, RevenueAccountCode: DefaultPropertyRevenueAccount},
	{Code: "rumah_subsidi", Name: "Rumah Subsidi", Category: ProductCategoryProperty, RevenueAccountCode: DefaultPropertyRevenueAccount, TaxCategory: taxCategoryPtr(domain.TaxCategorySubsidi)},
	{Code: "rumah_komersial", Name: "Rumah Komersial", Category: ProductCategoryProperty, RevenueAccountCode: DefaultPropertyRevenueAccount, TaxCategory: taxCategoryPtr(domain.TaxCategoryKomersial)},
	{Code: "ruko", Name: "Ruko", Category: ProductCategoryProperty, RevenueAccountCode: DefaultPropertyRevenueAccount},
	{Code: "kelebihan_tanah", Name: "Kelebihan Tanah", Category: ProductCategoryLand, RevenueAccountCode: DefaultLandRevenueAccount},
}

// SeedDefaultProductTypes menyemai katalog default utk tenant BARU (idempoten).
func SeedDefaultProductTypes(ctx context.Context, db *gorm.DB, tenantID uint64) error {
	for _, pt := range defaultProductTypes {
		row := pt
		row.TenantID = tenantID
		row.IsActive = true
		if err := db.WithContext(ctx).
			Where("tenant_id = ? AND code = ?", tenantID, row.Code).
			FirstOrCreate(&row).Error; err != nil {
			return fmt.Errorf("seed product type %s: %w", row.Code, err)
		}
	}
	return nil
}

// ── Service ──────────────────────────────────────────────────────────────────

// ProductTypeStore adalah kebutuhan service atas store product type.
type ProductTypeStore interface {
	ListProductTypes(ctx context.Context, tenantID uint64) ([]*ProductType, error)
	FindProductTypeByCode(ctx context.Context, tenantID uint64, code string) (*ProductType, error)
	CreateProductType(ctx context.Context, pt *ProductType) error
	UpdateProductType(ctx context.Context, tenantID, id uint64, updates map[string]interface{}) (*ProductType, error)
	// ValidateRevenueAccount menegakkan H-2: mapping akun pendapatan hanya boleh
	// menunjuk akun revenue yang ada dan aktif di COA tenant.
	ValidateRevenueAccount(ctx context.Context, tenantID uint64, code string) error
}

// SetProductTypeStore memasang store product type (wiring produksi; opsional —
// nil = validasi unit_type dilewati, perilaku legacy utk unit test lama).
func (s *Service) SetProductTypeStore(st ProductTypeStore) { s.productTypes = st }

func (s *Service) ListProductTypes(ctx context.Context, tenantID uint64) ([]*ProductType, error) {
	if s.productTypes == nil {
		return []*ProductType{}, nil
	}
	return s.productTypes.ListProductTypes(ctx, tenantID)
}

func (s *Service) CreateProductType(ctx context.Context, tenantID uint64, pt *ProductType) (*ProductType, error) {
	if s.productTypes == nil {
		return nil, errors.New("product type store belum dikonfigurasi")
	}
	pt.Code = normalizeProductCode(pt.Code)
	if pt.Code == "" || strings.TrimSpace(pt.Name) == "" || !pt.Category.Valid() {
		return nil, ErrProductTypeInvalid
	}
	if strings.TrimSpace(pt.RevenueAccountCode) == "" {
		pt.RevenueAccountCode = DefaultPropertyRevenueAccount
	}
	pt.RevenueAccountCode = strings.TrimSpace(pt.RevenueAccountCode)
	// H-2: mapping akun divalidasi SEBELUM disimpan — bukan saat BAST.
	if err := s.productTypes.ValidateRevenueAccount(ctx, tenantID, pt.RevenueAccountCode); err != nil {
		return nil, err
	}
	pt.TenantID = tenantID
	pt.IsActive = true
	if err := s.productTypes.CreateProductType(ctx, pt); err != nil {
		return nil, err
	}
	return pt, nil
}

func (s *Service) UpdateProductType(ctx context.Context, tenantID, id uint64, name, revenueAccountCode *string, isActive *bool, taxCategory *string) (*ProductType, error) {
	if s.productTypes == nil {
		return nil, errors.New("product type store belum dikonfigurasi")
	}
	updates := map[string]interface{}{}
	if name != nil && strings.TrimSpace(*name) != "" {
		updates["name"] = strings.TrimSpace(*name)
	}
	if revenueAccountCode != nil && strings.TrimSpace(*revenueAccountCode) != "" {
		code := strings.TrimSpace(*revenueAccountCode)
		// H-2: berlaku sama untuk perubahan mapping, bukan hanya saat dibuat.
		if err := s.productTypes.ValidateRevenueAccount(ctx, tenantID, code); err != nil {
			return nil, err
		}
		updates["revenue_account_code"] = code
	}
	if isActive != nil {
		updates["is_active"] = *isActive
	}
	// TaxCategory (rule klien UAT #3): "" mengosongkan (kembali ke jalur legacy
	// projects.tax_category), nilai lain harus subsidi|komersial.
	if taxCategory != nil {
		trimmed := strings.TrimSpace(*taxCategory)
		if trimmed == "" {
			updates["tax_category"] = nil
		} else {
			tc := domain.TaxCategory(trimmed)
			if !tc.Valid() {
				return nil, ErrProductTypeInvalid
			}
			updates["tax_category"] = string(tc)
		}
	}
	if len(updates) == 0 {
		return nil, ErrProductTypeInvalid
	}
	return s.productTypes.UpdateProductType(ctx, tenantID, id, updates)
}

// validateUnitType (dipakai CreateUnit/BulkCreateUnits): unit BARU wajib
// memakai product type aktif dari master. Store nil → lewati (legacy test).
func (s *Service) validateUnitType(ctx context.Context, tenantID uint64, unitType string) error {
	if s.productTypes == nil {
		return nil
	}
	pt, err := s.productTypes.FindProductTypeByCode(ctx, tenantID, unitType)
	if err != nil || !pt.IsActive {
		return fmt.Errorf("%w: %q", ErrUnitTypeUnknown, unitType)
	}
	// W-13: hanya produk PROPERTI yang boleh menjadi unit.
	//
	// Unit bukan sekadar baris di papan penjualan — ia penerima alokasi biaya
	// proyek dan pemilik HPP. PDAM/listrik tidak pernah menerima alokasi, dan
	// Kelebihan Tanah (kategori `land`, LT-1) memang ikut HPP tapi TIDAK PERNAH
	// jadi unit (standalone product, land_stock/land_sales — LT-3+). Unit
	// semacam itu akan terjual dengan HPP nol atau salah tempel: laba yang
	// tidak kelihatan salah di laporan mana pun. Sengaja pakai MayBecomeUnit(),
	// BUKAN ParticipatesInHPP() — sejak `land` ada, keduanya tidak lagi sama:
	// land ikut HPP tapi tidak boleh jadi unit. Tempatnya kategori non-unit
	// adalah produk tambahan pada penjualan unit (charge/addon.go) atau,
	// untuk `land`, jalur standalone LT-3+.
	if !pt.Category.MayBecomeUnit() {
		return fmt.Errorf("%w: %q", ErrUnitTypeNotProperty, unitType)
	}
	return nil
}
