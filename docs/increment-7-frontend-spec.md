# Increment 7 — Booking: Frontend Specification

> Spesifikasi UI untuk Next.js (App Router, pola existing: `lib/api/*` same-origin
> proxy, komponen per fitur, uang SELALU string dari backend — frontend tidak
> menghitung uang). Target kualitas: sekelas Jurnal.id — empty state informatif
> + CTA, status berwarna, angka bisa ditelusuri ke sumber.

---

## 1. Halaman yang diperlukan

| # | Route | Halaman | Prioritas |
|---|---|---|---|
| P1 | `/penjualan/booking` | **Daftar Booking** (semua status, filter) | Wajib |
| P2 | `/proyek/[id]` → UnitBoard (existing) | **Badge status `booked`** + aksi Booking di kartu unit | Wajib |
| P3 | Modal dari UnitBoard / halaman unit | **Form Booking Baru** | Wajib |
| P4 | `/penjualan/booking/[id]` (atau drawer dari P1) | **Detail Booking** + aksi (convert/cancel) | Wajib |
| P5 | `/penjualan/[unitId]` (existing UnitSalePanel) | **Kartu Booking Aktif** di panel penjualan unit | Wajib |
| P6 | Dashboard (existing) | Tile **"Titipan Booking Outstanding"** + funnel mini | Lanjutan |

## 2. Wireframe sederhana

**P1 — Daftar Booking**
```
┌─ Booking ────────────────────────────────────────────────────────┐
│ [Semua ▾] [Aktif] [Terkonversi] [Kedaluwarsa] [Batal]  [+ Booking]│
│ ⚠ 2 booking lewat masa berlaku  [Proses Kedaluwarsa]             │  ← muncul bila ada kandidat
├──────────────────────────────────────────────────────────────────┤
│ Unit    Customer      Fee         Berlaku s/d   Status    Aksi   │
│ BK-01   Budi S.       Rp 5.000.000  15 Jul 26   ● Aktif   [Detail]│
│ BK-02   Rina W.       Rp 5.000.000  01 Aug 26   ✓ Konversi [Detail]│
│ (kosong: "Belum ada booking. Booking mengunci unit dengan        │
│  booking fee sebelum PPJB."  [+ Buat Booking])                   │
└──────────────────────────────────────────────────────────────────┘
```

**P3 — Form Booking Baru (modal, dari kartu unit `available`)**
```
┌─ Booking Unit BK-01 ─────────────────────────────┐
│ Customer*        [cari customer… ▾]  [+ Baru]    │
│ Sales Person     [pilih ▾]                       │
│ Booking Fee*     [Rp ___________]                │
│ Diterima di*     [CashBankSelect ▾]              │  ← komponen existing
│ Tanggal Booking  [2026-07-17]                    │
│ Berlaku s/d*     [2026-07-31]                    │
│ Dapat direfund?  (○) Hangus bila batal (default) │
│                  ( ) Refundable                  │
│ Catatan          [___________________________]   │
│            [Batal]  [Simpan & Terima Fee]        │
│ ℹ Fee dicatat sebagai Titipan (kewajiban),       │
│   kwitansi terbit otomatis.                      │
└──────────────────────────────────────────────────┘
```

**P4 — Detail Booking (aktif)**
```
┌─ Booking #12 — Unit BK-01 ── ● Aktif ────────────────────────────┐
│ Customer: Budi Santoso     Sales: Rina    Berlaku s/d: 15 Jul 26 │
│ Fee: Rp 5.000.000 (hangus bila batal)   Kwitansi: KWT/2026/00071 │
│ Posisi dana: Titipan Booking (2-2100)                            │
├──────────────────────────────────────────────────────────────────┤
│ [→ Konversi ke Kontrak]   [Batalkan Booking]                     │
│ Timeline:                                                        │
│  01 Jul  Booking dibuat, fee diterima (BCA)                      │
└──────────────────────────────────────────────────────────────────┘
```
Terminal (converted): tombol hilang → tautan "Lihat Kontrak #45"; (expired/cancelled):
badge disposisi fee — "Hangus → Pendapatan Lain" / "Menunggu Refund".

## 3. User flow

**Flow A — Booking → Kontrak → DP (happy path):**
1. UnitBoard: kartu unit `available` → aksi **[Booking]** → Form P3 → simpan.
   Unit menjadi `booked` (badge), kwitansi bisa dicetak (PrintReceiptButton existing).
2. Buyer lanjut PPJB: dari P4/P5 klik **[Konversi ke Kontrak]** → membuka **ContractForm existing** dengan `unit_id`, `customer_id`, dan `booking_id` ter-prefill (readonly). Submit → kontrak terbentuk; toast: *"Kontrak dibuat. Booking fee Rp 5jt menjadi Saldo Kredit Buyer."*
3. Buat jadwal (existing ScheduleForm) → di statement, **Saldo Kredit Buyer** (kartu existing FE-3) menunjukkan fee → **[Gunakan Saldo Kredit]** (existing) menutup DP.
4. BAST berjalan normal (unit sudah `ppjb`/`reserved` — guard 6.1 lolos).

**Flow B — Kedaluwarsa:** banner P1 → [Proses Kedaluwarsa] → konfirmasi ("2 booking akan ditutup; fee non-refundable menjadi Pendapatan Lain-lain") → hasil per booking.

