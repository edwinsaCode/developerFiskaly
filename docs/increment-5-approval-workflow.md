# Increment 5 — Generic Approval Workflow (Cross-cutting Governance)

> Blueprint: `blueprint-domain-additions.md` §10 (CORE governance).
> Urutan implementasi sesuai keputusan owner: Approval Workflow → Unit
> Lifecycle → Booking → Refund/Cancellation (Booking menunggu dua pendahulunya).
> Prinsip: additive-only, TANPA dampak ledger (hanya MEM-GATE), append-only audit.

## Ringkasan

SATU mekanisme approval untuk semua modul via **target polimorfik**
(`target_type` + `target_id`) — bukan tombol approve per modul. Konfigurasi
(Workflow→Steps) milik Tenant; runtime (Request→Actions) menempel ke dokumen
target. **Governance OPT-IN**: modul digate hanya bila tenant mengonfigurasi
workflow aktif untuk `target_type`-nya — tenant tanpa konfigurasi berperilaku
persis seperti sebelumnya (backward compatible by design).

## Perubahan Database (migration 000041, reversible)

| Tabel | Isi |
|---|---|
| `approval_workflows` | config per (tenant, target_type): nama, is_active; **satu aktif per target_type** (UNIQUE active_key — pola budget_plans) |
| `approval_steps` | urutan step: `seq` (UNIQUE per workflow), `approver_role` (role JWT), **`min_amount`** (threshold Approval Matrix — step di-skip bila nominal di bawahnya), **`quorum`** (jumlah persetujuan per step) |
| `approval_requests` | instance per dokumen: status (CHECK pending\|in_review\|approved\|rejected\|cancelled), `current_seq`, `amount` (konteks threshold), **satu request AKTIF per target** (UNIQUE active_key) |
| `approval_actions` | **APPEND-ONLY** (tidak ada jalur update/delete): step_seq, actor_id, actor_role, decision (CHECK approve\|reject), comment |

`target_type` vocabulary bertipe (blueprint §10 tabel): `rab`, `hpp_trueup`,
`refund`, `cancellation`, `commission`, `discount`, `cost`, `payment`,
`manual_journal`.

## Engine (`internal/approval`)

- **Lifecycle** (blueprint §10): `pending → in_review → approved | rejected`;
  `pending/in_review → cancelled` (hanya pemohon). Terminal = immutable —
  active_key dilepas sehingga request BARU untuk target sama boleh dibuat.
- **Step evaluation**: role actor wajib = `approver_role` step berjalan; satu
  actor satu keputusan per step; `reject` = terminal seketika; `approve`
  dihitung terhadap **quorum** — terpenuhi → maju ke step BERLAKU berikutnya
  (threshold `min_amount` menyaring step per nominal request) atau APPROVED.
- **Threshold auto-approve**: bila SEMUA step di bawah threshold (nominal
  kecil), request langsung APPROVED — Approval Matrix "sampai nominal berapa".
- **Konkurensi**: keputusan diterapkan atomik dengan `SELECT ... FOR UPDATE`
  pada request (dua approver bersamaan tidak bisa double-advance).
- **`RequireApproved(target)`** — gate untuk modul konsumen: tanpa workflow
  aktif → lolos (opt-in); dengan workflow → dokumen wajib punya request
  APPROVED (`ErrApprovalRequired`).

## Konsumen pertama: RAB (blueprint §10 — "RAB & True-up jadi konsumen")

`budget.Service.ApprovePlan` kini melewati **`ApprovalGate`** (seam interface di
budget, adapter di wiring): bila workflow `rab` aktif terkonfigurasi, plan wajib
punya Approval Request APPROVED sebelum `draft→active`; tanpa konfigurasi,
approve langsung seperti sebelumnya. **True-up** menjadi konsumen berikutnya
saat modulnya dibangun (kode P0-4 belum ada — baru migrasinya); seam target
`hpp_trueup` sudah tersedia. Konsumen scheme (konversi/reschedule — Increment 3)
akan di-wire menyusul lewat kolom `approval_request_id` yang sudah disiapkan.

## API

| Method | Path | Keterangan |
|---|---|---|
| GET/POST | `/approval-workflows` | config; POST khusus role **owner** |
| GET | `/approval-workflows/{id}` | detail + steps |
| PUT | `/approval-workflows/{id}/active` | aktif/nonaktif (owner) |
| POST | `/approval-requests` | submit `{target_type, target_id, amount?, notes}` |
| GET | `/approval-requests?target_type=&target_id=` | riwayat request dokumen |
| GET | `/approval-requests/{id}` | detail + actions (audit) |
| POST | `/approval-requests/{id}/actions` | `{decision: approve\|reject, comment}` — role divalidasi thd step |
| POST | `/approval-requests/{id}/cancel` | hanya pemohon |

## Jurnal

**Tidak ada.** Engine murni governance/metadata — sesuai blueprint §10
("tanpa dampak ledger, tetapi mem-gate posting").

## Test (16 package hijau, `-tags integration`)

- **Unit**: validasi config (7 penolakan: target/nama/steps/seq/role/quorum/
  min_amount — store terbukti tak tersentuh), terminal-status matrix.
- **Integration (MySQL)**: multi-step + quorum 2 (approve 1/2 menahan step;
  double-act ditolak; role salah ditolak; audit 3 aksi append-only; request
  baru boleh setelah terminal); reject & cancel (hanya pemohon); threshold
  (nominal kecil auto-approved; step owner ter-skip utk nominal menengah);
  **gate RAB end-to-end opt-in**: tanpa workflow → approve langsung; dengan
  workflow → `ErrApprovalRequired` → submit+approve → plan aktif.
- **Regresi**: seluruh suite lama lulus (gate nil-safe; unit test budget lama
  tanpa gate tetap valid).

## Risiko & Backward Compatibility

- Rendah. Semua additive; tanpa ledger; gate opt-in per tenant per target_type.
- Perilaku berubah HANYA setelah tenant sengaja mengonfigurasi workflow —
  itulah fitur ini.
- `ErrApprovalRequired` di endpoint approve RAB → HTTP 422 dengan pesan jelas.

## Non-goals (menyusul)

Approval Matrix penuh per blueprint P2 (approver dinamis "PM proyek",
delegasi, eskalasi), notifikasi, konsumen True-up/Commission/Refund (modulnya
belum dibangun), UI konfigurasi workflow.

## Next

**Unit Lifecycle State Machine** (urutan owner) — lalu Booking (konsumen
Approval + Unit Lifecycle), lalu Refund/Cancellation.
