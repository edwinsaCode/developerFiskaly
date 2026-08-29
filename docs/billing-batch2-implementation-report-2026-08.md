# Billing Batch 2 — Laporan Implementasi & Verifikasi

**Tanggal:** 2026-08-04
**Basis keputusan:** K-1 s/d K-5 (keputusan final klien, menggantikan seluruh asumsi/rekomendasi audit sebelumnya)
**Desain acuan:** `docs/billing-batch2-final-design-2026-08.md`
**Status:** SHIPPED — build hijau, unit + integration test PASS, lifecycle E2E terverifikasi lewat HTTP terhadap API + DB nyata.

---

## 1. Aturan bisnis yang dikunci

| Kode | Aturan final klien | Konsekuensi teknis |
|---|---|---|
| **K-1** | PDAM, BPHTB, Notaris, Listrik = **Titipan Realisasi**. Bukan pendapatan developer, bukan bagian harga rumah, bukan DP/termin/booking. Uang masuk → liability; bayar vendor → mengurangi liability. | Semua uang realisasi menyentuh **2-2400 Titipan Realisasi** saja. Tidak ada akun 4-xxxx yang boleh tersentuh. Harga rumah (`sale_contracts`) tidak berubah sepeser pun. |
| **K-2** | Biaya realisasi **TIDAK wajib lunas** sebelum BAST. "Kalau sudah akad terus BAST masih ada terutang nanti tetap masuk piutang." Outstanding rumah dan outstanding realisasi = dua billing terpisah. **Jangan hardcode** — jadikan Company/Tenant Policy. | Dua gate terpisah: *Require House Outstanding = 0* (default aktif) dan *Require Realization Outstanding = 0* (`tenants.require_realization_settled`, default **mati** sesuai keputusan klien). |
| **K-3** | Alokasi pembayaran **FLEKSIBEL** — keputusan admin, tanpa FIFO/proporsional. | Sistem hanya memvalidasi: alokasi per item ≤ sisa item, Σ alokasi == nominal bayar, jurnal balanced. Tidak ada auto-allocation. |
| **K-4** | Aktual < titipan → sisa **BUKAN pendapatan**. Aksi admin: refund atau transfer. | Endpoint `refund`, `transfer-group`, `transfer-house`. Sisa titipan hanya bisa keluar lewat tiga jalur ini. |
| **K-5** | Aktual > titipan → outstanding tambahan **otomatis**. Kwitansi & statement selalu menampilkan 4 angka. | Payout > sisa item menaikkan `charge_items.amount` otomatis, menyimpan `original_amount` + baris audit `charge_item_adjustments` bertipe `payout_overrun`. |

---

## 2. Model data (migration `000059_charge_groups`)

Tabel baru:

| Tabel | Isi |
|---|---|
| `charge_groups` | Grup tagihan per kontrak. `kind` = `realization` \| `addon`; `status` = `open` \| `settled` \| `cancelled`. |
| `charge_items` | Item tagihan (PDAM/BPHTB/Notaris/Listrik/dst). `amount`, `original_amount`, `status`. |
| `charge_item_adjustments` | Jejak audit setiap perubahan `amount` (mis. `payout_overrun` + id payout pemicu). |
| `charge_payouts` | Pembayaran ke vendor. Unik per `(tenant_id, idempotency_key)`. |
| `charge_settlements` | Disposisi sisa titipan: `refund`, `transfer_group`, `transfer_house`. |

Kolom tambahan pada tabel yang sudah ada: `termin_payments.charge_group_id`, `payment_allocations.charge_item_id` + `charge_item_key`, `receipts.charge_group_id`, `invoices.charge_group_id`, `tenants.require_realization_settled`.

Semua kolom uang `DECIMAL(20,4) NOT NULL DEFAULT '0.0000'`; semua tabel ber-`tenant_id` + index; tidak ada AutoMigrate.

---

## 3. Rumus kanonik (satu sumber kebenaran)

Seluruh angka realisasi — panel kontrak, statement, kwitansi, invoice, portfolio, aging, dashboard — dihitung dari **satu** fungsi: `charge.Service.GroupSummary`.

```
billed      = Σ charge_items.amount   (status = open)
paid        = Σ payment_allocations   (charge_item + charge_item_void, neto)
outstanding = billed − paid
payout      = Σ charge_payouts
returned    = Σ charge_settlements (refund | transfer_group | transfer_house)
residual    = paid − payout − returned
```

