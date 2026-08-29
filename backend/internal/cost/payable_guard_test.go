package cost

// T-9 — penjaga jalur hutang usaha di gerbang HTTP biaya proyek/tenant.
//
// KONTEKS. Pemetaan domainnya sudah benar dan sengaja TIDAK diubah:
// resolveDebitCreditCodes memetakan payment_method=payable → kredit 2-1000
// Hutang Usaha, dan test pemetaan itu (service_test.go, tier_test.go,
// budget_link_test.go) tetap hijau. Yang belum ada adalah sisi DEBIT-nya —
// tidak satu pun kode di repo ini yang pernah mendebit 2-1000. Saldo yang lahir
// lewat HTTP karena itu tidak bisa dilunasi dari layar mana pun, dan karena
// ledger append-only satu-satunya jalan keluarnya adalah jurnal pembalik.
//
// /expenses sudah menolak sejak W-10. Berkas ini membuktikan gerbang LAMA —
// /projects/{id}/cost-entries dan /cost-entries — kini menolak dengan alasan
// yang sama, dan bahwa bank/kas tidak ikut tertutup.
//
// Test ini INTERNAL (package cost) supaya bisa memasang Handler tanpa database:
// penjaganya berada di decodeCostDTO, sebelum service atau DB pernah disentuh —
// sifat itu sendiri yang dibuktikan di sini dengan svc/types sengaja nil.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"esaproperti/internal/platform/auth"
)

// payableGuardRouter memasang rute biaya dengan Handler TANPA service dan TANPA
// DB. Kalau sebuah request pernah lolos sampai service, test-nya panic — itu
// justru bukti yang dicari: penjaga harus menolak sebelum lapisan mana pun.
func payableGuardRouter() http.Handler {
	h := &Handler{} // svc == nil, types == nil — disengaja
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			claims := &auth.Claims{TenantID: 1, UserID: 1, Role: "owner"}
			next.ServeHTTP(w, req.WithContext(auth.WithClaims(req.Context(), claims)))
		})
	})
	h.Mount(r)
	return r
}

func postJSON(t *testing.T, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	payableGuardRouter().ServeHTTP(rec, req)
	return rec
}

func errorBody(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body bukan JSON: %v (%s)", err, rec.Body.String())
	}
	return resp["error"]
}

const payableBody = `{
	"category": "land",
	"cost_tier": "shared",
	"amount": "100000000",
	"payment_method": "payable",
	"date": "2026-08-14",
	"vendor": "PT Kontraktor Sejahtera",
	"description": "Termin 1 pekerjaan struktur"
}`

// Seluruh gerbang lama yang menerima payment_method — create DAN preview.
// Preview ikut dijaga karena pratinjau yang menampilkan jurnal yang akan
// ditolak create adalah kebohongan yang lebih halus daripada menerimanya.
var payableGuardRoutes = []struct {
	name string
	path string
}{
	{"project create", "/projects/7/cost-entries"},
	{"project preview", "/projects/7/cost-entries/preview"},
	{"tenant create", "/cost-entries"},
	{"tenant preview", "/cost-entries/preview"},
}

func TestPayableRejected_OnLegacyCostEndpoints(t *testing.T) {
	for _, tc := range payableGuardRoutes {
		t.Run(tc.name, func(t *testing.T) {
			rec := postJSON(t, tc.path, payableBody)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body: %s)", rec.Code, rec.Body.String())
			}
			got := errorBody(t, rec)
			if got != ErrPayableNotSupported.Error() {
				t.Errorf("error = %q, want %q", got, ErrPayableNotSupported.Error())
			}
			// Alasannya harus terbaca manusia, bukan sekadar "invalid": staf yang
			// ditolak perlu tahu apa yang harus dilakukan sebagai gantinya.
			if !strings.Contains(got, "tunai/bank") {
				t.Errorf("pesan tidak menyebut jalan keluar yang sah: %q", got)
			}
		})
	}
}

