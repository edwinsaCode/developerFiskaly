# P0-4 — Completion & HPP True-Up — SHIPPED (Implementation Report)

> Status: **SHIPPED + tervalidasi (TU-1..TU-6 di real MySQL).** 2026-07-17.
> Design v3 FINAL (`p0-4-completion-true-up-design.md`) — D1–D4 + IMPL-1..5
> semuanya diimplementasikan sesuai kunci. Menghidupkan dead schema 000029–000032
> (temuan audit awal). Frontend spec: `p0-4-frontend-spec.md`.
> Urutan LOCKED berikutnya: **Increment 8 — Cancellation/Refund** (8b pasca-BAST
> kini TERBUKA karena P0-4 selesai & tervalidasi).

## 1. Ringkasan — apa yang ditutup

Kebijakan B2 (LOCKED): Persediaan berakru **aktual**, HPP di-relief **budgeted**
saat BAST → variance menumpuk di 1-3xxx. P0-4 menutup loop itu sebagai **proses
closing ber-approval**: `Completion (draft→completed→finalized)` membuka
`True-up (draft→calculated→approved→posted)` yang memposting SATU jurnal
adjustment dan mem-finalize act_HPP semua unit. Invariant #4 (HPP = biaya
aktual unit) kini terpenuhi penuh di akhir proyek; INV-COGS-SUM terjaga
(recognized COGS unit = Σ jurnal COGS posted: BAST + true-up — keduanya
ter-link: `sale_records.cogs_journal_id` + `hpp_trueup_lines.journal_entry_id`).

## 2. Yang dikirim (tanpa migration baru — 000029–000032 dihidupkan)

