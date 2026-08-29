# Product Sprint 8 — R3 Progress Fisik & Project Control — SHIPPED

> Status: **SHIPPED + smoke test Chrome.** 2026-07-22.
> Owner melihat Fisik vs Biaya vs Collection dalam satu pandangan — indikator dini over budget.

## Backend

- **Migration `000051`** — `project_progress_entries`: entitas OPERASIONAL append-only
  (tenant_id, project_id, phase_id?, progress_pct DECIMAL(5,2) CHECK 0–100, as_of_date,
  notes, created_by). **Bukan ledger, tidak ada angka uang** — no duplicate SoT.
- **`internal/project/progress.go`**: `POST/GET /projects/{id}/progress`.
  Guard: pct 0–100 (422); **penurunan dari entri terakhir wajib catatan alasan** (422) —
  append-only tetap mengizinkan koreksi tapi audit trail menjelaskan kenapa mundur.
- **Dashboard** (`reporting/dashboard.go`): join entri terbaru per proyek →
  `physical_pct`, `physical_as_of`, `cost_ahead_warning` (= biaya% − fisik% > 15pt).

## Frontend

- **`ProjectControlCard`** (workspace proyek Overview): tiga bar Fisik (aksen) /
  Biaya (kuning) / Collection (hijau) + "terakhir update X hari lalu" + banner ⚠
  bila biaya mendahului fisik >15pt + riwayat input collapsible + modal
  **"+ Update Fisik"** (pct/tanggal/catatan, teks menjelaskan append-only).
  Empty state informatif bila fisik belum pernah diinput.
- **Dashboard tabel PIPELINE PROYEK**: kolom baru **Fisik** ("belum diinput" bila kosong,
  ikon ⚠ bila warning).
- Polish: KPI Progress Biaya di-clamp ≥0 (konsisten dengan tabel dashboard).

## Verifikasi

API: input 35% ✓ → turun ke 30% tanpa catatan **ditolak 422** ✓ → dengan catatan 201 ✓ →
120% ditolak ✓ → list append-only urut terbaru ✓ → dashboard `physical_pct=30` ✓.
Visual: kartu kontrol tampil (fisik 30% + catatan koreksi, collection 97%), riwayat (2).
`go test ./...` hijau · `tsc` hijau · migration **51 clean**.

## Backlog R3

| # | Item |
|---|---|
| R3-B1 | Progress per FASE (phase_id sudah di schema+API; UI kini level proyek) |
| R3-B2 | Kurva-S chart dari riwayat (data sudah ada) |
| R3-B3 | Ambang warning konfigurabel per tenant (kini 15pt tetap) |
| R3-B4 | Reminder "fisik belum diupdate >30 hari" di attention strip dashboard |
