# Increment 5.1 — Approval Workflow Hardening

> Hardening fondasi governance (pasca-review Increment 5) — bukan domain baru.
> Prinsip utuh: approval tetap governance (tanpa jurnal), additive-only,
> reversible, backward compatible. Pola immutability disamakan dengan Payment
> Scheme Terms Snapshot / Tax Rule revision / Allocation Snapshot.

## Grounding — dua blind spot terkonfirmasi di kode aktual

1. **Evaluasi keputusan membaca workflow HIDUP.** `Act` memuat steps via
   `FindWorkflowByID` — baris workflow memang immutable-by-construction (tidak
   ada jalur edit step/nama; hanya create + toggle active), tapi request
   in-flight tetap bergantung baris hidup. Persis gap yang H1/H3 tutup.
2. **`Cancel` tidak meninggalkan jejak di trail actions** — siapa membatalkan
   tidak teraudit. Ditutup: cancel kini menulis Action `decision='cancel'`
   atomik (append-only, sejalan prinsip audit).

## Perubahan Database (migration 000042, reversible, ADD-only)

| Objek | Isi |
|---|---|
| `approval_workflows.revision` | H3 — revisi konfigurasi per (tenant, target_type); backfill baris lama berurutan |
| `approval_steps` + `escalation_after_hours`, `escalation_role` | H4 seam — vocabulary eskalasi; scheduler BELUM dibangun |
| `approval_requests` + `workflow_revision`, `workflow_snapshot` JSON, `target_snapshot` JSON | H1/H2 — snapshot beku; NULL = request pra-hardening |
| CHECK `approval_actions.decision` | diperluas: `approve\|reject\|delegate\|cancel` |

## H1 — Workflow Snapshot (WAJIB) ✅

Saat submit, request membekukan `WorkflowSnapshot` {workflow_id, **revision**,
name, target_type, steps[seq, approver_role, min_amount, quorum, escalation]}.
**Evaluasi keputusan (`Act`) kini membaca SNAPSHOT** — bukan workflow hidup;
fallback ke workflow hidup hanya untuk request pra-hardening (snapshot NULL).
Dibuktikan test: workflow diganti (role & quorum berbeda) di tengah jalan →
request lama tetap dievaluasi dengan step beku dan APPROVED; request baru
tunduk pada revisi baru.

## H2 — Target Snapshot (WAJIB) ✅

`TargetSnapshot` {target_type, target_id, **target_version** (seam),
amount (string decimal), currency (default IDR), notes} dibekukan saat submit.
Sesuai instruksi: **business rule invalidation TIDAK diimplementasikan** —
hanya snapshot + seam (konsumen dapat membandingkan `target_version` nanti).

## H3 — Workflow Revisioning (WAJIB) ✅

`revision` bertambah per (tenant, target_type) saat workflow pengganti dibuat
(`NextWorkflowRevision`). Baris workflow **immutable by construction** — tidak
ada jalur edit step/nama/quorum; "mengubah workflow" = nonaktifkan lama + baris
baru. Request lama menunjuk (workflow_id, revision) beku miliknya.

## H4 — Delegate & Escalation Seam (WAJIB) ✅

- Vocabulary `Decision`: `approve | reject | delegate | cancel`. `delegate`
  boleh diajukan tapi mengembalikan **`ErrDelegateNotImplemented`** (HTTP 501)
  — extension point eksplisit, eksekusi menyusul (Approval Matrix P2).
- Step: `escalation_after_hours` + `escalation_role` (config + ikut dibekukan
  ke snapshot). **Tanpa scheduler/otomasi** — desain siap.

## H5 — Idempotency (VERIFIKASI) ✅

Ditegakkan di DB: UNIQUE `(tenant, target_type, target_id, active_key)` — satu
request AKTIF per dokumen. **Kontrak API**: submit ulang saat masih ada request
aktif → `ErrActiveRequestExists` → **HTTP 409**; setelah request terminal
(approved/rejected/cancelled) active_key dilepas sehingga pengajuan ulang sah.
Diverifikasi ulang oleh test khusus.

## H6 — Domain Event Seam (WAJIB) ✅

`EventSink` interface + event `approval.approved` / `approval.rejected` /
`approval.cancelled` dipublikasikan SETELAH fakta persist. Default `noopSink`;
**engine tidak pernah memanggil modul lain langsung** (Booking/Unit/notifikasi
kelak menjadi subscriber lewat wiring). Tanpa bus, tanpa async, tanpa
subscriber — sesuai instruksi.

## API (perubahan additive)

- `POST /approval-workflows` steps: + `escalation_after_hours`, `escalation_role`.
- `POST /approval-requests`: + `target_version`, `currency` (opsional).
- Respons request kini memuat `workflow_revision`, `workflow_snapshot`,
  `target_snapshot`; actions memuat decision `cancel` untuk pembatalan.
- `delegate` pada actions → **501 Not Implemented** (seam terdokumentasi).

## Test (16 package hijau, `-tags integration`)

- **FrozenSnapshotEvaluation** — bukti inti H1/H3: ganti governance di tengah
  jalan tidak mengubah request berjalan; revisi 1→2 tercatat; request baru
  tunduk revisi baru.
- **TargetSnapshot** — amount/versi/currency beku benar.
- **DelegateSeam** — `ErrDelegateNotImplemented`.
- **Idempotency + CancelAudit + Events** — 409 eksplisit; cancel meninggalkan
  action `cancel`; sink menerima cancelled/approved/rejected berurutan.
- Regresi: seluruh test Increment 5 & suite lama lulus tanpa perubahan
  (fallback legacy utk request tanpa snapshot).

## Risiko

Rendah. Semua additive; request pra-hardening tetap berfungsi (fallback);
tidak ada perubahan perilaku kecuali yang diminta (cancel kini teraudit).

## Next

**Increment 6 — Unit Lifecycle State Machine** (urutan owner), setelah 5.1
di-approve.