// Penjaga harus menolak SEBELUM validasi lain. Kalau ia dipasang belakangan,
// payload payable dengan tanggal/nominal ngawur akan ditolak dengan alasan yang
// salah — dan pembuat integrasi menyimpulkan payable sebenarnya diterima.
func TestPayableRejected_BeforeOtherValidation(t *testing.T) {
	rec := postJSON(t, "/projects/7/cost-entries", `{
		"category": "bukan-kategori",
		"amount": "bukan-angka",
		"payment_method": "payable",
		"date": "kemarin"
	}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if got := errorBody(t, rec); got != ErrPayableNotSupported.Error() {
		t.Errorf("error = %q, want %q", got, ErrPayableNotSupported.Error())
	}
}

// Yang TIDAK boleh ikut tertutup: bank dan kas. Payload bank harus lolos
// penjaga ini dan diteruskan ke lapisan berikutnya. Karena Handler sengaja
// tanpa service, "lolos" terlihat dalam dua bentuk yang sama-sah: rute proyek
// menembus sampai h.svc (panic nil, dipulihkan), rute tenant berhenti lebih awal
// pada validasi lain (project_id wajib). Yang dibuktikan bukan sampai ke mana
// eksekusinya, melainkan bahwa yang menghentikannya BUKAN penjaga payable.
func TestBankPathUntouchedByGuard(t *testing.T) {
	for _, tc := range payableGuardRoutes {
		t.Run(tc.name, func(t *testing.T) {
			reached := reachesService(t, tc.path, `{
				"category": "land",
				"cost_tier": "shared",
				"amount": "100000000",
				"payment_method": "bank",
				"bank_account_code": "1-1300",
				"date": "2026-08-14",
				"description": "Termin 1 dibayar bank"
			}`)
			if !reached {
				t.Error("payload bank ikut tertahan penjaga payable — regresi")
			}
		})
	}
}

// reachesService melaporkan apakah request berhasil melewati decodeCostDTO dan
// mencapai service (yang nil, sehingga panic). Panic-nya dipulihkan di sini.
func reachesService(t *testing.T, path, body string) (reached bool) {
	t.Helper()
	defer func() {
		if recover() != nil {
			reached = true
		}
	}()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	payableGuardRouter().ServeHTTP(rec, req)

	// Tidak panic: pastikan setidaknya bukan penolakan payable yang menghentikannya.
	if rec.Code == http.StatusBadRequest &&
		strings.Contains(rec.Body.String(), "hutang usaha belum didukung") {
		return false
	}
	return true
}

// Pemetaan domain payable → 2-1000 HARUS tetap utuh: W-11 dibangun di atasnya.
// Penjaga di atas adalah pintu transport, bukan penghapusan domain.
func TestPayableDomainMappingPreserved(t *testing.T) {
	if !PaymentMethodPayable.Valid() {
		t.Fatal("PaymentMethodPayable tidak lagi valid — domain AP terhapus, bukan dijaga")
	}
	_, credit := resolveDebitCreditCodes("shared", CreateCostEntryRequest{
		PaymentMethod: PaymentMethodPayable,
	})
	if credit != "2-1000" {
		t.Errorf("kredit payable = %q, want 2-1000 (pemetaan domain harus utuh untuk W-11)", credit)
	}
}

// Konteks tenant tetap syarat pertama: penjaga payable tidak boleh menjadi
// jalan pintas yang menjawab sebelum autentikasi diperiksa.
func TestPayableGuard_DoesNotBypassAuth(t *testing.T) {
	h := &Handler{}
	r := chi.NewRouter()
	h.Mount(r) // tanpa middleware klaim

	req := httptest.NewRequest(http.MethodPost, "/projects/7/cost-entries/preview",
		strings.NewReader(payableBody)).WithContext(context.Background())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (body: %s)", rec.Code, rec.Body.String())
	}
}
