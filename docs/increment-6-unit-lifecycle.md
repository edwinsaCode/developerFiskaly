# Increment 6 — Unit Lifecycle State Machine — SHIPPED (+6.1 HARDENED)

> Status: **SHIPPED + 6.1 HARDENED, tervalidasi (unit + integration real MySQL).**
> 2026-07-17. Design: `increment-6-unit-lifecycle-design.md`. Prinsip: additive-only,
> reversible, append-only, TANPA dampak ledger (status unit = metadata stok).
> Urutan LOCKED berikutnya: Increment 7 (Booking) → P0-4 → Increment 8 (Cancellation).

## Keputusan D1–D4 (diadopsi sesuai rekomendasi design)

| # | Keputusan | Nilai |
|---|---|---|
| D1 | `sold` dipertahankan sebagai state BAST (tanpa rename) | ✅ |
| D2 | Approval gate `hold`/`blocked` via TargetType `unit_transition` (opt-in) | ✅ |
| D3 | Kontrak baru → proyeksi unit `ppjb` (best-effort, tak membatalkan kontrak) | ✅ |
| D4 | CHECK `units.status` (9 nilai) ditambahkan sekarang | ✅ |

## Yang dikirim

**Migration `000043_unit_lifecycle`** (reversible, terverifikasi up+down real MySQL 8.0):
- Tabel `unit_status_transitions` (APPEND-ONLY): `tenant_id, unit_id, from_status,
  to_status, event, event_date, reference_type, reference_id, actor_id, notes`.
  Index: tenant, (tenant,unit), (tenant,to_status,created_at) utk time-on-market. FK → units.
- CHECK `chk_ust_to_status` / `chk_ust_from_status` (9 nilai + '' utk backfill).
- CHECK `chk_units_status` (D4).
- Backfill: 1 baris sintetis per unit existing (`event='backfill'`, `event_date=units.updated_at`).

**Domain (`project/model.go`):**
- `UnitStatus` diperluas 3→9: `available|booked|reserved|ppjb|sold|occupied|hold|blocked|maintenance`.
  Nilai lama TIDAK di-rename (kompat penuh). `CanTransitionTo` = matriks blueprint §9.
- `TransitionEvent` typed (SSOT) + `Valid()`. `UnitStatus.RequiresApproval()` (hold/blocked).
- `UnitStatusTransition` model (append-only) + reference-type constants.

**Service (`project/service.go`):**
- `Transition(ctx, tenant, id, TransitionRequest)` — validasi matriks, gate approval
  opt-in (hold/blocked), tolak `sold→available` (D2 → `ErrPostBASTCancellation`),
  tulis status + log ATOMIK. `TransitionUnit` lama = wrapper (event=manual).
- `ApprovalGate` seam (`SetApprovalGate`). `ListTransitions`.

**Repository:** `TransitionUnitAtomic` (update status + insert log, satu tx, guard
optimistic `status=from`), `ListTransitions`, `LogTransitionTx` (dipakai lintas-package).

**BAST integration (`sale/repository.go`):** Execute menulis baris transisi
(`event=bast_executed`, `reference=sale_record`, `event_date=BAST date`) **DI DALAM
tx BAST yang sama** — gagal BAST = tanpa log. Terverifikasi integration.

**API:** `POST /units/{id}/transition` diperluas (`event`, `event_date`, `notes`,
actor dari JWT); `GET /units/{id}/transitions` (audit + time-on-market). Approval
gate `unit_transition` di-wire di `main.go` (opt-in). Sale→project transitioner (D3) di-wire.

**Approval:** TargetType `unit_transition` ditambah (additive; `target_type` VARCHAR
tanpa CHECK — tanpa migration).

## Test

- Unit (default `go test ./...`): matriks legal penuh, event typed, kompat 3 nilai
  lama, gate approval (mock: no-gate lolos / gate-reject tolak + tanpa log),
  non-gated skip gate, D2 pasca-BAST ditolak, tenant-scoped ListTransitions,
  no-log-on-illegal, event default manual.
- Integration (`-tags=integration`, real MySQL): BAST menulis 1 baris transisi
  atomik (from=available, to=sold, event=bast_executed, reference=sale_record,
  event_date instant benar) + status unit→sold. Seluruh suite existing hijau.
- Migration up+down terverifikasi reversible di MySQL 8.0.

## Catatan lifecycle

- `cancelled_post_bast` (sold→available) DISEDIAKAN di matriks tetapi endpoint
  manual menolaknya (`ErrPostBASTCancellation`) sampai domain Cancellation
  (Increment 8) dibangun — mencegah status berubah tanpa jurnal pembalik (D2).