Tidak ada satu pun konsumen yang menghitung ulang lewat SQL sendiri — invariant SSOT dari fase sebelumnya tetap utuh.

---

## 4. Peta jurnal

| Aksi | Debit | Kredit |
|---|---|---|
| Terima titipan | 1-1100 Kas/Bank | 2-2400 Titipan Realisasi |
| Bayar vendor (payout) | 2-2400 | 1-1100 |
| Refund ke pembeli | 2-2400 | 1-1100 |
| Transfer ke grup lain | 2-2400 | 2-2400 (memo grup tujuan) |
| Transfer ke harga rumah | 2-2400 | 2-2000 Uang Muka Penjualan |
| Void pembayaran | jurnal pembalik (append-only) + alokasi cermin negatif `charge_item_void` |

**Tidak ada akun pendapatan di mana pun** — K-1 ditegakkan secara struktural, bukan lewat konvensi.

---

## 5. Dua cacat yang ditemukan saat smoke test (dan diperbaiki)

### 5.1 Lubang idempotency pada payout & refund → sekarang HTTP 409

`RecordPayout` dan `Refund` sama sekali tidak punya replay guard. Mengirim ulang idempotency key yang sama mengembalikan **HTTP 500 dengan error SQL mentah** (`Duplicate entry '…' for key 'charge_payouts.uk_cp_idem'`) — kegagalan yang bocor ke klien dan tidak bisa dibedakan dari error server.

Perbaikan (`backend/internal/charge/service.go`):
- `RecordPayout` dan `Refund` kini memeriksa key yang sudah ada di dalam transaksi; replay dengan sasaran sama → kembalikan hasil pertama (tanpa payout/jurnal kedua).
- `TransferToGroup`, `TransferToHouse`, `findExistingPayment`: guard diperketat — key yang dipakai ulang untuk **sasaran berbeda** ditolak, bukan diam-diam mengembalikan transaksi lain.
- Error baru `ErrIdempotencyConflict` (`errors.go`) dipetakan ke **409 Conflict** di `handler.go`.

Terverifikasi: replay → 201 dengan summary identik dan tidak ada jurnal kedua; key dipakai ulang untuk item lain → 409.

### 5.2 Gate BAST harga rumah bocor sebagai HTTP 500 → sekarang 422

`scheme.ErrBASTGateNotMet` (gate outstanding harga rumah) jatuh ke cabang default dan keluar sebagai **500**, padahal gate saudaranya (`ErrRealizationOutstandingBAST`) sudah 422. Dua gate kebijakan yang sama sifatnya memberi kode status berbeda — penolakan bisnis tampil sebagai kerusakan server.

Perbaikan (`backend/internal/sale/handler.go`): `scheme.ErrBASTGateNotMet` dimasukkan ke cabang 422 bersama gate realisasi. Terverifikasi → 422 dengan pesan bisnisnya.

---

## 6. Bukti verifikasi end-to-end

Dijalankan lewat HTTP terhadap API nyata + verifikasi langsung ke MySQL (tenant `9900245`, unit `1433`, kontrak `775`).

**K-1 — tidak ada pendapatan.** Setiap jurnal yang dihasilkan hanya menyentuh 1-1100 / 2-2400 / 2-2000, termasuk pembayaran realisasi **pasca-BAST** (jurnal 2398: Dr 1-1100 / Cr 2-2400). Harga rumah pada kontrak tidak berubah.

**K-3 — alokasi manual.** Alokasi admin diterima apa adanya; Σ alokasi ≠ nominal → 400; alokasi > sisa item → ditolak.

**K-5 — outstanding tambahan otomatis.** Payout Rp 6.000.000 atas item Rp 5.000.000 menaikkan `amount` menjadi 6.000.000, `original_amount` 5.000.000 tersimpan, baris audit `payout_overrun` terhubung ke id payout pemicu. Kwitansi menampilkan empat angka (21jt total tagihan / 6jt bayar ini / 21jt total dibayar / 0 sisa).

**K-4 — disposisi sisa.** Refund > residual → 400. Refund 1jt, transfer-group 1jt (grup addon `paid` 1jt), transfer-house 1jt (termin 1450 dengan `counts_toward_price=1`, `payment_source=realization_transfer`). Settle ditolak selama residual > 0, lalu `settled: true` setelah residual nol.

**Void.** Jurnal asli 2393 tetap utuh + jurnal pembalik 2394 (ledger append-only), alokasi cermin `charge_item` +3jt / `charge_item_void` −3jt. Void kedua → 409. Cancel grup diizinkan setelah void; cancel saat masih ada uang → 409.

