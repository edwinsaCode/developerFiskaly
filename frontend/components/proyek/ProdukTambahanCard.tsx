import Link from "next/link";
import { LandStock } from "@/lib/api/land";
import { Card } from "@/components/ui/Card";
import { Rupiah } from "@/components/format/Rupiah";

function availableM2(pool: LandStock): number {
  return Number(pool.total_quantity_m2) - Number(pool.reserved_quantity_m2) - Number(pool.sold_quantity_m2);
}

// P0 (kelebihan-tanah-booking-integration) — ringkasan read-only "Produk
// Tambahan" di tab Unit, di samping daftar Unit Property. Sumber data sama
// dengan yang dipakai BookingFormModal/ContractForm (fetchLandStock) — tidak
// ada query/engine kedua. Aksi admin (buat/ubah pool, reservasi, Akad
// standalone) tetap di tab "Kelebihan Tanah" (owner/accountant saja).
export function ProdukTambahanCard({ pool, projectId }: { pool: LandStock | null; projectId: number }) {
  if (!pool) return null;

  const available = availableM2(pool);

  return (
    <Card padding="none">
      <div className="px-5 py-3 border-b border-border flex items-center justify-between">
        <p className="text-xs font-semibold uppercase tracking-wide text-text-secondary">
          Produk Tambahan
        </p>
        <Link
          href={`/proyek/${projectId}?tab=tanah`}
          className="text-xs text-accent hover:underline"
        >
          Kelola →
        </Link>
      </div>
      <div className="px-5 py-4 flex items-center justify-between gap-4 flex-wrap">
        <div>
          <p className="text-sm font-medium text-text-primary">Kelebihan Tanah</p>
          <p className="text-xs text-text-tertiary mt-0.5">
            <Rupiah value={pool.unit_price} />/m² &middot; dapat ditambahkan sebagai produk tambahan saat Booking
          </p>
        </div>
        <div className="flex items-center gap-4 text-sm tabular">
          <div className="text-right">
            <p className="text-[10px] uppercase tracking-wide text-text-tertiary">Total</p>
            <p className="font-medium text-text-primary">{Number(pool.total_quantity_m2).toLocaleString("id-ID")} m²</p>
          </div>
          <div className="text-right">
            <p className="text-[10px] uppercase tracking-wide text-text-tertiary">Direservasi</p>
            <p className="font-medium text-text-primary">{Number(pool.reserved_quantity_m2).toLocaleString("id-ID")} m²</p>
          </div>
          <div className="text-right">
            <p className="text-[10px] uppercase tracking-wide text-text-tertiary">Terjual</p>
            <p className="font-medium text-text-primary">{Number(pool.sold_quantity_m2).toLocaleString("id-ID")} m²</p>
          </div>
          <div className="text-right">
            <p className="text-[10px] uppercase tracking-wide text-text-tertiary">Tersedia</p>
            <p className="font-semibold text-accent">{available.toLocaleString("id-ID")} m²</p>
          </div>
        </div>
      </div>
    </Card>
  );
}
