"use client";

import { useState, useEffect, useCallback } from "react";
import Link from "next/link";
import { Card, CardHeader, CardTitle } from "@/components/ui/Card";
import { Badge, StatusBadge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Rupiah } from "@/components/format/Rupiah";
import { ContractForm } from "./ContractForm";
import { FinancingMilestones } from "./FinancingMilestones";
import { TerminForm } from "./TerminForm";
import { AkadForm } from "./AkadForm";
import { HandoverForm } from "./HandoverForm";
import { ScheduleForm } from "./ScheduleForm";
import { SaleRecordCard } from "./SaleRecordCard";
import { RiwayatPenerimaanTable } from "./RiwayatPenerimaanTable";
import { PrintReceiptButton } from "@/components/billing/PrintReceiptButton";
import { RecordPaymentButton } from "@/components/billing/RecordPaymentButton";
import {
  fetchContractFinancialSummary, fetchSchedulesByContract, listTermins, fetchContractByUnit, setAdminMarketing,
  type ContractFinancialSummary,
} from "@/lib/api/sale";
import { fetchSalesPersons, type SalesPerson } from "@/lib/api/party";
import { ApiError } from "@/lib/api/client";
import { useToast } from "@/components/ui/Toast";
import { Modal } from "@/components/ui/Modal";
import { Input } from "@/components/ui/Input";
import { Tanggal } from "@/components/format/Tanggal";
import { BookingFormModal } from "@/components/booking/BookingFormModal";
import { BookingStatusBadge, FeeDispositionBadge, isBookingExpired } from "@/components/booking/bookingUi";
import { CancelRequestModal } from "@/components/cancellation/CancelRequestModal";
import { UnitTimelineCard } from "@/components/proyek/UnitTimelineCard";
import { fetchActiveBookingByUnit, type Booking } from "@/lib/api/booking";
import { transitionUnit } from "@/lib/api/projects";
import { useUserSafe } from "@/lib/context/UserContext";
import type { Unit, SaleRecord, SaleContract, PaymentSchedule } from "@/lib/types/api";
import { unitLabel } from "@/lib/unit-label";

interface Props {
  unit: Unit;
  saleRecord: SaleRecord | null;
  contractId?: number;
  token: string;
  /** Increment 7 — datang dari papan booking utk konversi (?booking=). */
  bookingParam?: number;
}

function fmtDate(d: string) {
  return new Date(d).toLocaleDateString("id-ID", {
    day: "2-digit", month: "short", year: "numeric",
  });
}

