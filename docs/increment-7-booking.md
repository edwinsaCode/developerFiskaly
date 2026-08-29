# Increment 7 — Booking — SHIPPED

> Status: **SHIPPED + tervalidasi (unit + integration real MySQL, migration
> reversible).** 2026-07-17. Blueprint §2 (CORE). Konsumen pertama Unit
> Lifecycle (Increment 6). Design review + keputusan D1–D11 di percakapan;
> semua rekomendasi diadopsi (standing directive owner: lanjut tanpa approval
> per-increment). Frontend spec: `increment-7-frontend-spec.md`.
> Urutan LOCKED berikutnya: **P0-4 Completion True-Up** → Increment 8 (Cancellation).

## Keputusan (D1–D11, diadopsi sesuai rekomendasi design review)

| # | Keputusan |
|---|---|
| D1 | Akun **Titipan Booking 2-2100** terpisah (bukan reuse 2-2000) |
| D2 | State machine tanpa `draft`: `active → converted\|expired\|cancelled` |
| D3 | Konversi unit dua transisi `booked→reserved→ppjb` (blueprint-faithful; nol perubahan matriks) |
| D4 | Konversi via `POST /sale-contracts` + `booking_id` (satu aksi, SATU tx penuh) |
| D5 | Flag `refundable` per booking; non-refundable → forfeit ke 4-2000 SEKARANG; refundable → `pending_refund` (cash-out menunggu Increment 8 Refund) |
| D6 | Fee→DP: reklas jurnal + baris **buyer_credit** (refinement: BUKAN allocation_type baru — schedule belum ada saat konversi; fee jadi Saldo Kredit Buyer lalu diaplikasikan ke DP via apply-credit FE-3 yang sudah shipped) |
| D7 | Tanpa approval gate untuk booking (gate menyusul via TargetCancellation, Increment 8) |
| D8 | Seam: `lead_id` NULL tanpa FK + `sales_person_id` atribusi |
| D9 | Fee = `PaymentSource='booking_fee'`, kredit 2-2100, **TANPA alokasi** saat terima (temuan grounding: CommitPayment tanpa kontrak → semua jadi buyer_credit — SALAH utk fee yang tertahan lifecycle; maka atomic writer khusus, pola BAST) |
| D10 | Expiry via sweep endpoint idempoten `POST /bookings/mark-expired` (preseden mark-overdue) |
| D11 | `customer_id` WAJIB |

## Model akuntansi (ledger-centric; semua saldo derived)

| Peristiwa | Jurnal |
|---|---|
| Fee diterima | `Dr Bank / Cr Titipan Booking 2-2100` (kewajiban — Invariant #7) + termin (`payment_source=booking_fee`, `credit_account_code=2-2100`) + kwitansi in-tx |
| Konversi → kontrak | `Dr 2-2100 / Cr Uang Muka 2-2000` + 1 baris `buyer_credit` (termin fee) → DP ditutup via apply-credit |
| Expired/cancel non-refundable | `Dr 2-2100 / Cr Pendapatan Lain-lain 4-2000` (forfeit; PSAK 72 breakage) |
| Expired/cancel refundable | TANPA jurnal — `pending_refund`, saldo tertahan di 2-2100 (SEAM Refund Increment 8) |

**Invariant terverifikasi test:** saldo 2-2100 == Σ fee (active + pending_refund); jurnal balanced; tidak pernah menyentuh 4-1000; tanpa float.

## Yang dikirim

- **Migration `000044_booking`** (reversible, teruji up+down real MySQL): tabel `bookings` (CHECK status+disposition+konsistensi silang, UNIQUE satu-aktif-per-unit `active_key` pola budget_plans, index sweep expiry, FK unit/customer); seed idempoten akun 2-2100 utk SEMUA tenant existing (copy type/category dari 2-2000); safeguard aktivasi 4-2000. `coa.go` += 2-2100 (tenant baru).
- **`sale/booking.go`** — model + `BookingStatus` machine (`Terminal()`, `CanTransitionTo`) + `FeeDisposition` + konstanta akun.
- **`sale/booking_repo.go`** — atomic writers (pola BAST): `CreateBookingAtomic` (jurnal→termin→kwitansi→booking→unit `available→booked`+log, SATU tx, TANPA alokasi); `ConvertWithContractAtomic` (**kontrak dibuat DI DALAM tx konversi** → reklas → buyer_credit → booking converted → unit `booked→reserved`+log — atomik penuh, tanpa jendela partial); `CloseBookingAtomic` (cancel/expire + forfeit/pending_refund; resilien bila unit sudah dipindah manual — transisi unit dilewati, disposisi uang tetap jalan); reads + `ListExpiredBookingIDs`. Helper `transitionUnitPinnedTx` reusable.
- **`sale/booking_service.go`** — validasi (fee bulat>0, expiry>booking_date, customer wajib+exists via PartyLookup, unit available, bank COA-driven, akun titipan ada), sweep `MarkExpiredBookings` (idempoten, partial-progress aman), pra-validasi konversi (unit/customer match, unit masih booked). Opt-in `WithBookingStore` (unit test lama tak tersentuh).
- **`CreateContract` + `BookingID`** — konversi satu pintu; D3 existing (reserved→ppjb) menyusul best-effort pasca-commit.
- **API** (semua `/api/v1`): `POST /units/{id}/bookings` · `GET /units/{id}/booking` · `GET /bookings?status=` · `GET /bookings/{id}` · `POST /bookings/{id}/cancel` · `POST /bookings/mark-expired?as_of=` · `POST /sale-contracts` + `booking_id`.

## Test (semua hijau)

- **Unit:** state machine (terminal immutable, tanpa draft), 6 validasi create, store-not-configured, sweep idempoten, pra-validasi konversi (unit/customer mismatch) — store tak tersentuh saat validasi gagal.
- **Integration (real MySQL):** create (saldo 2-2100=fee, termin booking_fee, **0 alokasi**, unit booked, log ref booking, booking kedua ditolak); convert (kontrak+booking converted atomik, 2-2100→0 & 2-2000=fee, 1 baris buyer_credit, unit reserved, urutan log `booking_created→reservation_confirmed`, konversi ulang → 409); expire non-refundable (forfeit 4-2000=fee, unit available, sweep idempoten) + cancel refundable (pending_refund, **nol jurnal baru**, saldo 2-2100 tertahan). Regresi: 16 paket unit + 16 paket integration hijau.

## Seam & catatan

- **Refund cash-out** = Increment 8 (§1) — `pending_refund` hanya penanda; saldo menunggu di 2-2100.
- **Cancellation formal** (entity §1) = Increment 8; booking cancel ringan sudah cukup pra-PPJB.
- `TODO(tax-advisor)`: PPN atas uang muka/titipan properti — gap yang sama dengan termin existing (sistem tidak memungut PPN pra-BAST); bukan gap baru.
- Alur ber-booking otomatis memenuhi guard BAST 6.1 (`booked→reserved→ppjb` → BAST sah); alur non-booking tetap butuh aksi reservasi UI (follow-up F-1, lihat frontend spec).
- Booking + atribusi sales_person = sumber funnel KPI (blueprint §4, read-model menyusul).

## Next

**P0-4 Completion True-Up** (urutan LOCKED; prasyarat Increment 8 scope pasca-BAST). Design v3 FINAL di `p0-4-completion-true-up-design.md` — migrasi 000029–000032 sudah lama terpasang (dead schema) tinggal engine.
