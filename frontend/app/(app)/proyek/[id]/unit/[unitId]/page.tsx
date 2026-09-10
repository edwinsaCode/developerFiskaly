import { Fragment } from "react";
import { notFound } from "next/navigation";
import Link from "next/link";
import { fetchUnit, fetchProject } from "@/lib/api/projects";
import { fetchSaleRecord, fetchContractByUnit, fetchSchedulesByContract, fetchContractAllocations, fetchCustomerStatement } from "@/lib/api/sale";
import { fetchAllocationCompute } from "@/lib/api/allocation";
import { ApiError } from "@/lib/api/client";
import { AllocationResult, SaleRecord, SaleContract, PaymentSchedule, ScheduleStatus, ScheduleType, AllocationView, CustomerStatement } from "@/lib/types/api";
import { Card, CardHeader, CardTitle } from "@/components/ui/Card";
import { StatusBadge, Badge, type BadgeVariant } from "@/components/ui/Badge";
import { Table, TableBody, TableRow, Th, Td, TableHead } from "@/components/ui/Table";
import { EmptyState } from "@/components/ui/EmptyState";
import { Rupiah } from "@/components/format/Rupiah";
import { Tanggal } from "@/components/format/Tanggal";
import { UnitCostBreakdown } from "@/components/proyek/CostBreakdownTable";
import { LandAreaEditButton } from "@/components/proyek/LandAreaEditButton";
import { RecordPaymentButton } from "@/components/billing/RecordPaymentButton";
import { PrintReceiptButton } from "@/components/billing/PrintReceiptButton";
import { getTokenAndRole } from "@/lib/auth";
import { unitLabel, buyerRoleLabel } from "@/lib/unit-label";

interface PageProps {
  params: Promise<{ id: string; unitId: string }>;
}

