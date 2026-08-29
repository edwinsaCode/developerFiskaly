# Audit & Rencana — Booking Fee = PENDAPATAN BOOKING (Business Rule Final Klien)

> 2026-07-29 · Menggantikan seluruh asumsi R4 (booking = liability + disposisi Opsi A).
> **Rule klien (Source of Truth):** saat fee diterima → `Dr Kas/Bank / Cr Pendapatan
> Booking`. Bukan liability, bukan deposit, bukan DP, bukan buyer credit, tidak pernah
> mengurangi harga/outstanding, tidak pernah direklas, tidak ada refund/reversal saat
> batal, tidak dipindah ke Penjualan Rumah saat jadi beli. Booking hanya muncul di
> Laba Rugi sebagai **Pendapatan Booking** (akun tersendiri).

---

## A · HASIL AUDIT — semua titik yang masih mengasumsikan rule lama

### A1 · Ledger / COA / Migration
| Lokasi | Asumsi lama | Aksi |
|---|---|---|
| `ledger/coa.go:56` — seed `2-2100 Titipan Booking (Liability)` | booking = kewajiban | Akun TETAP (histori legacy memakainya); seed +akun baru `4-2100 Pendapatan Booking` |
| `ledger/coa.go` — tidak ada akun Pendapatan Booking | — | **Migration 000053**: insert `4-2100` utk semua tenant existing (idempoten, pola 000045 §5) + tambah ke seed |
| `migrations/000044_booking.*` | menciptakan 2-2100 | Legacy — biarkan (append-only) |
| `migrations/000045_cancellation_refund.up.sql:109-115` CHECK bookings | disposisi liability-only | Digantikan CHECK baru di 000053 |
| `migrations/000052_booking_fee_outside_price.*` | R4: fee held di 2-2100 + disposisi manual | **SEBAGIAN OBSOLETE** (lihat §C). Kolom `counts_toward_price` TETAP DIPAKAI (fondasi "tidak mengurangi harga"). CHECK-nya digantikan 000053 (izinkan `recognized`) |
| `ledger/account_role.go` — `RoleBookingLiability = 2-2100` | booking liability role | TETAP (pembaca saldo legacy); tambah komentar legacy |

### A2 · Backend — penulis jurnal & lifecycle booking (`internal/sale`)
| Lokasi | Asumsi lama | Aksi |
|---|---|---|
| `booking_service.go:92-134` `CreateBooking` | resolve 2-2100; jurnal `Cr Titipan (kewajiban)`; disposition `held` | Jurnal → `Cr 4-2100 Pendapatan Booking`; disposition `recognized`; error baru bila 4-2100 belum ada |
| `booking_repo.go:52-135` `CreateBookingAtomic` | termin `CreditAccountCode=2-2100` | CreditAccountCode dari service (4-2100); termin tetap dibuat (audit + kwitansi + EQ kas), `counts_toward_price=FALSE` tetap |
| `booking_repo.go:137-260` `ConvertWithContractAtomic` | branch flag: FALSE → disposition `held` (menunggu disposisi) | FALSE + disposition `recognized` → disposition TETAP `recognized` (tidak ada yang menunggu); jalur TRUE (histori pra-R4 in-flight) TETAP: reklas + buyer_credit |
| `booking_repo.go:262-370` `CloseBookingAtomic` (cancel/expire) | non-refundable → jurnal forfeit Dr 2-2100/Cr 4-2000; refundable → `pending_refund` | Booking `recognized` → **TANPA jurnal, TANPA refund** (pendapatan sudah final); disposition tetap `recognized`. Jalur lama tetap utk baris legacy (held) |
| `booking_repo.go` `DisposeConvertedFeeAtomic` (R4 Opsi A) | fee held pasca-konversi butuh disposisi | **OBSOLETE utk booking baru** (tidak pernah held). Dipertahankan sebagai alat pembersih LEGACY (guard `converted+held` sudah otomatis menolak booking recognized) |
| `booking.go:54-67` enum `FeeDisposition`, konst `accountCodeTitipanBooking`, header komentar flow | lifecycle liability | +`FeeRecognized="recognized"`; +konst `accountCodePendapatanBooking="4-2100"`; komentar flow ditulis ulang |
| `booking_service.go` `CreateBookingRequest.Refundable` | ada konsep refund | Field tetap (kompat legacy) tapi **DIABAIKAN** utk booking baru (tidak ada refund — rule klien); FE berhenti mengirim |
| `errors.go` `ErrTitipanAccountMissing` | — | +`ErrBookingRevenueAccountMissing` (4-2100) |
| `model.go:60-62` komentar `PaymentSourceBookingFee` | "kredit ke Titipan 2-2100" | Update komentar |

