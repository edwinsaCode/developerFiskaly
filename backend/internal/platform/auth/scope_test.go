package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"esaproperti/internal/domain"
)

// probe menjalankan satu request lewat Scope dan melaporkan status akhirnya.
// 200 berarti request menembus middleware ke handler di belakangnya.
func probe(t *testing.T, role domain.Role, method, path string) int {
	t.Helper()

	reached := false
	h := Scope("/api/v1")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(method, "/api/v1"+path, nil)
	if role != "" {
		req = req.WithContext(WithClaims(req.Context(), &Claims{
			TenantID: 1, UserID: 1, Role: string(role),
		}))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK && !reached {
		t.Fatalf("status 200 tapi handler tidak pernah dipanggil: %s %s", method, path)
	}
	return rec.Code
}

func TestScope_TanpaClaims401(t *testing.T) {
	if got := probe(t, "", "GET", "/projects"); got != http.StatusUnauthorized {
		t.Fatalf("tanpa claims: mau 401, dapat %d", got)
	}
}

func TestScope_RoleTidakDikenal403(t *testing.T) {
	// Sebelum W-12 role kosong/asing diam-diam dapat akses baca penuh.
	for _, role := range []domain.Role{"", "admin", "superuser", "Owner"} {
		if role == "" {
			continue // ditutup oleh test claims di atas
		}
		if got := probe(t, role, "GET", "/projects"); got != http.StatusForbidden {
			t.Fatalf("role %q: mau 403, dapat %d", role, got)
		}
	}
}

// Owner, accountant, viewer harus lewat persis seperti sebelum W-12 — Scope
// tidak boleh mengubah perilaku satu pun dari ketiganya.
func TestScope_RoleLamaTidakBerubah(t *testing.T) {
	paths := []struct {
		method, path string
	}{
		{"GET", "/ledger/accounts"},
		{"GET", "/ledger/journal-entries"},
		{"GET", "/ap/invoices"},
		{"POST", "/ap/payments"},
		{"GET", "/projects/7/cost-entries"},
		{"GET", "/reports/neraca"},
		{"GET", "/endpoint/yang/belum/ada"},
	}
	for _, role := range []domain.Role{domain.RoleOwner, domain.RoleAccountant, domain.RoleViewer} {
		for _, p := range paths {
			if got := probe(t, role, p.method, p.path); got != http.StatusOK {
				t.Errorf("%s %s %s: mau lewat (200), dapat %d", role, p.method, p.path, got)
			}
		}
	}
}

func TestScope_MarketingDiizinkan(t *testing.T) {
	allowed := []struct {
		method, path string
	}{
		{"GET", "/auth/me"},
		{"GET", "/projects"},
		{"GET", "/projects/12"},
		{"GET", "/projects/12/phases"},
		{"GET", "/projects/12/phases/3/units"},
		{"GET", "/product-types"},
		{"GET", "/units/88"},
		{"GET", "/leads"},
		{"POST", "/leads"},
		{"PUT", "/leads/4"},
		{"POST", "/leads/4/convert"},
		{"GET", "/customers"},
		{"POST", "/customers"},
		{"PUT", "/customers/9"},
		{"GET", "/bookings"},
		{"POST", "/units/88/bookings"},
		{"POST", "/bookings/5/cancel"},
		{"GET", "/units/88/contract"},
		{"POST", "/sale-contracts"},
		{"GET", "/sale-contracts/2/schedule"},
		{"POST", "/sale-contracts/2/schedule"},
		{"POST", "/sale-contracts/2/schedule-regenerate"},
		{"GET", "/sale-contracts/2/statement"},
		{"GET", "/payment-schemes"},
		{"GET", "/financing-sources"},
		{"GET", "/sales-persons"},
		// Pengecualian yang disengaja: daftar akun kas/bank tanpa saldo,
		// dibutuhkan form booking untuk memilih rekening penerima booking fee.
		{"GET", "/ledger/accounts/cash-bank"},
	}
	for _, p := range allowed {
		if got := probe(t, domain.RoleMarketing, p.method, p.path); got != http.StatusOK {
			t.Errorf("marketing %s %s: mau 200, dapat %d", p.method, p.path, got)
		}
	}
}

func TestScope_MarketingDitolak(t *testing.T) {
	denied := []struct {
		method, path, why string
	}{
		{"GET", "/ledger/accounts", "COA penuh"},
		{"POST", "/ledger/accounts", "ubah COA"},
		{"GET", "/ledger/journal-entries", "jurnal"},
		{"GET", "/ledger/journal-entries/9", "detail jurnal"},
		{"GET", "/ledger/trial-balance", "neraca saldo"},
		{"GET", "/ledger/general-ledger", "buku besar"},
		{"GET", "/ap/invoices", "hutang usaha"},
		{"POST", "/ap/invoices", "buat AP"},
		{"GET", "/ap/payments", "daftar pembayaran"},
		{"POST", "/ap/payments", "buat pembayaran"},
		{"POST", "/ap/payments/3/reverse", "balikkan pembayaran"},
		{"GET", "/vendors", "master vendor"},
		{"GET", "/projects/7/cost-entries", "biaya realisasi"},
		{"POST", "/cost-entries", "input biaya"},
		{"GET", "/projects/7/budget-plans", "RAB"},
		{"GET", "/units/88/accumulated-cost", "HPP terakumulasi"},
		{"GET", "/units/88/sale-record", "rincian HPP pada BAST"},
		{"GET", "/reports/neraca", "laporan keuangan"},
		{"GET", "/reports/laba-rugi", "laporan keuangan"},
		{"POST", "/units/88/termins", "terima uang"},
		{"GET", "/units/88/termins", "riwayat penerimaan uang"},
		{"POST", "/schedules/9/received", "terima cicilan"},
		{"POST", "/bookings/5/fee-disposition", "disposisi booking fee"},
		{"GET", "/sale-contracts/2/balance", "posisi keuangan kontrak"},
		{"GET", "/sale-contracts/2/buyer-credit", "saldo kredit pembeli"},
		{"GET", "/collections/payment/preview", "pratinjau terima uang"},
		{"POST", "/collections/payment", "terima uang"},
		{"POST", "/units/88/bast", "BAST"},
		{"GET", "/users", "manajemen user"},
		{"POST", "/users", "buat user"},
		{"GET", "/expenses", "pengeluaran"},
		{"GET", "/documents", "buku dokumen"},
	}
	for _, p := range denied {
		if got := probe(t, domain.RoleMarketing, p.method, p.path); got != http.StatusForbidden {
			t.Errorf("marketing %s %s (%s): mau 403, dapat %d", p.method, p.path, p.why, got)
		}
	}
}

// Wildcard cocok tepat satu segmen. Kalau ia pernah berperilaku sebagai prefix,
// "/units/*" akan membocorkan "/units/5/cost-entries" — biaya, ke marketing.
func TestScope_WildcardBukanPrefix(t *testing.T) {
	leaky := []string{
		"/units/5/cost-entries",
		"/units/5/accumulated-cost",
		"/units/5/journal-entries",
		"/projects/5/budget-plans",
		"/customers/5/statement",
		"/bookings/5/payment",
	}
	for _, path := range leaky {
		if got := probe(t, domain.RoleMarketing, "GET", path); got != http.StatusForbidden {
			t.Errorf("GET %s bocor lewat wildcard: mau 403, dapat %d", path, got)
		}
	}
}

// Method ikut dicocokkan: izin membaca bukan izin menulis.
func TestScope_MethodDicocokkan(t *testing.T) {
	cases := []struct {
		method, path string
	}{
		{"POST", "/projects"},
		{"PUT", "/projects/1"},
		{"DELETE", "/customers/1"},
		{"POST", "/product-types"},
		{"POST", "/payment-schemes"},
		{"DELETE", "/leads/1"},
	}
	for _, c := range cases {
		if got := probe(t, domain.RoleMarketing, c.method, c.path); got != http.StatusForbidden {
			t.Errorf("marketing %s %s: mau 403, dapat %d", c.method, c.path, got)
		}
	}
}

// Daftar-izin gagal-tertutup: endpoint yang belum ada saat baris ini ditulis
// tidak boleh otomatis terbuka untuk marketing.
func TestScope_EndpointBaruTertutupOtomatis(t *testing.T) {
	if got := probe(t, domain.RoleMarketing, "GET", "/fitur-yang-belum-dibuat"); got != http.StatusForbidden {
		t.Fatalf("endpoint tak dikenal: mau 403, dapat %d", got)
	}
}

func TestRoleCapabilities(t *testing.T) {
	cases := []struct {
		role                                       domain.Role
		write, sell, accounting, manageUsers, valid bool
	}{
		{domain.RoleOwner, true, true, true, true, true},
		{domain.RoleAccountant, true, true, true, false, true},
		{domain.RoleMarketing, false, true, false, false, true},
		{domain.RoleViewer, false, false, true, false, true},
		{domain.Role("hantu"), false, false, false, false, false},
	}
	for _, c := range cases {
		if got := c.role.CanWrite(); got != c.write {
			t.Errorf("%s CanWrite: mau %v, dapat %v", c.role, c.write, got)
		}
		if got := c.role.CanSell(); got != c.sell {
			t.Errorf("%s CanSell: mau %v, dapat %v", c.role, c.sell, got)
		}
		if got := c.role.SeesAccounting(); got != c.accounting {
			t.Errorf("%s SeesAccounting: mau %v, dapat %v", c.role, c.accounting, got)
		}
		if got := c.role.CanManageUsers(); got != c.manageUsers {
			t.Errorf("%s CanManageUsers: mau %v, dapat %v", c.role, c.manageUsers, got)
		}
		if got := c.role.Valid(); got != c.valid {
			t.Errorf("%s Valid: mau %v, dapat %v", c.role, c.valid, got)
		}
	}
}
