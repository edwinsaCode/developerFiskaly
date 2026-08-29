// Riwayat pembayaran satu tagihan vendor (W-11 Tahap 5).
//
// Panel murni tampilan: barisnya datang apa adanya dari
// GET /ap/invoices/{id}/payments. Layar tidak menjumlahkan, tidak mengurangkan,
// dan tidak menyimpulkan status apa pun dari daftar ini — `paid_amount`,
// `outstanding`, dan `payment_status` sudah dibawa tagihannya sendiri, dihitung
// backend dari sub-ledger yang sama.
//
// Baris yang sudah dibalik tetap tampil, diredupkan dan diberi tanda. "Belum
// pernah dibayar" dan "pernah dibayar lalu dibatalkan" adalah dua keadaan yang
// sangat berbeda ketika ada yang bertanya ke mana uangnya.

import Link from "next/link";

import { Badge } from "@/components/ui/Badge";
import { Card } from "@/components/ui/Card";
import { Table, TableBody, TableHead, TableRow, Td, Th } from "@/components/ui/Table";
import { Rupiah } from "@/components/format/Rupiah";
import { Tanggal } from "@/components/format/Tanggal";
import type { APInvoicePaymentRow, APInvoiceStatus } from "@/lib/types/api";

interface Props {
  rows: APInvoicePaymentRow[];
  invoiceStatus: APInvoiceStatus;
  loadError?: string;
}

// Jenis alokasi datang dari backend sebagai kode. Pada tahap ini praktis selalu
// `invoice`, tetapi kodenya ditampilkan lewat peta supaya baris lama (mis. hasil
// jalur uang muka) tetap terbaca manusia alih-alih memunculkan istilah mentah.
const ALLOCATION_TYPE_LABEL: Record<string, string> = {
  invoice: "Pembayaran tagihan",
  advance: "Uang muka",
  retention: "Retensi",
};

export function APInvoicePaymentHistory({ rows, invoiceStatus, loadError }: Props) {
  return (
    <Card padding="none">
      <div className="border-b border-border p-4">
        <p className="text-sm font-semibold text-text-primary">Riwayat Pembayaran</p>
        <p className="mt-0.5 text-xs text-text-secondary">
          Setiap baris adalah alokasi uang keluar ke tagihan ini, dengan bukti kas
          keluarnya. Sisa tagihan lahir dari daftar ini — bukan dari kolom yang ditulis
          ulang saat pembayaran masuk.
        </p>
      </div>

      {loadError ? (
        <p className="p-4 text-sm text-danger">Riwayat pembayaran gagal dimuat: {loadError}</p>
      ) : rows.length === 0 ? (
        <p className="p-4 text-sm text-text-secondary">
          {invoiceStatus === "draft"
            ? "Tagihan masih draft — kewajibannya belum lahir di buku, jadi belum ada yang bisa dibayar. Posting tagihannya lebih dulu."
            : invoiceStatus === "reversed"
              ? "Tagihan sudah dibalik dan kewajibannya tidak lagi berdiri, jadi tidak ada pembayaran yang tercatat untuknya."
              : "Belum ada pembayaran untuk tagihan ini. Seluruh nilainya masih menjadi hutang usaha yang berdiri."}
        </p>
      ) : (
        <>
          <Table>
            <TableHead>
              <tr>
                <Th>Nomor BKK</Th>
                <Th>Tanggal</Th>
                <Th>Sumber Dana</Th>
                <Th>Jenis</Th>
                <Th right>Nominal</Th>
                <Th>Keadaan</Th>
              </tr>
            </TableHead>
            <TableBody>
              {rows.map((r) => (
                <TableRow key={r.allocation_id} subtle={!!r.reversed_at}>
                  <Td>
                    {r.payment_id ? (
                      <Link
                        href={`/accounting/pembayaran-vendor/${r.payment_id}`}
                        className="font-mono font-medium text-accent hover:underline"
                      >
                        {r.document_number || `#${r.payment_id}`}
                      </Link>
                    ) : (
                      <span className="font-mono text-text-tertiary">—</span>
                    )}
                  </Td>
                  <Td>
                    {r.payment_date ? (
                      <Tanggal value={r.payment_date} />
                    ) : (
                      <span className="text-text-tertiary">—</span>
                    )}
                  </Td>
                  <Td mono>{r.cash_account_code || "—"}</Td>
                  <Td>{ALLOCATION_TYPE_LABEL[r.allocation_type] ?? r.allocation_type}</Td>
                  <Td right mono>
                    <Rupiah value={r.amount} colorSign={false} />
                  </Td>
                  <Td>
                    {r.reversed_at ? (
                      <Badge variant="danger">Dibalik</Badge>
                    ) : (
                      <Badge variant="success">Berlaku</Badge>
                    )}
                  </Td>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          <p className="border-t border-border px-4 py-2.5 text-[11px] text-text-tertiary">
            Baris yang dibalik tidak lagi mengurangi sisa tagihan, tetapi tetap
            ditampilkan: BKK-nya pernah terbit dan uangnya pernah keluar.
          </p>
        </>
      )}
    </Card>
  );
}