### A3 · Backend — pembaca yang menyentuh booking
| Lokasi | Asumsi lama | Aksi |
|---|---|---|
| `sale/statement.go` — payments memuat baris `booking_fee` | booking bagian perjalanan pembayaran kontrak | **Exclude** baris `counts_toward_price=FALSE` dari statement (statement = murni pembayaran RUMAH). Baris fee pra-R4 (TRUE, historis bagian harga) TETAP tampil |
| `sale/financial_summary.go:15` komentar TotalDibayar | "booking_fee yang dikonversi" | Sudah benar via flag; update komentar |
| `reporting/dashboard.go:47,53,279` KPI `booking_deposits` (2-2100) + `booking_fee_held` (Σ held) | fee tertahan sebagai titipan | TETAP sebagai indikator **LEGACY** (saldo lama menuju nol); rekonsiliasi dokumen↔ledger tetap tegak karena booking baru tidak pernah `held`. Label FE diperjelas |
| `reporting` collected/outstanding/KPR/invoice/collection | — | ✅ SUDAH BENAR sejak R4/S-series: semua via `SumTerminsByUnit(counts_toward_price=TRUE)` — booking fee (FALSE) otomatis tidak pernah masuk outstanding, invoice KEKURANGAN, collection rate, KPR shortfall, PortfolioFinancials |
| `reporting` revenue/P&L | — | ✅ Otomatis benar: 4-2100 ikut `ComputePL` (semua 4-%) per keputusan PO #1; tampil sebagai baris akun tersendiri di Laba Rugi |
| `billing` kwitansi fee | — | Kwitansi tetap terbit (bukti terima); tidak ada perubahan |
| `cancellation/refund_service.go` `CreateBookingRefund`/`PayRefund` | booking pending_refund di 2-2100 | LEGACY-only (guard `pending_refund` otomatis); booking baru tidak pernah masuk sini. Komentar legacy |
| `cancellation/service.go:20` `accTitipanBooking` | — | Dipakai jalur legacy refund — tetap |

### A4 · Test yang mengunci rule lama
| Test | Asumsi | Aksi |
|---|---|---|
| `sale/booking_test.go` (unit, mock) | cancel → forfeited/pending_refund; convert → held | Update ekspektasi kebijakan baru (`recognized` sepanjang lifecycle) + kasus legacy |
| `sale/booking_integration_test.go` | 2-2100 terisi saat create; forfeit 4-2000 saat expire; pending_refund saat cancel refundable; konversi held; disposisi Opsi A | Tulis ulang utk kebijakan baru (4-2100 terisi saat create; cancel/expire TANPA jurnal; konversi recognized) + pertahankan test cutover legacy (simulasi baris lama) |
| `reporting/consistency_integration_test.go` | EQ_BookingLiability (Σ aktif == 2-2100); EQ_R4 (disposition held) | 2-2100 skenario = 0 (booking baru tak menyentuhnya); +**EQ_BookingRevenue**: saldo `4-2100` == Σ fee booking kebijakan baru (dokumen↔ledger); disposition `recognized`; revenue proyek kini termasuk Pendapatan Booking |
| `cancellation/cancellation_integration_test.go:504` cek fee_disposition | jalur refund booking | Verifikasi tetap lulus (jalur legacy tak berubah); sesuaikan bila menyemai booking via service baru |

### A5 · Frontend
| Lokasi | Asumsi | Aksi |
|---|---|---|
| `BookingFormModal.tsx` checkbox `refundable` | ada refund | **Hapus** checkbox (tidak ada refund — rule klien); berhenti kirim field |
| `BookingBoard.tsx:95,297-299` copy "hangus jadi Pendapatan Lain-lain"/"menunggu refund" | forfeit/refund | Copy baru: "fee sudah diakui sebagai Pendapatan Booking; pembatalan tidak mengubah pendapatan" |
| `bookingUi.tsx` `FeeDispositionBadge` | held/transferred/forfeited/pending_refund/refunded | +varian `recognized` ("Pendapatan Booking") |
| `BookingBoard.tsx:249` tombol proses refund | refund booking | Hanya tampil utk baris legacy `pending_refund` (sudah dikondisikan) — tetap |
| `DashboardV2.tsx:298,373` "Titipan booking" | titipan | Label → "Titipan booking (legacy)" |
| `CancellationCenter.tsx:242` copy refund booking | refund booking | Copy: legacy saja |
| `CustomerStatementView.tsx` badge "di luar harga" | R4 menampilkan fee di statement | Baris fee tidak lagi dikirim backend; badge tetap (defensif, tidak aktif) |
| `UnitSalePanel.tsx:215` badge disposisi | — | Otomatis benar via badge baru |

