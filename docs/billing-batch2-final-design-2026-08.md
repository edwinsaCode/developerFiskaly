# Billing Batch 2 — Final Design (post K-1..K-5)

**Tanggal:** 2026-08-04 · Menggantikan bagian proposal di `billing-architecture-review-2026-08.md` yang bertentangan dengan keputusan final klien. Review lama tetap berlaku untuk analisis fondasi (§1–§5).

## 1. Business Rule FINAL (dari klien — tidak bisa ditawar)

| # | Rule |
|---|---|
| K-1 | PDAM/BPHTB/Notaris/Listrik = **Titipan Realisasi** (liability). Bukan pendapatan, bukan harga, bukan DP/termin/booking. Terima → Cr liability; bayar vendor → Dr liability. Pendapatan developer TIDAK PERNAH muncul dari transaksi ini. |
| K-2 | Realisasi TIDAK wajib lunas sebelum BAST. Outstanding rumah & realisasi = dua billing terpisah. BAST requirement = **tenant policy** (Require House Outstanding=0 default ON — perilaku existing via scheme gate; Require Realization Outstanding=0 configurable, default OFF). |
| K-3 | Alokasi pembayaran **FLEXIBLE — keputusan admin**. Tidak ada FIFO/proporsional/prioritas otomatis. Sistem hanya validasi: per-item ≤ outstanding item, Σ alokasi == nominal, jurnal balanced. |
| K-4 | Aktual < titipan → sisa titipan BUKAN pendapatan. Aksi admin: **refund** atau **transfer** (ke harga rumah / grup lain). Tidak ada pengakuan pendapatan otomatis. |
| K-5 | Aktual > titipan → sistem membuat **outstanding tambahan** otomatis. Kwitansi & statement selalu tampilkan: total tagihan / bayar ini / total dibayar / sisa — untuk rumah DAN realisasi. |

## 2. Perubahan desain vs proposal awal (audit ulang)

| Desain lama | Final | Alasan |
|---|---|---|
| Routing akun per item via `product_type_code` (rekomendasi K-1 Opsi C) | **DIBUANG.** Satu akun liability `2-2400 Titipan Realisasi` untuk semua item | K-1 final: semuanya titipan. Item cukup `label` |
| `allocation_policy priority\|proportional` di charge_groups | **DIBUANG.** Alokasi eksplisit dari admin per pembayaran | K-3 final |
| `invoices.sale_contract_id` → nullable | **TIDAK JADI** — tetap NOT NULL; tambah `charge_group_id` nullable | Grup selalu di bawah SaleContract (prinsip klien) → migrasi lebih kecil |
| Shadow group `kind=price` + backfill + gate EQ (B2-1/B2-2) | **DIBUANG.** charge_groups hanya `realization\|addon` | Harga rumah tidak disentuh sama sekali → nol risiko ke angka existing, tidak perlu backfill |
| BAST gate hardcode | Tenant policy `require_realization_settled` (kolom `tenants`, pola 000050) | K-2 enterprise requirement |

Semua keputusan anti-duplicate-SoT dari review awal TETAP: satu ledger kas (`termin_payments` + `charge_group_id`), satu sub-ledger alokasi (`payment_allocations` + `charge_item_id`), satu mesin aging (`BuildARAging`), delegasi ringkasan kwitansi, `counts_toward_price=false` untuk semua kas realisasi (primitif harga tidak berubah).

## 3. Model & formula kanonik

```
charge_groups   (kind realization|addon, status open|settled|cancelled,
                 sale_contract_id NOT NULL, unit_id, label)
charge_items    (charge_group_id, label, amount [tagihan berjalan],
                 original_amount [immutable], due_date NULL, status open|cancelled)
charge_item_adjustments (audit setiap perubahan amount: old, new, reason
                 payout_overrun|trueup_settlement, ref payout, actor)
charge_payouts  (charge_item_id, amount, date, bank, vendor, journal_entry_id)
charge_settlements (action refund|transfer_house|transfer_group|void_payment,
                 amount, journal/termin refs, actor)
```

**SATU formula per angka** (semua pembaca — receipt, invoice, statement, aging, dashboard, BAST gate — lewat `charge.Service.GroupSummary`):

```
billed      = Σ item.amount              (item status='open')
paid        = Σ payment_allocations       (type charge_item + charge_item_void, net)
outstanding = billed − paid
payout      = Σ charge_payouts
returned    = Σ settlements refund|transfer_* (bukan void)
residual    = paid − payout − returned    (dana titipan yang masih dipegang)
```

**K-5 (otomatis):** saat payout dicatat dan Σpayout(item) > item.amount → item.amount := Σpayout(item), tercatat di adjustments (reason `payout_overrun`) → outstanding naik.
**K-4 true-up saat settle:** per item `amount := max(Σpayout, Σalloc)` (reason `trueup_settlement`) → outstanding = Σ max(0, payout−alloc); sisa dana muncul sebagai `residual` → wajib refund/transfer dulu; `settled` hanya bila outstanding==0 && residual==0.