**K-2 — dua gate terpisah (terbukti end-to-end).** Policy ON + outstanding realisasi → BAST ditolak **422** dengan pesan bisnis. Policy OFF (default klien) → BAST berhasil (record 366) dan grup realisasi tetap `open` sebagai piutang — persis instruksi klien.

**Konsistensi silang.** Tidak ada jurnal tidak balanced. Neraca 2-2400 = Rp 6.000.000 == residual portfolio. Aging memakai engine `BuildARAging` yang sama (bucket b1_30 1jt, b31_60 2jt). Invoice tipe REALISASI auto-`paid` saat outstanding nol. Seri kwitansi KWR (realisasi) terpisah dari KWT (harga rumah) dan KWB (booking).

---

## 7. Status build & test

| Pemeriksaan | Hasil |
|---|---|
| `go build ./...` | hijau |
| `go test ./internal/charge/...` (unit) | PASS |
| `go test -tags=integration ./internal/charge/...` (`TEST_DB_DSN` diisi) | PASS — `TestIntegration_ChargeGroup_FullLifecycle`, `TestIntegration_ChargeGroup_VoidAndBASTGate` |
| `npm run build` (frontend, produksi) | PASS — seluruh route terkompilasi |

Catatan build frontend: `.next/` berisi artefak milik `root` sisa build docker 30 Juli yang memblokir build (`EACCES … .next/trace`). Direktori lama dipindahkan ke `frontend/.next.root-artifacts-20260730` (bukan dihapus, karena butuh root) dan build ulang menghasilkan `.next/` baru milik user. Direktori sisa itu aman dihapus kapan saja dengan `sudo rm -rf frontend/.next.root-artifacts-20260730`.

---

## 8. Berkas yang dikirim

**Backend**
- `backend/migrations/000059_charge_groups.{up,down}.sql` (170 baris up)
- `backend/internal/charge/` — `model.go`, `repository.go`, `service.go`, `query.go`, `handler.go`, `errors.go` (± 2.800 baris)
- `backend/internal/charge/validate_test.go`, `charge_integration_test.go` (565 baris)
- `backend/internal/sale/handler.go` — pemetaan status gate BAST

**Frontend**
- `frontend/lib/api/charge.ts` — klien seluruh endpoint lifecycle
- `frontend/components/penjualan/ChargeGroupsPanel.tsx` (967 baris) — panel grup tagihan di halaman penjualan
- `frontend/components/accounting/RealizationStatementSection.tsx` — bagian realisasi di Statement 360
- `frontend/components/settings/BillingPolicySection.tsx` — toggle Tenant Policy K-2

**Endpoint** (`/charges`): `GET groups`, `GET groups/{id}`, `GET portfolio`, `GET aging`, `GET|PUT policy`; `POST groups`, `groups/{id}/items|payments|refund|transfer-house|transfer-group|settle|cancel|invoice`, `items/{id}/payouts|cancel`, `payments/{terminID}/void`.

---

## 9. Arsitektur package (tanpa import siklik)

`internal/charge` mengimpor `domain`, `ledger`, `sale`, `billing`, `reporting`.
`internal/sale` **tidak pernah** mengimpor `charge` — sambungan dilakukan lewat seam: `sale.ApplyDepositTransfer` (transfer titipan → harga rumah) dan `sale.RealizationBASTGate` (gate K-2). Aturan import CLAUDE.md tetap utuh.

---

## 10. Yang sengaja tidak dikerjakan

- **Urutan gate di `sale.RecordBAST` tidak diubah.** Gate realisasi berjalan setelah gate skema harga rumah. Menukar urutan akan mengubah presedensi pesan error untuk flow yang sudah berjalan tanpa manfaat bisnis.
- **Aturan pajak atas titipan realisasi tidak ditebak.** Titipan bukan pendapatan, jadi tidak ada pengakuan pajak yang dibuat. Bila kelak ada pertanyaan perlakuan PPN/PPh atas dana titipan, tandai `// TODO(tax-advisor):` — jangan mengarang.
- **Anomali yang dicatat, tidak reproducible.** Di awal sesi, `tenants.require_realization_settled` untuk tenant 9900245 terbaca `0` padahal PUT sesi sebelumnya mengembalikan `true`. Pengujian ulang PUT → GET → DB menunjukkan persistensi benar (DB = 1). Cleanup integration test ber-scope tenant 9900244, jadi bukan penyebabnya. Belum ada penjelasan; diawasi bila muncul lagi.
