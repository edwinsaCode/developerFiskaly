import Link from "next/link";
import type { FixedAsset, FixedAssetCategory } from "@/lib/types/api";
import { Card, CardHeader, CardTitle } from "@/components/ui/Card";

// Daftar Register Aset Tetap. accumulated_depreciation dan book_value diturunkan
// backend saat baca (SUM baris penyusutan) — bukan kolom tersimpan — sehingga
// selalu konsisten dengan jurnal penyusutan yang benar-benar terposting.

function rupiah(v?: string): string {
  if (!v) return "Rp 0";
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
  active: "bg-success-bg text-success border-success/30",
  disposed: "bg-border-subtle text-text-tertiary border-border",
};

const STATUS_LABEL: Record<string, string> = {
  active: "Aktif",
  disposed: "Dilepas",
};

export function FixedAssetList({ assets, categories }: { assets: FixedAsset[]; categories: FixedAssetCategory[] }) {
  function categoryName(id: number): string {
    return categories.find((c) => c.id === id)?.name ?? `Kategori #${id}`;
  }

  return (
    <Card padding="none">
      <CardHeader className="px-5 pt-5">
        <CardTitle>Register Aset Tetap</CardTitle>
        <p className="text-xs text-text-secondary mt-0.5">
          Biaya perolehan, akumulasi penyusutan, dan nilai buku setiap aset —
          dihitung dari jurnal yang benar-benar terposting.
        </p>
      </CardHeader>

      {assets.length === 0 ? (
        <div className="px-5 pb-6 pt-2">
          <p className="text-sm text-text-secondary">
            Belum ada aset tetap yang dicatat.
          </p>
          <p className="text-xs text-text-tertiary mt-1 max-w-prose">
            Aset tetap — kendaraan, peralatan kantor, dan sejenisnya yang dipakai
            lebih dari satu tahun — dicatat lewat halaman Transaksi Pengeluaran
            dengan Jenis Pembelian "Aset Tetap". Setiap perolehan langsung terposting
            dan muncul di sini beserta jadwal penyusutannya.
          </p>
        </div>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border text-left text-[11px] uppercase tracking-wide text-text-tertiary">
                <th className="px-5 py-2 font-semibold">Aset</th>
                <th className="px-3 py-2 font-semibold">Perolehan</th>
                <th className="px-3 py-2 font-semibold text-right">Biaya Perolehan</th>
                <th className="px-3 py-2 font-semibold text-right">Akum. Penyusutan</th>
                <th className="px-3 py-2 font-semibold text-right">Nilai Buku</th>
                <th className="px-5 py-2 font-semibold text-right">Status</th>
              </tr>
            </thead>
            <tbody>
              {assets.map((a) => (
                <tr key={a.id} className="border-b border-border last:border-0 align-top">
                  <td className="px-5 py-3">
                    <p className="text-text-primary font-medium">{a.asset_name}</p>
                    <p className="text-[11px] text-text-tertiary mt-0.5 font-mono">
                      {a.asset_code} · {categoryName(a.category_id)}
                    </p>
                  </td>

                  <td className="px-3 py-3">
                    <p className="text-text-secondary whitespace-nowrap">{tanggal(a.acquisition_date)}</p>
                    <Link
                      href={`/accounting/jurnal/${a.acquisition_journal_id}`}
                      className="text-[11px] text-accent hover:underline"
                    >
                      JE-{a.acquisition_journal_id}
                    </Link>
                  </td>

                  <td className="px-3 py-3 text-right tabular-nums font-medium text-text-primary whitespace-nowrap">
                    {rupiah(a.acquisition_cost)}
                  </td>

                  <td className="px-3 py-3 text-right tabular-nums text-text-secondary whitespace-nowrap">
                    {rupiah(a.accumulated_depreciation)}
                  </td>

                  <td className="px-3 py-3 text-right tabular-nums font-medium text-text-primary whitespace-nowrap">
                    {rupiah(a.book_value)}
                  </td>

                  <td className="px-5 py-3 text-right">
                    <span
                      className={`inline-block rounded-full border px-2 py-0.5 text-[11px] font-medium ${
                        STATUS_STYLE[a.status] ?? STATUS_STYLE.active
                      }`}
                    >
                      {STATUS_LABEL[a.status] ?? a.status}
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