| Komponen | Isi |
|---|---|
| **`internal/closing`** (BARU) | `CompletionEvent` + `TrueupRun/Line` state machines (IMPL-3 guard keras); engine variance (A_c dari ledger posted-only, act_HPP via `allocation.Compute` largest-remainder — Invariant #3); `Post` atomik ber-row-lock + `journal_id` UNIQUE + reference `HPP-TRUEUP-{run}` (IMPL-4, 3 lapis); audit actor per transisi (IMPL-5); `FinalizedUnitHPP` (D2) |
| **Snapshot identity (000030 hidup)** | `AllocationSnapshot` += pin `allocation_config_version_id/_version`; lines += `unit_id` + `unit_name_snapshot` (nama SAAT BAST — auditor tanpa join master); penulis BAST mengisi semuanya |
| **Basis versioning D1 (000029 hidup)** | `allocation.AllocationConfigVersion` + `VersionStore` (opt-in `WithVersionStore`); `SetBasis`: pra-snapshot = in-place, pasca-snapshot = **version baru** (supersede, audited); resolver mem-pin version aktif ke snapshot |
| **D2 — post-completion sales** | `sale.FinalizedHPPSource` seam; `RecordBAST`: finalized → budgeted → actual. Proyek finalized TANPA run posted → BAST diblokir (`ErrTrueupNotPostedForBAST`, 422) — "no live recalculation after completion" ditegakkan |
| **D4 — periode** | Jurnal true-up jatuh di `post_date` (periode berjalan); `WithPeriodChecker` menjaga periode terbuka; deskripsi memuat "prior period HPP adjustment" |
| **🐛 FIX produksi** | `sale.NewHandler` TIDAK pernah memasang `WithHPPResolver` — BAST produksi selalu jatuh ke metode actual (resolver hanya ter-wire di test). Kini resolver budgeted + pin version ter-wire di produksi |

**API baru** (semua `/api/v1`, JWT + RequireWrite utk POST):

| Method | Path | Fungsi |
|---|---|---|
| POST | `/projects/{id}/completion` | draft→completed (+`projects.status='completed'`); body `{completed_date?}` |
| POST | `/projects/{id}/completion/finalize` | completed→finalized (kunci A_c) |
| GET | `/projects/{id}/completion` | status completion |
| GET | `/projects/{id}/hpp-variance` | preview read-only (Req #4): totals + lines sold/unsold |
| POST | `/projects/{id}/hpp-trueup/calculate` | hitung + simpan lines (tanpa jurnal); recalc hanya draft/calculated |
| GET | `/projects/{id}/hpp-trueup` · GET `/hpp-trueup/{runID}` | daftar run / detail + lines |
| POST | `/hpp-trueup/{runID}/approve` · `/post` · `/cancel` | transisi (post: body `{post_date?}`) |

**Aturan A_c** (per class, posted-only — Decision A): `Σ debit` non-reversal
− `Σ credit` dari jurnal `source='reversal'` pada 1-3xxx ber-tag project.
Kredit BAST/true-up tidak mengurangi A_c (bukan reversal) — A_c = total
kapitalisasi aktual, sesuai definisi §2 design.

## 3. Test (semua hijau; 17 paket unit + 17 paket integration)

- **Unit:** state machine completion & run (semua transisi legal/ilegal, posted/cancelled terminal); D1 SetBasis (in-place pra-freeze, version baru pasca-freeze, no-op, legacy tanpa store).
- **Integration (skenario design §10 persis):** RAB land 400jt, aktual 480jt, unit A 25%/B 75% → BAST A budgeted 100jt (snapshot **pin v1** + identitas nama); calculate pra-finalized ditolak; completed (+status proyek) → finalized; **BAST B diblokir pra-posting (D2)**; preview variance +20jt; calculate (line A: 100/120/+20 sold; B: 360 unsold Δ0); post pra-approve ditolak; approve→post → jurnal `Dr 5-1000 20jt / Cr 1-3000 20jt`; **TU-1** saldo 1-3000 = 360jt = act_HPP(B); **TU-3** saldo 5-1000 = 120jt = act_HPP(A); **TU-5** double-post ditolak, 1 jurnal; **TU-6** BAST B metode `finalized` 360jt; **TU-4** saldo 1-3000 = 0; **D1** ganti basis pasca-snapshot → v2 (v1 superseded).

## 4. Migration Impact

**Nol migration baru.** 000029–000032 (sudah terpasang sejak lama sebagai dead
schema) kini sepenuhnya dipakai. Kolom-kolom yang diaudit "SUSPECTED DRIFT"
di context-recovery kini hidup: `allocation_config_versions`,
`project_completion_events`, `hpp_trueup_runs/lines`,
`allocation_snapshots.allocation_config_version_id/_version`,
`allocation_snapshot_lines.unit_id/unit_name_snapshot`.

## 5. Backward Compatibility

- `sale_records` lama (`hpp_method='actual'`, snapshot NULL) **tidak tersentuh** — scope true-up otomatis hanya snapshot budgeted (IMPL-2).
- Snapshot pra-P0-4 tanpa pin version → diperlakukan legacy (ikut version ter-pin lain / config aktif); campuran ≥2 pin berbeda → `ErrMixedAllocationBasisVersion` (tolak, pisah scope — IMPL-1, no auto-merge).
- `SetBasis` tanpa `WithVersionStore` = perilaku lama (semua unit test existing lulus tanpa perubahan); wiring produksi memasangnya.
- Metode HPP baru `finalized` additive pada kolom VARCHAR tanpa CHECK.
- Proyek yang belum pernah completion → alur BAST persis seperti sebelum P0-4.

## 6. Dashboard Impact

- **Margin & HPP per proyek menjadi AKTUAL** setelah closing — tile "L/R per proyek" & "Margin" (product-ux-blueprint §3 baris 3) kini kredibel end-of-project.
- Baris "Perhatian": tambah sinyal **"Proyek finalized menunggu true-up"** (completion finalized tanpa run posted — kondisi yang memblokir BAST) dan **"True-up menunggu approval"** (run calculated/approved). Sumber: `GET /projects/{id}/completion` + `/hpp-trueup`.
- Persediaan 1-3xxx di Neraca kini bermakna: sisa = biaya aktual unit unsold (bukti §9 design, terverifikasi TU-1).

## 7. Report Impact

- **L/R per proyek (`project-pl`)**: HPP pasca-closing = aktual (BAST budgeted + adjustment); tanpa perubahan endpoint — jurnal true-up otomatis masuk (5-1000 bertag project/unit).
- **Neraca**: 1-3xxx akurat pasca-closing.
- **Drill-down HPP unit**: `hpp_trueup_lines` + snapshot memberi rantai audit lengkap: "HPP A-01 = 100jt (BAST, RAB v1, basis area 25%) + 20jt (true-up run #N, jurnal #M) = 120jt aktual".
- Laporan "13 laporan owner" (blueprint UX §4): item "HPP proyek" & "Margin per proyek" naik dari 🟡→✅ penuh.

## 8. Follow-up backlog

| # | Item | Catatan |
|---|---|---|
| P04-B1 | Approval Workflow gate utk true-up `approve/post` (TargetHPPTrueup sudah ada di vocabulary, inert) | Konsumen approval berikutnya; opt-in seperti RAB |
| P04-B2 | Phase-level scope (D3: schema siap `phase_id`, impl pertama project-level) | Aktivasi tanpa perubahan schema |
| P04-B3 | Disclosure label "prior period adjustment" di laporan (TODO tax/accounting-policy design §13.4) | Presentasi, bukan mekanisme |
| P04-B4 | `GET /units/{id}/hpp-detail` drill-down gabungan (snapshot + true-up line) — P0-2 lanjutan | Read-only, bahan UI |
| P04-B5 | Backfill snapshot lama: isi `unit_id/unit_name_snapshot/config pin` utk snapshot pra-P0-4 dari master saat ini (flag "reconstructed") | Opsional; dev env |