export default async function UnitDetailPage({ params }: PageProps) {
  const { id, unitId } = await params;
  const projectId = parseInt(id, 10);
  const unitIdNum  = parseInt(unitId, 10);
  if (isNaN(projectId) || isNaN(unitIdNum)) notFound();

  const { token, role } = await getTokenAndRole();
  // W-12 — biaya & HPP unit adalah data akuntansi: backend membalas 403 untuk
  // marketing. Jangan memintanya sama sekali, supaya yang muncul bukan kartu
  // merah "gagal memuat" atas data yang memang bukan haknya.
  const isMarketing = role === "marketing";

  const [unitRes, projectRes, allocRes, saleRes] = await Promise.allSettled([
    fetchUnit(token, unitIdNum),
    fetchProject(token, projectId),
    isMarketing
      ? Promise.resolve({ data: [] as AllocationResult[] })
      : fetchAllocationCompute(token, projectId),
    // Sale record memuat rincian HPP — 403 untuk marketing.
    isMarketing ? Promise.resolve(null) : fetchSaleRecord(token, unitIdNum),
  ]);

  if (unitRes.status === "rejected") {
    const err = unitRes.reason;
    if (err instanceof ApiError && err.status === 404) notFound();
    throw err;
  }

  const unit    = unitRes.value;
  const project = projectRes.status === "fulfilled" ? projectRes.value : null;

  // Cari alokasi milik unit ini saja
  let unitAlloc: AllocationResult | null = null;
  if (allocRes.status === "fulfilled") {
    unitAlloc = allocRes.value.data.find(r => r.unit_id === unitIdNum) ?? null;
  }

  let saleRecord: SaleRecord | null = null;
  if (saleRes.status === "fulfilled") {
    saleRecord = saleRes.value;
  }

  // Kontrak aktif unit ini (404 = belum ada kontrak → null, bukan error).
  let contract: SaleContract | null = null;
  try {
    contract = await fetchContractByUnit(token, unitIdNum);
  } catch (err) {
    if (!(err instanceof ApiError && err.status === 404)) {
      // Kegagalan non-404 jangan menjatuhkan seluruh halaman — tandai untuk UI.
      contract = null;
    }
  }

  // Jadwal cicilan hanya relevan bila ada kontrak.
  let schedules: PaymentSchedule[] = [];
  let allocations: AllocationView[] = [];
  if (contract) {
    try {
      schedules = await fetchSchedulesByContract(token, contract.id);
    } catch {
      schedules = [];
    }
    // Breakdown alokasi (FE-2 · P4) — best-effort; kegagalan tidak menjatuhkan halaman.
    allocations = await fetchContractAllocations(token, contract.id).catch(() => []);
  }

  // P1 Kelebihan Tanah: statement (mesin AR yang sama dengan halaman Piutang)
  // adalah SATU-SATUNYA sumber outstanding/status untuk kartu "Pembayaran" di
  // bawah — tidak dihitung ulang dari asumsi FE agar tidak pernah menyimpang
  // dari angka di Piutang.
  let statement: CustomerStatement | null = null;
  if (contract) {
    statement = await fetchCustomerStatement(token, contract.id).catch(() => null);
  }

  return (
    <div className="space-y-6">
      {/* Breadcrumb */}
      <nav className="flex items-center gap-1.5 text-xs text-text-tertiary">
        <Link href="/proyek" className="hover:text-text-secondary">Proyek</Link>
        <span>/</span>
        <Link href={`/proyek/${projectId}`} className="hover:text-text-secondary">
          {project?.name ?? `Proyek #${projectId}`}
        </Link>
        <span>/</span>
        <span className="text-text-primary">{unitLabel(unit.code, unit.buyer_name)}</span>
      </nav>

      {/* ── Header unit ────────────────────────────────────────────────────────
          Atribut unit dipecah jadi kolom-kolom yang mengisi lebar penuh; dulu
          berupa dua kolom sempit di kiri dengan sisa layar kosong. */}
      <div className="space-y-4">
        {/* Nama pemegang unit ikut di judul, bukan hanya di baris atribut:
            pertanyaan pertama saat membuka halaman unit adalah "ini punya
            siapa", dan jawabannya harus terlihat tanpa memindai isi kartu. */}
        <div className="flex items-center gap-3 flex-wrap">
          <h1 className="display-lg text-2xl text-text-primary">
            {unit.code}
            {unit.buyer_name && (
              <span className="text-text-secondary font-normal"> · {unit.buyer_name}</span>
            )}
          </h1>
          <StatusBadge status={unit.status} />
        </div>
        <Card padding="sm">
          <dl className="grid grid-cols-2 gap-4 text-sm sm:grid-cols-4">
            <InfoCell label="Tipe"      value={unit.unit_type} />
            <InfoCell label="Luas Jual" value={`${unit.saleable_area} m²`} />
            <InfoCell
              label="Luas Tanah"
              value={
                <span className="inline-flex items-center gap-1.5">
                  {unit.land_area} m²
                  {!isMarketing && (
                    <LandAreaEditButton token={token} unitId={unit.id} currentLandArea={unit.land_area} />
                  )}
                </span>
              }
            />
            <InfoCell
              label="Harga Jual"
              value={<Rupiah value={unit.list_price} colorSign={false} />}
            />
            {unit.buyer_name && (
              <InfoCell label={buyerRoleLabel(unit.buyer_source)} value={unit.buyer_name} />
            )}
            {unit.sale_date && (
              <InfoCell label="Tanggal Jual" value={<Tanggal value={unit.sale_date} />} />
            )}
          </dl>
        </Card>
      </div>

      {/* ── Biaya terakumulasi unit ─────────────────────────────────────────────
          Marketing tidak melihat blok ini sama sekali — bukan disembunyikan
          setelah gagal dimuat, melainkan tidak pernah diminta. */}
      {isMarketing ? null : unitAlloc ? (
        <UnitCostBreakdown result={unitAlloc} />
      ) : allocRes.status === "rejected" ? (
        <Card>
          <p className="text-sm text-danger">Gagal memuat data alokasi biaya.</p>
        </Card>
      ) : (
        <Card>
          <p className="text-sm text-text-secondary">
            Belum ada data alokasi biaya untuk unit ini.
          </p>
        </Card>
      )}

      {/* ── Data penjualan / Akad ─────────────────────────────────────────────── */}
      {saleRecord ? (
        <SaleRecordSection record={saleRecord} actualTotal={unitAlloc?.total.total} />
      ) : unit.status === "sold" || unit.status === "occupied" ? (
        <Card>
          <p className={isMarketing ? "text-sm text-text-secondary" : "text-sm text-danger"}>
            {isMarketing
              ? "Unit sudah Akad. Rincian HPP hanya untuk Accounting."
              : "Gagal memuat data Akad."}
          </p>
        </Card>
      ) : null}

      {/* ── Pembayaran (P1 Kelebihan Tanah) ──────────────────────────────────────
          Dulu tidak ada tempat di Unit Detail untuk melihat/menyelesaikan
          outstanding Kelebihan Tanah — user harus pindah ke halaman Piutang.
          Selalu ditampilkan begitu ada kontrak, terlepas dari saleRecord (lihat
          catatan di bawah). */}
      {contract && (
        <PaymentSummarySection token={token} contract={contract} statement={statement} />
      )}

      {/* ── Kontrak & jadwal cicilan ────────────────────────────────────────────
          Sebelumnya section ini disembunyikan begitu unit sudah Akad
          (`!saleRecord`), sehingga jadwal cicilan/piutang pasca-Akad — termasuk
          cicilan Kelebihan Tanah — tidak pernah terlihat di sini. Akad & jadwal
          adalah dua hal berbeda: keduanya harus tetap tampil bersisian. */}
      <ContractScheduleSection
        contract={contract}
        schedules={schedules}
        allocations={allocations}
        unitId={unitIdNum}
        unitStatus={unit.status}
        buyerRef={unit.buyer_ref}
      />
    </div>
  );
}

