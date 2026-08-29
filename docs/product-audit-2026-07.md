# Product Audit — Business Hardening & Executive Experience

> Tanggal: 2026-07-22 · Status: **DIGANTIKAN** — urutan roadmap digantikan product-architecture-review-2026-07.md (approved); temuan audit tetap berlaku sebagai referensi.
> Grounding: seluruh temuan diverifikasi terhadap kode aktual (path disebut per temuan).

---

## 1. Product Audit — apa yang SUDAH benar

Diverifikasi langsung di kode (bukan asumsi):

| Flow | Status | Bukti di kode |
|---|---|---|
| Booking fee ditahan (titipan) | ✅ | `sale/booking.go` — `FeeHeld`, Cr 2-2100 |
| Booking fee → bagian pembayaran (DP) | ✅ | `FeeTransferred` → reklas 2-2000 + buyer credit; tanpa dobel hitung |
| Booking fee hangus | ✅ | `FeeForfeited` → Dr 2-2100 / Cr 4-2000 |
| Booking refund | ✅ | `FeePendingRefund` + domain Refund |
| Pembatalan pra-BAST | ✅ | `cancellation/service.go` |
| Pembatalan pasca-BAST (reversal Event 3+4+true-up) | ✅ | `StagePostBAST`, `service.go:411` |
| Refund ber-penalti | ✅ | cancellation → refund payable → payment |
| Commission clawback | ✅ | `commission/model.go` — Dr 1-2100 / Cr Beban Komisi, `ClawbackJournalID` |
| Pelunasan dipercepat (restrukturisasi jadwal) | ✅ engine | `RegenerateSchedule` (`scheme_flow.go:589`) — baris terbayar tak disentuh, Σ == Gross |
| Pencatatan pencairan KPR (engine) | ✅ seam | `handler.go:264` — RecordTermin menerima `kpr_disbursement` + wajib `financing_source_id`; H-2: outstanding otomatis benar |
| Outstanding satu rumus | ✅ | `sale/financial_summary.go` (H-2) — receipt/invoice/balance satu jalur |
| Milestone KPR (state) | ✅ UI | `FinancingMilestones.tsx` — ajukan→setuju→akad→cair |
| Progress biaya vs RAB, collection, unit, penjualan | ✅ | dashboard + workspace proyek |

## 2. Business Gap Analysis — yang BELUM didukung

Diurutkan berdasarkan nilai bisnis (dampak nyata ke klien Nata Alam):

### GAP-A · Realisasi KPR end-to-end (KRITIS — feedback klien langsung)
Engine bisa menerima pencairan (`kpr_disbursement`), **tapi tidak ada flow yang merangkainya**:
1. Tombol "Catat Pencairan Dana" di `FinancingMilestones` hanya mengubah **state** — tidak mencatat **uang**. Dua aksi terpisah (catat termin + ubah state) tidak dirangkai atomik.
2. Tidak ada input "nilai pencairan" (hampir selalu ≠ plafon: 500jt harga, cair 430jt).
3. **Selisih pencairan** tidak menghasilkan apa-apa: tidak ada invoice kekurangan otomatis (tipe invoice hanya DP/TERMIN/PELUNASAN — `billing/model.go:15`), tidak ada restrukturisasi jadwal otomatis (RegenerateSchedule ada tapi tak dipanggil dari flow ini).
4. **Tidak ada KPR pipeline view**: tak ada satu layar "semua kontrak KPR & posisinya" (agregat `scheme_state` tidak dipakai reporting mana pun — diverifikasi grep kosong).

### GAP-B · Progress pembangunan FISIK (KRITIS — Prioritas 3 klien)
**Tidak ada model progress fisik sama sekali** (grep `physical|fisik|construction_progress` → hanya status unit `occupied`). "Progress" saat ini = progress **biaya** (realisasi/RAB) — owner tidak bisa melihat "fisik 35% vs biaya 58% ⚠️". Butuh entitas baru (operasional, bukan ledger — bukan duplicate SoT keuangan).

