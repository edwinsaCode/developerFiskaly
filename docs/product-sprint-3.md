# Product Sprint 3 — Sales Workspace + CRM — SHIPPED

> Status: **SHIPPED + diverifikasi visual (Chrome) & E2E API.** 2026-07-21.
> Nilai bisnis: workflow marketing yang Jurnal.id TIDAK punya — funnel
> Lead → Customer → Booking → Reservasi → PPJB → BAST → Komisi dalam SATU
> workspace dengan KPI, timeline, dan drill-down.

## 1. Backend (ringan — mengaktifkan SEAM blueprint, bukan domain baru)

- **Migration `000047_leads`** (reversible, teruji down/up): tabel `leads` —
  blueprint §2 mendefinisikan Lead/Prospect sebagai **SEAM "dibangun kemudian,
  dijamin additive"**; `bookings.lead_id` (Increment 7) kini punya rujukan.
  Master murni: nol ledger/jurnal.
- **`internal/crm`**: Lead lifecycle `new → contacted → qualified →
  converted | lost` (converted HANYA via endpoint Convert — bukan set-status);
  Convert membuat Customer master dari data lead (atau tautkan ke existing) +
  stempel `converted_at`. CRUD + filter status/sales. Guard teruji: convert
  ulang → 409.
- **`GET /reports/sales-performance`** (read-model blueprint §4/§8): per
  salesperson — leads (+converted), bookings (total/aktif), kontrak aktif +
  nilai, BAST count + nilai, collected (termin non-booking-fee), komisi
  earned/paid — SEMUA derived dari tabel dokumen + ledger (sumber yang sama
  dengan laporan keuangan; nol duplicate SoT). Plus totals funnel perusahaan.
- API: `POST/GET /leads` · `GET/PUT /leads/{id}` · `POST /leads/{id}/convert`.

## 2. Frontend — `/penjualan/sales` (SalesWorkspace)

- **KPI funnel strip**: Lead → Booking → Kontrak (PPJB) → Terjual (BAST)
  dengan panah antar kartu + sub-angka "aktif".
- **Tab Pipeline Lead**: chip filter per status (dengan counter), tabel lead
  (nama/telepon, sumber ber-label ID, minat proyek, sales, status badge) +
  **aksi cepat inline**: [Dihubungi] [Serius] [→ Customer] [batal (dengan
  alasan)] — sales memproses lead tanpa pindah halaman; lead terkonversi
  bertautan "lihat customer →".
- **Tab Customer**: daftar customer + jumlah booking → **drawer Riwayat**
  dengan **timeline perjalanan**: lead tercatat → dikonversi → booking (fee +
  status badge, klik → unit) → kontrak (klik → panel penjualan unit).
- **Tab Kinerja Sales (leaderboard)**: 🏆 top seller, lead (+konversi),
  booking, kontrak, BAST, nilai BAST, collected, komisi earned/dibayar —
  jawaban langsung untuk "penjualan per sales & bonus" di daftar KPI owner.
- **Modal Lead Baru**: nama/telepon/sumber (walk-in, referensi, online, iklan,
  pameran)/sales penanggung jawab/minat proyek.
- Sidebar: **Sales & CRM** di OPERASIONAL (setelah Unit & Penjualan).
- Empty/loading/error/success state lengkap; tabel scroll-x (mobile); aksi
  tulis di balik `RequireWrite` backend.

## 3. Alur 3-klik yang kini hidup

1. Pameran: [+ Lead Baru] → isi → simpan (2 klik + form).
2. Follow-up: chip "Baru" → [Dihubungi]/[Serius] inline (2 klik).
3. Closing: [→ Customer] → toast → buka unit → Booking (flow Increment 7).
4. Manajer: tab Kinerja Sales = leaderboard real-time dari engine akuntansi.

## 4. Verifikasi

- E2E API: lead → contacted → qualified → convert (customer terbentuk otomatis)
  → convert ulang 409 → list filter → sales-performance payload benar.
- 19 paket backend hijau; migration 000047 down/up bersih; `tsc` hijau.
- **Screenshot Chrome**: workspace penuh (KPI funnel 1→1→0→0, lead Andi
  Prospek "Jadi Customer", chip filter, 3 tab) render sempurna.

## 5. Backlog PS-3

| # | Item |
|---|---|
| PS3-B1 | Konversi lead → langsung wizard Booking (kini: toast + navigasi manual ke unit) |
| PS3-B2 | Timeline customer: sertakan cicilan/pembayaran (perlu endpoint kontrak per customer) |
| PS3-B3 | Lead: kolom follow-up date + pengingat (kartu Perhatian dashboard) |
| PS3-B4 | Dashboard: conversion rate funnel (lead→booking→BAST %) |
| PS3-B5 | Kinerja Sales: filter periode (kini all-time) |