- Time-on-market = read model DERIVED dari log (tanpa kolom `available_since`).
- `reset-demo` (util demo) meng-UPDATE sold→available langsung tanpa reversal —
  tak terpengaruh (reference_id tanpa FK; units tak dihapus). Diterima utk demo.

## 6.1 — Hardening (pasca Production Readiness Review, 2026-07-17)

Review eksternal menemukan F-1..F-6; owner memutuskan: F-1 + F-4 fix sekarang,
F-3 doc-only, F-2/F-5/F-6 → backlog. Tanpa migration baru.

### F-1 — BAST menegakkan lifecycle (BLOCKING, fixed)

- `sale/repository.go` Execute: guard DI DALAM tx SEBELUM jurnal apa pun —
  baca status unit; kosong → `ErrUnitNotFound`; `sold` → `ErrUnitAlreadySold`;
  selain itu WAJIB `project.UnitStatus.CanTransitionTo(sold)` (= `reserved|ppjb`,
  **satu sumber aturan, tanpa duplikasi**) → jika tidak: `ErrUnitNotBASTReady` (422).
- Predikat UPDATE di-pin `status = <status tervalidasi>` (bukan `!= 'sold'`):
  perubahan konkuren ⇒ 0 baris ⇒ `project.ErrUnitTransitionConflict` (409).
  Efek samping: `from_status` di log kini dijamin akurat (menutup F-5 sekalian).
- Posting akuntansi TIDAK berubah (Event 3/4/5 identik; hanya urutan guard).
- Integration test baru: BAST dari `available`/`hold`/`blocked` → ditolak,
  **rollback penuh** (0 jurnal, 0 snapshot, 0 sale_record, 0 log, status utuh).
- Fixture diselaraskan ke alur legal (seed `reserved`); `demo-seed` kini
  mereservasi unit dulu (tercatat di lifecycle log — audit demo konsisten).

**⚠️ Konsekuensi operasional (follow-up frontend):** frontend saat ini TIDAK
punya aksi transisi unit (tidak ada pemanggil `/units/{id}/transition`).
Alur UI lama "kontrak → termin → BAST" pada unit `available` kini berhenti di
BAST dengan 422 yang jelas. Follow-up UI: tombol "Reservasi Unit" (atau otomasi
reservasi saat DP — blueprint §9: `Booked → Reserved [Payment]`, arah Increment 7).

### F-3 — Ambiguitas `ppjb` (doc-only, decided)

Blueprint §9 (SSOT) hanya memuat `Reserved --> PPJB`. Teks design §3 yang
menulis "reserved/available → ppjb" adalah inkonsistensinya → dikoreksi di
dokumen design. **Keputusan dikunci: hanya `reserved → ppjb`.** Tanpa perubahan
perilaku. Konsekuensi D3 tetap: proyeksi kontrak→ppjb hanya fire untuk unit
yang sudah reserved.

### F-4 — Koherensi event ↔ transisi (fixed)

- `project/model.go`: peta `eventTransitions` (SSOT dari state diagram §9) +
  `TransitionEvent.Allows(from, to)`. `manual` = generik (matriks tetap
  penjaga); `backfill` ditolak via API (hanya untuk migration).
- `Transition` menolak label bohong → `ErrEventTransitionMismatch` (400).
  Mis. `bast_executed` pada `available → reserved` kini ditolak.

### Backlog (dari review — TIDAK dikerjakan sekarang)

| # | Item | Catatan |
|---|---|---|
| F-2 | Gate approval `unit_transition` "sekali approved, lolos selamanya" per unit | Prasyarat sebelum tenant mengaktifkan workflow `unit_transition`; butuh keputusan desain (approval dikonsumsi per-request/per-transisi) |
| F-5 | ~~`from_status` log BAST bisa basi~~ | **Tertutup gratis oleh F-1** (UPDATE di-pin) |
| F-6 | `UnitStore.UpdateUnitStatus` dorman (bypass tanpa log, 0 pemanggil produksi) | Deprecate/hapus di refactor kecil berikutnya |

Verifikasi 6.1: `go test ./...` 16 paket hijau; `go test -tags=integration ./...`
16 paket hijau (real MySQL) termasuk test penolakan BAST + rollback penuh.

## Next

**Increment 7 — Booking (+ booking fee).** Booking = konsumen state machine ini
(`available → booked`, event `booking_created`). Pra-BAST, tanpa dampak HPP.
Catatan: bawa serta follow-up UI reservasi (lihat F-1) — blueprint §9 memang
mengarahkan `Booked → Reserved` via Payment.
