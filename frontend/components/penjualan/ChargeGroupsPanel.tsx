"use client";

// Billing Batch 2 — Charge Group (Titipan Realisasi & Produk Tambahan).
//
// Rule klien FINAL 2026-08-03:
//  K-1 seluruh biaya realisasi = TITIPAN (kewajiban), bukan pendapatan. Akun
//      kewajibannya ATRIBUT jenis biaya (master), bukan konstanta — satu grup
//      boleh berakun campur dan jurnalnya terpecah per akun.
//  K-3 alokasi pembayaran = KEPUTUSAN ADMIN per item (tanpa auto-FIFO/proporsional);
//      UI memaksa Σ alokasi == nominal sebelum submit — backend validasi ulang.
//  K-4 sisa titipan → refund / transfer (rumah / grup lain), bukan pendapatan.
//  K-5 payout > tagihan → outstanding tambahan otomatis (ditampilkan dari backend).
// Semua angka dihitung backend (GroupSummary kanonik) — komponen ini display+input.
//
// W-1 (2026-08-06): jenis biaya realisasi TIDAK LAGI diketik bebas. Item grup
// realization wajib menunjuk master `realization_charge_types` (Pengaturan →
// Biaya Realisasi), sebab jenis biayalah yang menentukan ke akun kewajiban mana
// uang customer mendarat.
//
// W-13 (2026-08-19): grup addon TIDAK LAGI teks bebas juga — item produk
// tambahan menunjuk master Katalog Produk (Pengaturan → Katalog Produk).
// Bedanya dengan realisasi bukan ketat/longgar, melainkan ARAH UANGNYA:
// realisasi berhenti sebagai titipan pihak ketiga; produk tambahan mendarat di
// Uang Muka Penjualan dan menjadi PENDAPATAN saat serah terima. Selama produknya
// hanya label ketikan, uang kelebihan tanah tersangkut di akun titipan selamanya
// dan penjualannya tidak pernah muncul di laba rugi.

import { useCallback, useEffect, useMemo, useState } from "react";
import { Card, CardHeader, CardTitle } from "@/components/ui/Card";
import { Button } from "@/components/ui/Button";
import { Badge } from "@/components/ui/Badge";
import { Modal } from "@/components/ui/Modal";
import { Input, Select } from "@/components/ui/Input";
import { FormGrid, FormFull } from "@/components/ui/Form";
import { RupiahInput, validateRupiah } from "@/components/ui/RupiahInput";
import { EmptyState } from "@/components/ui/EmptyState";
import { Rupiah, formatRupiah } from "@/components/format/Rupiah";
import { Tanggal } from "@/components/format/Tanggal";
import { Table, TableHead, TableBody, TableRow, Th, Td } from "@/components/ui/Table";
import { CashBankSelect } from "@/components/accounting/CashBankSelect";
import { PrintReceiptButton } from "@/components/billing/PrintReceiptButton";
import { PrintTransferMemoButton } from "@/components/penjualan/PrintTransferMemoButton";
import { useToast } from "@/components/ui/Toast";
import { todayLocalStr } from "@/lib/date";
import {
  fetchChargeGroupsByUnit,
  fetchChargeGroupDetail,
  createChargeGroup,
  addChargeItems,
  payChargeGroup,
  recordChargePayout,
  refundChargeGroup,
  transferChargeToHouse,
  transferChargeToGroup,
  settleChargeGroup,
  cancelChargeGroup,
  cancelChargeItem,
  voidChargePayment,
  issueChargeInvoice,
  fetchChargeTypes,
  type RealizationChargeType,
  type ChargeGroupSummary,
  type ChargeGroupDetail,
  type ChargeItemSummary,
  type ChargeItemInput,
  type ChargeKind,
} from "@/lib/api/charge";
import { fetchProductTypes, isAddonProduct, type ProductType } from "@/lib/api/projects";

interface Props {
  token: string;
  unitId: number;
  contractId?: number;
  canWrite: boolean;
}

const KIND_LABEL: Record<ChargeKind, string> = {
  realization: "Biaya Realisasi",
  addon: "Produk Tambahan",
};

function newIdemKey(prefix: string) {
  return typeof crypto !== "undefined" && crypto.randomUUID
    ? `${prefix}-${crypto.randomUUID()}`
    : `${prefix}-${Date.now()}`;
}

function intOf(s: string): number {
  const n = parseInt((s || "0").split(".")[0], 10);
  return isNaN(n) ? 0 : n;
}

// ── W-1: master jenis biaya realisasi ────────────────────────────────────────

/** useChargeTypes memuat master sekali per modal dibuka — jenis biaya yang baru
 *  ditambahkan admin di Pengaturan langsung tersedia tanpa reload halaman. */
function useChargeTypes(token: string, enabled: boolean) {
  const [all, setAll] = useState<RealizationChargeType[]>([]);
  const [loaded, setLoaded] = useState(false);
  useEffect(() => {
    if (!enabled) return;
    void (async () => {
      try {
        setAll(await fetchChargeTypes(token));
      } catch {
        setAll([]);
      } finally {
        setLoaded(true);
      }
    })();
  }, [token, enabled]);
  // `all` dipakai untuk MEMBACA grup lama (jenis biaya yang kini nonaktif harus
  // tetap bisa dikenali); `active` dipakai untuk MEMILIH pada tagihan baru.
  const active = useMemo(() => all.filter((t) => t.is_active), [all]);
  return { all, active, loaded };
}

/** ChargeTypeSelect — dropdown jenis biaya dari master. Akun kewajiban adalah
 *  ATRIBUT jenis biaya, bukan properti grup: satu grup boleh memuat jenis
 *  berakun berbeda, dan jurnalnya terpecah per akun. Tidak ada opsi yang
 *  dinonaktifkan di sini — yang satu-per-grup adalah INVOICE, bukan akun. */
function ChargeTypeSelect({
  label, types, value, onChange,
}: {
  label?: string;
  types: RealizationChargeType[];
  value: string;
  onChange: (code: string) => void;
}) {
  return (
    <Select label={label} value={value} onChange={(e) => onChange(e.target.value)}>
      <option value="">— pilih jenis biaya —</option>
      {types.map((t) => (
        <option key={t.id} value={t.code}>{t.name}</option>
      ))}
    </Select>
  );
}

/** ChargeTypesEmptyNotice — master kosong berarti seluruh tagihan realisasi
 *  akan ditolak server (fail-closed). Lebih baik admin diarahkan sekarang
 *  daripada menabrak error setelah mengetik lima baris. */
function ChargeTypesEmptyNotice() {
  return (
    <div className="rounded-md border border-warning/40 bg-warning-bg px-3 py-2 text-xs text-text-secondary">
      Master <strong>Jenis Biaya Realisasi</strong> masih kosong. Tambahkan dulu di{" "}
      <strong>Pengaturan → Biaya Realisasi</strong> — tagihan realisasi tanpa jenis biaya
      yang terdaftar akan ditolak sistem.
    </div>
  );
}

/** useAddonProducts memuat katalog produk yang boleh dijual sebagai PRODUK
 *  TAMBAHAN — yaitu yang non-properti. Produk properti sengaja tidak muncul:
 *  rumah/ruko dijual sebagai unit karena unitlah yang menerima alokasi biaya
 *  dan melahirkan HPP; sebagai baris tagihan ia akan mengakui pendapatan tanpa
 *  lawan HPP. */
function useAddonProducts(token: string, enabled: boolean) {
  const [all, setAll] = useState<ProductType[]>([]);
  const [loaded, setLoaded] = useState(false);
  useEffect(() => {
    if (!enabled) return;
    void (async () => {
      try {
        setAll(await fetchProductTypes(token));
      } catch {
        setAll([]);
      } finally {
        setLoaded(true);
      }
    })();
  }, [token, enabled]);
  const active = useMemo(() => all.filter(isAddonProduct), [all]);
  return { all, active, loaded };
}

/** AddonProductSelect — dropdown produk tambahan dari Katalog Produk. Akun
 *  pendapatan ikut produk, jadi ditampilkan sebagai keterangan: admin berhak
 *  tahu ke akun mana penjualan ini akan diakui. */