// ── Sub-komponen ──────────────────────────────────────────────────────────────

// InfoRow — pasangan dt/dd sebaris (dipakai di dalam dl 2 kolom).
function InfoRow({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <>
      <dt className="text-text-secondary">{label}</dt>
      <dd className="text-text-primary font-medium">{value}</dd>
    </>
  );
}

// InfoCell — label di atas nilai; bentuk kartu atribut yang mengisi lebar.
function InfoCell({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="text-xs text-text-secondary">{label}</dt>
      <dd className="mt-0.5 font-semibold text-text-primary truncate">{value}</dd>
    </div>
  );
}

function SaleRecordSection({
  record,
  actualTotal,
}: {
  record: SaleRecord;
  /** Total biaya AKTUAL unit (realisasi). Dipakai hanya untuk pembanding. */
  actualTotal?: string;
}) {
  return (
    <Card padding="none">
      <CardHeader className="px-5 pt-5">
        <div className="flex items-center gap-3">
          <CardTitle>Akad & HPP</CardTitle>
          <Badge variant="success">Terjual</Badge>
        </div>
        <p className="text-xs text-text-secondary mt-0.5">
          Akad: <Tanggal value={record.recognition_date} format="long" />
          {record.handed_over_at && (
            <>
              {" · "}Serah terima fisik: <Tanggal value={record.handed_over_at} format="long" />
            </>
          )}
        </p>
      </CardHeader>

      <Table>
        <TableHead>
          <TableRow>
            <Th>Keterangan</Th>
            <Th right>Jumlah</Th>
          </TableRow>
        </TableHead>
        <TableBody>
          <TableRow>
            <Td>Harga Jual (DPP)</Td>
            <Td right><Rupiah value={record.sale_price} /></Td>
          </TableRow>
          {record.is_vat && (
            <TableRow subtle>
              <Td>
                PPN ({record.vat_rate})
                <Badge variant="accent" className="ml-2">PKP</Badge>
              </Td>
              <Td right className="text-text-secondary text-xs">terpisah</Td>
            </TableRow>
          )}
          <TableRow>
            <Td>DP / Uang Muka terkumpul saat Akad</Td>
            <Td right><Rupiah value={record.total_advance_at_bast} /></Td>
          </TableRow>
          <TableRow subtle>
            <Td>Pembeli</Td>
            <Td>{record.buyer_ref}</Td>
          </TableRow>

          {/* HPP breakdown — dari backend, bukan dihitung FE */}
          <TableRow>
            <Td><span className="font-semibold">HPP — Tanah</span></Td>
            <Td right><Rupiah value={record.hpp_land} /></Td>
          </TableRow>
          <TableRow>
            <Td><span className="font-semibold">HPP — Konstruksi</span></Td>
            <Td right><Rupiah value={record.hpp_hard} /></Td>
          </TableRow>
          {/* RULE KLIEN FREEZE (2026-09-04): HPP baru hanya Tanah + Konstruksi.
              Kedua baris di bawah legacy — selalu Rp 0 untuk penjualan setelah
              tanggal freeze, tapi tetap ditampilkan agar HPP historis (sebelum
              reklasifikasi) tetap terbaca apa adanya. */}
          <TableRow>
            <Td><span className="font-semibold">HPP — Biaya Lunak (legacy)</span></Td>
            <Td right><Rupiah value={record.hpp_soft} /></Td>
          </TableRow>
          <TableRow>
            <Td><span className="font-semibold">HPP — Pendanaan/Operasional (legacy)</span></Td>
            <Td right><Rupiah value={record.hpp_financing} /></Td>
          </TableRow>
          <TableRow>
            <Td><span className="font-semibold">HPP Diakui (total)</span></Td>
            <Td right>
              <span className="font-semibold"><Rupiah value={hppTotal(record)} /></span>
            </Td>
          </TableRow>
          {actualTotal !== undefined && (
            <TableRow subtle>
              <Td>Biaya aktual terealisasi sampai kini</Td>
              <Td right className="text-text-secondary"><Rupiah value={actualTotal} /></Td>
            </TableRow>
          )}
        </TableBody>
      </Table>

      {/* Kenapa dua angka ini boleh berbeda — dijelaskan di tempat orang
          melihatnya, bukan di dokumen yang tak pernah dibuka. Tanpa kalimat ini
          pembaca menyimpulkan sistem salah hitung, padahal keduanya benar. */}
      <p className="px-5 pb-5 pt-1 text-xs text-text-secondary">
        HPP yang diakui dihitung dari <strong>RAB</strong> (anggaran), sesuai PSAK 44 — bukan dari
        belanja yang sudah terjadi. Karena itu angkanya wajar berbeda dari biaya aktual di atas,
        terutama bila konstruksi masih berjalan. Selisihnya dirapikan lewat{" "}
        <strong>true-up</strong> saat proyek selesai.
      </p>
    </Card>
  );
}

