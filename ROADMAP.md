# esaProperti — Roadmap Build

Rencana berfase yang dirancang untuk dieksekusi oleh **Claude Code**. Bangun **satu fase dalam satu waktu, berurutan.** Setiap fase punya tujuan, deliverable, dan **Definition of Done (DoD)** yang harus lulus sebelum lanjut. Item DoD sebagian besar berupa test — untuk sistem akuntansi, "berhasil compile dan demonya kelihatan benar" itu belum selesai.

---

## Cara memakai roadmap ini

- **Sekuensial.** Fase N bergantung pada Fase N−1. Jangan scaffold sales (P7) sebelum ledger (P1) terbukti benar.
- **Test dulu.** Tiap fase mendaftar invariant-nya. Tulis test itu sebelum implementasi. Fase selesai saat test-nya hijau.
- **Invariant berlaku global.** Delapan invariant di `README.md §6` berlaku untuk *setiap* fase, bukan hanya fase yang memperkenalkannya. Fase berikutnya tidak boleh merusak invariant fase sebelumnya.
- **Tandai, jangan menebak.** Ketidakpastian pajak/pengakuan → `// TODO(tax-advisor):`, bukan aturan karangan.

## Prinsip pemandu

1. Backend Go adalah satu-satunya sumber kebenaran keuangan. Frontend menampilkan; tidak pernah menghitung uang atau persentase.
2. Uang adalah `decimal.Decimal` dari ujung ke ujung. `float64` di jalur uang gagal review.
3. Ledger itu sakral: balanced, append-only, ter-scope tenant.
4. Setiap event bisnis dipetakan ke jurnal eksplisit yang teruji di `docs/posting-rules.md`.
5. RAB selalu punya satu versi `active` per proyek/fase — versi lama `superseded`, tidak dihapus.

---

## Fase 0 — Fondasi & scaffolding

**Tujuan:** Kerangka kosong yang jalan dan teruji, tempat kedua stack dibangun.

**Deliverable**
- Layout monorepo sesuai `README.md §5`.
- `backend/`: Go module, GORM + Postgres tersambung, config dari env, HTTP server dengan route `/health`, `docker-compose` untuk Postgres, golang-migrate siap.
- `frontend/`: app Next.js + TS, API client stub yang memanggil `/health`.
- **`domain.Money`** value object yang membungkus `shopspring/decimal` (add, subtract, multiply-by-ratio, allocate, compare; tanpa konstruktor float).
- **`CLAUDE.md`** di-commit: delapan invariant, aturan uang, "test dulu", dan konvensi `TODO(tax-advisor)`.
- CI: `go test ./...`, `go vet`, lint, typecheck frontend.

**DoD**
- `go test ./...` hijau; `/health` mengembalikan 200; frontend menampilkan status health.
- `Money` punya unit test lengkap termasuk **test alokasi** yang membagi nominal ganjil ke 3 bucket dan rekonsiliasi persis (largest-remainder).
- Tidak ada `float64` di mana pun dalam `internal/domain`.

---

## Fase 1 — Inti ledger (bedrock)

**Tujuan:** General ledger double-entry yang benar. Belum ada apa pun soal real estat — murni akuntansi yang anti-bocor.

**Deliverable**
- Model `Account` (COA) + seed COA real-estat Indonesia (`docs/coa.md`): Kas/Bank, Piutang Usaha, **Persediaan Real Estat**, Hutang Usaha, **Uang Muka Penjualan**, PPN Keluaran, **Hutang PPh Final**, Modal, **Pendapatan Penjualan**, **HPP**, Beban PPh Final, beban G&A.
- `JournalEntry` + `JournalLine` (debit/kredit, akun, nominal, tag opsional `project_id`/`phase_id`/`unit_id`).
- **Posting service** yang menolak jurnal mana pun saat `Σ debit ≠ Σ kredit`.
- Neraca saldo (trial balance) + query buku besar (dengan filter proyek/fase/unit).
- Dukungan jurnal pembalik (jalur immutability).

**DoD**
- Property test: jurnal valid acak selalu ter-posting; jurnal tak seimbang selalu ditolak.
- Neraca saldo dari himpunan jurnal mana pun menjumlahkan debit == kredit.
- Jurnal yang sudah diposting tidak bisa diubah atau dihapus (hanya dibalik) — dibuktikan dengan test.

---

## Fase 2 — Multi-tenancy & auth

**Tujuan:** Setiap perusahaan pengembang adalah tenant yang terisolasi.