function AddonProductSelect({
  label, products, value, onChange,
}: {
  label?: string;
  products: ProductType[];
  value: string;
  onChange: (code: string) => void;
}) {
  return (
    <Select label={label} value={value} onChange={(e) => onChange(e.target.value)}>
      <option value="">— pilih produk —</option>
      {products.map((p) => (
        <option key={p.id} value={p.code}>{p.name}</option>
      ))}
    </Select>
  );
}

/** AddonProductsEmptyNotice — katalog tanpa produk non-properti berarti setiap
 *  item addon akan ditolak server. Arahkan sekarang, bukan setelah mengetik. */
function AddonProductsEmptyNotice() {
  return (
    <div className="rounded-md border border-warning/40 bg-warning-bg px-3 py-2 text-xs text-text-secondary">
      <strong>Katalog Produk</strong> belum punya produk non-properti (mis. Kelebihan Tanah).
      Tambahkan dulu di <strong>Pengaturan → Katalog Produk</strong> beserta akun
      pendapatannya — produk tambahan tanpa produk terdaftar akan ditolak sistem.
    </div>
  );
}

/** revenueAccountsOf mendaftar akun pendapatan yang akan dikredit sebuah grup
 *  addon lewat produk item-itemnya. Kosong untuk grup warisan pra-W-13. */
function revenueAccountsOf(codes: (string | undefined)[], products: ProductType[]): string[] {
  const out: string[] = [];
  for (const code of codes) {
    const p = code ? products.find((x) => x.code === code) : undefined;
    if (p && p.revenue_account_code && !out.includes(p.revenue_account_code)) {
      out.push(p.revenue_account_code);
    }
  }
  return out.sort();
}

/** depositAccountsOf mendaftar akun kewajiban yang dipakai sebuah grup, lewat
 *  jenis biaya item-itemnya. Bisa lebih dari satu — grup boleh berakun campur.
 *  Kosong untuk grup warisan pra-W-1 yang itemnya belum bertanda jenis biaya. */
function depositAccountsOf(codes: (string | undefined)[], types: RealizationChargeType[]): string[] {
  const out: string[] = [];
  for (const code of codes) {
    const t = code ? types.find((x) => x.code === code) : undefined;
    if (t && !out.includes(t.deposit_account_code)) out.push(t.deposit_account_code);
  }
  return out.sort();
}

function statusBadge(status: string) {
  if (status === "open") return <Badge variant="warning">Berjalan</Badge>;
  if (status === "settled") return <Badge variant="success">Selesai</Badge>;
  return <Badge variant="default">Dibatalkan</Badge>;
}

// ── Panel utama ──────────────────────────────────────────────────────────────

export function ChargeGroupsPanel({ token, unitId, contractId, canWrite }: Props) {
  const { toast } = useToast();
  const [groups, setGroups] = useState<ChargeGroupSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [createOpen, setCreateOpen] = useState(false);

  const reload = useCallback(async () => {
    try {
      setGroups(await fetchChargeGroupsByUnit(token, unitId));
    } catch (err) {
      toast(err instanceof Error ? err.message : "Gagal memuat tagihan", "error");
    } finally {
      setLoading(false);
    }
  }, [token, unitId, toast]);

  useEffect(() => {
    void reload();
  }, [reload]);

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-base font-bold text-text-primary">Tagihan Terpisah</h2>
          <p className="text-xs text-text-secondary mt-0.5">
            Biaya Realisasi (titipan pihak ketiga — jenisnya diatur di Pengaturan → Biaya Realisasi)
            &amp; produk tambahan. Terpisah dari tagihan harga rumah: outstanding, invoice,
            dan kwitansi sendiri (seri KWR).
          </p>
        </div>
        {canWrite && contractId && (
          <Button onClick={() => setCreateOpen(true)}>+ Grup Tagihan</Button>
        )}
      </div>

      {loading ? (
        <Card><p className="text-sm text-text-secondary">Memuat…</p></Card>
      ) : groups.length === 0 ? (
        <Card>
          <EmptyState
            icon={
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
                <rect x="3" y="4" width="18" height="16" rx="2" /><path d="M7 9h10M7 13h6" />
              </svg>
            }
            title="Belum ada tagihan terpisah"
            description={
              contractId
                ? canWrite
                  ? "Buat grup Biaya Realisasi (jenisnya diambil dari master Jenis Biaya Realisasi) atau tagihan produk tambahan untuk kontrak ini."
                  : "Grup tagihan realisasi/produk tambahan kontrak ini akan muncul di sini."
                : "Buat kontrak penjualan terlebih dahulu — grup tagihan menempel pada kontrak."
            }
          />
        </Card>
      ) : (
        groups.map((g) => (
          <ChargeGroupCard
            key={g.group_id}
            token={token}
            group={g}
            allGroups={groups}
            contractId={contractId}
            canWrite={canWrite}
            onChanged={reload}
          />
        ))
      )}

      {createOpen && contractId && (
        <CreateGroupModal
          token={token}
          contractId={contractId}
          onClose={() => setCreateOpen(false)}
          onCreated={() => {
            setCreateOpen(false);
            void reload();
          }}
        />
      )}
    </div>
  );
}

// ── Kartu satu grup ──────────────────────────────────────────────────────────