/** Total HPP yang diakui = penjumlahan empat kategori pada bukti BAST. */
function hppTotal(record: SaleRecord): string {
  const SCALE = BigInt(10000); // DECIMAL(20,4) — hindari float pada nilai uang
  const toUnits = (v: string) => {
    const [whole, frac = ""] = String(v ?? "0").split(".");
    const fracPadded = (frac + "0000").slice(0, 4);
    const neg = whole.trim().startsWith("-");
    const wholeAbs = whole.replace("-", "").trim() || "0";
    const n = BigInt(wholeAbs) * SCALE + BigInt(fracPadded);
    return neg ? -n : n;
  };
  const sum =
    toUnits(record.hpp_land) +
    toUnits(record.hpp_hard) +
    toUnits(record.hpp_soft) +
    toUnits(record.hpp_financing);
  const neg = sum < BigInt(0);
  const abs = neg ? -sum : sum;
  const whole = abs / SCALE;
  const frac = (abs % SCALE).toString().padStart(4, "0");
  return `${neg ? "-" : ""}${whole}.${frac}`;
}

// ── Pembayaran (P1 Kelebihan Tanah) ─────────────────────────────────────────────
// Rumah vs Kelebihan Tanah dikelompokkan dari statement.schedules per `type` —
// PERSIS sumber yang sama dipakai backend untuk StatementExposure (house vs
// land outstanding di halaman Piutang), jadi kedua tempat tidak pernah
// menyimpang (invariant task #7).