**Deliverable**
- Model `Tenant`; `tenant_id` di semua tabel domain (kolom pertama setelah PK, di-index).
- **GORM global scope** yang secara otomatis menambahkan `WHERE tenant_id = ?` ke setiap query — tidak ada repository yang boleh bypass scope ini.
- Middleware HTTP yang mengekstrak tenant dari JWT/sesi dan menyuntikkan ke context; setiap handler menarik tenant dari context, bukan dari request body.
- Auth (JWT), user milik tenant, role dasar (owner, accountant, viewer).
- Catatan: MySQL tidak memiliki Row Level Security bawaan. Isolasi ditegakkan **sepenuhnya** via GORM global scope + test integration.

**DoD**
- Test baca/tulis lintas-tenant **gagal membocorkan** di setiap route API — request dengan token tenant A tidak bisa membaca data tenant B.
- Test membuktikan: jika GORM scope sengaja dilepas (tanpa `tenant_id`), query mengembalikan data dari semua tenant — ini membuktikan scope-lah yang menjadi satu-satunya penjaga, sehingga wajib tidak pernah dilepas.
- Semua repository wajib punya test yang menjalankan query tanpa scope dan membuktikan hasilnya lintas-tenant (sebagai kontrol negatif).

---

## Fase 3 — Proyek, fase proyek & unit

**Tujuan:** Memodelkan pengembangan multi-fase dan unit-unit yang dijual.

**Deliverable**
- `Project` (nama, status, mulai, luas tanah, catatan) — akumulator biaya utama.
- `ProjectPhase` (project_id, nama/nomor: "Tahap 1", deskripsi, target_units, status: `planning|active|completed`). Fase adalah sub-divisi opsional — proyek boleh tidak punya fase eksplisit.
- `Unit` (project_id, **phase_id opsional**, kode, tipe, **saleable_area**, harga_jual, status: `available|reserved|sold`, ref pembeli, tgl_jual, harga_terjual).
- Siklus hidup status dengan transisi yang legal saja (mis. `sold` → `available` hanya via pembatalan).
- API CRUD + seed LITHOS sebagai data fixture (18 unit lintas fase).

**DoD**
- Bisa membuat proyek dengan 2 fase dan 18 unit tersebar; transisi status ilegal ditolak.
- Unit selalu milik tepat satu proyek; ter-scope tenant.
- Query bisa memfilter unit/biaya per fase maupun per proyek keseluruhan.

---

## Fase 4 — RAB (Rencana Anggaran Biaya) ★

**Tujuan:** Developer bisa membuat anggaran sebelum konstruksi dan merevisinya, dengan audit trail lengkap tiap versi.

**Deliverable**
- `BudgetPlan` (project_id, phase_id opsional, version: int, label: "RAB v1", status: `draft|active|superseded`, notes, created_at, approved_at, approved_by).
- `BudgetItem` (budget_plan_id, category: `land|construction|soft|financing|marketing|other`, subcategory string, description, budgeted_amount: NUMERIC).
- **Aturan versi:** hanya satu `BudgetPlan` yang boleh berstatus `active` per project/phase. Saat plan baru di-approve, plan lama otomatis `superseded` — tidak bisa dihapus.
- **Draft → Active** via endpoint approve; active plan tidak bisa diedit langsung — harus buat plan baru (version N+1) dari draft.
- Query: total RAB per kategori, detail per item, histori versi.
- Perbandingan **Realisasi vs RAB** (agregasi `CostEntry` aktual vs `BudgetItem` dari plan `active`): nilai budget, nilai realisasi, selisih, % realisasi — per kategori dan per proyek/fase.

**DoD**
- Tidak bisa ada dua plan `active` untuk proyek/fase yang sama — constraint + test.
- Approve plan baru otomatis supersede plan lama — dibuktikan dengan test.
- Plan `superseded` tetap bisa dibaca dan dibandingkan (tidak dihapus).
- Laporan realisasi vs RAB akurat setelah beberapa cost entry diposting (fase ini bisa dijalankan dengan mock cost entry, sebelum fase 5 selesai).
- `BudgetItem.budgeted_amount` adalah `decimal.Decimal` — tidak ada float.

---

## Fase 5 — Kapitalisasi biaya

**Tujuan:** Biaya pengembangan masuk ke neraca sebagai persediaan, ter-tag dengan benar, dan bisa dibandingkan RAB.

**Deliverable**
- `CostEntry` (project_id, phase_id opsional, unit_id opsional, category: `land|construction|soft|financing|marketing|other`, nominal, tanggal, vendor, deskripsi, budget_item_id opsional sebagai referensi).
- Posting: **Dr Persediaan Real Estat (ter-tag) / Cr Kas|Hutang Usaha** lewat service Fase 1.
- Query biaya terakumulasi per-proyek, per-fase, dan per-unit.
- Saat cost entry diposting, sistem memperbarui snapshot realisasi RAB (jika `budget_item_id` diisi).
- Semua aturan posting didokumentasikan di `docs/posting-rules.md`.

