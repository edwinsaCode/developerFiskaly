import Link from "next/link";
import type { ExpenseListItem } from "@/lib/types/api";
import { Card, CardHeader, CardTitle } from "@/components/ui/Card";

// Daftar pengeluaran terakhir. Status TIDAK disimpan di database — ia diturunkan
// backend dari keadaan jurnal setiap kali dibaca (§8), jadi baris yang dibalik
// lewat modul lain pun tampil benar di sini.

function rupiah(v: string): string {
  const n = parseInt(v, 10);
  return Number.isNaN(n) ? v : `Rp ${n.toLocaleString("id-ID")}`;
}

function tanggal(v: string): string {
  const d = new Date(v);
  return Number.isNaN(d.getTime())
    ? v
    : d.toLocaleDateString("id-ID", { day: "2-digit", month: "short", year: "numeric" });
}

const STATUS_STYLE: Record<string, string> = {
  posted: "bg-success-bg text-success border-success/30",
  draft: "bg-warning-bg text-warning border-warning/30",
  reversed: "bg-border-subtle text-text-tertiary border-border",
};

const STATUS_LABEL: Record<string, string> = {
  posted: "Terposting",
  draft: "Draft",
  reversed: "Dibalik",
};

export function ExpenseList({ items }: { items: ExpenseListItem[] }) {
  return (
    <Card padding="none">
      <CardHeader className="px-5 pt-5">
        <CardTitle>Pengeluaran Terakhir</CardTitle>
        <p className="text-xs text-text-secondary mt-0.5">
          Operasional dan proyek dalam satu daftar — sesuai urutan pengeluaran kas.
        </p>
      </CardHeader>

      {items.length === 0 ? (
        <div className="px-5 pb-6 pt-2">
          <p className="text-sm text-text-secondary">
            Belum ada pengeluaran yang dicatat.
          </p>
          <p className="text-xs text-text-tertiary mt-1 max-w-prose">
            Semua uang yang keluar dari kas atau bank perusahaan dicatat di sini —
            gaji, sewa, listrik, sampai pembayaran vendor proyek. Setiap pencatatan
            menerbitkan bukti kas keluar (BKK) dan memposting jurnalnya sekaligus.
            Gunakan formulir di atas untuk mencatat yang pertama.
          </p>
        </div>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border text-left text-[11px] uppercase tracking-wide text-text-tertiary">
                <th className="px-5 py-2 font-semibold">Tanggal</th>
                <th className="px-3 py-2 font-semibold">Uraian</th>
                <th className="px-3 py-2 font-semibold">Pembebanan</th>
                <th className="px-3 py-2 font-semibold">Bukti</th>
                <th className="px-3 py-2 font-semibold text-right">Nominal</th>
                <th className="px-5 py-2 font-semibold text-right">Status</th>
              </tr>
            </thead>
            <tbody>
              {items.map((it) => (
                <tr key={it.id} className="border-b border-border last:border-0 align-top">
                  <td className="px-5 py-3 whitespace-nowrap text-text-secondary">
                    {tanggal(it.date)}
                  </td>

                  <td className="px-3 py-3">
                    <p className="text-text-primary font-medium">{it.description}</p>
                    <p className="text-[11px] text-text-tertiary mt-0.5">
                      {it.vendor}
                      {it.expense_type_name ? ` · ${it.expense_type_name}` : ""}
                      {it.bank_account_name ? ` · dari ${it.bank_account_name}` : ""}
                    </p>
                  </td>

                  <td className="px-3 py-3">
                    {it.project_name ? (
                      <>
                        <p className="text-text-primary">{it.project_name}</p>
                        <p className="text-[11px] text-text-tertiary mt-0.5">
                          {it.unit_code ? `Unit ${it.unit_code} · ` : ""}
                          {/* BD-2 dibuat terlihat: tag proyek ≠ realisasi RAB. */}
                          {it.is_rab_realization ? "Realisasi RAB" : "Tanpa tautan RAB"}
                        </p>
                      </>
                    ) : (
                      <span className="text-text-tertiary">Kantor umum</span>
                    )}
                  </td>

                  <td className="px-3 py-3">
                    <p className="font-mono text-xs text-text-primary">
                      {it.document_number || "—"}
                    </p>
                    <Link
                      href={`/accounting/jurnal/${it.journal_entry_id}`}
                      className="text-[11px] text-accent hover:underline"
                    >
                      JE-{it.journal_entry_id}
                    </Link>
                  </td>

                  <td className="px-3 py-3 text-right tabular-nums font-medium text-text-primary whitespace-nowrap">
                    {rupiah(it.amount)}
                  </td>

                  <td className="px-5 py-3 text-right">
                    <span
                      className={`inline-block rounded-full border px-2 py-0.5 text-[11px] font-medium ${
                        STATUS_STYLE[it.status] ?? STATUS_STYLE.draft
                      }`}
                    >
                      {STATUS_LABEL[it.status] ?? it.status}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Card>
  );
}
