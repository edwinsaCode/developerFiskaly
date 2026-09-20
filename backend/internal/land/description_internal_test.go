package land

import "testing"

// Readability accounting (2026-09-18): deskripsi Jurnal Akad Kelebihan Tanah
// (jalur bundled dengan Akad unit rumah) harus menunjukkan kode Unit kanonik.
// describeWithUnit adalah duplikat sengaja dari sale.DescribeWithUnit — land
// tidak boleh impor sale (sale sudah impor land, akan jadi circular import).
func TestDescribeWithUnit(t *testing.T) {
	cases := []struct {
		name     string
		base     string
		project  string
		unitCode string
		want     string
	}{
		{"kode tersedia (bundled dgn Akad unit)", "Akad Kelebihan Tanah proyek 9", "Nata Alam", "A-15", "Akad Kelebihan Tanah proyek 9 — Proyek Nata Alam — Unit A-15"},
		{"kode kosong dipertahankan apa adanya (standalone, ketentuan #6)", "Akad Kelebihan Tanah proyek 9", "Nata Alam", "", "Akad Kelebihan Tanah proyek 9"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := describeWithUnit(c.base, c.project, c.unitCode)
			if got != c.want {
				t.Errorf("describeWithUnit(%q, %q, %q) = %q, want %q", c.base, c.project, c.unitCode, got, c.want)
			}
		})
	}
}
