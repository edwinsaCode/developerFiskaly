import Link from "next/link";
import { Rupiah } from "@/components/format/Rupiah";
import { Card } from "@/components/ui/Card";
import { Table, TableHead, TableBody, TableRow, Th, Td } from "@/components/ui/Table";
import type { HouseARReconciliation } from "@/lib/api/sale";

// W-8 · INV-AR-4 — panel tie-out Buku Besar ↔ sub-ledger piutang harga rumah.
//
// Yang dibandingkan BUKAN saldo akun kontrol seluruhnya, melainkan bagian yang
// bergerak karena jurnal milik alur penjualan. Akun 1-2000 juga menahan saldo
// awal piutang lama dan tagihan biaya realisasi; membandingkan totalnya akan
// selalu "tidak cocok" dan panel ini akan segera diabaikan orang.
//
// Tidak ada aksi perbaikan di sini: ledger append-only, koreksi hanya lewat
// jurnal pembalik. Panel ini mendeteksi, bukan menambal.

// Backend mengirim uang sebagai string desimal ("0", "0.0000", "-0"). Kesamaan
// string bukan kesamaan nilai — nol harus diuji sebagai angka.
function isZero(v: string): boolean {
  const n = Number(v);
  return Number.isFinite(n) ? n === 0 : v.trim() === "0";
}

const accountName: Record<string, string> = {
  "1-2000": "Piutang Customer",
  "1-2100": "Piutang Lain-lain",
  "1-2200": "Dana Jaminan Bank (KPR)",
};

export function HouseARReconPanel({
  recon,
}: {
  recon: HouseARReconciliation | null;
}) {
  if (recon === null) {
    return (
      <Card padding="sm">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div>
            <p className="text-sm font-semibold text-text-primary">
              Rekonsiliasi Buku Besar ↔ Sub-ledger
            </p>
            <p className="text-xs text-text-secondary mt-0.5">
              Panel kontrol tidak dapat dimuat. Angka piutang di bawah tetap sah — yang belum
              terbukti hanyalah kecocokannya dengan buku besar.
            </p>
          </div>
        </div>
      </Card>
    );
  }

  const ok = recon.matched;
  // Tie-out dinilai per akun kontrol, bukan per total (D-W8-6/T-3): antara akad
  // dan pencairan, `1-2200` dan `1-2000` bergerak berlawanan sehingga totalnya
  // bisa nol sementara lapisnya salah. Karena itu header tidak boleh berhenti di
  // "selisih Rp 0" saat statusnya tidak cocok — kalimat itu terbaca sebagai bug
  // panel, dan panel yang terbaca bug akan diabaikan.
  const mismatchedLayers = recon.layers.filter((l) => !l.matched);
  const netZero = !ok && isZero(recon.difference);

  return (
    <Card padding="none" className={ok ? "" : "border-danger/40"}>
      <div
        className={`flex flex-wrap items-center justify-between gap-3 px-5 py-3 rounded-t-xl ${
          ok ? "bg-success-bg" : "bg-danger-bg"
        }`}
      >
        <div className="flex items-center gap-2">
          <span
            className={`inline-block h-2 w-2 rounded-full ${ok ? "bg-success" : "bg-danger"}`}
            aria-hidden
          />
          <span className={`text-sm font-semibold ${ok ? "text-success" : "text-danger"}`}>
            {ok ? "Cocok dengan Buku Besar" : "TIDAK cocok dengan Buku Besar"}
          </span>
          <span className="text-xs text-text-secondary">
            Rekonsiliasi piutang harga rumah
            {recon.as_of_date ? ` · per ${recon.as_of_date}` : ""}
          </span>
        </div>
        <div className="text-sm tabular-nums text-text-primary">
          Sub-ledger <strong><Rupiah value={recon.subledger_total} colorSign={false} /></strong>
          <span className="text-text-tertiary mx-1.5">vs</span>
          Buku Besar (penjualan){" "}
          <strong><Rupiah value={recon.ledger_house_total} colorSign={false} /></strong>
          {!ok &&
            (netZero ? (
              <span className="text-danger ml-2">
                total sama, {mismatchedLayers.length} akun kontrol tidak cocok
              </span>
            ) : (
              <span className="text-danger ml-2">
                selisih <Rupiah value={recon.difference} colorSign={false} />
              </span>
            ))}
        </div>
      </div>

      {recon.layers.length > 0 && (
        <Table>
          <TableHead>
            <TableRow>
              <Th>Akun kontrol</Th>
              <Th right>Sub-ledger</Th>
              <Th right>BB (penjualan)</Th>
              <Th right>Selisih</Th>
              <Th right>Saldo akun</Th>
              <Th right>Bukan harga rumah</Th>
              <Th right>Unit</Th>
            </TableRow>
          </TableHead>
          <TableBody>
            {recon.layers.map((l) => (
              <TableRow key={l.control_account_code}>
                <Td mono>
                  {l.control_account_code}
                  <span className="ml-2 font-sans text-text-secondary">
                    {l.control_account_name || accountName[l.control_account_code] || ""}
                  </span>
                </Td>
                <Td right><Rupiah value={l.subledger} colorSign={false} /></Td>
                <Td right><Rupiah value={l.ledger_house} colorSign={false} /></Td>
                <Td right>
                  <span className={l.matched ? "text-text-tertiary" : "text-danger font-semibold"}>
                    <Rupiah value={l.difference} colorSign={false} />
                  </span>
                </Td>
                <Td right><Rupiah value={l.ledger_total} colorSign={false} /></Td>
                <Td right><Rupiah value={l.ledger_other} colorSign={false} /></Td>
                <Td right>{l.unit_count}</Td>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      <div className="px-5 py-3 border-t border-border space-y-1">
        {/* Kolom "bukan harga rumah" selalu dijelaskan — kalau tidak, orang yang
            membuka neraca akan menyimpulkan panel ini salah karena saldo akun
            kontrol memang lebih besar. */}
        <p className="text-[11px] text-text-tertiary">
          Yang dibandingkan adalah bagian akun kontrol yang bergerak karena{" "}
          {recon.owned_journal_count.toLocaleString("id-ID")} jurnal milik alur penjualan.
          Kolom <em>bukan harga rumah</em> memuat saldo awal piutang lama, tagihan biaya
          realisasi, dan jurnal manual — di luar cakupan rekonsiliasi ini.
        </p>
        {netZero && (
          <p className="text-xs text-danger">
            Totalnya nol karena dua akun kontrol bergerak berlawanan — misalnya piutang yang
            masih tercatat atas nama bank (<span className="font-mono">1-2200</span>) padahal
            sub-ledger sudah membebankannya ke customer (
            <span className="font-mono">1-2000</span>), atau sebaliknya. Membandingkan total
            saja akan menyatakan ini cocok; yang benar adalah per akun kontrol.
          </p>
        )}
        {!ok && (
          <p className="text-xs text-danger">
            Ada mutasi akun kontrol yang tidak berpasangan dengan sub-ledger. Telusuri di{" "}
            <Link href="/accounting/gl" className="underline">
              Buku Besar
            </Link>{" "}
            pada akun yang selisihnya bukan nol. Koreksi hanya lewat jurnal pembalik — jurnal
            yang sudah diposting tidak pernah diubah.
          </p>
        )}
        {recon.note && <p className="text-[11px] text-text-tertiary">{recon.note}</p>}
      </div>
    </Card>
  );
}