export function UnitSalePanel({ unit, saleRecord, contractId, token, bookingParam }: Props) {
  const { toast } = useToast();
  // W-12 — marketing menjual, accounting menerima uang. Panel ini memuat
  // keduanya, jadi seluruh sisi uang (penerimaan termin, Akad, pencairan KPR,
  // kwitansi, pembatalan/refund, invoice) disembunyikan untuk marketing.
  // Backend tetap membalas 403 bila tetap dipanggil — ini hanya agar layarnya
  // jujur, bukan pengamanan.
  const isMarketing = useUserSafe()?.role === "marketing";

  const [showContractForm,  setShowContractForm]  = useState(false);
  const [showTerminForm,    setShowTerminForm]     = useState(false);
  const [showAkadForm,      setShowAkadForm]       = useState(false);
  const [showHandoverForm,  setShowHandoverForm]   = useState(false);
  const [showScheduleForm,  setShowScheduleForm]   = useState(false);
  const [showBookingForm,   setShowBookingForm]    = useState(false);
  const [showReserveModal,  setShowReserveModal]   = useState(false);
  const [showCancelModal,   setShowCancelModal]    = useState(false);
  const [reserveBuyer,      setReserveBuyer]       = useState("");
  const [reserving,         setReserving]          = useState(false);

  const [balance,    setBalance]    = useState<string | null>(null);
  // Kelebihan Tanah: ringkasan finansial gabungan (rumah+tanah), dibekukan
  // sejak kontrak dikonversi — dipakai agar Sisa Tagihan langsung mencakup
  // komponen tanah, bukan menunggu Akad. balance = summary.total_outstanding.
  const [summary,    setSummary]    = useState<ContractFinancialSummary | null>(null);
  const [schedules,  setSchedules]  = useState<PaymentSchedule[]>([]);
  const [terminCount, setTerminCount] = useState(0);
  const [historyKey, setHistoryKey] = useState(0);
  const [activeBooking, setActiveBooking] = useState<Booking | null>(null);
  // W-13: kontrak lengkap (bukan cuma id) — dibutuhkan agar "+ Catat Penerimaan"
  // bisa menawarkan "Pencairan Dana Bank" ketika kontrak KPR sudah ber-akad.
  const [contract, setContract] = useState<SaleContract | null>(null);

  // Muat data kanonik dari server (dipakai saat mount & setelah tiap aksi):
  //   - saldo tagihan, jadwal cicilan (dengan paid_amount/status terbaru),
  //   - jumlah penerimaan (agar step "Penerimaan" akurat setelah reload).
  const refresh = useCallback(async () => {
    if (contractId) {
      // Saldo tagihan & daftar termin adalah data uang: 403 untuk marketing.
      // Jangan menembakkan request yang sudah pasti ditolak.
      if (!isMarketing) {
        fetchContractFinancialSummary(token, contractId)
          .then((s) => { setSummary(s); setBalance(s.total_outstanding); })
          .catch(() => { setSummary(null); setBalance(null); });
      }
      fetchSchedulesByContract(token, contractId)
        .then(setSchedules)
        .catch(() => {});
    }
    if (!isMarketing) {
      listTermins(token, unit.id)
        .then((r) => setTerminCount(r.data?.length ?? 0))
        .catch(() => {});
      fetchContractByUnit(token, unit.id)
        .then(setContract)
        .catch(() => setContract(null));
    }
    setHistoryKey((k) => k + 1);
  }, [contractId, token, unit.id, isMarketing]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  // Booking aktif unit (Increment 7) — utk kartu + konversi.
  useEffect(() => {
    if (unit.status === "booked") {
      fetchActiveBookingByUnit(token, unit.id).then(setActiveBooking);
    }
  }, [token, unit.id, unit.status]);

  async function handleReserve() {
    if (!reserveBuyer.trim()) return;
    setReserving(true);
    try {
      await transitionUnit(token, unit.id, {
        status: "reserved",
        event: "reservation_confirmed",
        buyer_ref: reserveBuyer.trim(),
      });
      toast("Unit direservasi", "success");
      window.location.reload();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal mereservasi unit", "error");
      setReserving(false);
    }
  }

  const isSold = unit.status === "sold" || saleRecord !== null;

  // W-13: "Pencairan Dana Bank" hanya masuk akal setelah kontrak KPR ber-Akad
  // Kredit — sebelum itu backend akan menolaknya (guard akad), jadi opsi
  // disembunyikan di UI supaya tidak mengundang error yang bisa diprediksi.
  const canDisburse =
    contract?.payment_type === "kpr" &&
    !!contract.financing_source_id &&
    (contract.scheme_state === "akad" || contract.scheme_state === "disbursed");

  return (
    <div className="space-y-6">
      {/* Breadcrumb + header */}
      <div>
        <nav className="text-xs text-text-secondary mb-2">
          <Link href="/penjualan" className="hover:text-accent">Penjualan</Link>
          <span className="mx-1">/</span>
          <span>{unitLabel(unit.code, unit.buyer_name)}</span>
        </nav>
        <div className="flex items-start justify-between gap-4">
          <div className="min-w-0">
            <h1 className="display-lg text-2xl text-text-primary">
              {unit.code}
              {unit.buyer_name && (
                <span className="text-text-secondary font-normal"> · {unit.buyer_name}</span>
              )}
            </h1>
            <p className="text-sm text-text-secondary mt-0.5">
              {unit.unit_type} · {Number(unit.saleable_area).toLocaleString("id-ID")} m²
            </p>
          </div>
          <span className="shrink-0"><StatusBadge status={unit.status} /></span>
        </div>
      </div>

      {/* Dua kolom di layar lebar: alur transaksi di kiri, konteks unit
          (ringkasan angka, tautan dokumen, riwayat) di kanan. Sebelumnya semua
          ditumpuk dalam satu kolom max-w-3xl sehingga separuh layar kosong. */}
      <div className="grid grid-cols-1 gap-6 xl:grid-cols-3">
        <div className="space-y-6 xl:col-span-2 min-w-0">

          {/* === BOOKED STATE: kartu booking aktif (Increment 7) === */}
          {unit.status === "booked" && activeBooking && (
            <Card padding="sm">
              <div className="flex items-start justify-between gap-3 flex-wrap">
                <div>
                  <p className="text-xs font-semibold text-text-secondary uppercase tracking-wide mb-1">
                    Booking Aktif #{activeBooking.id}
                  </p>
                  <p className="text-sm">
                    Fee <strong><Rupiah value={activeBooking.booking_fee} colorSign={false} /></strong>
                    {" · "}berlaku s/d <Tanggal value={activeBooking.expiry_date} />
                  </p>
                  {/* Kwitansi KWB terbit otomatis bersama booking — ditampilkan
                      di sini supaya admin tahu lembarannya ada dan bisa langsung
                      dicetak untuk pembeli. */}
                  {!!activeBooking.termin_payment_id && !isMarketing && (
                    <p className="text-xs text-text-secondary mt-1 flex items-center gap-2 flex-wrap">
                      <span>
                        Kwitansi{" "}
                        <span className="font-mono text-text-primary">
                          {activeBooking.receipt_number ?? "—"}
                        </span>
                      </span>
                      <PrintReceiptButton
                        token={token}
                        terminId={activeBooking.termin_payment_id}
                        notes="Booking fee"
                      />
                    </p>
                  )}
                  <div className="flex gap-2 mt-2">
                    <BookingStatusBadge status={activeBooking.status} expired={isBookingExpired(activeBooking)} />
                    <FeeDispositionBadge d={activeBooking.fee_disposition} />
                  </div>
                </div>
                <div className="flex gap-2">
                  <Button size="sm" onClick={() => setShowContractForm(true)}>
                    Konversi ke Kontrak
                  </Button>
                  <Link href="/penjualan/booking">
                    <Button size="sm" variant="secondary">Papan Booking</Button>
                  </Link>
                </div>
              </div>
            </Card>
          )}

          {/* === SOLD STATE: tampilkan sale record + aksi pembatalan === */}
          {isSold && saleRecord && (
            <>
              <SaleRecordCard record={saleRecord} />
              {/* UAT 2026-09-04: kontrak bundled Kelebihan Tanah punya piutang
                  tanah yang benar di Piutang Customer (aging), tapi tanpa
                  kartu ini halaman unit tak pernah bilang piutang itu ADA —
                  buyer/admin tak tahu harus melacak atau membayarnya ke mana.
                  landSchedule datang dari state `schedules` yang sudah dimuat
                  (tak ada request baru); ceiling pembayaran (outstanding di
                  RecordPaymentButton bawah) sudah mencakupnya lewat
                  total_outstanding_actual — kartu ini murni supaya piutangnya
                  TERLIHAT, bukan jalur bayar terpisah. */}
              {!isMarketing && (() => {
                const landSchedule = schedules.find(
                  (s) => s.type === "land" && (s.status as string) !== "superseded"
                );
                if (!landSchedule) return null;
                const sisa = (Number(landSchedule.amount) - Number(landSchedule.paid_amount)).toString();
                const lunas = landSchedule.status === "received";
                return (
                  <Card padding="sm">
                    <div className="flex items-center justify-between gap-3 mb-2 flex-wrap">
                      <p className="eyebrow">Piutang Kelebihan Tanah</p>
                      <Badge variant={lunas ? "success" : "warning"}>
                        {lunas ? "Lunas" : "Belum Lunas"}
                      </Badge>
                    </div>
                    <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
                      <div>
                        <p className="text-xs text-text-secondary">Tagihan</p>
                        <p className="font-medium"><Rupiah value={landSchedule.amount} /></p>
                      </div>
                      <div>
                        <p className="text-xs text-text-secondary">Terbayar</p>
                        <p className="font-medium"><Rupiah value={landSchedule.paid_amount} /></p>
                      </div>
                      <div>
                        <p className="text-xs text-text-secondary">Sisa</p>
                        <p className="font-medium"><Rupiah value={sisa} /></p>
                      </div>
                    </div>
                  </Card>
                );
              })()}
              {/* W-13: satu pintu masuk penerimaan tetap tersedia pasca-Akad —
                  kekurangan pelunasan, pencairan bertahap, atau penerimaan lain
                  semua lewat sini, bukan aksi tersembunyi di stepper. */}
              {!isMarketing && contractId && (() => {
                // UAT 2026-09-03 #1: RecordPaymentButton butuh id+sisa jadwal
                // Kelebihan Tanah SENDIRI (bukan outstanding gabungan di
                // bawah) supaya memilih "Kelebihan Tanah" di Jenis Penerimaan
                // menarget jadwal ini secara eksplisit — persis sumber yang
                // sama dipakai kartu "Piutang Kelebihan Tanah" di atas.
                const landSchedule = schedules.find(
                  (s) => s.type === "land" && (s.status as string) !== "superseded"
                );
                const landOutstanding = landSchedule
                  ? (Number(landSchedule.amount) - Number(landSchedule.paid_amount)).toString()
                  : undefined;
                return (
                  <Card padding="sm">
                    <div className="flex items-center justify-between gap-3 mb-3 flex-wrap">
                      <p className="eyebrow">Riwayat Penerimaan</p>
                      <RecordPaymentButton
                        token={token}
                        contractId={contractId}
                        // Pasca-Akad: piutang Kelebihan Tanah (bila ada) sudah
                        // ditagih terpisah lewat mesin piutang formal (W-5) —
                        // jangan pakai total_outstanding pra-Akad (proyeksi
                        // snapshot kontrak, bisa phantom bila land_sale
                        // dibatalkan pasca-Akad). total_outstanding_actual
                        // membaca piutang tanah SESUNGGUHNYA dari
                        // payment_schedules (anchor AR formal yang sama
                        // dipakai waterfall ReceivePayment) sehingga tetap
                        // benar walau land_sale-nya sudah dibatalkan.
                        outstanding={summary?.total_outstanding_actual ?? summary?.outstanding ?? balance ?? "0"}
                        buyerName={unit.buyer_name || undefined}
                        label="+ Catat Penerimaan"
                        size="sm"
                        financingSourceId={canDisburse ? contract?.financing_source_id : undefined}
                        bankName={contract?.bank_kpr}
                        landScheduleId={landSchedule?.id}
                        landOutstanding={landOutstanding}
                        onSuccess={refresh}
                      />
                    </div>
                    <RiwayatPenerimaanTable token={token} unitId={unit.id} refreshKey={historyKey} />
                  </Card>
                );
              })()}
              {/* R1: pencairan bank KPR sering terjadi PASCA-AKAD (piutang bank
                  1-2200 menunggu dana cair) — milestone & tombol pencairan harus
                  tetap tersedia sampai kontrak lunas. */}
              {!isMarketing && (
                <FinancingMilestones unitId={unit.id} token={token} onPaymentRecorded={refresh} refreshKey={historyKey} />
              )}
              {/* Temuan #7: serah terima fisik — non-finansial, tanpa jurnal,
                  terpisah dari kartu stepper penjualan di atas. */}
              {!isMarketing && (
                <Card padding="sm">
                  <div className="flex items-center justify-between gap-3 flex-wrap">
                    <div>
                      <p className="text-sm font-semibold text-text-primary">Serah Terima Fisik</p>
                      <p className="text-xs text-text-secondary mt-0.5">
                        {saleRecord.handed_over_at ? (
                          <>Diserahkan pada <Tanggal value={saleRecord.handed_over_at} format="long" />.</>
                        ) : (
                          "Belum diserahkan. Pencatatan ini murni fisik — tidak membuat jurnal."
                        )}
                      </p>
                    </div>
                    {!saleRecord.handed_over_at && (
                      <Button size="sm" variant="secondary" onClick={() => setShowHandoverForm(true)}>
                        Catat Serah Terima
                      </Button>
                    )}
                  </div>
                </Card>
              )}
              {!isMarketing && (
                <div className="flex justify-end">
                  <Button size="sm" variant="danger" onClick={() => setShowCancelModal(true)}>
                    Batalkan Penjualan
                  </Button>
                </div>
              )}
            </>
          )}
          {isSold && !saleRecord && (
            <Card padding="sm">
              <p className="text-sm text-text-secondary">
                {isMarketing
                  ? "Unit ini sudah Akad. Rincian HPP dan nilai serah terima hanya untuk Accounting."
                  : "Unit ini sudah terjual. Data Akad sedang dimuat atau tidak tersedia."}
              </p>
            </Card>
          )}

          {/* === AVAILABLE: aksi awal — Booking / Reservasi (Increment 6/7) === */}
          {unit.status === "available" && (
            <Card padding="sm">
              <div className="flex items-center justify-between gap-3 flex-wrap">
                <p className="text-sm text-text-secondary">
                  Mulai siklus jual: <strong>Booking</strong> (fee + masa berlaku) atau{" "}
                  <strong>Reservasi</strong> langsung (DP/komitmen).
                </p>
                <div className="flex gap-2">
                  <Button size="sm" onClick={() => setShowBookingForm(true)}>Booking Unit</Button>
                  <Button size="sm" variant="secondary" onClick={() => setShowReserveModal(true)}>
                    Reservasi
                  </Button>
                </div>
              </div>
            </Card>
          )}

          {/* === PRE-SOLD STATE: tampilkan flow penjualan === */}
          {!isSold && unit.status !== "booked" && (
            <div className="space-y-4">
              {/* Step 1: Kontrak */}
              <StepCard
                step={1}
                title="Kontrak Penjualan"
                done={!!contractId}
                action={!contractId ? (
                  <Button size="sm" onClick={() => setShowContractForm(true)}>
                    Buat Kontrak
                  </Button>
                ) : null}
              >
                {contractId ? (
                  <ContractSummary
                    contract={contract}
                    contractId={contractId}
                    balance={balance}
                    summary={summary}
                    token={token}
                    onAddSchedule={() => setShowScheduleForm(true)}
                    onContractUpdated={setContract}
                  />
                ) : (
                  <p className="text-sm text-text-secondary">
                    Buat kontrak terlebih dahulu untuk mengaktifkan jadwal cicilan KPR.
                    {isMarketing
                      ? " Penerimaan uang dicatat oleh Accounting."
                      : " Atau langsung catat termin di bawah."}
                  </p>
                )}
              </StepCard>

              {!isMarketing && (
                <>
                {/* Step 2: Penerimaan Pembayaran */}
                <StepCard
                  step={2}
                  title="Penerimaan Pembayaran"
                  done={terminCount > 0}
                  action={
                    contractId ? (
                      <RecordPaymentButton
                        token={token}
                        contractId={contractId}
                        outstanding={balance ?? "0"}
                        buyerName={unit.buyer_name || undefined}
                        label="+ Catat Penerimaan"
                        size="sm"
                        financingSourceId={canDisburse ? contract?.financing_source_id : undefined}
                        bankName={contract?.bank_kpr}
                        onSuccess={refresh}
                      />
                    ) : (
                      <Button size="sm" variant="secondary" onClick={() => setShowTerminForm(true)}>
                        + Catat Penerimaan
                      </Button>
                    )
                  }
                >
                  {terminCount > 0 ? (
                    <RiwayatPenerimaanTable token={token} unitId={unit.id} refreshKey={historyKey} />
                  ) : (
                    <p className="text-sm text-text-secondary">
                      Catat setiap penerimaan DP / cicilan. Jurnal Dr Bank / Cr Uang Muka Penjualan
                      diposting otomatis. Jadwal cicilan bersifat opsional — penerimaan bisa langsung
                      dicatat tanpa jadwal.
                    </p>
                  )}
                </StepCard>

                {/* W-13: Jadwal Cicilan — murni rencana penagihan, opsional.
                    Tidak menggerbang penerimaan apa pun (lihat Step 2 di atas). */}
                {schedules.length > 0 && (
                  <Card padding="sm">
                    <p className="text-xs font-semibold text-text-secondary uppercase tracking-wide mb-3">
                      Jadwal Cicilan <span className="normal-case font-normal">(opsional — rencana, bukan syarat)</span>
                    </p>
                    <div className="space-y-2">
                      {schedules.map((s) => (
                        <ScheduleRow
                          key={s.id}
                          schedule={s}
                          token={token}
                          showReceipt={!isMarketing}
                        />
                      ))}
                    </div>
                  </Card>
                )}

                {/* Milestone pembiayaan KPR — status pengajuan/approval/akad;
                    pencairan dana dicatat lewat "+ Catat Penerimaan" di atas. */}
                {contractId && (
                  <FinancingMilestones unitId={unit.id} token={token} onPaymentRecorded={refresh} refreshKey={historyKey} />
                )}

                {/* Step 3: Akad */}
                <StepCard
                  step={3}
                  title="Akad"
                  done={false}
                  action={
                    <Button size="sm" variant="danger" onClick={() => setShowAkadForm(true)}>
                      Catat Akad
                    </Button>
                  }
                >
                  <p className="text-sm text-text-secondary">
                    Akad mengakui pendapatan dan HPP secara atomik. Unit berubah menjadi
                    <strong> Terjual</strong> setelah Akad tercatat. Serah terima fisik unit dicatat
                    terpisah, belakangan — lihat kartu &quot;Serah Terima Fisik&quot; setelah Akad.
                    {unit.status === "available" && (
                      <span className="block mt-1 text-warning">
                        ⚠ Akad butuh unit ber-status <strong>Dipesan</strong> atau <strong>PPJB</strong> —
                        reservasi/booking dulu.
                      </span>
                    )}
                    {" "}Untuk kontrak KPR, gunakan tombol <strong>Catat Akad Kredit</strong> di kartu
                    Pembiayaan KPR di atas — itu sudah termasuk langkah ini.
                  </p>
                </StepCard>
                </>
              )}

              {/* Pembatalan pra-Akad (Increment 8) — bila sudah ada uang/kontrak */}
              {!isMarketing && (unit.status === "reserved" || unit.status === "ppjb") &&
                (terminCount > 0 || contractId) && (
                <div className="flex justify-end">
                  <button
                    className="text-xs text-danger hover:underline"
                    onClick={() => setShowCancelModal(true)}
                  >
                    Batalkan pemesanan ini (refund/penalti) →
                  </button>
                </div>
              )}
            </div>
          )}

        </div>

        {/* ── Kolom konteks ──────────────────────────────────────────────────
            Sticky di layar lebar supaya angka kunci & riwayat tetap terlihat
            saat alur transaksi di kiri di-scroll. */}
        <aside className="space-y-6 min-w-0 xl:sticky xl:top-20 xl:self-start">
          {/* Ringkasan angka unit */}
          <Card padding="sm">
            <p className="eyebrow mb-3">Ringkasan Unit</p>
            <dl className="grid grid-cols-2 gap-4 text-sm xl:grid-cols-1 xl:gap-3">
              <div>
                <dt className="text-xs text-text-secondary">Harga List</dt>
                <dd className="font-semibold text-text-primary tabular">
                  <Rupiah value={unit.list_price} colorSign={false} />
                </dd>
              </div>
              {unit.sale_price && (
                <div>
                  <dt className="text-xs text-text-secondary">Harga Jual</dt>
                  <dd className="font-semibold text-success tabular">
                    <Rupiah value={unit.sale_price} colorSign={false} />
                  </dd>
                </div>
              )}
              {unit.sale_date && (
                <div>
                  <dt className="text-xs text-text-secondary">Tanggal Jual</dt>
                  <dd className="font-medium text-text-primary">{fmtDate(unit.sale_date)}</dd>
                </div>
              )}
              {unit.buyer_ref && (
                <div className="min-w-0">
                  <dt className="text-xs text-text-secondary">Pembeli</dt>
                  <dd className="font-medium text-text-primary truncate">{unit.buyer_ref}</dd>
                </div>
              )}
            </dl>
          </Card>

          {/* Dokumen terkait — tautan yang dulu mengambang di kanan sebagai teks
              lepas, kini satu blok yang jelas bisa diklik. */}
          {contractId && !isMarketing && (
            <Card padding="sm">
              <p className="eyebrow mb-3">Dokumen &amp; Tagihan</p>
              <div className="flex flex-col divide-y divide-border-subtle">
                <Link
                  href={`/penjualan/${unit.id}/tagihan`}
                  className="flex items-center justify-between gap-2 py-2 text-sm
                    font-medium text-text-primary hover:text-accent transition-colors"
                >
                  Biaya Realisasi &amp; Tagihan Lain
                  <span aria-hidden="true" className="text-text-tertiary">→</span>
                </Link>
                <Link
                  href={`/penjualan/${unit.id}/invoice`}
                  className="flex items-center justify-between gap-2 py-2 text-sm
                    font-medium text-text-primary hover:text-accent transition-colors"
                >
                  Kelola Invoice
                  <span aria-hidden="true" className="text-text-tertiary">→</span>
                </Link>
              </div>
            </Card>
          )}

          {/* Riwayat lifecycle unit (Increment 6) */}
          <UnitTimelineCard token={token} unitId={unit.id} />
        </aside>
      </div>

      {/* Modals */}
      <ContractForm
        open={showContractForm}
        onClose={() => setShowContractForm(false)}
        unitId={unit.id}
        projectId={unit.project_id}
        listPrice={unit.list_price}
        token={token}
        bookingId={activeBooking?.id ?? bookingParam}
        lockedCustomerId={activeBooking?.customer_id}
        lockedSalesPersonId={activeBooking?.sales_person_id}
      />
      <BookingFormModal
        open={showBookingForm}
        onClose={() => setShowBookingForm(false)}
        token={token}
        unitId={unit.id}
        unitCode={unit.code}
        projectId={unit.project_id}
        onSuccess={() => window.location.reload()}
      />
      {!isMarketing && <CancelRequestModal
        open={showCancelModal}
        onClose={() => setShowCancelModal(false)}
        token={token}
        unitId={unit.id}
        unitCode={unit.code}
        postBAST={isSold}
      />}
      <Modal
        open={showReserveModal}
        onClose={() => setShowReserveModal(false)}
        title={`Reservasi Unit ${unit.code}`}
        size="md"
        footer={
          <>
            <Button variant="secondary" onClick={() => setShowReserveModal(false)} disabled={reserving}>
              Batal
            </Button>
            <Button onClick={handleReserve} loading={reserving} disabled={!reserveBuyer.trim()}>
              Reservasi
            </Button>
          </>
        }
      >
        <div className="space-y-3">
          <Input
            label="Nama Pembeli" required
            value={reserveBuyer}
            onChange={(e) => setReserveBuyer(e.target.value)}
            placeholder="cth: Budi Santoso"
          />
          <p className="text-xs text-text-secondary">
            Reservasi menandai komitmen buyer (unit → <strong>Dipesan</strong>) dan
            membuka langkah Akad. Tercatat di riwayat unit.
          </p>
        </div>
      </Modal>
      {!isMarketing && (
        <>
          <TerminForm
            open={showTerminForm}
            onClose={() => setShowTerminForm(false)}
            unitId={unit.id}
            token={token}
            onSuccess={() => refresh()}
          />
          <AkadForm
            open={showAkadForm}
            onClose={() => setShowAkadForm(false)}
            unitId={unit.id}
            listPrice={unit.list_price}
            token={token}
            onSubmitted={() => refresh()}
          />
          <HandoverForm
            open={showHandoverForm}
            onClose={() => setShowHandoverForm(false)}
            unitId={unit.id}
            token={token}
            onSuccess={() => refresh()}
          />
        </>
      )}
      {contractId && (
        <ScheduleForm
          open={showScheduleForm}
          onClose={() => setShowScheduleForm(false)}
          contractId={contractId}
          token={token}
          onSuccess={() => refresh()}
        />
      )}
    </div>
  );
}

// ── sub-components ────────────────────────────────────────────────────────────

function StepCard({
  step, title, done, action, children,
}: {
  step: number;
  title: string;
  done: boolean;
  action?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <Card padding="sm">
      <div className="flex items-start justify-between mb-3">
        <div className="flex items-center gap-3">
          <span
            className={`flex h-7 w-7 items-center justify-center rounded-full text-xs font-bold shrink-0
              ${done ? "bg-success text-white" : "bg-accent text-white"}`}
          >
            {done ? "✓" : step}
          </span>
          <h3 className="text-sm font-semibold text-text-primary">{title}</h3>
        </div>
        {action}
      </div>
      <div className="ml-10">{children}</div>
    </Card>
  );
}

function ContractSummary({
  contract, contractId, balance, summary, token, onAddSchedule, onContractUpdated,
}: {
  contract: SaleContract | null;
  contractId: number;
  balance: string | null;
  summary: ContractFinancialSummary | null;
  token: string;
  onAddSchedule: () => void;
  onContractUpdated: (c: SaleContract) => void;
}) {
  const { toast } = useToast();
  const [salesPersons, setSalesPersons] = useState<SalesPerson[]>([]);
  const [editingAdmin, setEditingAdmin] = useState(false);
  const [adminChoice, setAdminChoice] = useState("");
  const [savingAdmin, setSavingAdmin] = useState(false);

  useEffect(() => {
    fetchSalesPersons(token).then(setSalesPersons).catch(() => {});
  }, [token]);

  const nameOf = (id?: number) => salesPersons.find((s) => s.id === id)?.name ?? (id ? `#${id}` : null);
  const salesName = nameOf(contract?.sales_person_id);
  const adminName = nameOf(contract?.admin_marketing_person_id);

  async function saveAdmin() {
    setSavingAdmin(true);
    try {
      const updated = await setAdminMarketing(token, contractId, adminChoice ? parseInt(adminChoice, 10) : null);
      onContractUpdated(updated);
      setEditingAdmin(false);
      toast("Admin Marketing diperbarui", "success");
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal memperbarui Admin Marketing", "error");
    } finally {
      setSavingAdmin(false);
    }
  }

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap gap-4 text-sm">
        <div>
          <p className="text-xs text-text-secondary">ID Kontrak</p>
          <p className="font-medium">#{contractId}</p>
        </div>
        {balance !== null && (
          <div>
            <p className="text-xs text-text-secondary">
              {summary?.has_land ? "Sisa Tagihan (Rumah + Kelebihan Tanah)" : "Sisa Tagihan"}
            </p>
            <p className="font-medium text-warning">
              <Rupiah value={balance} colorSign={false} />
            </p>
          </div>
        )}
        {summary?.has_land && (
          <div>
            <p className="text-xs text-text-secondary">termasuk Kelebihan Tanah</p>
            <p className="font-medium">
              <Rupiah value={summary.land_amount} colorSign={false} />
            </p>
          </div>
        )}
        {salesName && (
          <div>
            <p className="text-xs text-text-secondary">Sales</p>
            <p className="font-medium">{salesName}</p>
          </div>
        )}
        <div>
          <p className="text-xs text-text-secondary">Admin Marketing</p>
          {!editingAdmin ? (
            <p className="font-medium flex items-center gap-2">
              {adminName ?? <span className="text-text-tertiary font-normal">— belum ditetapkan —</span>}
              <button
                type="button"
                className="text-xs text-accent hover:underline cursor-pointer"
                onClick={() => { setAdminChoice(contract?.admin_marketing_person_id ? String(contract.admin_marketing_person_id) : ""); setEditingAdmin(true); }}
              >
                Ubah
              </button>
            </p>
          ) : (
            <div className="flex items-center gap-1.5">
              <select
                className="rounded border border-border bg-surface px-2 py-1 text-xs outline-none focus:border-accent"
                value={adminChoice}
                onChange={(e) => setAdminChoice(e.target.value)}
              >
                <option value="">— kosongkan —</option>
                {salesPersons.filter((s) => s.is_active).map((s) => (
                  <option key={s.id} value={s.id}>{s.name}</option>
                ))}
              </select>
              <button type="button" disabled={savingAdmin} className="text-xs text-accent hover:underline cursor-pointer disabled:opacity-50" onClick={saveAdmin}>
                Simpan
              </button>
              <button type="button" className="text-xs text-text-tertiary hover:underline cursor-pointer" onClick={() => setEditingAdmin(false)}>
                Batal
              </button>
            </div>
          )}
        </div>
      </div>
      <div className="flex gap-2">
        <Button size="sm" variant="secondary" onClick={onAddSchedule}>
          + Jadwal Cicilan
        </Button>
      </div>
    </div>
  );
}

