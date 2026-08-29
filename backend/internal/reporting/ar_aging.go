package reporting

import (
	"context"
	"errors"
	"sort"
	"time"

	"esaproperti/internal/receivable"
)

// ErrARReaderNotConfigured dikembalikan bila GetARAging dipanggil tanpa
// memasang HouseReceivableReader lebih dulu (lihat Service.WithHouseReader).
var ErrARReaderNotConfigured = errors.New("reporting: AR aging reader belum dikonfigurasi")

// BuildARAging menyusun laporan Piutang Customer dari baris kewajiban mentah.
//
// W-4: mesinnya sekarang tinggal di paket `receivable` (INV-AR-1 — satu mesin
// bucketing untuk seluruh repo). Fungsi ini tetap ada sebagai nama yang sudah
// dipakai luas di dalam reporting; ia TIDAK menghitung apa pun sendiri.
func BuildARAging(rows []ARScheduleRow, asOf time.Time) ARAgingReport {
	return receivable.BuildAging(rows, asOf)
}

// ── Service method ────────────────────────────────────────────────────────────

// GetARAging menyusun eksposur piutang customer per asOf.
//
// W-4 (D-W4-4): SATU laporan untuk seluruh yang ditagih ke customer — cicilan
// harga rumah DAN tagihan biaya realisasi. Sebelumnya keduanya hidup di dua
// endpoint yang tidak pernah dijumlahkan, sehingga tidak ada satu angka pun di
// sistem yang menjawab "customer ini berutang berapa".
//
// Sumber realisasi bersifat opsional: tenant yang belum memakai Charge Group
// tetap mendapat laporan yang benar, dan kegagalan pembacanya TIDAK boleh
// menelan piutang harga rumah — ia dikembalikan sebagai error, bukan diam-diam
// dihilangkan dari total.
//
// src menyaring sumber; kosong / tidak dikenal berarti "semua".
func (s *Service) GetARAging(ctx context.Context, tenantID uint64, asOf time.Time, src receivable.Source) (*ARAgingReport, error) {
	if s.house == nil {
		return nil, ErrARReaderNotConfigured
	}

	var rows []receivable.Row

	if wantsSource(src, receivable.SourceHouse) {
		houseRows, err := s.house.ReceivableRows(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		rows = append(rows, houseRows...)
	}

	// Satu pembaca, dua sumber: charge_items memuat baris realisasi DAN produk
	// tambahan (W-13). Filter akhir BuildAging yang memisahkannya — kalau
	// pembacanya hanya dipanggil saat src=realization, memfilter "addon" akan
	// mengembalikan daftar kosong padahal piutangnya ada.
	if (wantsSource(src, receivable.SourceRealization) || wantsSource(src, receivable.SourceAddon)) && s.realization != nil {
		realRows, err := s.realization.ReceivableRows(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		rows = append(rows, realRows...)
	}

	if wantsSource(src, receivable.SourceLegacy) && s.legacy != nil {
		legacyRows, err := s.legacy.ReceivableRows(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		rows = append(rows, legacyRows...)
	}

	// Urut jatuh tempo menaik: baris rumah dan realisasi BERBAUR. Mengelompokkan
	// kembali per sumber akan mengembalikan dua daftar yang justru dihapus W-4.
	sortByDueDate(rows)

	rpt := receivable.BuildAging(receivable.FilterBySource(rows, src), asOf)
	return &rpt, nil
}

// wantsSource melaporkan apakah pembaca untuk sumber s perlu dipanggil.
//
// Ditulis positif ("apakah diminta") dan bukan negatif ("src != X"): dengan dua
// sumber kedua bentuk itu setara, dengan tiga sumber bentuk negatif diam-diam
// jadi salah — `src != SourceHouse` ikut membolehkan `legacy` masuk ke daftar
// realisasi. Filter akhir di BuildAging akan membuangnya lagi, jadi bug-nya
// tidak kelihatan di angka; yang terjadi hanya query yang tidak pernah dipakai.
func wantsSource(src, s receivable.Source) bool {
	if !src.Valid() {
		return true // kosong / tidak dikenal = semua
	}
	return src == s
}

// sortByDueDate mengurutkan baris jatuh tempo menaik, ties dipecah oleh sumber
// lalu RefID supaya tampilan deterministik (laporan keuangan tidak boleh
// berubah susunan tiap kali dimuat).
func sortByDueDate(rows []receivable.Row) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if !a.DueDate.Equal(b.DueDate) {
			return a.DueDate.Before(b.DueDate)
		}
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		return a.RefID < b.RefID
	})
}
