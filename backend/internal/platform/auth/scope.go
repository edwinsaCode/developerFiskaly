package auth

import (
	"net/http"
	"strings"

	"esaproperti/internal/domain"
)

// ── W-12: batas baca per-role, ditegakkan di router ───────────────────────────
//
// Sampai W-11 sistem ini tidak punya otorisasi BACA sama sekali: setiap GET
// terbuka untuk setiap role yang terautentikasi. Menambah role Marketing
// membuat lubang itu tak bisa dibiarkan — marketing tidak boleh membaca jurnal,
// kas, COA, hutang usaha, biaya, anggaran, atau laporan keuangan.
//
// Dua cara menutupnya, dan pilihan di sini disengaja:
//
//	(a) tempel guard di setiap rute sensitif — ~200 rute di 20 paket. Setiap
//	    endpoint baru yang lupa dijaga akan terbuka diam-diam. Gagal terbuka.
//	(b) satu daftar-izin di depan router: marketing hanya boleh menyentuh apa
//	    yang tertulis, sisanya 403. Endpoint baru otomatis tertutup untuk
//	    marketing sampai seseorang sengaja membukanya. Gagal tertutup.
//
// (b) yang dipakai. Konsekuensinya jujur: menambah layar baru untuk marketing
// berarti menambah baris di sini — dan itu memang harus menjadi keputusan
// sadar, bukan efek samping.
//
// Yang TIDAK dilakukan lapisan ini: mengganti RequireWrite. Guard tulis lama
// tetap di tempatnya; ini hanya lapisan tambahan di atasnya. Owner, accountant,
// dan viewer melewatinya tanpa perubahan perilaku sama sekali.

// scopeRule is one allowed (method, path-shape) pair. A "*" segment matches
// exactly one path segment — never a prefix, so "/units/*" can never
// accidentally let "/units/5/cost-entries" through.
type scopeRule struct {
	method string
	segs   []string
}

func rule(method, path string) scopeRule {
	return scopeRule{method: method, segs: splitPath(path)}
}

func splitPath(p string) []string {
	return strings.Split(strings.Trim(p, "/"), "/")
}

func (s scopeRule) matches(method string, segs []string) bool {
	if s.method != method || len(s.segs) != len(segs) {
		return false
	}
	for i, want := range s.segs {
		if want != "*" && want != segs[i] {
			return false
		}
	}
	return true
}