### A6 · Endpoint
| Endpoint | Status |
|---|---|
| `POST /units/{id}/bookings` | Berubah perilaku jurnal (Cr 4-2100) — kontrak API tetap |
| `POST /bookings/{id}/cancel`, `/mark-expired` | Berubah perilaku (tanpa jurnal utk booking baru) — kontrak tetap |
| `POST /bookings/{id}/fee-disposition` | **LEGACY** (hanya baris R4 converted+held); booking baru selalu 409 |
| Refund endpoints (cancellation) | LEGACY utk booking; jalur kontrak tidak berubah |
| Report/dashboard/statement | Payload sama; statement tidak lagi memuat baris fee |

## B · Yang SUDAH sesuai rule baru (tidak perlu diubah)
`SumTerminsByUnit(counts_toward_price)` + semua turunannya (outstanding, invoice
KEKURANGAN, collection, KPR, PortfolioFinancials, sales performance, TotalAdvance,
BAST advance) — booking fee FALSE sudah di luar harga sejak R4. `ComputePL` otomatis
menampilkan 4-2100. Konversi kontrak jalur-FALSE sudah tanpa reklas/buyer_credit.

## C · Artefak R4 yang OBSOLETE + cara ganti tanpa technical debt
1. **Konsep "fee menetap di 2-2100 + disposisi manual (Opsi A)"** — obsolete untuk
   booking baru. Pengganti: pengakuan pendapatan LANGSUNG saat terima (4-2100).
   Jalur disposisi TIDAK dihapus: menjadi **legacy rail** yang dibutuhkan baris
   histori (booking berstatus held/pending_refund yang sudah ada di DB) — dihapus
   nanti setelah saldo legacy 2-2100 nol (dipantau KPI legacy).
2. **Jurnal forfeit (Dr 2-2100/Cr 4-2000) & refund utk booking baru** — obsolete;
   tidak pernah terpicu karena branch data-driven (`fee_disposition='recognized'`).
3. **Migration 000052** — TIDAK direvisi/direvert (append-only): kolom
   `counts_toward_price` justru fondasi rule baru; hanya CHECK-nya digantikan 000053.
4. **Design doc `r4-booking-fee-outside-price-design.md`** — ditandai OBSOLETE
   (banner) dengan rujukan ke dokumen ini.
5. Branch data-driven (bukan berdasarkan waktu deploy) = tidak ada dead code yang
   diam-diam salah: setiap baris histori diproses persis dengan rule saat ia dibuat.

## D · Rencana implementasi (satu rangkaian)
1. **Migration 000053_booking_fee_revenue**: akun `4-2100 Pendapatan Booking`
   (semua tenant, idempoten) + CHECK bookings baru (izinkan `recognized` di semua
   status) + index tidak berubah. Down: kembalikan CHECK 000052 (akun tidak dihapus
   bila berjurnal — pola 000044).
2. **Seed COA** (`ledger/coa.go`): +4-2100.
3. **sale**: konst + enum + CreateBooking (jurnal revenue, disposition recognized,
   abaikan refundable) + CreateBookingAtomic (credit account param) + CloseBookingAtomic
   branch recognized (tanpa jurnal) + ConvertWithContractAtomic pertahankan recognized +
   komentar/error.
4. **Statement**: exclude counts_toward_price=FALSE.
5. **Suite**: skenario & EQ baru (EQ_BookingRevenue; 2-2100=0; revenue termasuk
   Pendapatan Booking); update booking unit+integration tests (kebijakan baru + legacy).
6. **FE**: form (hapus refundable), board copy, badge recognized, label KPI legacy.
7. **Dokumen**: banner OBSOLETE di design R4; update registry #5; memory.
8. **Verifikasi**: build+vet+unit+integration penuh, consistency+strict, tsc, smoke
   API nyata (create booking → cek jurnal 4-2100 → cancel → tidak ada jurnal baru).
Invariant dijaga: jurnal balanced, append-only (nol rewrite histori), satu rule per
baris data, satu SoT (flag + disposition), laporan konsisten via suite.
