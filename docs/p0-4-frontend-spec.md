# P0-4 — Closing Wizard: Frontend Specification

> UI untuk Completion & HPP True-Up. Konsistensi: `product-ux-blueprint.md`
> (badge global, pola halaman, drill-down U4). Persona utama: **akuntan/finance
> manager** yang TIDAK perlu paham internals — wizard memandu; **owner** melihat
> dampak margin. Uji U1: seluruh closing ≤ 5 langkah terpandu.

---

## 1. Halaman / komponen

| # | Lokasi | Komponen |
|---|---|---|
| C1 | `/proyek/[id]` → tab baru **"Penyelesaian"** (workspace proyek) | **Closing Wizard** (stepper 5 langkah) — halaman utama fitur ini |
| C2 | Header halaman proyek | Badge status closing: `—` / `Selesai Konstruksi` / `Biaya Final` / `True-up Diposting` |
| C3 | Dalam C1 | **Tabel Variance** (hasil preview/calculate) |
| C4 | Modal konfirmasi | Finalize / Approve / Post (aksi ireversibel → konfirmasi eksplisit) |
| C5 | Dashboard | Kartu "Perhatian": *Proyek finalized menunggu true-up* · *True-up menunggu approval* |

## 2. Wireframe — Closing Wizard (C1)

```
┌─ Penyelesaian Proyek — LITHOS Villas ────────────────────────────────────────┐
│  ①──────────②──────────③──────────④──────────⑤                              │
│  Tandai     Finalisasi  Hitung     Setujui    Posting                        │
│  Selesai    Biaya       Variance   (Approve)  Jurnal                         │
│  [selesai]  [selesai]   [aktif]    [ ]        [ ]                            │
├──────────────────────────────────────────────────────────────────────────────┤
│  RINGKASAN VARIANCE                                    Basis: Luas Jual (v1) │
│  HPP Budgeted (terjual) : Rp 100.000.000                                     │
│  Biaya Aktual Proyek    : Rp 480.000.000                                     │
│  Penyesuaian HPP        : Rp +20.000.000  ⚠ menambah HPP                     │
│  Unit terjual: 1 · belum terjual: 1                                          │
├──────────────────────────────────────────────────────────────────────────────┤
│  Unit   Kategori  Status     Budgeted        Aktual         Selisih          │
│  A-01   Tanah     ● Terjual  100.000.000     120.000.000    +20.000.000      │
│  B-01   Tanah     ◌ Stok     —               360.000.000    (tetap di stok)  │
│                                                                              │
│  [Hitung Ulang]                                   [Lanjut: Setujui →]        │
│  ℹ Menyetujui membekukan angka; posting membuat jurnal penyesuaian.          │
└──────────────────────────────────────────────────────────────────────────────┘
```

**Per langkah:**
1. **Tandai Selesai** — form kecil: tanggal selesai konstruksi (default hari ini) + penjelasan *"Status proyek menjadi 'completed'; biaya masih bisa dicatat sampai finalisasi."* → `POST /projects/{id}/completion`.
2. **Finalisasi Biaya** — modal konfirmasi C4: *"Biaya aktual dikunci per hari ini (Rp 480jt). Setelah ini penjualan unit baru menunggu true-up selesai."* → `POST .../completion/finalize`. Ireversibel (badge merah kecil "final").
3. **Hitung Variance** — tampilkan preview (`GET .../hpp-variance`) dulu; tombol [Hitung & Simpan] → `POST .../hpp-trueup/calculate`. Boleh diulang selama belum approve.
4. **Setujui** — modal C4 ringkasan angka + siapa menyetujui → `POST /hpp-trueup/{run}/approve`.
5. **Posting Jurnal** — modal C4: tanggal posting (default hari ini; teks D4: *"Jurnal masuk periode berjalan — 'prior period HPP adjustment'"*) → `POST /hpp-trueup/{run}/post` → sukses: tautan **"Lihat Jurnal #M"** (drill-down U4) + banner hijau *"Closing selesai. HPP proyek kini aktual."*

Sekunder: [Batalkan Run] (visible saat draft/calculated/approved) → `POST .../cancel` dengan alasan konfirmasi.