type GroupStatus = "BELUM_LUNAS" | "SEBAGIAN" | "LUNAS";

const GROUP_STATUS_META: Record<GroupStatus, { label: string; variant: BadgeVariant }> = {
  BELUM_LUNAS: { label: "Belum Lunas", variant: "danger" },
  SEBAGIAN:    { label: "Sebagian",    variant: "warning" },
  LUNAS:       { label: "Lunas",       variant: "success" },
};

function PaymentSummarySection({
  token,
  contract,
  statement,
}: {
  token: string;
  contract: SaleContract;
  statement: CustomerStatement | null;
}) {
  if (!statement) {
    return (
      <Card>
        <p className="text-sm text-danger">Gagal memuat ringkasan pembayaran.</p>
      </Card>
    );
  }

  const houseLines = statement.schedules.filter((l) => l.type !== "land");
  const landLines = statement.schedules.filter((l) => l.type === "land");
  const hasLand = landLines.length > 0;

  const houseTotal = sumMoneyExact(houseLines.map((l) => l.amount));
  const housePaid = sumMoneyExact(houseLines.map((l) => l.paid));
  const houseOutstanding = sumMoneyExact(houseLines.map((l) => l.outstanding));
  const houseStatus = groupStatus(houseOutstanding, housePaid);

  const landTotal = sumMoneyExact(landLines.map((l) => l.amount));
  const landPaid = sumMoneyExact(landLines.map((l) => l.paid));
  const landOutstanding = sumMoneyExact(landLines.map((l) => l.outstanding));
  const landStatus = groupStatus(landOutstanding, landPaid);
  // Kelebihan Tanah bundled = satu schedule per kontrak (sentinel installment
  // ke-9999); ambil detail produk (m², harga/m²) dari baris itu.
  const landLine = landLines[0];
  const landReceiptTerminId = [...landLines].reverse().find((l) => l.termin_payment_id)?.termin_payment_id;
  const landNotes = landLine
    ? `Kelebihan Tanah${landLine.land_quantity_m2 ? ` ${landLine.land_quantity_m2} m²` : ""}${landLine.land_unit_price ? ` x Rp${landLine.land_unit_price}/m²` : ""}`
    : undefined;

  return (
    <Card padding="none">
      <CardHeader className="px-5 pt-5">
        <CardTitle>Pembayaran</CardTitle>
      </CardHeader>
      <div className="px-5 pb-5 space-y-5">
        <PaymentGroupBlock title="Rumah" total={houseTotal} paid={housePaid} outstanding={houseOutstanding} status={houseStatus} />

        {hasLand && (
          <div className="pt-4 border-t border-border space-y-3">
            <PaymentGroupBlock
              title="Kelebihan Tanah"
              subtitle={landLine?.land_quantity_m2 ? `${landLine.land_quantity_m2} m²` : undefined}
              unitPrice={landLine?.land_unit_price}
              total={landTotal}
              paid={landPaid}
              outstanding={landOutstanding}
              status={landStatus}
            />
            <div>
              {landStatus !== "LUNAS" ? (
                <RecordPaymentButton
                  token={token}
                  contractId={contract.id}
                  outstanding={landOutstanding}
                  scheduleId={landLine?.schedule_id}
                  buyerName={contract.buyer_name}
                  label="Bayar Kelebihan Tanah"
                  defaultNotes={landNotes}
                />
              ) : landReceiptTerminId ? (
                <PrintReceiptButton
                  token={token}
                  terminId={landReceiptTerminId}
                  variant="button"
                  label="Cetak Kwitansi"
                  notes={`${landNotes ?? "Kelebihan Tanah"} — ${contract.buyer_name}`}
                />
              ) : null}
            </div>
          </div>
        )}
      </div>
    </Card>
  );
}