const scheduleTypeLabel: Record<string, string> = {
  dp: "DP",
  installment: "Cicilan",
  final: "Pelunasan",
};

function ScheduleRow({
  schedule, token, showReceipt,
}: {
  schedule: PaymentSchedule;
  token: string;
  /** Marketing melihat jadwalnya, tetapi tidak menerima uangnya. */
  showReceipt: boolean;
}) {
  const isReceived = schedule.status === "received";
  const isOverdue  = schedule.status === "overdue";

  return (
    <div className="flex items-center justify-between text-sm border-b border-border last:border-0 pb-2 last:pb-0">
      <div className="flex items-center gap-3">
        <Badge variant={isReceived ? "success" : isOverdue ? "danger" : "warning"}>
          {scheduleTypeLabel[schedule.type] ?? schedule.type} #{schedule.installment_number}
        </Badge>
        <span className="text-text-secondary text-xs">
          {new Date(schedule.due_date).toLocaleDateString("id-ID")}
        </span>
      </div>
      <div className="flex items-center gap-3">
        <Rupiah value={schedule.amount} colorSign={false} />
        {!isReceived && (
          <span className="text-xs text-text-tertiary">Belum diterima</span>
        )}
        {isReceived && (
          <>
            <span className="text-xs text-success">Lunas</span>
            {schedule.termin_payment_id && showReceipt && (
              <PrintReceiptButton
                token={token}
                terminId={schedule.termin_payment_id}
                notes={`${scheduleTypeLabel[schedule.type] ?? schedule.type} #${schedule.installment_number}`}
              />
            )}
          </>
        )}
      </div>
    </div>
  );
}