## 3. Tabel Variance (C3)

Kolom: Unit (nama historis snapshot) · Kategori (Tanah/Konstruksi/Soft/Financing — label ID) · Status (badge `● Terjual` biru / `◌ Stok` abu) · Budgeted (kanan) · Aktual (kanan) · Selisih (kanan; hijau bila negatif=mengurangi HPP, oranye bila positif) · [ikon jurnal 🔗 setelah posted → `/accounting/jurnal/{journal_entry_id}`].
Filter: toggle "Hanya unit terjual". Sort default: terjual dulu, lalu unit.

## 4. State / badge / warna (palet global blueprint §6)

| Kondisi | Badge |
|---|---|
| Completion: belum ada | abu "Belum ditandai selesai" |
| `completed` | biru "Selesai Konstruksi" |
| `finalized` | ungu "Biaya Final" (+ oranye "Menunggu True-up" bila belum posted) |
| Run `draft/calculated` | biru "Dihitung" |
| Run `approved` | oranye "Menunggu Posting" |
| Run `posted` | hijau "Diposting" (terminal) |
| Run `cancelled` | abu "Dibatalkan" |

**Empty state** (tab Penyelesaian, proyek berjalan): *"Penyelesaian proyek dilakukan saat konstruksi selesai untuk merekonsiliasi HPP budgeted ke biaya aktual. Belum ada yang perlu dilakukan sekarang."* + tautan dokumentasi singkat. Bila belum ada unit terjual ber-RAB: jelaskan kenapa true-up belum relevan.

**Loading:** skeleton ringkasan + tabel. **Error map:** 422 `ErrCompletionNotFinalized` → arahkan ke langkah 2; 422 `ErrMixedAllocationBasisVersion` → pesan + saran pisah scope; 409 transisi → refresh state wizard (state diambil ulang dari `GET completion` + `GET hpp-trueup`); 422 `ErrNoSoldUnits` → empty state khusus. **Success:** toast per langkah + stepper maju.

**Permission:** viewer = read-only (stepper tanpa tombol); accountant = semua langkah; (backlog P04-B1: approve/post di-gate Approval Workflow). Mobile: ringkasan + status + approve/post (konsumsi & persetujuan — U7); tabel variance scroll horizontal.

## 5. User Flow

**Flow utama (akuntan, akhir proyek):**
Dashboard → kartu "Perhatian: LITHOS menunggu true-up" → tab Penyelesaian →
stepper sudah di langkah 3 → lihat preview → Hitung → (opsional: owner review) →
Setujui → Posting → banner sukses + margin proyek ter-update aktual.

**Flow penjualan pasca-finalisasi (guard D2):** staff mencoba BAST unit →
422 *"proyek sudah difinalisasi: selesaikan & posting HPP true-up dulu"* →
UI BAST menampilkan tautan langsung ke tab Penyelesaian proyek tsb.

**Flow batal:** run keliru → [Batalkan Run] → hitung ulang dari langkah 3
(slot run terbebas — satu run non-cancelled per proyek).

## 6. Dashboard & Report Impact (rangkuman — detail di implementation report §6–7)

- Dashboard: 2 sinyal "Perhatian" baru; margin per proyek menjadi aktual pasca-closing.
- Laporan: L/R proyek & Neraca otomatis benar (jurnal true-up bertag project/unit); drill-down variance → jurnal.

## 7. Endpoint yang dipakai

```
POST /api/v1/projects/{id}/completion              {completed_date?}
POST /api/v1/projects/{id}/completion/finalize
GET  /api/v1/projects/{id}/completion
GET  /api/v1/projects/{id}/hpp-variance            → {basis, totals, sold/unsold, lines[]}
POST /api/v1/projects/{id}/hpp-trueup/calculate    → TrueupRun {status, totals, lines[]}
GET  /api/v1/projects/{id}/hpp-trueup              → TrueupRun[] (riwayat)
GET  /api/v1/hpp-trueup/{runID}                    → TrueupRun + lines
POST /api/v1/hpp-trueup/{runID}/approve | /post {post_date?} | /cancel
```
Semua nominal string desimal; frontend TIDAK menghitung (U8) — selisih & total
datang dari backend.