function PaymentGroupBlock({
  title, subtitle, unitPrice, total, paid, outstanding, status,
}: {
  title: string;
  subtitle?: string;
  unitPrice?: string;
  total: string;
  paid: string;
  outstanding: string;
  status: GroupStatus;
}) {
  const meta = GROUP_STATUS_META[status];
  return (
    <div>
      <div className="flex items-baseline gap-2 mb-2">
        <h3 className="text-sm font-semibold text-text-primary">{title}</h3>
        {subtitle && <span className="text-xs text-text-secondary">{subtitle}</span>}
      </div>
      {unitPrice && (
        <p className="text-xs text-text-secondary mb-2">
          Harga/m² <Rupiah value={unitPrice} colorSign={false} />
        </p>
      )}
      <dl className="grid grid-cols-2 gap-x-6 gap-y-2 text-sm sm:grid-cols-4">
        <InfoRow label="Total"    value={<Rupiah value={total} colorSign={false} />} />
        <InfoRow label="Terbayar" value={<Rupiah value={paid} colorSign={false} />} />
        <InfoRow label="Sisa"     value={<Rupiah value={outstanding} colorSign={false} />} />
        <InfoRow label="Status"   value={<Badge variant={meta.variant}>{meta.label}</Badge>} />
      </dl>
    </div>
  );
}

// toMinorUnitsExact/sumMoneyExact/groupStatus — sama pola dengan remainingExact
// di bawah: DECIMAL(20,4) via BigInt, TIDAK PERNAH float (invariant #2).
function toMinorUnitsExact(raw: string): bigint {
  const SCALE = BigInt(10000);
  const cleaned = String(raw ?? "0").replace(",", ".").trim();
  const neg = cleaned.startsWith("-");
  const [intPart, fracPart = ""] = cleaned.replace("-", "").split(".");
  const frac = (fracPart + "0000").slice(0, 4);
  const v = BigInt(intPart || "0") * SCALE + BigInt(frac || "0");
  return neg ? -v : v;
}

function sumMoneyExact(values: string[]): string {
  const SCALE = BigInt(10000);
  let sum = BigInt(0);
  for (const v of values) sum += toMinorUnitsExact(v);
  const neg = sum < BigInt(0);
  const abs = neg ? -sum : sum;
  const whole = abs / SCALE;
  const frac = (abs % SCALE).toString().padStart(4, "0");
  return `${neg ? "-" : ""}${whole}.${frac}`;
}

function groupStatus(outstanding: string, paid: string): GroupStatus {
  if (toMinorUnitsExact(outstanding) <= BigInt(0)) return "LUNAS";
  if (toMinorUnitsExact(paid) > BigInt(0)) return "SEBAGIAN";
  return "BELUM_LUNAS";
}

// ── Kontrak & jadwal cicilan ────────────────────────────────────────────────────

const SCHEDULE_STATUS: Record<ScheduleStatus, { label: string; variant: BadgeVariant }> = {
  scheduled: { label: "Dijadwalkan", variant: "neutral" },
  received:  { label: "Diterima",    variant: "success" },
  overdue:   { label: "Jatuh Tempo", variant: "danger" },
};

const SCHEDULE_TYPE: Record<ScheduleType, string> = {
  dp:          "DP / Uang Muka",
  installment: "Termin",
  final:       "Pelunasan",
  land:        "Kelebihan Tanah",
};

const PAYMENT_TYPE_LABEL: Record<string, string> = { kpr: "KPR", tunai: "Tunai" };

