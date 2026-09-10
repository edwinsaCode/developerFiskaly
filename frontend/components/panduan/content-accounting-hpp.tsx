import { Callout, Code, DataTable, H3, Lead, P, SectionHeader, UL, XRef } from "./blocks";

// Bagian khusus "Accounting & HPP" — menjawab langsung 9 poin yang diminta:
// HPP Tanah, Construction HPP, Subsidi vs Komersial, Sarana & Prasarana,
// Perizinan, Pemasaran = Beban, Lain-lain = Beban, RAB approved ≠ Persediaan,
// rantai actual cost → allocation → inventory → HPP saat terjual.

export function AccountingHppSection() {
  return (
    <div className="space-y-8">
      <SectionHeader
        eyebrow="Referensi Inti"
        title="Accounting & HPP"
        lead={
          <Lead>
            Rincian bagaimana setiap kategori biaya berakhir sebagai HPP (masuk Persediaan lalu
            dipindah ke Beban Pokok Penjualan saat unit terjual) atau sebagai Beban Operasional
            langsung. Semua contoh angka mengikuti nilai yang sama dengan{" "}
            <XRef id="alur">Alur End-to-End Bisnis</XRef>.
          </Lead>
        }
      />

      <Callout variant="frozen" title="Aturan final yang mengikat bagian ini">
        <UL
          items={[
            <>Uang tidak pakai float — semua nominal <Code>decimal.Decimal</Code> / <Code>DECIMAL(20,4)</Code>.</>,
            <>Alokasi rekonsiliasi sampai rupiah terakhir — sisa pembulatan largest-remainder, tidak pernah dibuang.</>,
            <>HPP = biaya terakumulasi unit saat penjualan — bukan estimasi kasar.</>,
          ]}
        />
      </Callout>

      <div className="space-y-3">
        <H3>6 Kategori Biaya — mana yang jadi HPP, mana yang jadi Beban</H3>
        <DataTable
          head={["Kategori Biaya", "Tujuan Akun", "Jadi HPP saat unit terjual?"]}
          rows={[
            ["Tanah", "1-3000 Persediaan Tanah", "Ya"],
            ["Hard Cost (Konstruksi)", "1-3100 Persediaan Hard Cost", "Ya"],
            ["Sarana & Prasarana", "1-3100 Persediaan Hard Cost (pool General)", "Ya"],
            ["Perizinan", "1-3100 Persediaan Hard Cost (pool General)", "Ya"],
            ["Pemasaran", "6-xxxx Beban Pemasaran", "Tidak — beban periode berjalan"],
            ["Lain-lain (non-capital)", "6-xxxx Beban Lain-lain / Operasional", "Tidak — beban periode berjalan"],
          ]}
        />
      </div>

      <div className="space-y-3">
        <H3>1. HPP Tanah</H3>
        <P>
          Ada dua mekanisme berbeda tergantung konteksnya — penting untuk tidak disamaratakan:
        </P>
        <UL
          items={[
            <>
              <strong>Kelebihan Tanah</strong> (produk tambahan, bukan unit): HPP dihitung langsung —{" "}
              <Code>HPP = land_area (m²) × harga beli tanah per m²</Code>. Formula tetap ini dipakai
              karena Kelebihan Tanah adalah bidang tanah spesifik dengan harga beli yang bisa
              ditelusuri langsung. Detail di <XRef id="kelebihan-tanah">§12 Kelebihan Tanah</XRef>.
            </>,
            <>
              <strong>Unit rumah/ruko/kavling</strong>: HPP Tanah <em>bukan</em> tarif tetap per m² —
              melainkan porsi proporsional dari <em>pool</em> biaya tanah project-wide, dibagi
              berdasarkan <Code>land_area</Code> tiap unit relatif terhadap total. Alasannya: biaya
              perolehan tanah biasanya dicatat sebagai satu kesatuan/pool per proyek, bukan per unit.
              Lihat contoh alokasi di <XRef id="alokasi-hpp">§7 Alokasi HPP</XRef>.
            </>,
          ]}
        />
        <Callout variant="warning" title="Jangan disamakan">
          Menganggap semua HPP Tanah unit sebagai "luas × tarif tetap per m²" adalah simplifikasi yang
          keliru untuk unit reguler — tarif efektif per m² justru merupakan <em>hasil</em> dari
          pembagian pool (pool ÷ total land_area), bukan input tetap yang di-set di awal.
        </Callout>
      </div>

      <div className="space-y-3">
        <H3>2. HPP Konstruksi — 3 Pool berdasarkan tax_category, bukan basis "biaya aktual langsung"</H3>
        <P>
          Hard cost dialokasikan lewat 3 pool: <strong>Produksi Subsidi</strong> (khusus unit
          subsidi), <strong>Produksi Komersial</strong> (khusus unit komersial), dan{" "}
          <strong>General</strong> (Sarana &amp; Prasarana + Perizinan — dibagi ke <em>semua</em> unit
          apa pun kategori pajaknya).
        </P>
        <DataTable
          head={["Pool", "Total Biaya", "Hasil Alokasi per Unit (contoh)"]}
          rows={[
            ["Produksi Subsidi (Unit D)", "Rp 200.000.000", "Rp 112.888.889"],
            ["Produksi Komersial (Unit E)", "Rp 150.000.000", "Rp 141.111.111"],
            ["General — Unit F (proporsional)", "Rp 90.000.000 (bagian dari total General)", "Rp 186.000.000 (gabungan pool relevan)"],
          ]}
          rightCols={[1, 2]}
        />
        <Callout variant="info" title="Bukan 'actual posted cost langsung' saat Akad — basis RAB approved + True-Up">
          Saat Akad, HPP konstruksi yang diakui memakai basis <strong>RAB approved (budgeted)</strong>{" "}
          untuk kategori tanah &amp; hard cost — bukan angka biaya aktual final saat itu juga. Ini
          karena tidak semua biaya proyek/pool tentu sudah 100% tercatat pada tanggal Akad. Sistem
          mengakui HPP tepat waktu memakai basis budgeted, lalu menjalankan <strong>True-Up</strong>{" "}
          saat proyek/fase selesai untuk mengoreksi selisih ke biaya aktual final — dijurnal sebagai
          koreksi terpisah (ledger append-only), bukan mengedit jurnal lama. Yang <em>benar</em> dari
          "berbasis actual cost, bukan full RAB": HPP konstruksi memang{" "}
          <strong>bukan seluruh nilai total RAB</strong> — Pemasaran dan Lain-lain di luar hitungan
          HPP sama sekali — dan pada akhirnya (via True-Up) HPP memang bermuara ke biaya aktual, hanya
          saja prosesnya dua tahap: budgeted dulu di Akad, direkonsiliasi kemudian. Lihat langkah 14 di{" "}
          <XRef id="alur">Alur End-to-End Bisnis</XRef> untuk contoh jurnal True-Up.
        </Callout>
      </div>

      <div className="space-y-3">
        <H3>3. Produksi Subsidi vs Produksi Komersial</H3>
        <P>
          Setiap unit membawa <Code>tax_category</Code> (<Code>subsidi</Code> atau{" "}
          <Code>komersial</Code>). Hard cost yang di-tag langsung ke kategori tersebut hanya
          dialokasikan ke unit dengan kategori yang sama — unit subsidi tidak pernah menanggung
          bagian dari pool Produksi Komersial, dan sebaliknya. Kategori ini juga menentukan tarif
          PPh Final saat penjualan (lihat <XRef id="pajak">§13 Pajak</XRef>).
        </P>
      </div>

      <div className="space-y-3">
        <H3>4. Sarana &amp; Prasarana</H3>
        <P>
          Biaya jalan, drainase, taman, dan fasilitas umum lain dalam kompleks. Masuk pool{" "}
          <strong>General</strong> — dialokasikan ke <em>semua</em> unit di proyek (subsidi maupun
          komersial) secara proporsional, karena manfaatnya dinikmati seluruh unit tanpa memandang
          kategori pajak.
        </P>
      </div>

      <div className="space-y-3">
        <H3>5. Perizinan</H3>
        <P>
          Biaya izin (site plan, IMB/PBG, dan sejenisnya) — sama seperti Sarana &amp; Prasarana, masuk
          pool <strong>General</strong> dan dialokasikan ke semua unit secara proporsional.
        </P>
      </div>

      <div className="space-y-3">
        <H3>6. Pemasaran = Beban, bukan HPP</H3>
        <P>
          Biaya iklan, event, brosur, komisi marketing, dan sejenisnya <strong>tidak pernah</strong>{" "}
          masuk Persediaan/HPP — langsung dicatat sebagai Beban Operasional pada periode terjadinya,
          terlepas dari unit mana yang akhirnya terjual.
        </P>
      </div>

      <div className="space-y-3">
        <H3>7. Lain-lain = Beban, bukan HPP</H3>
        <P>
          Biaya non-capital lain (administrasi umum, dsb.) yang tidak terkait langsung ke perolehan
          fisik unit juga langsung menjadi Beban Operasional — tidak dikapitalisasi ke Persediaan.
        </P>
      </div>

      <div className="space-y-3">
        <H3>8. RAB approved ≠ Persediaan</H3>
        <P>
          Persetujuan RAB (<Code>draft</Code> → <Code>active</Code>) adalah perubahan status murni,
          tanpa jurnal. Persediaan baru terbentuk saat biaya <em>aktual</em> dicatat dan
          dialokasikan — RAB hanyalah pagu/rencana yang membatasi berapa banyak biaya aktual yang
          boleh direalisasikan, bukan nilai yang otomatis masuk neraca.
        </P>
      </div>

      <div className="space-y-3">
        <H3>9. Rantai lengkap: actual cost → allocation → inventory → HPP saat unit terjual</H3>
        <DataTable
          head={["Tahap", "Apa yang terjadi", "Akun terkait"]}
          rows={[
            ["1. Actual Cost", "Biaya sesungguhnya dicatat & posted (tanah/hard cost)", "Dr Persediaan / Cr Kas-Bank atau Hutang"],
            ["2. Allocation", "Biaya pool dibagi ke unit (proporsional, largest-remainder)", "Snapshot alokasi — tidak ada jurnal baru"],
            ["3. Inventory", "Saldo tersimpan per unit, menunggu unit terjual", "1-3000 / 1-3100 (saldo berjalan)"],
            ["4. HPP saat terjual", "Saldo unit dipindah ke Beban Pokok Penjualan saat Akad", "Dr 5-1000 / Cr 1-3000, 1-3100"],
          ]}
        />
      </div>
    </div>
  );
}
