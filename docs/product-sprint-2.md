# Product Sprint 2 — Dashboard Owner V2 · Workspace Project · Navigation — SHIPPED

> Status: **SHIPPED + diverifikasi visual di browser riil (screenshot).** 2026-07-21.
> SSOT desain: `product-ux-blueprint.md`. Target: owner paham kondisi perusahaan
> ≤10 detik; semua flow umum ≤3 klik.

## 1. Dashboard Owner V2 (`/dashboard`)

**Backend baru — `GET /api/v1/reports/dashboard`** (`internal/reporting/dashboard.go`):
SATU payload komposit, semua angka derived dari ledger posted + tabel dokumen
(nol saldo tersimpan). Isi: 5 saldo posisi (kas/bank via kategori COA, piutang
1-2xxx, hutang 2-1000, titipan 2-2100, utang komisi 2-6200) · 5 metrik MTD
(penjualan, booking aktif+fee, unit BAST, margin, arus kas in/out/net) · 5
counter perhatian (overdue, pembatalan, refund, komisi, true-up) · baris per
proyek (progress biaya vs RAB, funnel 9-status, pendapatan/HPP/margin%, nilai
kontrak vs collected%).

**Frontend (`DashboardV2`)**: strip "⚠ Perlu perhatian" (hanya tampil bila ada,
setiap item klik → halaman aksi) → BARIS 1 Posisi Hari Ini (5 KPI card, semua
klik → drill) → BARIS 2 bulan berjalan (5 KPI) → BARIS 3 Pipeline Proyek (tabel:
progress bar, funnel bar berwarna per status unit, margin ± warna, collection
bar; klik nama → workspace) → baris chip **Laporan** (semua laporan dari
dashboard — owner tidak mencari menu).

## 2. Workspace Project (`/proyek/[id]`)

Satu tempat untuk semua area proyek, **URL konsisten**:

| Tab | URL | Isi |
|---|---|---|
| Overview | `/proyek/[id]` | 8 KPI proyek + funnel unit klik-ke-tab + 2 progress bar (biaya vs RAB, collection) — dari endpoint dashboard |
| Unit | `?tab=unit` | AddUnit + UnitBoard + tabel Fase (gabungan tab lama board+fase) |
| Penjualan | `?tab=penjualan` | Semua unit + status siklus + [Kelola →] panel penjualan unit |
| Booking | `?tab=booking` | BookingBoard mode embed (filter proyek ini) |
| RAB | `/proyek/[id]/rab` | Halaman existing, kini ber-WorkspaceNav + breadcrumb konsisten |
| Biaya | `/proyek/[id]/biaya` | idem |
| Alokasi HPP | `?tab=alokasi` | Config + Preview + hasil per unit + riwayat (pindahan) |
| Closing | `?tab=closing` | ClosingPanel P0-4 (alias lama `?tab=penyelesaian` tetap jalan) |
| Timeline | `?tab=timeline` | **Baru**: transisi lifecycle terbaru lintas unit proyek (endpoint baru `GET /projects/{id}/transitions`, di subtree milik project handler — aman dari pelajaran chi PS-1) |
| Dokumen | `?tab=dokumen` | Empty state jujur (Document Management = SEAM blueprint) |

`ProyekDetailTabs` (komponen tab lama) diganti; `ProyekHeader` dipertahankan.

## 3. Navigation polish

- Sidebar: **RAB & Biaya top-level DIHAPUS** (redundan — kini di workspace;
  rute `/rab` & `/biaya` picker tetap hidup utk deep-link lama). 15 item → 13.
- Alur umum ≤3 klik: Dashboard → proyek (1) → tab mana pun (2); Dashboard →
  kartu perhatian → halaman aksi (1); laporan apa pun ≤2 klik dari dashboard.
- Breadcrumb rab/biaya konsisten: Proyek / [nama] / RAB.

## 4. Verifikasi

- `tsc --noEmit` hijau; backend build + suite project hijau (mock store diperluas).
- Smoke sesi riil: 11 URL workspace/dashboard semuanya 200 tanpa error marker.
- Endpoint dashboard diuji: arus kas booking-refund tenant smoke terbaca benar
  (in 5jt / out 5jt / net 0) — bukti derivasi ledger akurat.
- **Screenshot browser riil** (Chrome): login → Dashboard V2 (3 baris + chip
  laporan) → Workspace Overview (KPI+funnel) → Timeline (event audit booking
  dibuat/dibatalkan nyata) → Closing (stepper langkah 1). Semua tampil benar.

## 5. Deliverable produk (15 wajib) — ringkas

Screen inventory (§1–2) · IA & nav flow (§3) · wireframe = implementasi + screenshot ·
component spec: KPICard, AttentionStrip, UnitFunnel, Pct bar, WorkspaceNav,
OverviewTab, PenjualanTab, TimelineTab, AlokasiTab (semua reusable) · table spec
(pipeline & penjualan sortir status) · filter/search (tab status existing per
board) · empty/loading (skeleton)/error/success state di semua komponen baru ·
permission (aksi tulis tetap dibalik `RequireWrite` backend; viewer read-only) ·
mobile: grid KPI 2-kolom, tabel scroll-x, tab bar scroll-x · dashboard impact =
inti sprint · report impact: semua laporan kini terjangkau dari dashboard.

## 6. Backlog PS-2

| # | Item |
|---|---|
| PS2-B1 | `/laporan` dukung `?tab=` deep-link (chip dashboard kini mendarat di tab default) |
| PS2-B2 | Kartu Perhatian per-proyek "menunggu true-up" tautan langsung ke `?tab=closing` proyek tsb (kini → /proyek) |
| PS2-B3 | Dashboard: sparkline tren 6 bulan (penjualan/arus kas) |
| PS2-B4 | Halaman `/penjualan` (papan global) pakai funnel bar yang sama |
| PS2-B5 | Hapus komponen mati: ProyekDetailTabs, MetricCard/PipelineBar/BudgetProgress lama |
| PS2-B6 | (tetap dari PS-1) Pengaturan, approval inbox, denormalized lists, float FE |
