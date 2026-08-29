package tax

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"esaproperti/internal/platform/auth"
)

// Modul pajak dulu adalah satu-satunya modul yang route tulisnya tidak memakai
// auth.RequireWrite. Marketing tetap tertolak karena allow-list router, tetapi
// Viewer — peran baca — lolos sampai ke business logic dan bisa memasang tarif
// serta memposting jurnal akrual/pelunasan pajak.
//
// Test ini menjaga penjaga itu. Ia sengaja tidak menyentuh database: kalau
// permintaan sampai ke handler, handler akan gagal karena Service nil — dan
// itulah bedanya "ditolak" dengan "diteruskan".
func mountTaxRoutes() http.Handler {
	h := &Handler{} // svc nil: hanya route yang diuji, bukan business logic
	r := chi.NewRouter()
	h.Mount(r)
	return r
}

// requestAs mengembalikan status HTTP, atau 0 bila permintaan sampai ke
// handler dan panik di sana karena Service/repository memang nil. Nol berarti
// "diteruskan" — dan untuk test ini itu jawaban yang sah, bukan kegagalan.
func requestAs(t *testing.T, role, method, path string) (code int) {
	t.Helper()
	defer func() {
		if recover() != nil {
			code = 0
		}
	}()
	req := httptest.NewRequest(method, path, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	if role != "" {
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{
			TenantID: 1, UserID: 1, Role: role,
		}))
	}
	rec := httptest.NewRecorder()
	mountTaxRoutes().ServeHTTP(rec, req)
	return rec.Code
}

func TestTaxWriteRoutesRequireWriteRole(t *testing.T) {
	writeRoutes := []string{
		"/tax/tax-rates",
		"/units/7/tax/accrue",
		"/tax/obligations/7/pay",
	}
	for _, path := range writeRoutes {
		for _, role := range []string{"viewer", "marketing", "peran-tak-dikenal", ""} {
			got := requestAs(t, role, http.MethodPost, path)
			if got != http.StatusForbidden {
				t.Errorf("POST %s sebagai role %q: dapat %d, mau 403", path, role, got)
			}
		}
	}
}

func TestTaxReadRoutesTetapTerbukaUntukViewer(t *testing.T) {
	// Penjaga tulis tidak boleh ikut menutup jalur baca: auditor dan pemilik
	// yang hanya membaca tetap perlu melihat tarif dan kewajiban pajak.
	for _, path := range []string{"/tax/tax-rates", "/tax/obligations/7"} {
		if got := requestAs(t, "viewer", http.MethodGet, path); got == http.StatusForbidden {
			t.Errorf("GET %s sebagai viewer: 403, seharusnya tidak diblokir peran", path)
		}
	}
}