### GAP-C · Booking fee DI LUAR harga unit
Konversi booking **selalu** mereklas fee menjadi bagian pembayaran. Praktik umum: booking fee sebagai **biaya administrasi** yang TIDAK mengurangi harga (langsung jadi pendapatan lain saat konversi). Butuh opsi disposisi per booking (`counts_toward_price: bool`) + jurnal konversi bercabang → **menyentuh posting jurnal → butuh design review terpisah**.

### GAP-D · Invoice kekurangan pembayaran
Kekurangan (mis. pasca-pencairan bank) harus ditagihkan manual dengan menghitung sendiri. Harusnya: satu aksi → jadwal sisa + invoice "KEKURANGAN" dari `ContractFinancialSummary.Outstanding` (sumber sudah ada sejak H-2).

### GAP-E · Flow lain yang lazim tapi belum ada (nilai menengah–rendah)
- **AJB & biaya balik nama** (pasca-BAST, sering ditagih terpisah) — belum dimodelkan.
- **Retensi kontraktor** (biaya) — belum; relevan saat modul biaya vendor diperdalam.
- **PPN DTP / insentif pemerintah** — TaxRule seam ada, formula belum diregister.
- **Denda keterlambatan cicilan** — schedule overdue ada, denda tidak dihitung.
- **Kop legal dokumen** (alamat + NPWP tenant) — backlog H-2, referensi klien mencantumkannya.

## 3. UX Gap Analysis (audit sebagai user baru)

| # | Friction | Lokasi | Beban user |
|---|---|---|---|
| U1 | Jadwal cicilan tidak auto-generate dari skema (KPR DP 10% harus diketik manual) | UnitSalePanel | Tinggi |
| U2 | Halaman Invoice Customer polos: tanpa filter/search/CTA/ringkasan status | `/accounting/invoices` | Sedang |
| U3 | "Catat Pencairan" tidak menanyakan nominal (lihat GAP-A) | FinancingMilestones | Tinggi |
| U4 | `?tab=` tidak dihormati router tab proyek (deep-link patah) | ProyekDetailTabs | Sedang |
| U5 | Tidak ada bulk action (mis. tandai terkirim banyak invoice, approve banyak komisi — komisi sudah ada; invoice belum) | beberapa tabel | Sedang |
| U6 | Tidak ada global search / command palette (lompat ke unit/customer/kontrak) | global | Sedang |
| U7 | Mobile: tabel lebar tanpa mode kartu; sidebar tak kolaps | global | Sedang |
| U8 | Beberapa empty state lama belum ber-CTA (halaman lama pra-PS-8) | tersebar | Rendah |
| U9 | Keyboard: fokus ring sudah ada; belum ada shortcut (mis. `/` cari, `n` baru) | global | Rendah |

## 4. Dashboard 2.0 — Improvement Proposal

Per seksi permintaan owner — **ada vs kurang** (semua sumber = accounting engine, no duplicate SoT):

| Seksi | Sudah ada | Kurang (proposal) |
|---|---|---|
| Posisi Keuangan | kas, piutang, utang, titipan, cash in/out, runway | **Outstanding total** (Σ `ContractFinancialSummary.Outstanding` kontrak aktif — endpoint additive), **Forecast kas 30/60/90** (reuse query Collection — sudah dihitung, hanya belum tampil di dashboard) |
| Pengendalian Proyek | RAB vs realisasi, variance, budget health | **Progress fisik** (butuh GAP-B), **Top cost category** (dari rab-vs-realisasi, urutkan realisasi desc), over-budget alert → masuk attention strip |
| Penjualan | funnel, ranking, heatmap | **Conversion rate %** antar stage (angka, bukan hanya bar) |
| KPR | — (kosong total) | **Seksi baru**: count per state (pengajuan/disetujui/akad/cair) dari `scheme_state`, Σ pencairan vs Σ plafon (selisih realisasi), outstanding KPR — endpoint reporting additive |
| Collection | rate per proyek, overdue count | **Top customer menunggak** (dari collection rows, urutkan outstanding), prediksi kas ke dashboard |
| Profitability | revenue/HPP/margin per proyek | **Profit per unit** (dari `sale_records` — data sudah ada), **margin waterfall** (Pendapatan → −HPP → −Beban → Margin; dari income-statement) |

