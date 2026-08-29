import { cookies } from "next/headers";
import Link from "next/link";
import { fetchProjects, fetchUnitsByProject } from "@/lib/api/projects";
import { Card } from "@/components/ui/Card";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";
import { UnitDirectory, type UnitWithProject } from "@/components/penjualan/UnitDirectory";

const COOKIE_NAME = "esa_session";

async function getToken() {
  const store = await cookies();
  return store.get(COOKIE_NAME)?.value ?? "";
}

// Ringkasan jumlah per status dihitung di server dari SELURUH unit — sengaja
// tidak ikut menyusut saat pemakai mengetik di kotak cari. Chip di header
// menjawab "berapa unit yang saya punya", bukan "berapa yang sedang tampil";
// angka yang ikut bergerak saat mencari membuat orang mengira stoknya berubah.
const COUNT_CHIPS: { key: string; label: string; statuses: string[]; tone: ChipTone }[] = [
  { key: "available", label: "Tersedia",      statuses: ["available"],                 tone: "accent"  },
  { key: "booked",    label: "Dibooking",     statuses: ["booked"],                    tone: "accent"  },
  { key: "process",   label: "Dipesan/PPJB",  statuses: ["reserved", "ppjb"],          tone: "warning" },
  { key: "sold",      label: "Terjual",       statuses: ["sold", "occupied"],          tone: "success" },
  { key: "nonsale",   label: "Tidak Dijual",  statuses: ["hold", "blocked", "maintenance"], tone: "neutral" },
];

export default async function PenjualanPage() {
  const token = await getToken();

  let allUnits: UnitWithProject[] = [];
  let fetchError = false;

  try {
    const projects = await fetchProjects(token);
    const unitResults = await Promise.allSettled(
      projects.map((p) =>
        fetchUnitsByProject(token, p.id).then((units) =>
          units.map((u) => ({ ...u, project_name: p.name })),
        ),
      ),
    );
    allUnits = unitResults
      .filter((r): r is PromiseFulfilledResult<UnitWithProject[]> => r.status === "fulfilled")
      .flatMap((r) => r.value);
  } catch {
    fetchError = true;
  }

  if (fetchError) {
    return (
      <div className="space-y-6">
        <PageHeading />
        <Card>
          <ErrorState
            title="Gagal Memuat Data Unit"
            description="Tidak dapat mengambil data dari server. Pastikan backend berjalan dan coba lagi."
          />
        </Card>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Header: di layar sempit judul & ringkasan menumpuk (tidak lagi saling
          menghimpit), di layar lebar berjajar dengan ringkasan rata kanan. */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
        <PageHeading />
        <div className="flex flex-wrap items-center gap-2">
          {COUNT_CHIPS.map((c) => {
            const value = allUnits.filter((u) => c.statuses.includes(u.status)).length;
            if (c.key === "nonsale" && value === 0) return null;
            return <CountChip key={c.key} label={c.label} value={value} tone={c.tone} />;
          })}
        </div>
      </div>

      {allUnits.length === 0 ? (
        <Card>
          <EmptyState
            icon={
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
                <path d="M4 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2V6zm10 0a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2V6zM4 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2v-2zm10 0a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2v-2z" />
              </svg>
            }
            title="Belum ada unit"
            description="Tambahkan proyek dan unit terlebih dahulu untuk mulai mengelola penjualan."
            action={
              <Link
                href="/proyek"
                className="text-sm px-4 py-2 rounded bg-accent text-white font-medium
                  hover:bg-accent-hover transition-colors"
              >
                Ke Daftar Proyek
              </Link>
            }
          />
        </Card>
      ) : (
        <UnitDirectory units={allUnits} />
      )}
    </div>
  );
}

function PageHeading() {
  return (
    <div>
      <h1 className="display-lg text-2xl text-text-primary">Unit &amp; Penjualan</h1>
      <p className="text-sm text-text-secondary mt-1">
        Manajemen kontrak, pembayaran, dan serah terima unit
      </p>
    </div>
  );
}

type ChipTone = "accent" | "warning" | "success" | "neutral";

// Ringkasan jumlah unit per status — chip, bukan teks lepas, agar sejajar
// sebagai satu kelompok dan tidak "mengambang" di kanan header.
function CountChip({
  label, value, tone,
}: {
  label: string;
  value: number;
  tone: ChipTone;
}) {
  const toneClass = {
    accent:  "text-accent",
    warning: "text-warning",
    success: "text-success",
    neutral: "text-text-secondary",
  }[tone];
  return (
    <span className="inline-flex items-baseline gap-1.5 rounded-lg border border-border
      bg-surface px-3 py-1.5 shadow-sm">
      <span className={`text-sm font-bold tabular ${toneClass}`}>{value}</span>
      <span className="text-xs text-text-secondary">{label}</span>
    </span>
  );
}