function ContractScheduleSection({
  contract,
  schedules,
  allocations,
  unitId,
  unitStatus,
  buyerRef,
}: {
  contract: SaleContract | null;
  schedules: PaymentSchedule[];
  allocations: AllocationView[];
  unitId: number;
  unitStatus: string;
  buyerRef?: string;
}) {
  // FE-2 · P4 — kelompokkan alokasi per cicilan untuk baris breakdown "Dibayar oleh".
  const allocBySchedule = new Map<number, AllocationView[]>();
  for (const a of allocations) {
    if (a.allocation_type !== "schedule" || a.payment_schedule_id == null) continue;
    const list = allocBySchedule.get(a.payment_schedule_id) ?? [];
    list.push(a);
    allocBySchedule.set(a.payment_schedule_id, list);
  }
  // Empty state — belum ada kontrak untuk unit ini.
  if (!contract) {
    return (
      <Card padding="none">
        <CardHeader className="px-5 pt-5">
          <CardTitle>Kontrak & Jadwal Cicilan</CardTitle>
        </CardHeader>
        <EmptyState
          icon={
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
              strokeLinecap="round" strokeLinejoin="round">
              <path d="M9 12h6m-6 4h6m2 4H7a2 2 0 01-2-2V6a2 2 0 012-2h7l5 5v11a2 2 0 01-2 2z" />
            </svg>
          }
          title="Belum ada kontrak penjualan"
          description={
            buyerRef
              ? `Unit dipesan oleh ${buyerRef}, namun kontrak penjualan belum dibuat. Buat kontrak agar nilai jual, jadwal cicilan, dan tagihan buyer terlacak.`
              : "Unit ini belum memiliki kontrak penjualan. Buat kontrak untuk mulai mencatat penjualan dan jadwal cicilan."
          }
          action={
            <Link
              href={`/penjualan/${unitId}`}
              className="inline-flex items-center gap-2 px-4 py-2 rounded-md bg-accent text-white text-sm font-medium hover:bg-accent/90 transition-colors"
            >
              Buka Penjualan Unit →
            </Link>
          }
        />
      </Card>
    );
  }

  const hasSchedules = schedules.length > 0;

  return (
    <Card padding="none">
      <CardHeader className="px-5 pt-5">
        <div className="flex items-center gap-3 flex-wrap">
          <CardTitle>Kontrak & Jadwal Cicilan</CardTitle>
          <Badge variant="accent">{PAYMENT_TYPE_LABEL[contract.payment_type] ?? contract.payment_type}</Badge>
          <StatusBadge status={unitStatus} />
        </div>
      </CardHeader>

      {/* Ringkasan kontrak */}
      {/* lg:grid-cols-4 = dua pasang label/nilai per baris — ringkasan kontrak
          mengisi lebar kartu alih-alih menumpuk di sisi kiri. */}
      <dl className="grid grid-cols-2 gap-x-6 gap-y-2 text-sm px-5 pb-4 lg:grid-cols-4">
        <InfoRow label="Pembeli"        value={contract.buyer_name} />
        <InfoRow label="No. Identitas"  value={contract.buyer_id} />
        <InfoRow label="Nomor Kontrak"  value={`KTR-${String(contract.id).padStart(6, "0")}`} />
        <InfoRow label="Tanggal Kontrak" value={<Tanggal value={contract.contract_date} />} />
        <InfoRow label="Nilai Kontrak"  value={<Rupiah value={contract.total_price} colorSign={false} />} />
        {contract.payment_type === "kpr" && contract.bank_kpr && (
          <InfoRow label="Bank KPR" value={contract.bank_kpr} />
        )}
      </dl>

      {/* Jadwal cicilan */}
      {!hasSchedules ? (
        <EmptyState
          className="border-t border-border"
          icon={
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
              strokeLinecap="round" strokeLinejoin="round">
              <path d="M8 7V3m8 4V3m-9 8h10M5 21h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z" />
            </svg>
          }
          title="Belum ada jadwal cicilan"
          description="Kontrak sudah dibuat tetapi jadwal pembayaran (DP, termin, pelunasan) belum disusun. Buat jadwal agar tagihan buyer dapat dilacak dan ditagih."
          action={
            <Link
              href={`/penjualan/${unitId}`}
              className="inline-flex items-center gap-2 px-4 py-2 rounded-md bg-accent text-white text-sm font-medium hover:bg-accent/90 transition-colors"
            >
              Susun Jadwal →
            </Link>
          }
        />
      ) : (
        <Table>
          <TableHead>
            <TableRow>
              <Th>#</Th>
              <Th>Jenis</Th>
              <Th>Jatuh Tempo</Th>
              <Th right>Nominal</Th>
              <Th right>Dibayar</Th>
              <Th right>Sisa</Th>
              <Th>Status</Th>
            </TableRow>
          </TableHead>
          <TableBody>
            {schedules.map((s) => {
              const meta = SCHEDULE_STATUS[s.status] ?? { label: s.status, variant: "default" as BadgeVariant };
              const allocs = allocBySchedule.get(s.id) ?? [];
              return (
                <Fragment key={s.id}>
                  <TableRow>
                    <Td>{s.installment_number}</Td>
                    <Td>{SCHEDULE_TYPE[s.type] ?? s.type}</Td>
                    <Td><Tanggal value={s.due_date} /></Td>
                    <Td right><Rupiah value={s.amount} colorSign={false} /></Td>
                    <Td right><Rupiah value={s.paid_amount} colorSign={false} /></Td>
                    <Td right><Rupiah value={remainingExact(s.amount, s.paid_amount)} colorSign={false} /></Td>
                    <Td><Badge variant={meta.variant}>{meta.label}</Badge></Td>
                  </TableRow>
                  {allocs.length > 0 && (
                    <TableRow>
                      <Td className="!py-1.5 text-text-secondary" colSpan={7}>
                        <span className="text-xs">Dibayar oleh: </span>
                        <span className="inline-flex flex-wrap gap-1.5 align-middle">
                          {allocs.map((a) => (
                            <span
                              key={a.id}
                              className="inline-flex items-center gap-1 rounded bg-border-subtle/50 px-1.5 py-0.5 text-xs tabular-nums"
                            >
                              <Rupiah value={a.amount} colorSign={false} />
                              <span className="text-text-tertiary">·</span>
                              <Tanggal value={a.termin_date} />
                              {a.receipt_number && (
                                <span className="text-text-tertiary font-mono">{a.receipt_number}</span>
                              )}
                            </span>
                          ))}
                        </span>
                      </Td>
                    </TableRow>
                  )}
                </Fragment>
              );
            })}
          </TableBody>
        </Table>
      )}
    </Card>
  );
}