Prinsip: KPI strip menjawab "30 detik pagi hari" → baris teratas jadi: Kas · Outstanding · Overdue · Runway · Margin MTD + attention strip diperluas (over-budget, KPR macet >30 hari di satu state).

## 5. Project Control (Prioritas 3) — rancangan

Workspace proyek Overview menjadi kartu kontrol:

```
Fisik 35% ▓▓▓░░░░░░ (input manual per fase, tanggal+catatan)
Biaya 58% ▓▓▓▓▓░░░░ (ledger — sudah ada)
⚠️ Biaya berjalan 23pt di depan fisik — indikasi over budget / prestasi tertinggal
```

Model baru (operasional, additive): `project_progress_entries(project_id, phase_id?, progress_pct, as_of_date, notes, created_by)` — append-only, kurva-S sederhana dari riwayatnya. **Bukan** data akuntansi; tidak menyentuh ledger. Status ⚠️ = rule sederhana: biaya% − fisik% > ambang (mis. 15pt) ATAU budget health Over.

## 6. New Product Roadmap (menunggu approval)

| Sprint | Isi | Nilai bisnis | Risiko |
|---|---|---|---|
| **S1 — KPR Realization** | Modal "Catat Pencairan" (nominal + bank + tanggal) → atomik: termin `kpr_disbursement` + state `disbursed`; selisih → tawarkan invoice KEKURANGAN + regenerate jadwal sisa; KPR pipeline board; seksi KPR dashboard | ⭐⭐⭐⭐⭐ feedback klien langsung; fondasi H-2 sudah siap | Rendah–sedang: pakai ReceivePayment existing; invoice tipe baru additive; TIDAK menyentuh jurnal (Event 2 biasa) |
| **S2 — Progress Fisik & Project Control** | Model progress fisik + input UI + kartu kontrol workspace + dashboard fisik-vs-biaya | ⭐⭐⭐⭐⭐ permintaan eksplisit klien | Rendah: additive murni, non-ledger; risiko = disiplin input manual |
| **S3 — Executive Dashboard 2.0** | Outstanding total, forecast kas, seksi KPR, top penunggak, profit per unit, margin waterfall, conversion %, alert diperluas | ⭐⭐⭐⭐ | Rendah: endpoint reporting read-only; perhatikan berat query komposit (ukur, pecah bila perlu) |
| **S4 — UX Hardening batch** | U1 auto-schedule dari skema, U2 invoice page, U4 deep-link tab, U5 bulk invoice, U8 empty states | ⭐⭐⭐⭐ (U1 mengurangi kerja terbanyak) | Rendah; U1 sentuh generator jadwal (pakai schedule-plan endpoint yang sudah ada) |
| **S5 — Enterprise Design pass** | Density & tabel kelas Linear/Stripe, command palette + shortcut (U6/U9), mobile kartu (U7) | ⭐⭐⭐ | Rendah; murni frontend |
| **S6 — Booking fee di luar harga (GAP-C)** | Opsi disposisi fee per booking + jurnal konversi bercabang | ⭐⭐⭐ | **Sedang-tinggi: mengubah posting konversi → WAJIB design review akuntansi terpisah sebelum kode** |
| Backlog | GAP-E: AJB/balik nama, denda telat, retensi, PPN DTP, kop legal tenant | ⭐⭐ | per item |

## 7. Risiko perubahan (ringkasan)

1. **S6 menyentuh jurnal konversi booking** — satu-satunya item roadmap yang menyentuh posting; dipisahkan dan digerbangi design review.
2. **Progress fisik = input manual** — kualitas data bergantung disiplin; mitigasi: append-only + siapa/kapan tercatat, tampilkan "terakhir update X hari lalu".
3. **Dashboard komposit membengkak** — tiap seksi baru menambah query; mitigasi: ukur latency, seksi berat dipecah jadi endpoint lazy per-seksi.
4. **Invoice KEKURANGAN** — harus SELALU dihitung dari `ContractFinancialSummary` (H-2), tidak boleh ada rumus kedua.
5. Semua lainnya additive & backward compatible; tidak ada perubahan ledger/histori.