function ChargeGroupCard({
  token, group, allGroups, contractId, canWrite, onChanged,
}: {
  token: string;
  group: ChargeGroupSummary;
  allGroups: ChargeGroupSummary[];
  contractId?: number;
  canWrite: boolean;
  onChanged: () => void;
}) {
  const { toast } = useToast();
  const [detail, setDetail] = useState<ChargeGroupDetail | null>(null);
  const [showHistory, setShowHistory] = useState(false);
  const [modal, setModal] = useState<
    null | { kind: "pay" } | { kind: "payout"; item: ChargeItemSummary } | { kind: "addItem" }
    | { kind: "refund" } | { kind: "transferHouse" } | { kind: "transferGroup" } | { kind: "invoice" }
  >(null);
  const [busy, setBusy] = useState(false);

  const open = group.status === "open";
  const residual = intOf(group.residual);
  const outstanding = intOf(group.outstanding);
  // W-5: sisa terutang terbelah menjadi dua hal yang BERBEDA di buku besar —
  // yang sudah ditagihkan (piutang customer, 1-2000) dan yang belum (masih
  // titipan). Angka inilah yang menentukan tombol invoice dan blok peringatan.
  const isRealizationGroup = group.kind === "realization";
  const receivable = intOf(group.receivable);
  const unbilled = intOf(group.unbilled);

  // Akun kewajiban yang benar-benar dipakai grup ini — dibaca dari snapshot per
  // item, bukan ditebak dari konstanta. Bisa lebih dari satu.
  const cardAccounts = useMemo(() => {
    const out: string[] = [];
    for (const it of group.items) {
      if (it.deposit_account_code && !out.includes(it.deposit_account_code)) {
        out.push(it.deposit_account_code);
      }
    }
    return out.sort();
  }, [group.items]);

  // Rincian sisa per akun, hanya bila memang tersebar — angka tambahan yang
  // tidak menjelaskan apa pun malah membebani layar.
  const residualByAccount =
    (group.deposit_residuals?.length ?? 0) > 1
      ? ` (${group.deposit_residuals.map((d) => `${d.account_code}: ${formatRupiah(d.amount)}`).join(", ")})`
      : "";

  async function loadHistory() {
    if (showHistory) {
      setShowHistory(false);
      return;
    }
    try {
      setDetail(await fetchChargeGroupDetail(token, group.group_id));
      setShowHistory(true);
    } catch (err) {
      toast(err instanceof Error ? err.message : "Gagal memuat riwayat", "error");
    }
  }

  // run — satu aksi, SATU toast. Aksi yang tahu detail hasilnya (mis. nomor
  // invoice dan nominal yang benar-benar diakui) cukup mengembalikan kalimatnya;
  // yang lain memakai label generik.
  async function run(label: string, fn: () => Promise<unknown>) {
    setBusy(true);
    try {
      const hasil = await fn();
      toast(typeof hasil === "string" ? hasil : label, "success");
      setModal(null);
      onChanged();
    } catch (err) {
      toast(err instanceof Error ? err.message : "Gagal", "error");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card padding="none">
      <div className="px-5 py-4 border-b border-border flex items-start justify-between gap-3 flex-wrap">
        <div>
          <div className="flex items-center gap-2">
            <h3 className="text-sm font-semibold text-text-primary">{group.label}</h3>
            <Badge variant={group.kind === "realization" ? "accent" : "neutral"}>{KIND_LABEL[group.kind]}</Badge>
            {statusBadge(group.status)}
            {isRealizationGroup && (
              group.recognized ? (
                <Badge variant="success">
                  Sudah ditagihkan{group.invoice_number ? ` · ${group.invoice_number}` : ""}
                </Badge>
              ) : (
                <Badge variant="default">Belum ditagihkan</Badge>
              )
            )}
          </div>
          {group.kind === "realization" && (
            <p className="text-[11px] text-text-tertiary mt-1">
              Titipan{cardAccounts.length > 0 && <> ({cardAccounts.join(", ")})</>} — bukan
              pendapatan. Pembayaran dialokasikan manual oleh admin per item.
            </p>
          )}
        </div>
        {canWrite && open && (
          <div className="flex gap-2 flex-wrap">
            <Button size="sm" onClick={() => setModal({ kind: "pay" })} disabled={outstanding <= 0}>
              Terima Pembayaran
            </Button>
            <Button size="sm" variant="secondary" onClick={() => setModal({ kind: "addItem" })}>
              + Item
            </Button>
            {isRealizationGroup && (
              <Button
                size="sm"
                variant="secondary"
                onClick={() => setModal({ kind: "invoice" })}
                disabled={unbilled <= 0 || busy}
                title={
                  unbilled <= 0
                    ? "Tidak ada tagihan yang belum ditagihkan"
                    : undefined
                }
              >
                {group.recognized ? "Terbitkan Invoice Susulan" : "Terbitkan Invoice"}
              </Button>
            )}
          </div>
        )}
      </div>

      {/* Ringkasan 4 angka (K-5) + residual (K-4) + sisi piutang (W-5) */}
      <div className="px-5 py-3 grid grid-cols-2 md:grid-cols-5 gap-3 border-b border-border bg-bg">
        <SummaryCell label="Total Tagihan" value={group.billed} />
        <SummaryCell label="Total Dibayar" value={group.paid} />
        <SummaryCell label="Sisa Terutang" value={group.outstanding} strong />
        <SummaryCell label="Dibayarkan ke Vendor" value={group.payout} />
        <SummaryCell label="Sisa Titipan" value={group.residual} />
      </div>
      {isRealizationGroup && (receivable > 0 || unbilled > 0) && (
        <div className="px-5 py-3 grid grid-cols-2 md:grid-cols-4 gap-3 border-b border-border">
          <SummaryCell
            label="Piutang Customer"
            value={group.receivable}
            strong
            hint="Sudah ditagihkan lewat invoice — masuk laporan piutang (1-2000)."
          />
          <SummaryCell
            label="Belum Ditagihkan"
            value={group.unbilled}
            hint="Belum ada invoice, jadi belum menjadi piutang dan belum muncul di laporan piutang."
          />
        </div>
      )}
      {isRealizationGroup && open && unbilled > 0 && (
        <div className="px-5 py-3 border-b border-border bg-warning-bg/40 flex items-start justify-between gap-3 flex-wrap">
          <p className="text-xs text-text-secondary max-w-2xl">
            <strong className="text-text-primary"><Rupiah value={group.unbilled} /> belum ditagihkan</strong>{" "}
            — nominal ini belum menjadi piutang customer dan belum muncul di laporan piutang.
            Terbitkan invoice untuk menagihkannya. Bila terlewat, invoice akan terbit otomatis
            saat BAST — serah terima kunci tidak pernah tertahan karenanya.
          </p>
          {canWrite && (
            <Button size="sm" variant="secondary" onClick={() => setModal({ kind: "invoice" })} disabled={busy}>
              {group.recognized ? "Terbitkan Invoice Susulan" : "Terbitkan Invoice"}
            </Button>
          )}
        </div>
      )}

      {/* Item */}
      <Table>
        <TableHead>
          <TableRow>
            <Th>Item</Th>
            <Th right>Tagihan</Th>
            <Th right>Dibayar</Th>
            <Th right>Sisa</Th>
            <Th right>Payout Vendor</Th>
            <Th>Status Tagih</Th>
            <Th>Jatuh Tempo</Th>
            {canWrite && open && <Th>Aksi</Th>}
          </TableRow>
        </TableHead>
        <TableBody>
          {group.items.map((it) => (
            <TableRow key={it.item_id}>
              <Td>
                <span className={it.status === "cancelled" ? "line-through text-text-tertiary" : ""}>{it.label}</span>
                {intOf(it.amount) !== intOf(it.original_amount) && (
                  <span className="ml-1.5 text-[10px] px-1 py-0.5 rounded bg-warning-bg text-warning" title={`Tagihan awal ${formatRupiah(it.original_amount)}`}>
                    disesuaikan
                  </span>
                )}
              </Td>
              <Td right><Rupiah value={it.amount} /></Td>
              <Td right><Rupiah value={it.paid} /></Td>
              <Td right><Rupiah value={it.outstanding} /></Td>
              <Td right><Rupiah value={it.payout} /></Td>
              <Td>{it.status === "open" ? recognitionBadge(it) : <span className="text-text-tertiary">—</span>}</Td>
              <Td>{it.due_date ? <Tanggal value={it.due_date} /> : <span className="text-text-tertiary">—</span>}</Td>
              {canWrite && open && (
                <Td>
                  {it.status === "open" && (
                    <div className="flex gap-2">
                      <button
                        className="text-xs text-accent hover:underline"
                        onClick={() => setModal({ kind: "payout", item: it })}
                      >
                        Payout
                      </button>
                      {intOf(it.paid) === 0 && intOf(it.payout) === 0 && (
                        <button
                          className="text-xs text-danger hover:underline"
                          onClick={() => run("Item dibatalkan", () => cancelChargeItem(token, it.item_id))}
                        >
                          Batalkan
                        </button>
                      )}
                    </div>
                  )}
                </Td>
              )}
            </TableRow>
          ))}
        </TableBody>
      </Table>

      {/* Aksi disposisi (K-4) + tutup grup */}
      <div className="px-5 py-3 border-t border-border flex items-center justify-between flex-wrap gap-2">
        <button className="text-xs text-accent hover:underline" onClick={loadHistory}>
          {showHistory ? "Sembunyikan riwayat" : "Lihat riwayat pembayaran & payout"}
        </button>
        {canWrite && open && (
          <div className="flex gap-2 flex-wrap">
            {residual > 0 && (
              <>
                <Button size="sm" variant="secondary" onClick={() => setModal({ kind: "refund" })}>
                  Refund Sisa
                </Button>
                {contractId && (
                  <Button size="sm" variant="secondary" onClick={() => setModal({ kind: "transferHouse" })}>
                    Alihkan ke Harga Rumah
                  </Button>
                )}
                {allGroups.some((g) => g.group_id !== group.group_id && g.status === "open") && (
                  <Button size="sm" variant="secondary" onClick={() => setModal({ kind: "transferGroup" })}>
                    Alihkan ke Grup Lain
                  </Button>
                )}
              </>
            )}
            <Button
              size="sm"
              variant="secondary"
              loading={busy}
              onClick={() =>
                run("True-up dijalankan", async () => {
                  const res = await settleChargeGroup(token, group.group_id);
                  if (res.settled) {
                    toast("Grup selesai (settled)", "success");
                  } else {
                    toast(
                      `Belum bisa ditutup — sisa terutang ${res.summary.outstanding}, sisa titipan ${res.summary.residual}. Selesaikan dengan pembayaran/refund/transfer.`,
                      "error",
                    );
                  }
                })
              }
            >
              True-up &amp; Tutup
            </Button>
            {intOf(group.paid) === 0 && intOf(group.payout) === 0 && (
              <Button
                size="sm"
                variant="danger"
                onClick={() => run("Grup dibatalkan", () => cancelChargeGroup(token, group.group_id))}
              >
                Batalkan Grup
              </Button>
            )}
          </div>
        )}
      </div>

      {/* Riwayat */}
      {showHistory && detail && (
        <div className="px-5 py-3 border-t border-border space-y-3">
          <div>
            <p className="text-xs font-semibold text-text-primary mb-1">Pembayaran</p>
            {detail.payments.length === 0 ? (
              <p className="text-xs text-text-tertiary">Belum ada pembayaran.</p>
            ) : (
              detail.payments.map((p) => (
                <div key={p.termin_id} className="flex items-center gap-3 text-xs py-1 border-b border-border-subtle last:border-0">
                  <Tanggal value={p.date} />
                  <span className="font-medium tabular-nums"><Rupiah value={p.amount} /></span>
                  <span className="text-text-tertiary">{p.receipt_number || (p.payment_source === "realization_transfer" ? "transfer titipan" : "")}</span>
                  {p.voided && <Badge variant="danger">VOID</Badge>}
                  <span className="flex-1" />
                  {!p.voided && p.payment_source === "realization" && (
                    <PrintReceiptButton token={token} terminId={p.termin_id} variant="link" label="Cetak KWR" />
                  )}
                  {canWrite && group.status === "open" && !p.voided && p.payment_source === "realization" && (
                    <button
                      className="text-danger hover:underline"
                      onClick={() => {
                        const reason = window.prompt("Alasan void (jurnal pembalik akan diposting):");
                        if (reason) void run("Pembayaran di-void", () => voidChargePayment(token, p.termin_id, reason));
                      }}
                    >
                      Void
                    </button>
                  )}
                </div>
              ))
            )}
          </div>
          <div>
            <p className="text-xs font-semibold text-text-primary mb-1">Payout Vendor</p>
            {detail.payouts.length === 0 ? (
              <p className="text-xs text-text-tertiary">Belum ada payout.</p>
            ) : (
              detail.payouts.map((p) => (
                <div key={p.id} className="flex items-center gap-3 text-xs py-1 border-b border-border-subtle last:border-0">
                  <Tanggal value={p.date} />
                  <span className="font-medium tabular-nums"><Rupiah value={p.amount} /></span>
                  <span className="text-text-secondary">{p.vendor}</span>
                </div>
              ))
            )}
          </div>
          {detail.settlements.length > 0 && (
            <div>
              <p className="text-xs font-semibold text-text-primary mb-1">Disposisi Dana</p>
              {detail.settlements.map((sSet) => (
                <div key={sSet.id} className="flex items-center gap-3 text-xs py-1 border-b border-border-subtle last:border-0">
                  <Tanggal value={sSet.date} />
                  <span className="font-medium tabular-nums"><Rupiah value={sSet.amount} /></span>
                  <span className="text-text-secondary">
                    {sSet.action === "refund" && "Refund ke customer"}
                    {sSet.action === "transfer_house" && "Dialihkan ke pembayaran harga rumah"}
                    {sSet.action === "transfer_group" && `Dialihkan ke grup #${sSet.target_group_id}`}
                    {sSet.action === "void_payment" && `Void pembayaran #${sSet.voided_termin_id}`}
                  </span>
                  {/* T-1: transfer = dokumen MEMO (MTI), bukan kwitansi. */}
                  {sSet.memo_number && <span className="text-text-tertiary">{sSet.memo_number}</span>}
                  <span className="flex-1" />
                  {sSet.memo_number && <PrintTransferMemoButton token={token} settlementId={sSet.id} />}
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* Modals */}
      {modal?.kind === "pay" && (
        <PaymentModal token={token} group={group} busy={busy}
          onClose={() => setModal(null)}
          onSubmit={(data) => run("Pembayaran dicatat", () => payChargeGroup(token, group.group_id, data))}
        />
      )}
      {modal?.kind === "payout" && (
        <PayoutModal token={token} item={modal.item} busy={busy}
          onClose={() => setModal(null)}
          onSubmit={(data) => run("Payout dicatat", () => recordChargePayout(token, modal.item.item_id, data))}
        />
      )}
      {modal?.kind === "addItem" && (
        <AddItemsModal token={token} group={group} busy={busy}
          onClose={() => setModal(null)}
          onSubmit={(items) => run("Item ditambahkan", () => addChargeItems(token, group.group_id, items))}
        />
      )}
      {modal?.kind === "refund" && (
        <MoneyActionModal
          title="Refund Sisa Titipan"
          hint={`Sisa titipan grup ${formatRupiah(group.residual)}${residualByAccount}. Dana dikembalikan ke customer (Dr akun titipan / Cr Kas), proporsional bila lebih dari satu akun. BUKAN pendapatan.`}
          token={token} withBank busy={busy} maxAmount={residual}
          onClose={() => setModal(null)}
          onSubmit={({ amount, bank, notes }) =>
            run("Refund dicatat", () =>
              refundChargeGroup(token, group.group_id, {
                amount, bank_account_code: bank, notes, idempotency_key: newIdemKey(`rf-${group.group_id}`),
              }))
          }
        />
      )}
      {modal?.kind === "transferHouse" && (
        <MoneyActionModal
          title="Alihkan Sisa Titipan ke Harga Rumah"
          hint={`Sisa titipan ${formatRupiah(group.residual)}. Dana menjadi pembayaran harga rumah (mengurangi outstanding kontrak) lewat pintu pembayaran resmi. Ini TRANSFER INTERNAL — bukan kas masuk baru: tidak ada kwitansi, dokumennya Memo Transfer Internal (MTI) yang bisa dicetak dari Riwayat → Disposisi Dana.`}
          token={token} withBank={false} busy={busy} maxAmount={residual}
          onClose={() => setModal(null)}
          onSubmit={({ amount, notes }) =>
            run("Transfer ke harga rumah dicatat", () =>
              transferChargeToHouse(token, group.group_id, {
                amount, notes, idempotency_key: newIdemKey(`th-${group.group_id}`),
              }))
          }
        />
      )}
      {modal?.kind === "invoice" && (
        <IssueInvoiceModal
          token={token}
          group={group}
          busy={busy}
          onClose={() => setModal(null)}
          onSubmit={(data) =>
            run("Invoice realisasi diterbitkan", async () => {
              const inv = await issueChargeInvoice(token, group.group_id, data);
              // Nominal diambil dari respons backend (recognized_amount), bukan
              // dihitung ulang di browser — yang ditagihkan adalah yang benar-benar
              // diakui di jurnal.
              return `Invoice ${inv.invoice_number} terbit — ${formatRupiah(inv.recognized_amount)} ditagihkan`;
            })
          }
        />
      )}
      {modal?.kind === "transferGroup" && (
        <TransferGroupModal
          token={token} source={group}
          targets={allGroups.filter((g) => g.group_id !== group.group_id && g.status === "open")}
          busy={busy}
          onClose={() => setModal(null)}
          onSubmit={(data) => run("Transfer antar grup dicatat", () => transferChargeToGroup(token, group.group_id, data))}
        />
      )}
    </Card>
  );
}

function SummaryCell({ label, value, strong, hint }: { label: string; value: string; strong?: boolean; hint?: string }) {
  return (
    <div>
      <p className="text-[10px] uppercase tracking-wide text-text-tertiary">{label}</p>
      <p className={`text-sm tabular-nums ${strong ? "font-bold text-text-primary" : "font-medium text-text-secondary"}`}>
        <Rupiah value={value} />
      </p>
      {hint && <p className="text-[10px] text-text-tertiary mt-0.5 leading-snug">{hint}</p>}
    </div>
  );
}

// recognitionBadge — satu kolom, tiga keadaan (W-5). Yang dibedakan bukan
// "sudah bayar atau belum", melainkan APAKAH TAGIHAN INI SUDAH MENJADI PIUTANG:
// item tanpa invoice masih kewajiban titipan, dan tidak boleh terbaca sebagai
// tagihan yang sedang ditagih.
function recognitionBadge(it: ChargeItemSummary) {
  if (it.recognition_status === "unbilled") {
    return (
      <span title="Belum ada invoice — belum menjadi piutang customer">
        <Badge variant="default">Belum ditagihkan</Badge>
      </span>
    );
  }
  if (it.recognition_status === "paid") {
    return <Badge variant="success">Lunas</Badge>;
  }
  return (
    <span title={`Sudah ditagihkan lewat invoice — Piutang Customer ${formatRupiah(it.receivable)}`}>
      <Badge variant="warning">Piutang</Badge>
    </span>
  );
}

// ── Modal: terbitkan invoice realisasi (W-5) ─────────────────────────────────
//
// Menerbitkan invoice BUKAN sekadar mencetak dokumen: pada detik itu juga sisa
// tagihan berpindah dari kewajiban titipan menjadi PIUTANG CUSTOMER di buku
// besar. Karena itu layarnya menyebut angka dan akibatnya lebih dulu, bukan
// menanyakan "lanjutkan?" tanpa isi.

function IssueInvoiceModal({
  token, group, busy, onClose, onSubmit,
}: {
  token: string;
  group: ChargeGroupSummary;
  busy: boolean;
  onClose: () => void;
  onSubmit: (data: { due_date?: string; notes?: string }) => void;
}) {
  void token;
  const [due, setDue] = useState("");
  const [notes, setNotes] = useState("");
  const unbilled = intOf(group.unbilled);
  const dibayarDiMuka = intOf(group.paid);
  const susulan = group.recognized;

  return (
    <Modal
      open
      onClose={onClose}
      title={susulan ? "Terbitkan Invoice Susulan" : "Terbitkan Invoice Biaya Realisasi"}
      size="md"
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button
            loading={busy}
            disabled={unbilled <= 0}
            onClick={() => onSubmit({ due_date: due || undefined, notes: notes.trim() || undefined })}
          >
            Terbitkan
          </Button>
        </>
      }
    >
      <div className="space-y-5">
        <div className="rounded-lg border border-border bg-bg px-4 py-3">
          <p className="text-xs text-text-secondary">Nilai yang akan ditagihkan</p>
          <p className="mt-0.5 text-2xl font-bold text-text-primary tabular">
            <Rupiah value={group.unbilled} />
          </p>
          <p className="mt-2 text-xs text-text-secondary">
            Setelah invoice terbit, nominal ini tercatat sebagai piutang customer
            (Dr Piutang Customer · Cr Titipan Biaya Realisasi) dan muncul di laporan
            piutang serta customer statement. Pembayaran berikutnya melunasi piutang
            ini lebih dulu.
          </p>
        </div>

        {/* Uang yang sudah diterima di muka langsung memotong piutang, jadi
            Piutang Customer yang terbentuk lebih kecil dari nilai yang ditagihkan.
            Selisih itu harus disebut di sini — kalau tidak, admin membaca angka di
            atas sebagai piutang dan bingung melihat nominal lain setelah submit.
            Nilai pastinya dihitung backend; frontend hanya memperingatkan. */}
        {dibayarDiMuka > 0 && (
          <p className="rounded-lg border border-warning/40 bg-warning-bg px-3.5 py-2.5 text-xs text-warning">
            <Rupiah value={group.paid} /> sudah diterima lebih dulu untuk grup ini.
            Pembayaran itu langsung memotong piutang, jadi Piutang Customer yang
            terbentuk lebih kecil dari nilai yang ditagihkan.
          </p>
        )}
        {susulan && (
          <p className="rounded-lg border border-border-subtle bg-border-subtle/60 px-3.5 py-2.5 text-xs text-text-secondary">
            Grup ini sudah pernah ditagihkan. Yang diakui sekarang hanya selisihnya —
            bagian yang sudah menjadi piutang tidak ditagihkan dua kali.
          </p>
        )}

        <FormGrid>
          <Input
            label="Jatuh tempo (opsional)"
            type="date"
            value={due}
            onChange={(e) => setDue(e.target.value)}
            hint="Kosongkan untuk memakai jatuh tempo bawaan tagihan."
          />
          <Input
            label="Catatan invoice (opsional)"
            value={notes}
            onChange={(e) => setNotes(e.target.value)}
            placeholder="mis. Tagihan biaya realisasi sebelum serah terima"
          />
        </FormGrid>
      </div>
    </Modal>
  );
}

// ── Modal: buat grup ─────────────────────────────────────────────────────────

function CreateGroupModal({
  token, contractId, onClose, onCreated,
}: {
  token: string; contractId: number; onClose: () => void; onCreated: () => void;
}) {
  const { toast } = useToast();
  const [kind, setKind] = useState<ChargeKind>("realization");
  const [label, setLabel] = useState("Biaya Realisasi");
  // W-1: baris kosong, bukan lagi tiga nama hardcode. Jenis biaya yang tersedia
  // adalah milik tenant (master), bukan asumsi frontend.
  const [items, setItems] = useState<{ code: string; label: string; amount: string; due_date: string }[]>([
    { code: "", label: "", amount: "", due_date: "" },
    { code: "", label: "", amount: "", due_date: "" },
  ]);
  const [busy, setBusy] = useState(false);
  const { active: chargeTypes, loaded: typesLoaded } = useChargeTypes(token, true);
  const { active: addonProducts, loaded: productsLoaded } = useAddonProducts(token, true);
  const isRealization = kind === "realization";

  function setItem(i: number, k: "code" | "label" | "amount" | "due_date", v: string) {
    setItems((arr) => arr.map((it, idx) => (idx === i ? { ...it, [k]: v } : it)));
  }

  // Memilih jenis biaya sekaligus mengunci label item: satu kode, satu nama —
  // label disimpan sebagai snapshot agar tagihan lama tidak ikut berubah kalau
  // master di-rename kemudian.
  function setItemType(i: number, code: string) {
    const t = chargeTypes.find((x) => x.code === code);
    setItems((arr) => arr.map((it, idx) => (idx === i ? { ...it, code, label: t?.name ?? "" } : it)));
  }

  // W-13: pola yang sama untuk produk tambahan — kode menunjuk master, label
  // adalah snapshot namanya saat tagihan dibuat.
  function setItemProduct(i: number, code: string) {
    const p = addonProducts.find((x) => x.code === code);
    setItems((arr) => arr.map((it, idx) => (idx === i ? { ...it, code, label: p?.name ?? "" } : it)));
  }

  // Akun kewajiban yang akan dikredit — ikut jenis biaya tiap item, jadi bisa
  // lebih dari satu. Ditampilkan supaya admin tahu uangnya mendarat di mana,
  // bukan untuk membatasi pilihan.
  const usedAccounts = useMemo(
    () => (isRealization ? depositAccountsOf(items.map((it) => it.code), chargeTypes) : []),
    [isRealization, items, chargeTypes],
  );

  // Akun pendapatan yang akan dikredit saat serah terima — ikut produk tiap item.
  const usedRevenue = useMemo(
    () => (isRealization ? [] : revenueAccountsOf(items.map((it) => it.code), addonProducts)),
    [isRealization, items, addonProducts],
  );

  // Kedua jenis kini sama-sama menuntut kode master; yang berbeda hanya
  // masternya. Tidak ada lagi cabang "kalau addon, cukup labelnya terisi".
  const filled = items.filter((it) => intOf(it.amount) > 0 && it.code !== "");
  const total = filled.reduce((acc, it) => acc + intOf(it.amount), 0);

  async function submit() {
    setBusy(true);
    try {
      await createChargeGroup(token, {
        sale_contract_id: contractId,
        kind,
        label: label.trim(),
        items: filled.map((it): ChargeItemInput => ({
          label: it.label.trim(),
          amount: String(intOf(it.amount)),
          due_date: it.due_date || undefined,
          charge_type_code: isRealization ? it.code : undefined,
          product_code: isRealization ? undefined : it.code,
        })),
      });
      toast("Grup tagihan dibuat", "success");
      onCreated();
    } catch (err) {
      toast(err instanceof Error ? err.message : "Gagal membuat grup", "error");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal open onClose={onClose} title="Buat Grup Tagihan" size="xl"
      description="Grup mengelompokkan tagihan di luar harga rumah — biaya realisasi (titipan pihak ketiga) atau produk tambahan."
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button onClick={submit} loading={busy} disabled={filled.length === 0 || !label.trim()}>
            Buat ({filled.length} item — Rp {total.toLocaleString("id-ID")})
          </Button>
        </>
      }
    >
      <div className="space-y-5">
        <FormGrid>
          <Select label="Jenis" value={kind} onChange={(e) => {
            const k = e.target.value as ChargeKind;
            setKind(k);
            setLabel(k === "realization" ? "Biaya Realisasi" : "Produk Tambahan");
          }}>
            <option value="realization">Biaya Realisasi (titipan)</option>
            <option value="addon">Produk Tambahan (PDAM, dll.)</option>
          </Select>
          <Input label="Nama Grup" required value={label} onChange={(e) => setLabel(e.target.value)} />
          {isRealization && (
            <FormFull>
              <p className="rounded-lg border border-warning/30 bg-warning-bg px-3.5 py-2.5 text-xs text-warning">
                Semua pembayaran grup ini dicatat sebagai <strong>Titipan Realisasi</strong>
                {usedAccounts.length > 0 && (
                  <> (akun <span className="font-mono">{usedAccounts.join(", ")}</span>)</>
                )} — bukan pendapatan, dan tidak mengurangi harga rumah.
              </p>
            </FormFull>
          )}
          {isRealization && typesLoaded && chargeTypes.length === 0 && (
            <FormFull><ChargeTypesEmptyNotice /></FormFull>
          )}
          {!isRealization && (
            <FormFull>
              <p className="rounded-lg border border-accent/30 bg-accent-light px-3.5 py-2.5 text-xs text-text-secondary">
                Produk tambahan adalah barang yang <strong>dijual perusahaan</strong>. Pembayaran
                masuk sebagai <strong>Uang Muka Penjualan</strong> (kewajiban), lalu diakui menjadi{" "}
                <strong>pendapatan</strong>
                {usedRevenue.length > 0 && (
                  <> pada akun <span className="font-mono">{usedRevenue.join(", ")}</span></>
                )}{" "}
                saat serah terima — berbeda dari biaya realisasi yang tidak pernah menjadi pendapatan.
              </p>
            </FormFull>
          )}
          {!isRealization && productsLoaded && addonProducts.length === 0 && (
            <FormFull><AddonProductsEmptyNotice /></FormFull>
          )}
        </FormGrid>

        <div>
          <p className="mb-2 text-sm font-medium text-text-primary">Item Tagihan</p>
          <div className="space-y-2.5">
            {items.map((it, i) => (
              <div key={i} className="grid grid-cols-12 gap-3 items-end">
                <div className="col-span-12 sm:col-span-4">
                  {isRealization ? (
                    <ChargeTypeSelect
                      label={i === 0 ? "Jenis Biaya" : undefined}
                      types={chargeTypes}
                      value={it.code}
                      onChange={(code) => setItemType(i, code)}
                    />
                  ) : (
                    <AddonProductSelect
                      label={i === 0 ? "Produk" : undefined}
                      products={addonProducts}
                      value={it.code}
                      onChange={(code) => setItemProduct(i, code)}
                    />
                  )}
                </div>
                <div className="col-span-6 sm:col-span-4">
                  <RupiahInput label={i === 0 ? "Jumlah" : undefined} value={it.amount}
                    onChange={(v) => setItem(i, "amount", v)} />
                </div>
                <div className="col-span-5 sm:col-span-3">
                  <Input label={i === 0 ? "Jatuh Tempo (ops.)" : undefined} type="date" value={it.due_date}
                    onChange={(e) => setItem(i, "due_date", e.target.value)} />
                </div>
                <div className="col-span-1">
                  <button type="button" aria-label={`Hapus baris ${i + 1}`}
                    className="flex h-[42px] w-full items-center justify-center rounded-lg text-text-tertiary transition-colors hover:bg-danger-bg hover:text-danger"
                    onClick={() => setItems((arr) => arr.filter((_, idx) => idx !== i))}>
                    <svg viewBox="0 0 20 20" className="h-4 w-4" fill="none" stroke="currentColor"
                      strokeWidth="1.6" strokeLinecap="round" aria-hidden="true">
                      <path d="M5 5l10 10M15 5L5 15" />
                    </svg>
                  </button>
                </div>
              </div>
            ))}
          </div>
          <button type="button"
            className="mt-3 rounded-md px-2 py-1 text-xs font-medium text-accent transition-colors hover:bg-accent-light"
            onClick={() => setItems((arr) => [...arr, { code: "", label: "", amount: "", due_date: "" }])}>
            + Tambah baris
          </button>
          {isRealization && (
            <p className="mt-2 text-xs text-text-tertiary">
              Jenis biaya diambil dari master (Pengaturan → Biaya Realisasi). Satu grup hanya boleh
              memakai <strong>satu</strong> akun titipan — jenis berbeda-akun dibuatkan grup sendiri.
            </p>
          )}
        </div>
      </div>
    </Modal>
  );
}

// ── Modal: pembayaran dengan alokasi MANUAL admin (K-3) ──────────────────────

function PaymentModal({
  token, group, busy, onClose, onSubmit,
}: {
  token: string;
  group: ChargeGroupSummary;
  busy: boolean;
  onClose: () => void;
  onSubmit: (data: {
    amount: string; date?: string; bank_account_code: string;
    allocations: { charge_item_id: number; amount: string }[];
    reference?: string; notes?: string; idempotency_key?: string;
  }) => void;
}) {
  const openItems = group.items.filter((it) => it.status === "open" && intOf(it.outstanding) > 0);
  const [amount, setAmount] = useState("");
  const [date, setDate] = useState(() => todayLocalStr());
  const [bank, setBank] = useState("");
  const [reference, setReference] = useState("");
  const [notes, setNotes] = useState("");
  const [alloc, setAlloc] = useState<Record<number, string>>({});
  const [idemKey] = useState(() => newIdemKey(`pay-${group.group_id}`));

  const amountInt = intOf(amount);
  const allocSum = useMemo(
    () => openItems.reduce((acc, it) => acc + intOf(alloc[it.item_id] ?? ""), 0),
    [openItems, alloc],
  );
  const remainder = amountInt - allocSum;
  const overItem = openItems.find((it) => intOf(alloc[it.item_id] ?? "") > intOf(it.outstanding));
  const amountErr = validateRupiah(amount);
  const valid = !amountErr && amountInt > 0 && remainder === 0 && !overItem && bank !== "";

  return (
    <Modal open onClose={onClose} title="Terima Pembayaran — Alokasi Ditentukan Admin" size="xl"
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button loading={busy} disabled={!valid}
            onClick={() =>
              onSubmit({
                amount: String(amountInt),
                date,
                bank_account_code: bank,
                allocations: openItems
                  .filter((it) => intOf(alloc[it.item_id] ?? "") > 0)
                  .map((it) => ({ charge_item_id: it.item_id, amount: String(intOf(alloc[it.item_id])) })),
                reference: reference || undefined,
                notes: notes || undefined,
                idempotency_key: idemKey,
              })
            }
          >
            Simpan &amp; Cetak KWR
          </Button>
        </>
      }
    >
      <div className="space-y-5">
        <FormGrid columns={3}>
          <RupiahInput label="Nominal Diterima" required value={amount} onChange={setAmount} error={amountErr || undefined} />
          <Input label="Tanggal" type="date" required value={date} onChange={(e) => setDate(e.target.value)} />
          <CashBankSelect token={token} value={bank} onChange={setBank} />
        </FormGrid>

        <div>
          <div className="mb-2 flex items-baseline justify-between gap-3">
            <p className="text-sm font-medium text-text-primary">Alokasi per Item</p>
            <p className="text-xs text-text-tertiary">
              Keputusan admin — tidak ada alokasi otomatis.
            </p>
          </div>
          <div className="rounded-lg border border-border divide-y divide-border-subtle overflow-hidden">
            {openItems.map((it) => (
              <div key={it.item_id} className="flex items-center gap-4 px-4 py-3">
                <div className="flex-1 min-w-0">
                  <p className="text-sm text-text-primary truncate">{it.label}</p>
                  <p className="text-xs text-text-tertiary">
                    Sisa tagihan: <Rupiah value={it.outstanding} />
                  </p>
                </div>
                <div className="w-44 shrink-0">
                  <RupiahInput value={alloc[it.item_id] ?? ""}
                    onChange={(v) => setAlloc((a) => ({ ...a, [it.item_id]: v }))} />
                </div>
                <button type="button"
                  className="shrink-0 rounded-md px-2 py-1 text-xs font-medium text-accent transition-colors hover:bg-accent-light"
                  onClick={() => setAlloc((a) => ({ ...a, [it.item_id]: String(Math.min(intOf(it.outstanding), Math.max(remainder + intOf(a[it.item_id] ?? ""), 0))) }))}>
                  Isi sisa
                </button>
              </div>
            ))}
          </div>
          {/* Tiga keadaan, bukan dua: sebelum nominal diisi form belum salah —
              ia baru belum lengkap. Banner kuning saat modal baru dibuka
              membuat layar terasa menuduh sebelum apa pun dikerjakan. */}
          <div className={`mt-3 rounded-lg border px-3.5 py-2.5 text-xs ${
            overItem
              ? "border-danger/30 bg-danger-bg text-danger"
              : amountInt === 0
                ? "border-border bg-border-subtle/60 text-text-secondary"
                : remainder === 0
                  ? "border-success/30 bg-success-bg text-success"
                  : "border-warning/30 bg-warning-bg text-warning"
          }`}>
            {overItem
              ? `Alokasi ${overItem.label} melebihi sisa tagihannya.`
              : amountInt === 0
                ? "Isi nominal diterima, lalu bagikan ke item di atas. Total alokasi wajib sama persis dengan nominal."
                : remainder === 0
                  ? "Total alokasi sama dengan nominal — siap disimpan."
                  : `Belum teralokasi: Rp ${remainder.toLocaleString("id-ID")} dari Rp ${amountInt.toLocaleString("id-ID")}.`}
          </div>
        </div>

        <FormGrid>
          <Input label="Referensi (ops.)" value={reference} onChange={(e) => setReference(e.target.value)} placeholder="No. transfer" />
          <Input label="Catatan (ops.)" value={notes} onChange={(e) => setNotes(e.target.value)} />
        </FormGrid>
      </div>
    </Modal>
  );
}

// ── Modal: payout vendor ─────────────────────────────────────────────────────

function PayoutModal({
  token, item, busy, onClose, onSubmit,
}: {
  token: string;
  item: ChargeItemSummary;
  busy: boolean;
  onClose: () => void;
  onSubmit: (data: { amount: string; date?: string; bank_account_code: string; vendor: string; notes?: string; idempotency_key?: string }) => void;
}) {
  const [amount, setAmount] = useState("");
  const [date, setDate] = useState(() => todayLocalStr());
  const [bank, setBank] = useState("");
  const [vendor, setVendor] = useState("");
  const [notes, setNotes] = useState("");
  const [idemKey] = useState(() => newIdemKey(`po-${item.item_id}`));
  const amountInt = intOf(amount);
  const willRaise = amountInt + intOf(item.payout) > intOf(item.amount);

  return (
    <Modal open onClose={onClose} title={`Payout Vendor — ${item.label}`} size="md"
      description="Dana titipan yang sudah diterima dari customer diteruskan ke vendor/penerima."
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button loading={busy} disabled={amountInt <= 0 || !vendor.trim() || !bank}
            onClick={() => onSubmit({
              amount: String(amountInt), date, bank_account_code: bank,
              vendor: vendor.trim(), notes: notes || undefined, idempotency_key: idemKey,
            })}
          >
            Bayarkan
          </Button>
        </>
      }
    >
      <FormGrid>
        <RupiahInput label="Jumlah Dibayarkan" required value={amount} onChange={setAmount}
          hint={`Tagihan item ${formatRupiah(item.amount)} · sudah payout ${formatRupiah(item.payout)}`} />
        <Input label="Vendor / Penerima" required value={vendor} onChange={(e) => setVendor(e.target.value)} placeholder="nama vendor atau penerima dana" />
        <Input label="Tanggal" type="date" value={date} onChange={(e) => setDate(e.target.value)} />
        <CashBankSelect token={token} value={bank} onChange={setBank} label="Dibayar dari" />
        <FormFull>
          <Input label="Catatan (ops.)" value={notes} onChange={(e) => setNotes(e.target.value)} />
        </FormFull>
        {willRaise && (
          <FormFull>
            <p className="rounded-lg border border-warning/30 bg-warning-bg px-3.5 py-2.5 text-xs text-warning">
              Biaya aktual melebihi tagihan — sistem otomatis menaikkan tagihan item ini dan
              customer memiliki outstanding tambahan (aturan K-5).
            </p>
          </FormFull>
        )}
        <FormFull>
          <p className="rounded-lg border border-border-subtle bg-border-subtle/60 px-3.5 py-2.5 text-xs text-text-secondary">
            Jurnal: Dr Titipan Realisasi{item.deposit_account_code ? ` (${item.deposit_account_code})` : ""} · Cr Kas/Bank.
          </p>
        </FormFull>
      </FormGrid>
    </Modal>
  );
}

// ── Modal: tambah item ───────────────────────────────────────────────────────

function AddItemsModal({
  token, group, busy, onClose, onSubmit,
}: {
  token: string; group: ChargeGroupSummary; busy: boolean;
  onClose: () => void; onSubmit: (items: ChargeItemInput[]) => void;
}) {
  const [code, setCode] = useState("");
  const [label, setLabel] = useState("");
  const [amount, setAmount] = useState("");
  const [due, setDue] = useState("");
  const { all, active: chargeTypes, loaded } = useChargeTypes(token, true);
  const { all: allProducts, active: addonProducts, loaded: productsLoaded } = useAddonProducts(token, true);
  const isRealization = group.kind === "realization";
  const amountInt = intOf(amount);

  // Akun kewajiban yang sudah dipakai grup ini — informasi, bukan pembatas.
  // Jenis biaya berakun lain boleh ikut masuk; jurnalnya terpecah per akun.
  const groupAccounts = isRealization
    ? depositAccountsOf(group.items.map((it) => it.charge_type_code), all)
    : [];

  // Akun pendapatan yang sudah dipakai grup addon ini — informasi, bukan pembatas.
  const groupRevenue = isRealization
    ? []
    : revenueAccountsOf(group.items.map((it) => it.product_code), allProducts);

  function pick(c: string) {
    setCode(c);
    const master = isRealization ? chargeTypes : addonProducts;
    setLabel(master.find((x) => x.code === c)?.name ?? "");
  }

  const ready = amountInt > 0 && code !== "";

  return (
    <Modal open onClose={onClose} title="Tambah Item Tagihan" size="md"
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button loading={busy} disabled={!ready}
            onClick={() => onSubmit([{
              label: label.trim(),
              amount: String(amountInt),
              due_date: due || undefined,
              charge_type_code: isRealization ? code : undefined,
              product_code: isRealization ? undefined : code,
            }])}>
            Tambah
          </Button>
        </>
      }
    >
      <FormGrid>
        {isRealization ? (
          <>
            {loaded && chargeTypes.length === 0 && (
              <FormFull><ChargeTypesEmptyNotice /></FormFull>
            )}
            <FormFull>
              <ChargeTypeSelect label="Jenis Biaya" types={chargeTypes} value={code} onChange={pick} />
            </FormFull>
            {groupAccounts.length > 0 && (
              <FormFull>
                <p className="-mt-1 text-xs text-text-tertiary">
                  Grup ini memakai akun kewajiban{" "}
                  <span className="font-mono">{groupAccounts.join(", ")}</span>. Jenis biaya dengan
                  akun lain tetap boleh ditambahkan — jurnalnya terpecah per akun.
                </p>
              </FormFull>
            )}
          </>
        ) : (
          <>
            {productsLoaded && addonProducts.length === 0 && (
              <FormFull><AddonProductsEmptyNotice /></FormFull>
            )}
            <FormFull>
              <AddonProductSelect label="Produk" products={addonProducts} value={code} onChange={pick} />
            </FormFull>
            <FormFull>
              <p className="-mt-1 text-xs text-text-tertiary">
                Diakui sebagai pendapatan
                {groupRevenue.length > 0 && (
                  <> pada akun <span className="font-mono">{groupRevenue.join(", ")}</span></>
                )}{" "}
                saat serah terima. Sebelum itu, pembayarannya adalah Uang Muka Penjualan.
              </p>
            </FormFull>
          </>
        )}
        <RupiahInput label="Jumlah" required value={amount} onChange={setAmount} />
        <Input label="Jatuh Tempo (ops.)" type="date" value={due} onChange={(e) => setDue(e.target.value)} />
      </FormGrid>
    </Modal>
  );
}

// ── Modal generik aksi uang (refund / transfer rumah) ────────────────────────

function MoneyActionModal({
  title, hint, token, withBank, busy, maxAmount, onClose, onSubmit,
}: {
  title: string;
  hint: string;
  token: string;
  withBank: boolean;
  busy: boolean;
  maxAmount: number;
  onClose: () => void;
  onSubmit: (data: { amount: string; bank: string; notes?: string }) => void;
}) {
  const [amount, setAmount] = useState(String(maxAmount));
  const [bank, setBank] = useState("");
  const [notes, setNotes] = useState("");
  const amountInt = intOf(amount);
  const valid = amountInt > 0 && amountInt <= maxAmount && (!withBank || bank !== "");
  return (
    <Modal open onClose={onClose} title={title} size="md" description={hint}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button loading={busy} disabled={!valid}
            onClick={() => onSubmit({ amount: String(amountInt), bank, notes: notes || undefined })}>
            Proses
          </Button>
        </>
      }
    >
      <FormGrid>
        <RupiahInput label="Jumlah" required value={amount} onChange={setAmount}
          error={amountInt > maxAmount ? "Melebihi sisa titipan" : undefined} />
        {withBank && <CashBankSelect token={token} value={bank} onChange={setBank} label="Dikembalikan via" />}
        <FormFull>
          <Input label="Catatan (ops.)" value={notes} onChange={(e) => setNotes(e.target.value)} />
        </FormFull>
      </FormGrid>
    </Modal>
  );
}

// ── Modal: transfer antar grup (alokasi manual di grup tujuan — K-3) ─────────

function TransferGroupModal({
  token, source, targets, busy, onClose, onSubmit,
}: {
  token: string;
  source: ChargeGroupSummary;
  targets: ChargeGroupSummary[];
  busy: boolean;
  onClose: () => void;
  onSubmit: (data: {
    target_group_id: number; amount: string; allocations: { charge_item_id: number; amount: string }[];
    notes?: string; idempotency_key?: string;
  }) => void;
}) {
  const residual = intOf(source.residual);
  const [targetId, setTargetId] = useState<number>(targets[0]?.group_id ?? 0);
  const [amount, setAmount] = useState(String(residual));
  const [alloc, setAlloc] = useState<Record<number, string>>({});
  const [notes, setNotes] = useState("");
  const [idemKey] = useState(() => newIdemKey(`tg-${source.group_id}`));

  const target = targets.find((t) => t.group_id === targetId);
  const openItems = (target?.items ?? []).filter((it) => it.status === "open" && intOf(it.outstanding) > 0);
  const amountInt = intOf(amount);
  const allocSum = openItems.reduce((acc, it) => acc + intOf(alloc[it.item_id] ?? ""), 0);
  const remainder = amountInt - allocSum;
  const overItem = openItems.find((it) => intOf(alloc[it.item_id] ?? "") > intOf(it.outstanding));
  const valid = amountInt > 0 && amountInt <= residual && remainder === 0 && !overItem && !!target;

  return (
    <Modal open onClose={onClose} title="Alihkan Sisa Titipan ke Grup Lain" size="xl"
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button loading={busy} disabled={!valid}
            onClick={() => onSubmit({
              target_group_id: targetId,
              amount: String(amountInt),
              allocations: openItems
                .filter((it) => intOf(alloc[it.item_id] ?? "") > 0)
                .map((it) => ({ charge_item_id: it.item_id, amount: String(intOf(alloc[it.item_id])) })),
              notes: notes || undefined,
              idempotency_key: idemKey,
            })}
          >
            Alihkan
          </Button>
        </>
      }
    >
      <div className="space-y-5">
        <FormGrid>
          <Select label="Grup Tujuan" value={String(targetId)} onChange={(e) => { setTargetId(parseInt(e.target.value, 10)); setAlloc({}); }}>
            {targets.map((t) => (
              <option key={t.group_id} value={String(t.group_id)}>{t.label} (sisa {t.outstanding})</option>
            ))}
          </Select>
          <RupiahInput label="Jumlah Dialihkan" required value={amount} onChange={setAmount}
            hint={`Sisa titipan ${source.label}: ${formatRupiah(source.residual)}`}
            error={amountInt > residual ? "Melebihi sisa titipan" : undefined} />
        </FormGrid>

        <div>
          <div className="mb-2 flex items-baseline justify-between gap-3">
            <p className="text-sm font-medium text-text-primary">Alokasi di Grup Tujuan</p>
            <p className="text-xs text-text-tertiary">Ditentukan admin — tanpa alokasi otomatis.</p>
          </div>
          {openItems.length === 0 ? (
            <p className="rounded-lg border border-warning/30 bg-warning-bg px-3.5 py-2.5 text-xs text-warning">
              Grup tujuan tidak punya item dengan sisa tagihan.
            </p>
          ) : (
            <div className="rounded-lg border border-border divide-y divide-border-subtle overflow-hidden">
              {openItems.map((it) => (
                <div key={it.item_id} className="flex items-center gap-4 px-4 py-3">
                  <div className="flex-1 min-w-0">
                    <p className="text-sm text-text-primary truncate">{it.label}</p>
                    <p className="text-xs text-text-tertiary">Sisa: <Rupiah value={it.outstanding} /></p>
                  </div>
                  <div className="w-44 shrink-0">
                    <RupiahInput value={alloc[it.item_id] ?? ""} onChange={(v) => setAlloc((a) => ({ ...a, [it.item_id]: v }))} />
                  </div>
                </div>
              ))}
            </div>
          )}
          <div className={`mt-3 rounded-lg border px-3.5 py-2.5 text-xs ${
            overItem
              ? "border-danger/30 bg-danger-bg text-danger"
              : amountInt === 0
                ? "border-border bg-border-subtle/60 text-text-secondary"
                : remainder === 0
                  ? "border-success/30 bg-success-bg text-success"
                  : "border-warning/30 bg-warning-bg text-warning"
          }`}>
            {overItem
              ? `Alokasi ${overItem.label} melebihi sisa tagihannya.`
              : amountInt === 0
                ? "Isi jumlah yang dialihkan, lalu bagikan ke item grup tujuan."
                : remainder === 0
                  ? "Total alokasi sama dengan jumlah — siap dialihkan."
                  : `Belum teralokasi: Rp ${remainder.toLocaleString("id-ID")} dari Rp ${amountInt.toLocaleString("id-ID")}.`}
          </div>
        </div>

        <FormGrid>
          <FormFull>
            <Input label="Catatan (ops.)" value={notes} onChange={(e) => setNotes(e.target.value)} />
          </FormFull>
          <FormFull>
            <p className="rounded-lg border border-border-subtle bg-border-subtle/60 px-3.5 py-2.5 text-xs text-text-secondary">
              Transfer internal — bukan kas masuk baru. Tidak ada kwitansi; dokumennya
              Memo Transfer Internal (MTI), bisa dicetak dari Riwayat → Disposisi Dana.
            </p>
          </FormFull>
        </FormGrid>
      </div>
    </Modal>
  );
}