**DoD**
- Jumlah posting persediaan untuk sebuah proyek == jumlah cost entry-nya.
- Biaya langsung (ter-tag unit) vs tingkat-proyek (tanpa tag) bisa dibedakan dalam query.
- Setelah posting cost entry, laporan realisasi vs RAB (Fase 4) langsung mencerminkan angka terbaru.

---

## Fase 6 — Engine alokasi HPP ★ (jantungnya)

**Tujuan:** Menyebar biaya tingkat-proyek ke unit-unit sehingga setiap unit punya biaya terakumulasi yang akurat — rekonsiliasi sampai rupiah terakhir.

**Deliverable**
- `AllocationBasis`: `saleable_area` atau `sales_value`, ditetapkan per proyek, tercatat dan bisa diaudit.
- Engine: `biaya_terakumulasi_unit = biaya langsung unit + porsi alokasi biaya tingkat-proyek`.
- Alokasi bisa dijalankan per-fase atau per-proyek keseluruhan (tergantung tag biaya).
- **Pembulatan deterministik** (largest-remainder) agar porsi teralokasi menjumlah kembali ke total persis.
- Re-alokasi saat biaya tingkat-proyek baru ditambah atau sebuah unit berubah (dengan jejak audit).
- API: rincian biaya per-unit (langsung vs teralokasi).

**DoD** (fase yang membangun kepercayaan)
- **Test rekonsiliasi:** `Σ(biaya_teralokasi_unit) == total_biaya_proyek_dikapitalisasi` persis, di banyak himpunan biaya acak dan kedua basis.
- Sisa pembulatan ditetapkan deterministik dan tidak pernah hilang.
- Mengganti basis menurunkan ulang seluruh biaya unit dan tetap rekonsiliasi.

---

## Fase 7 — Penjualan, jadwal pembayaran & pengakuan pendapatan

**Tujuan:** Menjual unit memicu pendapatan + HPP dalam satu transaksi seimbang; cicilan KPR/tunai dilacak dengan benar sampai BAST.

**Deliverable**

**Sub-modul PaymentSchedule:**
- `SaleContract` (unit_id, buyer_name, buyer_id, payment_type: `kpr|tunai`, bank_kpr opsional, loan_amount opsional, contract_date, total_price).
- `PaymentSchedule` (sale_contract_id, installment_number, due_date, amount, type: `dp|installment|final`, status: `scheduled|received|overdue`).
- Saat pembayaran diterima sebelum BAST: **Dr Kas / Cr Uang Muka Penjualan**.
- Cron/scheduler untuk menandai installment `overdue` saat melewati due_date.
- Query: tagihan yang jatuh tempo minggu ini, total uang muka terkumpul per unit, sisa hutang buyer.

**Sub-modul Revenue Recognition:**
- **Pengakuan saat BAST (point-in-time, default PSAK 72):**
  - Dr Uang Muka Penjualan + Dr Piutang Usaha / Cr Pendapatan Penjualan (+ Cr PPN Keluaran bila PKP)
  - Dr HPP (biaya terakumulasi unit) / Cr Persediaan Real Estat
- Status unit → `sold`; persediaan untuk unit itu menjadi nol.
- Penanda `// TODO(tax-advisor):` di titik pengakuan untuk pertanyaan leasehold LITHOS.

**DoD**
- Test siklus penuh (biaya → pembayaran DP → cicilan → BAST) menyisakan ledger seimbang, persediaan unit terjual nol, HPP == biaya terakumulasi unit itu.
- Uang muka tidak pernah muncul sebagai pendapatan sebelum BAST.
- Installment `overdue` terdeteksi dan dapat di-query.
- Tidak ada `float64` di seluruh modul payment.

---

## Fase 8 — Pajak properti Indonesia

**Tujuan:** Menghitung dan memposting pajak khusus pengembang yang tidak dicakup esaFiskaly.

**Deliverable**
- **PPh Final Pengalihan 2,5%** (PP 34/2016) di setiap pengalihan: Dr Beban PPh Final / Cr Hutang PPh Final; pelunasan saat pembayaran.
- Penanganan **PPN** keluaran (tarif + DPP sesuai regulasi berlaku; tandai ambang PPnBM/mewah sebagai config).
- Laporan kewajiban pajak per periode.
- Setiap tarif di balik aturan bertanggal & dapat dikonfigurasi — jangan di-hardcode inline — agar perubahan regulasi tidak butuh edit kode.

**DoD**
- PPh Final atas sebuah penjualan sama dengan 2,5% nilai pengalihan bruto, terposting dan seimbang.
- Angka pajak cocok dengan laporan pajak periode.

---

