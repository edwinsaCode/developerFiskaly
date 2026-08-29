# Product Sprint 10 — R6 UX Hardening — SHIPPED

> Status: **SHIPPED + smoke test Chrome.** 2026-07-22.

## U1 — Auto-schedule dari skema (beban kerja terbesar staf)

`ScheduleForm` kini punya tombol **"⚡ Isi Otomatis dari Skema"** →
`GET /sale-contracts/{id}/schedule-plan` (engine PaymentSchemePolicy, Σ == gross
persis via largest-remainder) → baris terisi otomatis (jenis/tanggal/nominal),
staf tinggal koreksi lalu simpan. Terverifikasi kontrak #386: DP 6,5jt +
Pelunasan 643,5jt (= 650jt persis). API `fetchSchedulePlan` baru di `lib/api/sale.ts`.

## U2 — Halaman Invoice Customer naik kelas

`AllInvoiceList`: chip filter status ber-count (Semua/Diterbitkan/Jatuh Tempo/
Lunas/Batal), pencarian nomor/customer/unit, ringkasan "Belum terbayar" (Σ issued
+ overdue), empty state hasil filter. Client-side (data sudah dimuat server).

## U4 — Deep-link `?tab=` proyek

Diverifikasi SUDAH beres sejak PS-2 (routing `searchParams` di
`app/(app)/proyek/[id]/page.tsx` + `VALID_TABS`); funnel unit link ke `?tab=`.
Tidak ada perubahan.

## U8 — Empty states

Sweep menyeluruh: sisa "Belum ada …" adalah teks kontekstual pendek yang sudah
menjelaskan langkah berikutnya (mis. InvoiceList mengarahkan ke Generate dari
jadwal). Tidak ada empty state buta tersisa.

## Backlog R6

| # | Item |
|---|---|
| R6-B1 | U5 bulk action invoice (tandai terkirim banyak) — butuh field/endpoint status kirim di backend |
| R6-B2 | Simpan filter invoice di query param (shareable URL) |
| R6-B3 | Auto-schedule: tawarkan langsung saat kontrak baru dibuat (kini via modal jadwal) |
