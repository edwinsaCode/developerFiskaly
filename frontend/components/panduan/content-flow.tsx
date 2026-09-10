import { Callout, Code, DataTable, FlowStep, JournalEntryCard, Lead, P, SectionHeader, UL, XRef } from "./blocks";

// Alur End-to-End Bisnis — mengikuti urutan persis yang diminta:
// Project → RAB → Approval → Unit/Block → Actual Cost → Realisasi →
// Alokasi HPP → Persediaan → Booking → DP → Akad → KPR/Payment →
// Penjualan → Pengakuan HPP → Laba/Rugi
//
// Semua angka pada kartu jurnal di bawah dikutip apa adanya dari contoh yang
// sudah diverifikasi balance di docs/SYSTEM-DOCUMENTATION.md (§3.3, §3.4,
// §6.2, §6.6e, §6.8, §13.2). Karena tiap contoh diambil dari kasus proyek
// yang berbeda di dokumen sumber, label unit/proyek pada tiap kartu dibiarkan
// generik ("Unit A", "Unit contoh") — yang penting akun dan nominal bisa
// ditelusuri balik ke SYSTEM-DOCUMENTATION.md.

export function FlowSection() {
  return (
    <div className="space-y-8">
      <SectionHeader
        eyebrow="Alur Utama"
        title="Alur End-to-End Bisnis"
        lead={
          <Lead>
            Satu rantai transaksi dari proyek dibuat sampai laba/rugi tercatat. Setiap langkah
            menunjukkan <em>kapan terjadi</em>, akun <strong>Debit</strong>/<strong>Credit</strong>,
            contoh nominal, dampak Neraca, dan dampak Laba/Rugi — bila langkah tersebut menghasilkan
            jurnal. Beberapa langkah (RAB, Approval, Unit/Block, Realisasi) murni perubahan status,
            tidak menghasilkan jurnal sama sekali.
          </Lead>
        }
      />

      <div>
        <FlowStep n={1} title="Project — proyek dibuat">
          <P>
            Admin membuat proyek (nama, lokasi, <Code>tax_category</Code> default: subsidi/komersial).
            Belum ada biaya, belum ada unit. Tidak ada jurnal.
          </P>
        </FlowStep>

        <FlowStep n={2} title="RAB (BudgetPlan) — susun rencana anggaran biaya">
          <P>
            Tim menyusun RAB per kategori (Tanah, Konstruksi, Sarana &amp; Prasarana, Perizinan,
            Pemasaran, Lain-lain) dan subkategori/tier (direct/shared/overhead). Status awal{" "}
            <Code>draft</Code>. Tidak ada jurnal — RAB adalah rencana, bukan transaksi keuangan.
          </P>
        </FlowStep>

        <FlowStep n={3} title="Approval — RAB disetujui">
          <P>
            RAB yang disetujui menjadi <Code>active</Code>; versi lama otomatis <Code>superseded</Code>{" "}
            (tidak dihapus, tetap ada untuk audit trail). Hanya satu RAB <Code>active</Code> per
            proyek/fase pada satu waktu.
          </P>
          <Callout variant="frozen" title="RAB approved ≠ Persediaan">
            Persetujuan RAB murni perubahan status (<Code>draft</Code> → <Code>active</Code>). Tidak
            ada jurnal, tidak ada nilai yang masuk ke Persediaan. Persediaan baru terbentuk ketika
            biaya <em>aktual</em> dicatat dan dialokasikan — lihat <XRef id="accounting-hpp">Accounting &amp; HPP</XRef>.
          </Callout>
        </FlowStep>

        <FlowStep n={4} title="Unit / Block — unit dibuat di atas proyek">
          <P>
            Unit rumah/ruko/kavling dibuat dengan <Code>land_area</Code> (m²) dan{" "}
            <Code>saleable_area</Code> (m²) wajib diisi — keduanya jadi bobot alokasi biaya nanti.
            Unit juga membawa <Code>tax_category</Code> (subsidi/komersial) yang menentukan pool
            alokasi konstruksi dan tarif pajak. Belum ada jurnal pada tahap ini.
          </P>
        </FlowStep>

        <FlowStep n={5} title="Actual Cost — biaya aktual dicatat">
          <P>
            Biaya sesungguhnya (bukan RAB) dicatat lewat Cost Entry per kategori: tanah, hard cost
            (konstruksi), soft cost, sarana &amp; prasarana, perizinan, pemasaran, lain-lain,
            operasional — masing-masing dengan tier <Code>direct</Code>/<Code>shared</Code>/
            <Code>overhead</Code>. Biaya langsung capital (tanah &amp; hard cost) masuk Persediaan;
            Pemasaran dan Lain-lain langsung jadi Beban periode berjalan (lihat{" "}
            <XRef id="accounting-hpp">Accounting &amp; HPP</XRef>).
          </P>
          <JournalEntryCard
            title="Contoh — realisasi biaya konstruksi (pool Produksi Subsidi)"
            when="Saat biaya hard cost dicatat & posted (belum dialokasikan ke unit)"
            debit={[{ account: "1-3100", label: "Persediaan Hard Cost — Produksi Subsidi", amount: 200_000_000 }]}
            credit={[{ account: "2-1000", label: "Hutang Usaha (vendor konstruksi)", amount: 200_000_000 }]}
            neraca="Persediaan (aset) naik, Hutang Usaha (kewajiban) naik — total aset & kewajiban proyek bertambah seimbang."
            labaRugi="Tidak ada dampak — biaya capital, belum jadi beban sampai unit terjual."
          />
        </FlowStep>

        <FlowStep n={6} title="Realisasi — bandingkan aktual vs RAB">
          <P>
            <Code>Realisasi % = Σ biaya aktual ter-link / RAB approved</Code> per kategori. Ini murni
            angka pemantauan (dashboard/laporan), bukan transaksi — tidak ada jurnal yang dihasilkan
            dari perhitungan realisasi itu sendiri.
          </P>
        </FlowStep>

        <FlowStep n={7} title="Alokasi HPP — biaya pool dibagi ke unit">
          <P>
            Biaya yang dicatat sebagai pool project-wide (bukan langsung ke satu unit) dialokasikan ke
            setiap unit secara proporsional dan direkonsiliasi sampai rupiah terakhir (largest-remainder,
            tidak ada sisa pembulatan yang dibuang). Detail lengkap mekanismenya ada di{" "}
            <XRef id="alokasi-hpp">§7 Alokasi HPP</XRef>.
          </P>
          <DataTable
            head={["Unit", "Land Area", "Alokasi Tanah (dari pool)"]}
            rows={[
              ["Unit A", "100 m²", "Rp 200.000.000"],
              ["Unit B", "150 m²", "Rp 300.000.000"],
              ["Unit C", "150 m²", "Rp 300.000.000"],
            ]}
          />
          <P>
            Contoh di atas: pool biaya tanah project-wide Rp900.000.000, dikurangi carve-out Kelebihan
            Tanah Rp100.000.000, sisa Rp800.000.000 dibagi proporsional berdasarkan <Code>land_area</Code>{" "}
            tiap unit — hasilnya tepat Rp800.000.000 (200jt + 300jt + 300jt), tidak lebih tidak kurang.
          </P>
        </FlowStep>

        <FlowStep n={8} title="Persediaan — saldo tersimpan sampai unit terjual">
          <P>
            Setelah dialokasikan, biaya tanah &amp; konstruksi per unit "mengendap" sebagai saldo
            Persediaan (<Code>1-3000</Code> Persediaan Tanah, <Code>1-3100</Code> Persediaan Hard
            Cost) — tidak jadi beban sampai unit tersebut benar-benar terjual (Akad).
          </P>
        </FlowStep>

        <FlowStep n={9} title="Booking — calon pembeli membayar tanda jadi">
          <P>
            Booking fee bersifat non-refundable dan langsung diakui sebagai pendapatan saat diterima
            (bukan uang muka). Jika booking fee Rp0, tidak ada jurnal — langkah booking hanya mengunci
            unit.
          </P>
          <JournalEntryCard
            title="Contoh — booking fee diterima"
            when="Saat pembayaran booking fee diterima (sebelum Akad)"
            debit={[{ account: "1-1300", label: "Bank BCA", amount: 5_000_000 }]}
            credit={[{ account: "4-2100", label: "Pendapatan Booking", amount: 5_000_000 }]}
            neraca="Kas/Bank naik. Tidak ada kewajiban yang dibentuk — berbeda dari uang muka."
            labaRugi="Pendapatan Booking Rp5.000.000 langsung diakui final — tidak dibalik meski booking dibatalkan."
          />
        </FlowStep>

        <FlowStep n={10} title="DP (Uang Muka) — dibayar sebelum kriteria pengakuan terpenuhi">
          <P>
            Kas/termin yang diterima sebelum Akad terjadi <strong>bukan pendapatan</strong> — ini
            kewajiban (Uang Muka Penjualan) sampai kriteria pengakuan (BAST/Akad) terpenuhi.
          </P>
          <JournalEntryCard
            title="Contoh — DP diterima sebelum Akad"
            when="Sebelum Akad, saat calon pembeli menyetor uang muka"
            debit={[{ account: "1-1300", label: "Bank BCA", amount: 10_000_000 }]}
            credit={[{ account: "2-2000", label: "Uang Muka Penjualan", amount: 10_000_000 }]}
            neraca="Kas/Bank naik, Kewajiban (Uang Muka) naik — belum menyentuh ekuitas/laba."
            labaRugi="Tidak ada dampak — belum diakui sebagai pendapatan."
          />
        </FlowStep>

        <FlowStep n={11} title="Akad — kriteria pengakuan pendapatan terpenuhi">
          <P>
            Saat Akad (dengan gate: RAB approved + basis Alokasi HPP tersedia + syarat lunas skema
            terpenuhi), dua hal terjadi sekaligus dalam satu transaksi atomik: pendapatan diakui penuh,
            dan HPP unit yang terjual dipindah dari Persediaan ke Beban Pokok Penjualan.
          </P>
          <JournalEntryCard
            title="Contoh — pengakuan pendapatan saat Akad (Unit subsidi, harga Rp180.000.000)"
            when="Saat Akad / BAST — kriteria pengakuan terpenuhi"
            debit={[
              { account: "2-2000", label: "Uang Muka Penjualan (DP dipakai)", amount: 10_000_000 },
              { account: "1-2200", label: "Dana Jaminan Bank (KPR disetujui, belum cair)", amount: 165_000_000 },
              { account: "1-2000", label: "Piutang Usaha (sisa kekurangan)", amount: 5_000_000 },
            ]}
            credit={[{ account: "4-1000", label: "Pendapatan Penjualan Unit", amount: 180_000_000 }]}
            note="Rp10jt DP sebelumnya + Rp165jt bank approved (KPR) + Rp5jt piutang customer = Rp180jt harga jual."
            neraca="Uang Muka (kewajiban) berkurang; Dana Jaminan Bank & Piutang Usaha (aset) bertambah — total aset/kewajiban bergeser, ekuitas belum berubah di baris ini."
            labaRugi="Pendapatan Penjualan Rp180.000.000 diakui penuh."
          />
          <JournalEntryCard
            title="Contoh — pengakuan HPP bersamaan (unit yang sama)"
            when="Bersamaan dengan Akad, dalam transaksi atomik yang sama"
            debit={[{ account: "5-1000", label: "Beban Pokok Penjualan (HPP)", amount: 115_000_000 }]}
            credit={[
              { account: "1-3000", label: "Persediaan Tanah — unit ini", amount: 20_000_000 },
              { account: "1-3100", label: "Persediaan Hard Cost — unit ini", amount: 95_000_000 },
            ]}
            neraca="Persediaan (aset) turun Rp115.000.000."
            labaRugi="HPP Rp115.000.000 diakui — laba kotor unit ini = Rp180jt − Rp115jt = Rp65.000.000."
          />
        </FlowStep>

        <FlowStep n={12} title="KPR / Payment — bank mencairkan pinjaman bertahap">
          <P>
            Dana Jaminan Bank (<Code>1-2200</Code>) hanya dipakai antara Akad sampai pencairan penuh.
            Setiap pencairan mengurangi saldo <Code>1-2200</Code> dan menambah kas/bank — pencairan
            tidak boleh melebihi saldo yang dijamin.
          </P>
          <JournalEntryCard
            title="Contoh — pencairan tahap pertama KPR"
            when="Saat bank mencairkan dana ke rekening perusahaan"
            debit={[{ account: "1-1400", label: "Bank Mandiri", amount: 100_000_000 }]}
            credit={[{ account: "1-2200", label: "Dana Jaminan Bank", amount: 100_000_000 }]}
            note="Saldo Dana Jaminan Bank untuk unit ini tersisa Rp65.000.000 (dari Rp165.000.000) menunggu pencairan berikutnya."
            neraca="Kas/Bank naik, Dana Jaminan Bank (piutang ke bank) turun — total aset tetap."
            labaRugi="Tidak ada dampak — murni pergeseran antar akun aset."
          />
        </FlowStep>

        <FlowStep n={13} title="Penjualan — pendapatan sudah tercatat penuh sejak Akad">
          <P>
            "Penjualan" bukan langkah jurnal baru — pendapatan Rp180.000.000 sudah diakui penuh di
            langkah Akad (langkah 11). Yang membedakan dari sekadar DP: kriteria pengakuan
            (BAST/Akad) sudah terpenuhi, sehingga saldo yang tadinya kewajiban (Uang Muka) resmi
            menjadi bagian dari pendapatan yang diakui.
          </P>
        </FlowStep>

        <FlowStep n={14} title="Pengakuan HPP — reguler saat Akad, direkonsiliasi saat True-Up">
          <P>
            HPP unit sudah diakui saat Akad (langkah 11) menggunakan basis RAB <strong>approved</strong>{" "}
            (budgeted), bukan biaya aktual final — karena saat Akad terjadi, tidak semua biaya proyek
            sudah 100% tercatat. Saat proyek/fase selesai (semua biaya aktual sudah masuk), sistem
            menjalankan <strong>True-Up</strong>: selisih antara HPP budgeted yang sudah diakui vs
            biaya aktual final dijurnal sebagai koreksi tambahan — bukan mengedit jurnal HPP lama
            (ledger append-only).
          </P>
          <JournalEntryCard
            title="Contoh — True-Up saat proyek selesai (selisih HPP tanah Unit A)"
            when="Saat proyek/fase ditutup — biaya tanah aktual final lebih besar dari budgeted"
            debit={[{ account: "5-1000", label: "Beban Pokok Penjualan (koreksi True-Up)", amount: 15_000_000 }]}
            credit={[{ account: "1-3000", label: "Persediaan Tanah — unit ini", amount: 15_000_000 }]}
            note="Ilustrasi: HPP tanah Unit A dibudget Rp200.000.000 saat Akad, aktual final Rp215.000.000 → selisih +Rp15.000.000 dijurnal sebagai koreksi terpisah, bukan mengubah jurnal Akad yang sudah posted."
            neraca="Persediaan turun Rp15.000.000 lagi — menutup selisih ke nol."
            labaRugi="Laba kotor unit ini terkoreksi turun Rp15.000.000 pada periode True-Up."
          />
          <Callout variant="info" title="Kenapa bukan 'actual cost langsung' saat Akad?">
            Pada saat Akad terjadi, belum tentu semua biaya proyek (terutama yang dibagi ke banyak
            unit/pool) sudah selesai tercatat — sebagian vendor mungkin baru menagih belakangan. Basis
            RAB approved dipakai supaya HPP bisa diakui tepat waktu saat penjualan, dengan mekanisme
            True-Up sebagai koreksi berkala begitu biaya aktual final diketahui. Detail lengkap ada di{" "}
            <XRef id="persediaan-hpp">§6 Persediaan &amp; HPP</XRef> dan{" "}
            <XRef id="accounting-hpp">Accounting &amp; HPP</XRef>.
          </Callout>
        </FlowStep>

        <FlowStep n={15} title="Laba/Rugi — hasil akhir periode" isLast>
          <P>Semua pendapatan dan beban di atas bermuara ke Laporan Laba Rugi periode berjalan.</P>
          <DataTable
            head={["Baris", "Nominal"]}
            rows={[
              ["Pendapatan (Penjualan + Booking + lainnya)", "Rp 835.000.000"],
              ["Beban Pokok Penjualan (HPP)", "(Rp 425.000.000)"],
              ["Laba Kotor", "Rp 410.000.000"],
              ["Beban Operasional (Pemasaran, Lain-lain, dll.)", "(Rp 78.600.000)"],
              ["Laba Operasional", "Rp 331.400.000"],
              ["Laba Bersih (setelah pos non-operasional/pajak)", "Rp 319.500.000"],
            ]}
            rightCols={[1]}
          />
          <P>
            Angka pada tabel ini adalah contoh gabungan satu periode proyek (bukan satu unit) — lihat
            rincian lengkap di <XRef id="laporan-keuangan">§16 Laporan Keuangan</XRef>.
          </P>
        </FlowStep>
      </div>

      <Callout variant="frozen" title="8 Invariant yang mengikat seluruh alur ini">
        <UL
          items={[
            "Jurnal selalu balanced — Σ debit = Σ kredit, tanpa pengecualian.",
            "Uang tidak pernah float — semua nominal decimal.Decimal / DECIMAL(20,4).",
            "Alokasi rekonsiliasi sampai rupiah terakhir — largest-remainder, tidak ada sisa dibuang.",
            "HPP = biaya terakumulasi unit saat penjualan — bukan estimasi.",
            "Ledger append-only — koreksi selalu via jurnal baru, tidak pernah edit/hapus histori.",
            "Isolasi tenant ketat di setiap query.",
            "Penerimaan sebelum kriteria pengakuan = kewajiban, bukan pendapatan.",
            "Satu BudgetPlan active per proyek/fase — versi lama superseded, tidak dihapus.",
          ]}
        />
      </Callout>
    </div>
  );
}