**Flow C — Batal:** P4 → [Batalkan] → modal alasan + tanggal kejadian → jika refundable tampilkan info "dana menunggu proses refund (fitur menyusul)".

## 4. State / status (vocabulary UI)

| Status booking | Badge | Fee (disposition) |
|---|---|---|
| `active` | ● biru "Aktif" (+ merah "Lewat masa berlaku" bila expiry < hari ini) | `held` — "Titipan" |
| `converted` | ✓ hijau "Terkonversi" | `transferred` — "Menjadi Saldo Kredit Buyer" |
| `expired` | ◌ abu "Kedaluwarsa" | `forfeited` — "Hangus (Pendapatan Lain)" / `pending_refund` — "Menunggu Refund" |
| `cancelled` | ✕ merah "Dibatalkan" | idem expired |

Status unit (badge UnitBoard, lengkap pasca-Increment 6): `available` hijau ·
`booked` biru muda · `reserved` biru · `ppjb` ungu · `sold` abu tua · `hold`/`blocked`
oranye/merah · `occupied`/`maintenance` abu.

## 5. Aksi utama (dan siapa boleh)

| Aksi | Lokasi | Role | Endpoint |
|---|---|---|---|
| Buat booking + terima fee | UnitBoard/P1 | owner, accountant (RequireWrite) | `POST /units/{id}/bookings` |
| Konversi ke kontrak | P4/P5 → ContractForm | idem | `POST /sale-contracts` (+`booking_id`) |
| Batalkan booking | P4 | idem | `POST /bookings/{id}/cancel` |
| Proses kedaluwarsa (sweep) | Banner P1 | idem | `POST /bookings/mark-expired?as_of=` |
| Lihat/cetak kwitansi fee | P4 | semua | `GET /termins/{terminID}/receipt` (existing) |

## 6. Tabel / list

**Daftar Booking (P1):** kolom Unit (link), Customer (link), Fee (Rupiah, kanan), Refundable (ikon), Berlaku s/d (merah bila lewat), Status (badge), Dibuat oleh/pada. Filter: status (tab), proyek (dropdown), pencarian customer/unit. Sort default: aktif dulu, lalu terbaru. Data: `GET /bookings?status=`.

**Timeline Detail (P4):** gabungan `unit_status_transitions` unit tsb (`GET /units/{id}/transitions` — Increment 6) yang ber-`reference_type=booking` + kolom audit booking (`created_at`, `closed_at`, `closed_event_date`, `close_reason`).

## 7. Dashboard yang terdampak

| Widget | Perubahan | Sumber angka |
|---|---|---|
| Unit pipeline (existing) | + segmen **booked** (funnel: available → booked → reserved/ppjb → sold) | `GET /projects/{id}/units` (status baru sudah dikirim backend) |
| **Titipan Booking Outstanding** (baru) | Saldo akun 2-2100 (derived ledger) | `GET /ledger/reports/trial-balance` (akun 2-2100) — tanpa endpoint baru |
| Pendapatan Lain-lain | Forfeit muncul di L/R | Laporan existing |
| Collection/Statement | Kartu Saldo Kredit Buyer (existing FE-3) kini bisa berasal dari booking fee — label sumber "Dari Booking #N" (`termin.payment_source=booking_fee`) | `GET /sale-contracts/{id}/buyer-credit` (existing) |

## 8. Endpoint yang dipakai (rangkuman kontrak)

```
POST /api/v1/units/{unitID}/bookings
  body : { customer_id*, sales_person_id?, booking_fee*: "5000000",
           refundable: false, bank_account_code*: "1-1300",
           booking_date?: "YYYY-MM-DD", expiry_date*: "YYYY-MM-DD", notes? }
  201  : Booking (lihat bentuk di bawah)
  409  : unit sudah punya booking aktif · 422: unit bukan available / akun titipan hilang
GET  /api/v1/bookings?status=active|converted|expired|cancelled
GET  /api/v1/bookings/{id}
GET  /api/v1/units/{unitID}/booking            → booking aktif unit (404 bila tak ada)
POST /api/v1/bookings/{id}/cancel              { reason, event_date? }   → 409 bila terminal
POST /api/v1/bookings/mark-expired?as_of=YYYY-MM-DD → { "expired": n }
POST /api/v1/sale-contracts                    (+ "booking_id": 12)      → konversi atomik

Bentuk Booking (JSON): id, project_id, unit_id, customer_id, sales_person_id?,
booking_fee (string), refundable, booking_date, expiry_date, status,
fee_disposition, termin_payment_id, converted_contract_id?, close_reason?,
closed_at?, closed_event_date?, notes?, created_at
```

## 9. Follow-up UI yang menyertai (dari Increment 6.1 F-1)

- **Aksi "Reservasi Unit"** di UnitBoard untuk alur TANPA booking (unit `available`
  → `reserved` via `POST /units/{id}/transition` `{status:"reserved", event:"reservation_confirmed", buyer_ref}`) —
  tanpa ini, BAST alur non-booking tertolak 422 oleh guard 6.1.
- Riwayat lifecycle unit (`GET /units/{id}/transitions`) sebagai tab kecil di detail unit.
- `lib/api/booking.ts` baru + perluasan `lib/api/sale.ts` (booking_id di createContract).