// marketingAllow adalah SELURUH yang boleh disentuh role marketing.
// Apa pun di luar daftar ini dibalas 403 — termasuk endpoint yang belum ada
// saat baris ini ditulis.
var marketingAllow = []scopeRule{
	// Identitas diri. Setiap role harus bisa tahu ia sedang login sebagai siapa.
	rule("GET", "/auth/me"),

	// ── Katalog yang dijual: proyek, fase, unit, tipe produk (baca saja) ──
	rule("GET", "/projects"),
	rule("GET", "/projects/*"),
	rule("GET", "/projects/*/transitions"),
	rule("GET", "/projects/*/phases"),
	rule("GET", "/projects/*/phases/*"),
	rule("GET", "/projects/*/phases/*/units"),
	rule("GET", "/projects/*/units"),
	rule("GET", "/product-types"),
	rule("GET", "/units/*"),
	rule("GET", "/units/*/transitions"),

	// ── Prospek (CRM) ──
	rule("GET", "/leads"),
	rule("GET", "/leads/*"),
	rule("POST", "/leads"),
	rule("PUT", "/leads/*"),
	rule("POST", "/leads/*/convert"),

	// ── Pelanggan ──
	rule("GET", "/customers"),
	rule("GET", "/customers/*"),
	rule("POST", "/customers"),
	rule("PUT", "/customers/*"),

	// ── Booking ──
	rule("GET", "/bookings"),
	rule("GET", "/bookings/*"),
	rule("GET", "/units/*/booking"),
	rule("POST", "/units/*/bookings"),
	rule("POST", "/bookings/*/cancel"),

	// ── Kontrak penjualan & jadwal pembayaran ──
	rule("GET", "/units/*/contract"),
	// GET /units/*/sale-record TIDAK di sini: payload-nya memuat rincian HPP
	// (tanah, konstruksi, biaya lunak, pendanaan) — itu data biaya, bukan data
	// penjualan. Marketing mengetahui unit sudah serah terima dari status unit.
	rule("POST", "/sale-contracts"),
	rule("GET", "/sale-contracts/*/schedule"),
	rule("POST", "/sale-contracts/*/schedule"),
	rule("GET", "/sale-contracts/*/schedule-plan"),
	rule("POST", "/sale-contracts/*/schedule-regenerate"),
	// Statement kontrak: keputusan pemilik — marketing perlu menjawab
	// "sudah bayar berapa?" untuk pembelinya sendiri. Ini rekening satu
	// pembeli, bukan posisi keuangan perusahaan. Balance, financial-summary,
	// buyer-credit, dan payment-events TIDAK ikut.
	rule("GET", "/sale-contracts/*/statement"),

	// ── Master data yang dibutuhkan form penjualan (baca saja) ──
	rule("GET", "/payment-schemes"),
	rule("GET", "/payment-schemes/*"),
	rule("GET", "/financing-sources"),
	rule("GET", "/financing-sources/*"),
	rule("GET", "/sales-persons"),
	rule("GET", "/sales-persons/*"),
	rule("GET", "/sales-teams"),
	rule("GET", "/sales-teams/*"),

	// ── Satu pengecualian yang disengaja ──
	//
	// Booking fee diterima saat booking dibuat, jadi form booking harus memilih
	// akun kas/bank penerimanya. Endpoint ini mengembalikan kode, nama, dan
	// kategori akun — TIDAK ada saldo di dalamnya (lihat ledger.Account: tak
	// punya field saldo sama sekali). Jadi marketing tahu ada rekening BCA,
	// tidak pernah tahu isinya. Larangan "melihat saldo kas/bank" tetap utuh.
	//
	// GET /ledger/accounts (COA penuh), jurnal, buku besar, dan neraca saldo
	// tetap 403 — tidak ada di daftar ini.
	rule("GET", "/ledger/accounts/cash-bank"),
}

// Scope enforces the per-role read boundary. Place it directly after Middleware,
// once, at the top of the authenticated router group.
//
// Roles other than marketing pass through untouched — their authorization is
// still the RequireWrite / RequireRole guards on individual routes.
func Scope(apiPrefix string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			roleStr, ok := RoleFrom(r.Context())
			if !ok {
				writeAuthError(w, http.StatusUnauthorized, "missing auth context")
				return
			}
			role := domain.Role(roleStr)

			// Token dengan role yang tidak dikenal tidak diberi hak apa pun.
			// Sebelumnya role kosong diam-diam mendapat akses baca penuh.
			if !role.Valid() {
				writeAuthError(w, http.StatusForbidden, "role tidak dikenal")
				return
			}
			if role != domain.RoleMarketing {
				next.ServeHTTP(w, r)
				return
			}

			path := strings.TrimPrefix(r.URL.Path, apiPrefix)
			segs := splitPath(path)
			for _, ru := range marketingAllow {
				if ru.matches(r.Method, segs) {
					next.ServeHTTP(w, r)
					return
				}
			}
			writeAuthError(w, http.StatusForbidden,
				"role marketing tidak punya akses ke data ini")
		})
	}
}

// RequireSalesWrite permits the sales workflow: owner, accountant, marketing.
// Use it for writes that belong to selling — leads, customers, bookings, sale
// contracts, payment schedules — and never for anything that moves cash.
func RequireSalesWrite() func(http.Handler) http.Handler {
	return RequireRole(
		string(domain.RoleOwner),
		string(domain.RoleAccountant),
		string(domain.RoleMarketing),
	)
}