## Fase 9 — Pelaporan

**Tujuan:** Seluruh output laporan yang menjadi alasan sistem ini dibangun.

**Deliverable**

**Laporan keuangan inti:**
- **Laba Rugi per-proyek/fase:** pendapatan diakui − HPP − biaya periode langsung proyek; margin proyek.
- **Konsolidasi:** **Neraca** + **Laba Rugi** lintas semua proyek + G&A korporat.
- **Arus Kas** (cash flow statement): arus dari operasi, investasi, pendanaan — per proyek dan konsolidasi.
- Neraca saldo dan drill-down buku besar (bisa difilter proyek/fase/unit).

**Laporan manajerial developer:**
- **RAB vs Realisasi** per proyek/fase: tabel per kategori (budget, realisasi, selisih, % terpakai), highlight item yang melebihi budget.
- **Pipeline penjualan unit:** ringkasan available/reserved/sold per proyek/fase, total nilai kontrak, total uang muka terkumpul, perkiraan pendapatan saat semua unit BAST.
- **Laporan tagihan & koleksi:** daftar installment jatuh tempo per periode, total overdue, total terlunasi vs outstanding per unit.
- **Laporan kewajiban pajak:** PPh Final dan PPN per periode.

**Ekspor:** CSV/Excel untuk semua laporan di atas.

**DoD**
- Neraca konsolidasi seimbang (Aset == Kewajiban + Modal).
- Jumlah hasil per-proyek + item korporat tak teralokasi == Laba Rugi konsolidasi.
- RAB vs Realisasi: kolom realisasi cocok dengan total CostEntry yang diposting.
- Drill-down dari baris laporan mana pun kembali ke jurnal sumber.
- Export CSV menghasilkan file yang bisa dibuka di Excel tanpa error encoding.

---

## Fase 10 — Frontend

**Tujuan:** Membuatnya bisa dipakai.

**Deliverable**
- **Dashboard:** kas, nilai persediaan, diakui vs belum diakui, pajak terhutang, total RAB vs realisasi (ringkasan), unit pipeline (available/reserved/sold).
- **Tampilan proyek + fase:** papan unit per fase (available/reserved/sold) dengan biaya/harga per-unit; progress bar RAB per kategori.
- **RAB Manager:** buat draft, edit item, approve, lihat histori versi, perbandingan antar versi.
- **Input biaya:** form cost entry dengan referensi ke budget item RAB aktif.
- **Manajemen penjualan:** kontrak unit, payment schedule (KPR/tunai), mark payment received, input BAST.
- **Layar laporan keuangan:** semua laporan Fase 9 dengan filter periode/proyek/fase, tombol export CSV/Excel.
- **Laporan tagihan:** daftar installment jatuh tempo, toggle overdue.

**DoD**
- Tidak ada komputasi uang di kode frontend (gerbang lint/review).
- Setiap angka di layar bisa ditelusuri ke respons API.
- RAB manager tidak mengizinkan edit plan yang sudah `active` — harus draft baru.

---

## Fase 11 — Hardening & tutup buku

**Tujuan:** Layak produksi & bisa dipercaya.

**Deliverable**
- **Tutup periode** (kunci periode terposting; perubahan pasca-tutup hanya via jurnal pembalik).
- Audit log penuh (siapa/apa/kapan di setiap mutasi keuangan, termasuk approve/supersede RAB).
- Backup/restore + disiplin migrasi.
- OpenAPI difinalkan di `docs/api`.

**DoD**
- Periode terkunci menolak posting baru; audit log lengkap dan bisa di-query.
- Latihan restore-dari-backup lulus.

---

## Invariant lintas-sektoral (harus berlaku di setiap fase)

1. `Σ debit == Σ kredit` di setiap jurnal.
2. Tidak ada `float64` di jalur uang mana pun.
3. `Σ(biaya unit) == total proyek`, persis, dengan pembulatan deterministik.
4. HPP saat penjualan == biaya terakumulasi unit.
5. Ledger append-only; koreksi via pembalik.
6. Isolasi tenant ditegakkan via GORM global scope `tenant_id` — tidak boleh ada query yang bypass scope.
7. Penerimaan pra-pengakuan adalah kewajiban, bukan pendapatan.
8. Hanya satu `BudgetPlan` berstatus `active` per proyek/fase; versi lama `superseded` tidak dihapus.

## Di luar cakupan v1

Payroll/PPh 21 (wilayah esaFiskaly), POS, integrasi langsung e-Faktur/e-Bupot, multi-mata uang, pengakuan over-time (percentage-of-completion), Earned Value Analysis lanjutan (BCWS/BCWP/ACWP). Ditinjau ulang setelah inti project-costing terbukti pada data riil LITHOS.