// remainingExact — sisa cicilan = nominal − dibayar, dihitung EKSAK pada minor unit
// 4 desimal (DECIMAL(20,4)) memakai BigInt. TIDAK memakai float (invariant #2:
// "uang tidak pakai float"). Hanya untuk tampilan; otoritas angka tetap di backend.
function remainingExact(amount: string, paid: string): string {
  const SCALE = BigInt(10000); // 4 desimal (DECIMAL(20,4)); BigInt() — bukan literal `n` (target ES2017)
  const ZERO = BigInt(0);
  const toMinor = (raw: string): bigint => {
    const cleaned = String(raw ?? "0").replace(",", ".").trim();
    const neg = cleaned.startsWith("-");
    const [intPart, fracPart = ""] = cleaned.replace("-", "").split(".");
    const frac = (fracPart + "0000").slice(0, 4);
    const v = BigInt(intPart || "0") * SCALE + BigInt(frac || "0");
    return neg ? -v : v;
  };
  let diff = toMinor(amount) - toMinor(paid);
  if (diff < ZERO) diff = ZERO; // overpay tidak ditampilkan sebagai sisa negatif
  const intp = diff / SCALE;
  const frac = (diff % SCALE).toString().padStart(4, "0");
  return `${intp.toString()}.${frac}`;
}
