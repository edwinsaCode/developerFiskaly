# Product Sprint 11 — R7 Enterprise Design Polish — SHIPPED

> Status: **SHIPPED + smoke test Chrome (palette).** 2026-07-22.

## U6/U9 — Command Palette + shortcut keyboard

> Update (follow-up, revisi feedback owner): pintu masuk terlihat = tombol KECIL
> "🔍 Cari `Ctrl+K`" di **pojok kanan atas Dashboard** (sejajar judul) —
> `GlobalSearchButton` membuka palette via event global `open-command-palette`.
> Versi kotak di Header dicoba lalu DIBATALKAN (bikin header ramai).
> Terverifikasi klik → palette terbuka; header kembali bersih.

`components/layout/CommandPalette.tsx`, terpasang global via `AppShell` (token dari
layout cookie):
- Buka: **Ctrl+K / ⌘K** dari mana pun, atau **"/"** di luar input. Esc tutup.
- Sumber: 19 destinasi navigasi + **Unit** (kode, per proyek) + **Customer** —
  data dimuat sekali saat pertama dibuka (lazy, cached).
- Navigasi keyboard penuh: ↑↓ pilih, Enter buka; footer hint shortcut.
- Terverifikasi: Ctrl+K → ketik "A-03" → hasil UNIT (Griya E2E · ppjb) → Enter →
  mendarat di `/penjualan/473`.

## U7 — Mobile

- `Sidebar` prop `drawer`; desktop `hidden md:flex` (rail sticky seperti semula),
  mobile disembunyikan.
- `Header`: tombol hamburger `md:hidden` → slide-over drawer berisi Sidebar penuh,
  klik luar/route change menutup.
- (Verifikasi visual terbatas: window manager menolak resize <md; pola Tailwind
  responsive standar, tsc hijau.)

## Deep-link bonus

`SalesWorkspace` kini menerima `?tab=` (`pipeline|customer|kinerja`) — dipakai
palette (Customer → `/penjualan/sales?tab=customer`) dan link luar.

## Backlog R7

| # | Item |
|---|---|
| R7-B1 | Palette: hasil customer langsung ke statement kontrak aktifnya (butuh lookup kontrak by customer) |
| R7-B2 | Shortcut `n` = aksi "baru" kontekstual per halaman |
| R7-B3 | Mode kartu untuk tabel lebar di mobile (kini overflow-x scroll) |
| R7-B4 | Verifikasi visual mobile di perangkat nyata / emulasi devtools |
