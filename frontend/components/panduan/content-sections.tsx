import { Callout, Code, Collapsible, DataTable, H3, JournalEntryCard, Lead, OL, P, SectionHeader, UL, XRef } from "./blocks";

// 19 section panduan lengkap, urutan & judul persis sesuai permintaan.
// Konten mengutip docs/SYSTEM-DOCUMENTATION.md — bukan salinan mentah,
// tapi disusun ulang jadi UI yang mudah dipindai (tabel, bullet, callout).

export function OverviewSection() {
  return (
    <div className="space-y-6">
      <SectionHeader
        eyebrow="1. Overview Sistem"
        title="Overview Sistem"
        lead={<Lead>Sistem akuntansi &amp; operasional untuk developer perumahan — dari proyek dibuat sampai unit terjual dan laporan keuangan terbit.</Lead>}
      />
      <P>
        Aplikasi ini menggantikan kombinasi Excel + software akuntansi umum (mis. Jurnal.id) dengan
        satu sistem yang memahami alur bisnis developer properti secara native: RAB per proyek,
        alokasi HPP ke unit, skema pembayaran KPR/tunai/bertahap, sampai pengakuan pendapatan &amp;
        HPP yang otomatis balance secara akuntansi.
      </P>
      <div className="space-y-3">
        <H3>Empat audiens utama</H3>
        <DataTable
          head={["Peran", "Fokus penggunaan"]}
          rows={[
            ["Accounting", "Cost entry, alokasi HPP, jurnal, laporan keuangan, tutup buku"],
            ["Admin", "Setup proyek, unit/block, booking, pembayaran, kwitansi"],
            ["Management", "Dashboard, pipeline penjualan, laporan, monitoring realisasi RAB"],
            ["Sales / Marketing", "Booking unit, komisi, status penjualan (akses dibatasi)"],
          ]}
        />
      </div>
      <div className="space-y-3">
        <H3>Objek inti sistem</H3>
        <UL
          items={[
            <><strong>Project</strong> — akar/aggregate root; semua RAB, unit, biaya bermuara ke satu proyek.</>,
            <><strong>RAB (BudgetPlan)</strong> — rencana anggaran biaya, versi ber-status draft/active/superseded.</>,
            <><strong>Unit / Block</strong> — properti yang dijual, membawa land_area &amp; saleable_area.</>,
            <><strong>Cost Entry</strong> — biaya aktual, per kategori &amp; tier (direct/shared/overhead).</>,
            <><strong>Allocation Snapshot</strong> — hasil pembagian biaya pool ke tiap unit, beku setelah Akad.</>,
            <><strong>Journal</strong> — jurnal akuntansi, selalu balanced &amp; append-only.</>,
          ]}
        />
      </div>
      <Callout variant="info" title="Arsitektur package (untuk developer)">
        <P>
          <Code>internal/domain</Code> → value object saja, tanpa DB/HTTP. <Code>internal/*/repository.go</Code>{" "}
          → akses DB dengan GORM global scope per tenant. <Code>internal/*/service.go</Code> → business
          logic. <Code>internal/*/handler.go</Code> → HTTP handler tanpa business logic.
        </P>
      </Callout>
      <P>
        Mulai dari <XRef id="alur">Alur End-to-End Bisnis</XRef> untuk melihat gambaran besar sebelum
        masuk ke modul spesifik.
      </P>
    </div>
  );
}

export function SetupProjectSection() {
  return (
    <div className="space-y-6">
      <SectionHeader eyebrow="2. Setup Project" title="Setup Project" lead={<Lead>Langkah pertama sebelum RAB atau unit bisa dibuat.</Lead>} />
      <div className="space-y-3">
        <H3>Yang perlu diisi saat membuat proyek</H3>
        <DataTable
          head={["Field", "Keterangan"]}
          rows={[
            ["Nama & lokasi proyek", "Identitas proyek, dipakai di semua laporan"],
            ["tax_category default", "subsidi / komersial — jadi default untuk unit baru, bisa di-override per unit"],
            ["Chart of Account (COA)", "Terisi dari seed default sistem — akun tanah, hard cost, pendapatan, dsb. sudah tersedia"],
          ]}
        />
      </div>
      <Callout variant="frozen" title="Belum ada jurnal pada tahap ini">
        Membuat proyek murni mencatat metadata. Tidak ada dampak Neraca atau Laba/Rugi sampai RAB
        disusun, disetujui, dan biaya aktual mulai dicatat.
      </Callout>
      <P>
        Setelah proyek dibuat, langkah berikutnya adalah menyusun RAB (<XRef id="rab-realisasi">§3</XRef>)
        dan membuat unit (<XRef id="unit-block">§4</XRef>).
      </P>
    </div>
  );
}

export function RabRealisasiSection() {
  return (
    <div className="space-y-6">
      <SectionHeader
        eyebrow="3. RAB & Realisasi"
        title="RAB & Realisasi"
        lead={<Lead>RAB (Rencana Anggaran Biaya) adalah pagu; Realisasi adalah pemantauan seberapa jauh biaya aktual sudah menyerap pagu itu.</Lead>}
      />
      <div className="space-y-3">
        <H3>Versi & status RAB (BudgetPlan)</H3>
        <DataTable
          head={["Status", "Arti"]}
          rows={[
            ["draft", "Sedang disusun, belum berlaku, belum membatasi realisasi biaya"],
            ["active", "Sudah disetujui, satu-satunya versi yang berlaku untuk proyek/fase ini"],
            ["superseded", "Versi lama yang digantikan versi baru — tetap tersimpan untuk audit trail, tidak dihapus"],
          ]}
        />
        <Callout variant="frozen" title="Satu BudgetPlan active per proyek/fase">
          Saat versi baru di-approve, versi lama otomatis menjadi <Code>superseded</Code>. Ini
          invariant final — tidak bisa ada dua RAB <Code>active</Code> berbarengan untuk proyek/fase
          yang sama.
        </Callout>
      </div>
      <div className="space-y-3">
        <H3>Kategori & tier RAB</H3>
        <P>RAB disusun per kategori biaya, dan tiap item membawa tier yang menentukan cara alokasinya:</P>
        <DataTable
          head={["Tier", "Arti", "Contoh"]}
          rows={[
            ["direct", "Biaya langsung ke satu unit tertentu", "Finishing khusus unit corner"],
            ["shared", "Biaya dibagi ke sekelompok unit (pool)", "Hard cost per tipe subsidi/komersial"],
            ["overhead", "Biaya proyek-wide, tidak terikat unit tertentu (boleh project_id kosong)", "Marketing umum, biaya operasional kantor proyek"],
          ]}
        />
      </div>
      <div className="space-y-3">
        <H3>Realisasi</H3>
        <P>
          <Code>Realisasi % = Σ biaya aktual ter-link ke item RAB / RAB approved item tersebut</Code>.
          Angka ini murni untuk pemantauan (dashboard, laporan) — tidak menghasilkan jurnal apa pun.
          Realisasi 0% berarti belum ada biaya aktual yang dicatat untuk item RAB tersebut; realisasi
          bisa melebihi 100% jika biaya aktual melampaui pagu (ditampilkan sebagai peringatan, bukan
          diblokir secara keras kecuali kebijakan tenant mengatur gate).
        </P>
      </div>
      <Callout variant="info" title="RAB approved ≠ Persediaan">
        Lihat penjelasan lengkap di <XRef id="accounting-hpp">Accounting &amp; HPP</XRef> — persetujuan
        RAB tidak pernah menghasilkan jurnal.
      </Callout>
    </div>
  );
}

export function UnitBlockSection() {
  return (
    <div className="space-y-6">
      <SectionHeader
        eyebrow="4. Unit & Block"
        title="Unit & Block"
        lead={<Lead>Unit adalah properti yang dijual (rumah/ruko/kavling); Block mengelompokkan unit secara spasial.</Lead>}
      />
      <div className="space-y-3">
        <H3>Field wajib saat membuat unit</H3>
        <DataTable
          head={["Field", "Kenapa wajib"]}
          rows={[
            ["land_area (m²)", "Bobot alokasi biaya tanah — tanpa ini unit tidak bisa dialokasikan HPP tanah"],
            ["saleable_area (m²)", "Dasar perhitungan harga jual per m² dan beberapa alokasi hard cost"],
            ["tax_category", "subsidi / komersial — menentukan pool alokasi konstruksi & tarif PPh"],
          ]}
        />
      </div>
      <div className="space-y-3">
        <H3>Status unit</H3>
        <UL
          items={[
            <><Code>available</Code> — siap dijual, belum ada transaksi.</>,
            <><Code>booked</Code> — sudah ada booking fee, terkunci sementara untuk calon pembeli tertentu.</>,
            <><Code>sold</Code> — sudah Akad, pendapatan &amp; HPP sudah diakui.</>,
            <><Code>cancelled</Code> — pembatalan, lihat aturan pra/pasca-BAST di <XRef id="penjualan-akad">§9</XRef>.</>,
          ]}
        />
      </div>
      <P>
        Unit dalam jumlah banyak bisa dibuat sekaligus lewat wizard bulk unit — tetap mewajibkan
        land_area &amp; saleable_area per baris.
      </P>
    </div>
  );
}

export function BiayaSection() {
  return (
    <div className="space-y-6">
      <SectionHeader
        eyebrow="5. Biaya / Cost Entry"
        title="Biaya / Cost Entry"
        lead={<Lead>Tempat mencatat biaya aktual — bukan RAB, bukan rencana, tapi uang yang benar-benar keluar atau terhutang.</Lead>}
      />
      <div className="space-y-3">
        <H3>Kategori biaya</H3>
        <DataTable
          head={["Kategori", "Sifat"]}
          rows={[
            ["Tanah", "Capital — masuk Persediaan Tanah"],
            ["Hard Cost (Konstruksi)", "Capital — masuk Persediaan Hard Cost"],
            ["Sarana & Prasarana", "Capital — pool General, masuk Persediaan Hard Cost"],
            ["Perizinan", "Capital — pool General, masuk Persediaan Hard Cost"],
            ["Pemasaran", "Beban — tidak dikapitalisasi"],
            ["Lain-lain", "Beban — tidak dikapitalisasi (kecuali ditandai capital khusus)"],
            ["Operasional", "Beban — biaya kantor/operasional proyek"],
          ]}
        />
      </div>
      <Callout variant="frozen" title="Akun taksonomi tidak boleh di-tag proyek">
        Untuk kategori biaya yang sifatnya akun taksonomi (bukan biaya proyek spesifik), sistem
        menolak jika biaya tersebut dipaksa terikat ke satu proyek — mencegah salah kategorisasi yang
        bisa merusak alokasi HPP.
      </Callout>
      <div className="space-y-3">
        <H3>Dua jalur masuk biaya</H3>
        <UL
          items={[
            <><strong>Pengeluaran langsung</strong> — satu pintu masuk uang keluar, otomatis menghasilkan bukti kas bernomor.</>,
            <><strong>AP (Hutang Usaha)</strong> — vendor invoice dicatat sebagai hutang dulu, cost entry mengikuti baris AP; pembayaran AP menyusul terpisah lewat modul Pembayaran Vendor.</>,
          ]}
        />
      </div>
      <Callout variant="frozen" title="Ledger append-only">
        Biaya yang salah tidak diedit atau dihapus setelah posted — koreksi dilakukan lewat jurnal
        pembalik (reversing entry), menjaga histori tetap utuh untuk audit.
      </Callout>
    </div>
  );
}

export function PersediaanHppSection() {
  return (
    <div className="space-y-6">
      <SectionHeader
        eyebrow="6. Persediaan & HPP"
        title="Persediaan & HPP"
        lead={<Lead>Persediaan adalah biaya capital yang "mengendap" menunggu unit terjual; HPP adalah saat biaya itu dipindah jadi beban.</Lead>}
      />
      <div className="space-y-3">
        <H3>Dua akun Persediaan utama</H3>
        <DataTable
          head={["Akun", "Isi"]}
          rows={[
            ["1-3000 Persediaan Tanah", "Biaya tanah yang sudah dialokasikan ke unit, belum terjual"],
            ["1-3100 Persediaan Hard Cost", "Biaya konstruksi + Sarana & Prasarana + Perizinan yang sudah dialokasikan, belum terjual"],
          ]}
        />
      </div>
      <Callout variant="frozen" title="HPP = biaya terakumulasi unit, bukan estimasi">
        Saat penjualan, HPP yang diakui untuk sebuah unit sama dengan biaya terakumulasi unit itu pada
        saat tersebut — prinsip ini berlaku baik di pengakuan awal (basis budgeted) maupun setelah
        True-Up (basis aktual final).
      </Callout>
      <div className="space-y-3">
        <H3>Mekanisme dua tahap: Budgeted dulu, True-Up kemudian</H3>
        <OL
          items={[
            <>Saat Akad, HPP diakui memakai basis RAB <strong>approved</strong> (budgeted) — karena belum tentu semua biaya proyek sudah tercatat 100% pada tanggal itu.</>,
            <>Saat proyek/fase selesai (biaya aktual final sudah lengkap), sistem menjalankan True-Up: selisih budgeted vs aktual dijurnal sebagai koreksi tambahan.</>,
            <>Jurnal Akad yang sudah posted tidak pernah diedit — True-Up selalu jurnal baru terpisah (ledger append-only).</>,
          ]}
        />
        <P>Contoh lengkap ada di langkah 11 &amp; 14 pada <XRef id="alur">Alur End-to-End Bisnis</XRef>.</P>
      </div>
    </div>
  );
}

export function AlokasiHppSection() {
  return (
    <div className="space-y-6">
      <SectionHeader
        eyebrow="7. Alokasi HPP"
        title="Alokasi HPP"
        lead={<Lead>Cara biaya pool (tanah & hard cost) dibagi ke tiap unit — dan dibekukan sebagai snapshot setelah unit terjual.</Lead>}
      />
      <div className="space-y-3">
        <H3>Alokasi tanah — proporsional berdasarkan land_area</H3>
        <DataTable
          head={["Unit", "Land Area", "Alokasi Tanah"]}
          rows={[
            ["Unit A", "100 m²", "Rp 200.000.000"],
            ["Unit B", "150 m²", "Rp 300.000.000"],
            ["Unit C", "150 m²", "Rp 300.000.000"],
          ]}
        />
        <P>
          Pool tanah project-wide Rp900.000.000, dikurangi carve-out Kelebihan Tanah
          Rp100.000.000, sisa Rp800.000.000 dibagi proporsional per land_area — hasil penjumlahan
          alokasi persis Rp800.000.000, tanpa sisa.
        </P>
      </div>
      <div className="space-y-3">
        <H3>Alokasi hard cost — 3 pool berdasarkan tax_category</H3>
        <P>
          Produksi Subsidi, Produksi Komersial, dan General (Sarana &amp; Prasarana + Perizinan,
          dibagi ke semua unit). Lihat contoh angka lengkap di{" "}
          <XRef id="accounting-hpp">Accounting &amp; HPP</XRef>.
        </P>
      </div>
      <Callout variant="frozen" title="Rekonsiliasi sampai rupiah terakhir">
        <Code>Σ(biaya teralokasi per unit) = total biaya proyek dikapitalisasi</Code>, persis. Sisa
        pembulatan dibagikan lewat metode largest-remainder yang deterministik — tidak pernah dibuang
        begitu saja.
      </Callout>
      <div className="space-y-3">
        <H3>Snapshot beku setelah Akad</H3>
        <P>
          Begitu unit di-Akad, allocation snapshot untuk unit tersebut dibekukan — perubahan biaya
          setelahnya tidak lagi mengubah alokasi historis unit itu, hanya memengaruhi True-Up (lihat{" "}
          <XRef id="persediaan-hpp">§6</XRef>).
        </P>
      </div>
    </div>
  );
}

export function BookingSection() {
  return (
    <div className="space-y-6">
      <SectionHeader
        eyebrow="8. Booking"
        title="Booking"
        lead={<Lead>Langkah pertama calon pembeli mengunci sebuah unit, dengan atau tanpa biaya.</Lead>}
      />
      <JournalEntryCard
        title="Booking fee diterima"
        when="Saat pembayaran booking fee diterima"
        debit={[{ account: "1-1300", label: "Bank BCA", amount: 5_000_000 }]}
        credit={[{ account: "4-2100", label: "Pendapatan Booking", amount: 5_000_000 }]}
        neraca="Kas/Bank naik, tidak ada kewajiban yang dibentuk."
        labaRugi="Diakui final saat diterima — tidak dibalik meski booking kemudian dibatalkan."
      />
      <Callout variant="frozen" title="Booking fee boleh Rp0">
        Booking fee tidak wajib lebih dari nol. Jika Rp0, unit tetap ter-lock ke calon pembeli tanpa
        menghasilkan jurnal apa pun — hanya perubahan status unit menjadi <Code>booked</Code>.
      </Callout>
      <P>Booking non-refundable — berbeda dari DP (Uang Muka) yang statusnya kewajiban sampai Akad, lihat <XRef id="alur">Alur End-to-End Bisnis</XRef> langkah 9-10.</P>
    </div>
  );
}

export function PenjualanAkadSection() {
  return (
    <div className="space-y-6">
      <SectionHeader
        eyebrow="9. Penjualan / Kontrak / Akad"
        title="Penjualan / Kontrak / Akad"
        lead={<Lead>Titik di mana kriteria pengakuan pendapatan terpenuhi — pendapatan dan HPP diakui bersamaan dalam satu transaksi atomik.</Lead>}
      />
      <div className="space-y-3">
        <H3>Gate sebelum Akad bisa diproses</H3>
        <UL
          items={[
            "RAB proyek berstatus approved.",
            "Basis Alokasi HPP (allocation snapshot) sudah tersedia untuk unit tersebut.",
            "Syarat lunas skema pembayaran terpenuhi (mis. KPR sudah fully_paid dari bank, atau tunai bertahap sudah sesuai termin akad).",
          ]}
        />
      </div>
      <P>Contoh lengkap jurnal pengakuan pendapatan + HPP ada di langkah 11 pada <XRef id="alur">Alur End-to-End Bisnis</XRef>.</P>
      <div className="space-y-3">
        <H3>Pembatalan (Cancellation)</H3>
        <DataTable
          head={["Kapan", "Perlakuan"]}
          rows={[
            ["Pra-BAST (belum Akad)", "Uang Muka (kewajiban) dibalik / dikembalikan sesuai kebijakan; belum ada pendapatan/HPP yang perlu dibalik"],
            ["Pasca-BAST (sudah Akad)", "Jurnal pembalik penuh — membalik pendapatan & HPP yang sudah diakui, tanpa perlu tahu detail bagaimana HPP itu dulu terbentuk"],
          ]}
        />
        <Callout variant="info" title="INV-COGS-SUM">
          HPP yang diakui untuk sebuah unit selalu sama dengan jumlah semua jurnal HPP unit itu yang
          sudah posted. Karena itu pembatalan cukup membalik seluruh jurnal terkait — tidak perlu
          logika khusus yang memahami asal-usul tiap komponen HPP.
        </Callout>
      </div>
    </div>
  );
}

export function PembayaranReceiptSection() {
  return (
    <div className="space-y-6">
      <SectionHeader
        eyebrow="10. Pembayaran & Receipt"
        title="Pembayaran & Receipt"
        lead={<Lead>Satu pintu masuk penerimaan pembayaran dari customer, dengan sub-ledger dan kwitansi otomatis.</Lead>}
      />
      <div className="space-y-3">
        <H3>ReceivePayment — satu pintu masuk</H3>
        <P>
          Semua penerimaan pembayaran customer (DP, termin, pelunasan) melalui satu service yang
          sama. Sistem otomatis menentukan pembayaran itu mengurangi Uang Muka (pra-Akad) atau
          Piutang Usaha (pasca-Akad) berdasarkan status unit — pengguna tidak perlu memilih akun
          secara manual.
        </P>
      </div>
      <div className="space-y-3">
        <H3>Sub-ledger payment_allocations</H3>
        <P>
          Setiap pembayaran termin dipecah ke tingkat cicilan (bukan hanya invoice/ledger level),
          sehingga status "sudah bayar termin ke berapa" bisa ditelusuri persis per unit.
        </P>
      </div>
      <div className="space-y-3">
        <H3>Kwitansi</H3>
        <UL
          items={[
            <>Nomor otomatis, format <Code>KWT/YYYY/NNNNNN</Code>, idempoten — tidak pernah dobel untuk pembayaran yang sama.</>,
            <>Tidak menghasilkan jurnal sendiri — kwitansi adalah bukti dokumen atas jurnal penerimaan yang sudah tercatat.</>,
            <>Pembayaran parsial (kurang dari nominal termin) tetap menghasilkan kwitansi untuk nominal yang diterima.</>,
          ]}
        />
      </div>
      <Callout variant="warning" title="Sisa realisasi biaya pasca-BAST menjadi Piutang Customer">
        Saat BAST, sisa biaya realisasi (mis. PDAM/Listrik/BPHTB/Notaris yang belum lunas) menjadi
        Piutang Customer — bukan piutang kedua yang terpisah dari mesin piutang utama. Lebih bayar
        dicatat sebagai saldo kredit, bukan dikembalikan otomatis.
      </Callout>
    </div>
  );
}

export function KprJaminanBankSection() {
  return (
    <div className="space-y-6">
      <SectionHeader
        eyebrow="11. KPR & Dana Jaminan Bank"
        title="KPR & Dana Jaminan Bank"
        lead={<Lead>Akun perantara untuk pinjaman KPR yang sudah disetujui bank tapi belum cair penuh.</Lead>}
      />
      <div className="space-y-3">
        <H3>Akun 1-2200 Dana Jaminan Bank</H3>
        <P>
          Dipakai antara Akad (bank approved) sampai pencairan penuh. Setiap pencairan bertahap
          mengurangi saldo akun ini dan menambah kas/bank — pencairan tidak boleh melebihi saldo yang
          dijamin untuk unit tersebut.
        </P>
      </div>
      <Callout variant="frozen" title="Kekurangan pasca-pencairan → Piutang Customer">
        Jika nilai yang akhirnya cair dari bank lebih kecil dari yang disepakati saat Akad, sisanya
        direklas ke <Code>1-2000 Piutang Usaha</Code> — akun 1-2200 hanya dipakai murni untuk rentang
        akad → pencairan, bukan untuk menampung kekurangan permanen.
      </Callout>
      <P>
        Gate Akad menerima status skema <Code>fully_paid</Code> dari KPR sebagai syarat lunas — lihat{" "}
        <XRef id="penjualan-akad">§9</XRef>.
      </P>
    </div>
  );
}

export function KelebihanTanahSection() {
  return (
    <div className="space-y-6">
      <SectionHeader
        eyebrow="12. Kelebihan Tanah"
        title="Kelebihan Tanah"
        lead={<Lead>Bidang tanah sisa/lebih yang dijual terpisah dari unit rumah — modul berdiri sendiri.</Lead>}
      />
      <P>
        Kelebihan Tanah adalah produk tambahan (addon), bukan unit rumah/ruko/kavling. HPP-nya
        dihitung langsung: <Code>HPP = land_area (m²) × harga beli tanah per m²</Code> — berbeda dari
        HPP tanah unit reguler yang berbasis pool proporsional (lihat{" "}
        <XRef id="accounting-hpp">Accounting &amp; HPP</XRef>).
      </P>
      <JournalEntryCard
        title="Contoh — pengakuan pendapatan & HPP Kelebihan Tanah"
        when="Saat Kelebihan Tanah terjual (bundled dengan transaksi Akad/pembatalan unit terkait)"
        debit={[{ account: "1-1300", label: "Bank (atau Piutang, tergantung skema)", amount: 25_000_000 }]}
        credit={[{ account: "4-1000", label: "Pendapatan Penjualan (Kelebihan Tanah)", amount: 25_000_000 }]}
        neraca="Kas/Piutang naik."
        labaRugi="Pendapatan diakui penuh saat transaksi terjadi."
      />
      <Callout variant="info" title="Transaksi bundel">
        Penjualan Kelebihan Tanah dibundel dalam satu transaksi atomik dengan Akad atau pembatalan
        unit terkait — tidak pernah berdiri sendiri secara terpisah dari siklus hidup unit induknya.
      </Callout>
    </div>
  );
}

export function PajakSection() {
  return (
    <div className="space-y-6">
      <SectionHeader
        eyebrow="13. Pajak"
        title="Pajak"
        lead={<Lead>Tarif dan akun pajak ditentukan oleh tax_category unit, dikonfigurasi lewat Tax Rule — bukan hardcode di kode.</Lead>}
      />
      <div className="space-y-3">
        <H3>PPh Final atas penjualan unit</H3>
        <DataTable
          head={["tax_category", "Tarif ilustratif", "Contoh (harga jual Rp 180.000.000)"]}
          rows={[
            ["subsidi", "1%", "Rp 1.800.000"],
            ["komersial", "2,5%", "Rp 4.500.000"],
          ]}
          rightCols={[2]}
        />
        <Callout variant="warning" title="Tarif adalah konfigurasi, verifikasi ke Tax Rule aktif">
          Tarif di atas ilustratif berdasarkan rate yang umum berlaku saat dokumen ini dibuat. Tarif
          final yang benar-benar dipakai sistem berasal dari Tax Rule aktif (dengan provenance{" "}
          <Code>rule_id</Code> + <Code>revision</Code>) — cek konfigurasi pajak berjalan sebelum
          menjadikan tabel ini acuan mutlak.
        </Callout>
      </div>
      <div className="space-y-3">
        <H3>Kapan pajak dihitung</H3>
        <P>
          Pajak dipicu oleh <em>trigger event</em> bertipe (mis. Akad selesai), bukan tanggal kalender
          sembarang — memastikan perhitungan konsisten dengan kapan pendapatan diakui.
        </P>
      </div>
      <Callout variant="frozen" title="Aturan ketidakpastian pajak">
        Saat ragu soal aturan pajak atau titik pengakuan, tim menandai dengan{" "}
        <Code>// TODO(tax-advisor)</Code> daripada menebak — celah yang ditandai lebih baik daripada
        angka yang salah.
      </Callout>
    </div>
  );
}

export function SalesCommissionSection() {
  return (
    <div className="space-y-6">
      <SectionHeader
        eyebrow="14. Sales & Commission"
        title="Sales & Commission"
        lead={<Lead>Penugasan salesperson per kontrak dan perhitungan komisi otomatis.</Lead>}
      />
      <JournalEntryCard
        title="Contoh — komisi sales atas Akad (2% dari harga jual)"
        when="Saat Akad selesai dan komisi disetujui untuk dibayar"
        debit={[{ account: "6-xxxx", label: "Beban Komisi Sales", amount: 3_600_000 }]}
        credit={[{ account: "2-1000", label: "Hutang Komisi", amount: 3_600_000 }]}
        note="2% × Rp180.000.000 = Rp3.600.000."
        neraca="Hutang Komisi (kewajiban) naik sampai dibayar."
        labaRugi="Beban Komisi Sales diakui pada periode Akad."
      />
      <Callout variant="warning" title="Clawback saat pembatalan pasca-Akad">
        Jika unit dibatalkan setelah Akad dan komisi sudah dibayar, komisi tersebut ikut dibalik
        (clawback) sejalan dengan pembalikan pendapatan &amp; HPP unit — lihat{" "}
        <XRef id="penjualan-akad">§9 Penjualan / Kontrak / Akad</XRef>.
      </Callout>
      <P>Salesperson wajib ditentukan sejak kontrak baru dibuat (bagian dari data kontrak, bukan opsional belakangan).</P>
    </div>
  );
}

export function FixedAssetSection() {
  return (
    <div className="space-y-6">
      <SectionHeader
        eyebrow="15. Fixed Asset & Depreciation"
        title="Fixed Asset & Depreciation"
        lead={<Lead>Aset tetap perusahaan (bukan unit yang dijual) — dari perolehan sampai penyusutan berjalan.</Lead>}
      />
      <div className="space-y-3">
        <H3>Alur pendaftaran</H3>
        <OL
          items={[
            "Perolehan aset dicatat lewat modul Pengeluaran (satu pintu masuk uang keluar) yang sama dengan biaya lain.",
            "Aset didaftarkan di Register aset tetap dengan umur manfaat & metode penyusutan.",
            "Depreciation run dijalankan berkala (biasanya bulanan) untuk menghasilkan jurnal penyusutan otomatis.",
          ]}
        />
      </div>
      <JournalEntryCard
        title="Contoh — jurnal penyusutan bulanan"
        when="Setiap tutup periode bulanan, untuk aset yang masih dalam umur manfaat"
        debit={[{ account: "6-xxxx", label: "Beban Penyusutan", amount: 2_000_000 }]}
        credit={[{ account: "1-6xxx", label: "Akumulasi Penyusutan", amount: 2_000_000 }]}
        neraca="Nilai buku aset (aset dikurangi akumulasi penyusutan) turun."
        labaRugi="Beban Penyusutan diakui setiap periode berjalan."
      />
      <P>Pelepasan (disposal) aset dicatat terpisah, membandingkan nilai buku sisa dengan hasil penjualan/pelepasan aset.</P>
    </div>
  );
}

export function LaporanKeuanganSection() {
  return (
    <div className="space-y-6">
      <SectionHeader
        eyebrow="16. Laporan Keuangan"
        title="Laporan Keuangan"
        lead={<Lead>Tujuh laporan inti, semua diturunkan dari ledger yang sama — tidak ada hitungan ulang terpisah di layer laporan.</Lead>}
      />
      <DataTable
        head={["Laporan", "Isi"]}
        rows={[
          ["Neraca", "Posisi aset, kewajiban, ekuitas per tanggal"],
          ["Laba Rugi", "Pendapatan dikurangi HPP dan beban operasional per periode"],
          ["Arus Kas", "Pergerakan kas masuk/keluar per periode"],
          ["Neraca Saldo", "Saldo semua akun sebelum penutupan"],
          ["Laporan Pajak", "Rekap PPh Final & kewajiban pajak lain per periode"],
          ["Pipeline Penjualan", "Status unit dari booking sampai Akad, untuk monitoring management"],
          ["Laporan Historis (per tahun tutup buku)", "Snapshot laporan tahun-tahun sebelumnya, di luar ledger berjalan"],
        ]}
      />
      <Callout variant="info" title="Single Source of Truth">
        Dashboard dan laporan wajib memakai engine kanonik yang sama (revenue, HPP, biaya aktual) —
        bukan query SQL mentah yang menghitung ulang secara terpisah. Semua laporan bisa diexport ke
        PDF dengan nomor dokumen yang konsisten dengan bukti kas terkait.
      </Callout>
    </div>
  );
}

export function AccountingJurnalSection() {
  return (
    <div className="space-y-6">
      <SectionHeader
        eyebrow="17. Accounting & Jurnal"
        title="Accounting & Jurnal"
        lead={<Lead>Aturan dasar yang mengikat setiap jurnal di sistem, dan bagaimana periode ditutup.</Lead>}
      />
      <Callout variant="frozen" title="Jurnal selalu balanced">
        Setiap Journal Entry: <Code>Σ debit = Σ kredit</Code>. Posting service menolak apa pun yang
        tidak seimbang, tanpa pengecualian, selamanya.
      </Callout>
      <div className="space-y-3">
        <H3>Ledger append-only</H3>
        <P>
          Jurnal yang sudah diposting bersifat immutable. Koreksi hanya lewat jurnal pembalik —
          tidak pernah edit atau hapus histori. Ini berlaku untuk semua modul: cost entry, AP,
          True-Up, pembatalan, penyusutan.
        </P>
      </div>
      <div className="space-y-3">
        <H3>Satu dokumen bernomor per pergerakan kas</H3>
        <P>
          Setiap pergerakan kas (masuk atau keluar) wajib punya satu nomor dokumen unik — dicek secara
          fail-closed di dalam transaksi database yang sama, bukan validasi terpisah setelahnya.
          Penomoran pakai satu engine terpusat, reset tahunan berdasarkan tahun fiskal saat dokumen
          terbit (forward-only, tidak mundur).
        </P>
      </div>
      <div className="space-y-3">
        <H3>Isolasi tenant</H3>
        <P>
          Setiap baris data membawa <Code>tenant_id</Code>; setiap repository memakai GORM global
          scope <Code>WHERE tenant_id = ?</Code>. Tidak ada query yang boleh lintas tenant — diverifikasi
          lewat integration test karena MySQL tidak punya RLS bawaan.
        </P>
      </div>
      <div className="space-y-3">
        <H3>Tutup buku (closing)</H3>
        <P>
          Tutup buku tahunan memindahkan saldo akun nominal (pendapatan/beban) ke Laba Ditahan.
          Laporan historis untuk tahun yang sudah ditutup dijaga terpisah dari ledger berjalan, dengan
          guard berbasis jurnal yang benar-benar sudah posted pada tahun tersebut — bukan sekadar
          tanggal go-live proyek.
        </P>
      </div>
    </div>
  );
}

export function TroubleshootingSection() {
  return (
    <div className="space-y-6">
      <SectionHeader
        eyebrow="18. Troubleshooting"
        title="Troubleshooting"
        lead={<Lead>Pertanyaan yang paling sering muncul saat memakai sistem sehari-hari.</Lead>}
      />
      <div className="space-y-3">
        <Collapsible title="Kenapa Realisasi menunjukkan 0% padahal saya sudah input biaya?" defaultOpen>
          <P>
            Cek apakah biaya sudah <em>posted</em> (bukan draft) dan sudah ter-link ke item RAB yang
            benar. Biaya yang belum posted tidak dihitung dalam Realisasi.
          </P>
        </Collapsible>
        <Collapsible title="Kenapa unit tidak bisa di-Akad?">
          <P>
            Periksa tiga gate: (1) RAB proyek berstatus <Code>approved</Code>, (2) Alokasi HPP untuk
            unit tersebut sudah tersedia, (3) syarat lunas skema pembayaran (mis. KPR{" "}
            <Code>fully_paid</Code>) sudah terpenuhi. Lihat <XRef id="penjualan-akad">§9</XRef>.
          </P>
        </Collapsible>
        <Collapsible title="Kenapa HPP yang tampil beda dari total biaya aktual saat ini?">
          <P>
            HPP yang sudah diakui memakai basis RAB approved pada saat Akad, bukan biaya aktual yang
            terus berubah. Selisihnya baru dikoreksi lewat True-Up saat proyek/fase ditutup — lihat{" "}
            <XRef id="persediaan-hpp">§6 Persediaan &amp; HPP</XRef>.
          </P>
        </Collapsible>
        <Collapsible title="Pembayaran customer sudah diterima tapi tidak muncul di kwitansi">
          <P>
            Pastikan pembayaran dicatat lewat alur ReceivePayment (bukan input manual di luar sistem).
            Kwitansi idempoten dibuat otomatis mengikuti jurnal penerimaan — kwitansi ganda untuk
            pembayaran yang sama tidak akan pernah dibuat oleh sistem.
          </P>
        </Collapsible>
        <Collapsible title="Kenapa saya tidak bisa mengedit jurnal yang sudah posted?">
          <P>
            Ini bukan bug — ledger bersifat append-only (lihat <XRef id="accounting-jurnal">§17</XRef>).
            Buat jurnal pembalik atau koreksi baru, jangan mencoba mengedit histori.
          </P>
        </Collapsible>
        <Collapsible title="Kenapa akun saya (role Marketing) tidak bisa melihat beberapa halaman?">
          <P>
            Akses per peran diatur fail-closed di router — role tertentu memang sengaja dibatasi hanya
            pada halaman yang relevan dengan tugasnya. Hubungi admin bila butuh akses tambahan.
          </P>
        </Collapsible>
        <Collapsible title="Dev server tidak bisa diakses dari perangkat lain di jaringan">
          <P>
            Ini sengaja — server development hanya boleh diakses dari <Code>127.0.0.1</Code>{" "}
            (localhost) karena database berisi data tenant nyata. Ini bukan bug yang perlu diperbaiki.
          </P>
        </Collapsible>
      </div>
    </div>
  );
}

interface GlossaryTerm {
  term: string;
  def: string;
}

const GLOSSARY: GlossaryTerm[] = [
  { term: "RAB (BudgetPlan)", def: "Rencana Anggaran Biaya — pagu rencana per kategori biaya proyek, punya versi berstatus draft/active/superseded." },
  { term: "Realisasi", def: "Persentase biaya aktual yang sudah tercatat dibanding pagu RAB untuk kategori yang sama." },
  { term: "Cost Entry", def: "Catatan biaya aktual (bukan rencana), per kategori dan tier direct/shared/overhead." },
  { term: "Alokasi HPP", def: "Proses membagi biaya pool (tanah, hard cost) ke tiap unit secara proporsional." },
  { term: "Allocation Snapshot", def: "Hasil beku alokasi HPP per unit, terkunci setelah unit di-Akad." },
  { term: "Persediaan", def: "Saldo biaya capital (tanah & hard cost) yang belum jadi HPP karena unit belum terjual." },
  { term: "HPP (Harga Pokok Penjualan)", def: "Biaya terakumulasi unit yang dipindah dari Persediaan menjadi beban saat unit terjual." },
  { term: "True-Up", def: "Koreksi HPP dari basis budgeted (saat Akad) ke basis biaya aktual final, dijurnal terpisah saat proyek/fase selesai." },
  { term: "Booking Fee", def: "Tanda jadi non-refundable, langsung diakui sebagai pendapatan booking saat diterima." },
  { term: "Uang Muka (DP)", def: "Kas yang diterima sebelum kriteria pengakuan (Akad) terpenuhi — dicatat sebagai kewajiban, bukan pendapatan." },
  { term: "Akad / BAST", def: "Titik pengakuan pendapatan & HPP — saat semua gate kelengkapan terpenuhi." },
  { term: "Dana Jaminan Bank", def: "Akun perantara untuk pinjaman KPR yang sudah disetujui bank tapi belum cair penuh." },
  { term: "Kelebihan Tanah", def: "Bidang tanah sisa yang dijual sebagai produk tambahan (addon), HPP dihitung langsung dari luas × harga beli/m²." },
  { term: "tax_category", def: "Kategori pajak unit — subsidi atau komersial — menentukan pool alokasi konstruksi & tarif PPh Final." },
  { term: "Piutang Customer / Piutang Usaha", def: "Tagihan ke customer setelah kriteria pengakuan terpenuhi, termasuk sisa biaya realisasi pasca-BAST." },
  { term: "Ledger append-only", def: "Prinsip bahwa jurnal yang sudah posted tidak pernah diedit/dihapus — hanya dibalik lewat jurnal baru." },
  { term: "Largest-remainder", def: "Metode pembulatan alokasi yang deterministik, memastikan total alokasi persis sama dengan pool sumber tanpa sisa yang dibuang." },
  { term: "Tenant isolation", def: "Setiap baris data terikat tenant_id; setiap query dibatasi GORM global scope per tenant." },
];

export function GlossarySection() {
  return (
    <div className="space-y-6">
      <SectionHeader eyebrow="19. Glossary" title="Glossary" lead={<Lead>Istilah yang sering muncul di seluruh panduan ini.</Lead>} />
      <Collapsible title={`Daftar istilah A–Z (${GLOSSARY.length} istilah)`} defaultOpen>
        <DataTable head={["Istilah", "Arti"]} rows={GLOSSARY.map((g) => [<strong key={g.term}>{g.term}</strong>, g.def])} />
      </Collapsible>
    </div>
  );
}