## 4. Jurnal (semua via posting service, balanced, append-only)

| Peristiwa | Jurnal |
|---|---|
| Terima pembayaran realisasi | Dr Kas/Bank / **Cr 2-2400 Titipan Realisasi** |
| Payout vendor | Dr 2-2400 / Cr Kas/Bank |
| Refund sisa titipan | Dr 2-2400 / Cr Kas/Bank |
| Transfer → harga rumah | Dr 2-2400 / Cr 2-2000 (pra-BAST) atau akun piutang policy (pasca-BAST) — via seam `sale.ApplyDepositTransfer`, termin `counts_toward_price=TRUE` + alokasi waterfall → masuk primitif kanonik harga TANPA jalur baru |
| Transfer → grup lain | Dr 2-2400 / Cr 2-2400 (memo balanced) + termin non-kas di grup target + alokasi eksplisit admin |
| Void pembayaran | Jurnal pembalik (Dr 2-2400 / Cr Kas) + baris alokasi mirror negatif type `charge_item_void` (histori utuh, Invariant #5) |

Membuat tagihan / invoice TIDAK memposting jurnal (konsisten dengan invoice existing — dokumen, bukan peristiwa ledger; kewajiban baru lahir saat kas diterima, Invariant #7).

Akun `2-2400` ditambahkan ke seed COA + `AccountRoleRegistry` (role `RealizationDeposit`).

## 5. Lifecycle — status penutupan celah

| Lifecycle | Mekanisme | Guard |
|---|---|---|
| Create Charge | POST group + items (kind realization/addon) | kontrak ada, amount bulat > 0, tenant-scoped |
| Partial Payment | termin (source `realization`, counts=false, charge_group_id) | idempotency key termin existing |
| Flexible Allocation | payload `allocations[{item_id, amount}]` dari admin | Σ == amount PERSIS; per-item ≤ outstanding (FOR UPDATE); item open milik grup |
| Outstanding | formula §3 | satu-satunya, tidak di-cache |
| Refund Titipan | settlement `refund` + jurnal | ≤ residual |
| Transfer Titipan | settlement `transfer_house` / `transfer_group` | ≤ residual; house-leg lewat pintu ReceivePayment yang ada |
| Additional Charge | POST item baru di grup open; otomatis dari K-5 | grup open |
| Cancel item | status cancelled | tanpa alokasi & tanpa payout |
| Cancel group | status cancelled | paid==0 && payout==0 |
| Void payment | jurnal pembalik + mirror negatif + settlement `void_payment` | 1× per termin (UNIQUE), termin milik grup |
| Settlement | true-up + status settled | outstanding==0 && residual==0 |
| BAST Gate | `tenants.require_realization_settled` (default 0) → seam di RecordBAST | K-2: default TIDAK menahan |
| Ledger | semua via ledger.PostingService | balanced, immutable |
| Reporting/Aging | item ber-due_date → `[]ARScheduleRow` → `BuildARAging` yang SAMA, laporan terpisah | mesin aging tunggal |
| Dashboard | KPI "Titipan Realisasi" dari GroupSummary (endpoint terpisah) | KPI rumah tidak berubah arti |
| Statement | seksi Biaya Realisasi dari GroupSummary di halaman statement | sumber sama dengan kwitansi |
| Kwitansi | KWR/{yyyy}/{seq} (seri sendiri), tampil 4 angka §K-5 dari GroupSummary | idempoten per termin (UNIQUE existing) |
| Invoice | type `REALISASI`, `charge_group_id`, amount = outstanding kanonik, max 1 unpaid per grup, auto-paid saat outstanding 0 (pola KEKURANGAN) | nomor dari generator existing |

## 6. Arsitektur package (bebas circular import)

```
internal/charge  (BARU) → import: domain, ledger, sale (model termin/alokasi),
                          billing (kwitansi + invoice), reporting (BuildARAging pure)
sale   → TIDAK import charge. Seam baru:
         - sale.ApplyDepositTransfer (generic: pembayaran harga didanai akun liability)
         - sale.RealizationBASTGate interface (RecordBAST) — adapter di main
billing → TIDAK import charge/sale (tetap). Tambahan generik:
         - ReceiptType 'realization' (KWR) — derived dari payment_source (pola KWB)
         - ChargeSummaryProvider utk print kwitansi (adapter di main)
         - GenerateChargeGroupInvoice / SettleChargeInvoice (amount dipasok pemanggil)
```

Tidak ada keputusan bisnis menggantung; tidak ada operasi yang mengubah/menghapus jurnal historis. Implementasi langsung jalan end-to-end.
