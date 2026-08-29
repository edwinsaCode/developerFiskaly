"use client";

// Increment 8 — Pusat Pembatalan & Refund (docs/increment-8-frontend-spec.md X1/X3/X4).
// Tab Pembatalan: daftar + drawer detail (Journal Preview + approve/reject/process).
// Tab Refund: daftar pending/paid + aksi bayar.

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { Card } from "@/components/ui/Card";
import { Button } from "@/components/ui/Button";
import { Badge } from "@/components/ui/Badge";
import { Modal } from "@/components/ui/Modal";
import { Input } from "@/components/ui/Input";
import { EmptyState } from "@/components/ui/EmptyState";
import { Rupiah } from "@/components/format/Rupiah";
import { Tanggal } from "@/components/format/Tanggal";
import { CashBankSelect } from "@/components/accounting/CashBankSelect";
import { useToast } from "@/components/ui/Toast";
import { ApiError } from "@/lib/api/client";
import { useUnitMap } from "@/lib/hooks/useUnitMap";
import {
  fetchCancellations,
  fetchCancellationPreview,
  approveCancellation,
  rejectCancellation,
  processCancellation,
  fetchRefunds,
  payRefund,
  type Cancellation,
  type CancellationStatus,
  type ProcessPreview,
  type Refund,
} from "@/lib/api/cancellation";

// ── badges ────────────────────────────────────────────────────────────────────

function CxStatusBadge({ s }: { s: CancellationStatus }) {
  const map: Record<CancellationStatus, { v: "warning" | "accent" | "success" | "neutral"; label: string }> = {
    requested: { v: "warning", label: "Menunggu Persetujuan" },
    approved:  { v: "accent",  label: "Siap Diproses" },
    processed: { v: "success", label: "Selesai" },
    rejected:  { v: "neutral", label: "Ditolak" },
  };
  const m = map[s];
  return <Badge variant={m.v}>{m.label}</Badge>;
}

function StageChip({ stage }: { stage: string }) {
  return stage === "post_bast"
    ? <Badge variant="danger">Pasca-BAST</Badge>
    : <Badge variant="default">Pra-BAST</Badge>;
}

// ── main ──────────────────────────────────────────────────────────────────────

interface Props {
  token: string;
  initialTab?: "cancellation" | "refund";
  openId?: number; // buka drawer cancellation tertentu (dari ?open=)
}

export function CancellationCenter({ token, initialTab = "cancellation", openId }: Props) {
  const { toast } = useToast();
  const { unitMap } = useUnitMap(token);

  const [tab, setTab] = useState<"cancellation" | "refund">(initialTab);
  const [rows, setRows] = useState<Cancellation[]>([]);
  const [refunds, setRefunds] = useState<Refund[]>([]);
  const [loading, setLoading] = useState(true);

  const [detail, setDetail] = useState<Cancellation | null>(null);
  const [preview, setPreview] = useState<ProcessPreview | null>(null);
  const [previewLoading, setPreviewLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [rejectMode, setRejectMode] = useState(false);
  const [rejectReason, setRejectReason] = useState("");

  const [payTarget, setPayTarget] = useState<Refund | null>(null);
  const [payBank, setPayBank] = useState("");

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const [cs, rs] = await Promise.all([
        fetchCancellations(token),
        fetchRefunds(token),
      ]);
      setRows(cs);
      setRefunds(rs);
    } catch {
      toast("Gagal memuat data pembatalan", "error");
    } finally {
      setLoading(false);
    }
  }, [token, toast]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  // Buka drawer dari ?open= (deep-link dari wizard pengajuan).
  useEffect(() => {
    if (openId && rows.length > 0 && !detail) {
      const c = rows.find((r) => r.id === openId);
      if (c) openDetail(c);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [openId, rows]);

  async function openDetail(c: Cancellation) {
    setDetail(c);
    setPreview(null);
    setRejectMode(false);
    if (c.status === "requested" || c.status === "approved") {
      setPreviewLoading(true);
      try {
        setPreview(await fetchCancellationPreview(token, c.id));
      } catch (err) {
        toast(err instanceof ApiError ? err.message : "Gagal memuat pratinjau", "error");
      } finally {
        setPreviewLoading(false);
      }
    }
  }

  async function act(fn: () => Promise<unknown>, okMsg: string) {
    setBusy(true);
    try {
      await fn();
      toast(okMsg, "success");
      setDetail(null);
      await refresh();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal memproses", "error");
    } finally {
      setBusy(false);
    }
  }

  async function handlePayRefund() {
    if (!payTarget || !payBank) return;
    setBusy(true);
    try {
      await payRefund(token, payTarget.id, payBank);
      toast("Refund dibayar — jurnal kas keluar diposting", "success");
      setPayTarget(null);
      await refresh();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal membayar refund", "error");
    } finally {
      setBusy(false);
    }
  }

  const pendingRefunds = refunds.filter((r) => r.status === "pending");

  return (
    <div className="space-y-4">
      <div>
        <h1 className="display-lg text-2xl text-text-primary">Pembatalan &amp; Refund</h1>
        <p className="text-sm text-text-secondary mt-0.5">
          Pembatalan pemesanan/penjualan dengan jurnal pembalik otomatis; refund dana buyer.
        </p>
      </div>

      {/* Tabs */}
      <div className="flex gap-1 border-b border-border">
        {([
          { key: "cancellation", label: `Pembatalan (${rows.length})` },
          { key: "refund", label: `Refund${pendingRefunds.length ? ` (${pendingRefunds.length} belum dibayar)` : ""}` },
        ] as const).map((t) => (
          <button
            key={t.key}
            onClick={() => setTab(t.key)}
            className={`px-3 py-2 text-sm border-b-2 -mb-px transition-colors
              ${tab === t.key
                ? "border-accent text-accent font-medium"
                : "border-transparent text-text-secondary hover:text-accent"}`}
          >
            {t.label}
          </button>
        ))}
      </div>

      {loading ? (
        <Card padding="sm">
          <div className="animate-pulse space-y-2">
            {[...Array(3)].map((_, i) => <div key={i} className="h-9 bg-border-subtle rounded" />)}
          </div>
        </Card>
      ) : tab === "cancellation" ? (
        rows.length === 0 ? (
          <EmptyState
            title="Belum ada pembatalan"
            description="Pembatalan menangani buyer yang mundur — dana dikembalikan (dikurangi penalti) dan unit kembali ke stok, dengan seluruh jurnal dibuat otomatis. Ajukan dari halaman unit."
          />
        ) : (
          <Card padding="sm" className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-left text-xs text-text-secondary border-b border-border">
                  <th className="py-2 pr-3">Unit</th>
                  <th className="py-2 pr-3">Tahap</th>
                  <th className="py-2 pr-3">Alasan</th>
                  <th className="py-2 pr-3 text-right">Penalti</th>
                  <th className="py-2 pr-3 text-right">Refund</th>
                  <th className="py-2 pr-3">Status</th>
                  <th className="py-2" />
                </tr>
              </thead>
              <tbody>
                {rows.map((c) => {
                  const u = unitMap[c.unit_id];
                  return (
                    <tr key={c.id} className="border-b border-border last:border-0">
                      <td className="py-2 pr-3">
                        <Link href={`/penjualan/${c.unit_id}`} className="text-accent hover:underline font-medium">
                          {u?.code ?? `#${c.unit_id}`}
                        </Link>
                        {u && <span className="block text-xs text-text-secondary">{u.projectName}</span>}
                      </td>
                      <td className="py-2 pr-3"><StageChip stage={c.stage} /></td>
                      <td className="py-2 pr-3 max-w-[180px] truncate" title={c.reason}>{c.reason}</td>
                      <td className="py-2 pr-3 text-right"><Rupiah value={c.penalty} colorSign={false} /></td>
                      <td className="py-2 pr-3 text-right"><Rupiah value={c.refund_amount} colorSign={false} /></td>
                      <td className="py-2 pr-3"><CxStatusBadge s={c.status} /></td>
                      <td className="py-2 text-right">
                        <Button size="sm" variant="secondary" onClick={() => openDetail(c)}>
                          {c.status === "requested" || c.status === "approved" ? "Tinjau" : "Detail"}
                        </Button>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </Card>
        )
      ) : pendingRefunds.length === 0 && refunds.length === 0 ? (
        <EmptyState
          title="Belum ada refund"
          description="Refund muncul dari pembatalan kontrak (sisa dana buyer) atau baris booking legacy pending-refund. Booking baru: fee = Pendapatan Booking, tidak ada refund."
        />
      ) : (
        <Card padding="sm" className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs text-text-secondary border-b border-border">
                <th className="py-2 pr-3">Penerima</th>
                <th className="py-2 pr-3">Sumber</th>
                <th className="py-2 pr-3">Unit</th>
                <th className="py-2 pr-3 text-right">Jumlah</th>
                <th className="py-2 pr-3">Status</th>
                <th className="py-2" />
              </tr>
            </thead>
            <tbody>
              {refunds.map((r) => {
                const u = unitMap[r.unit_id];
                return (
                  <tr key={r.id} className="border-b border-border last:border-0">
                    <td className="py-2 pr-3 font-medium">{r.payee || "—"}</td>
                    <td className="py-2 pr-3 text-xs">
                      {r.source_type === "booking" ? `Booking #${r.booking_id}` : `Pembatalan #${r.cancellation_id}`}
                    </td>
                    <td className="py-2 pr-3">{u?.code ?? `#${r.unit_id}`}</td>
                    <td className="py-2 pr-3 text-right font-medium"><Rupiah value={r.amount} colorSign={false} /></td>
                    <td className="py-2 pr-3">
                      {r.status === "pending"
                        ? <Badge variant="warning">Belum Dibayar</Badge>
                        : r.status === "paid"
                          ? <Badge variant="success">Dibayar</Badge>
                          : <Badge variant="neutral">Batal</Badge>}
                    </td>
                    <td className="py-2 text-right">
                      {r.status === "pending" ? (
                        <Button size="sm" onClick={() => { setPayTarget(r); setPayBank(""); }}>
                          Bayar
                        </Button>
                      ) : r.payment_journal_id ? (
                        <Link href={`/accounting/jurnal/${r.payment_journal_id}`}
                          className="text-xs text-accent hover:underline">
                          jurnal
                        </Link>
                      ) : null}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </Card>
      )}

      {/* ── Drawer detail cancellation ── */}
      <Modal
        open={!!detail}
        onClose={() => setDetail(null)}
        title={detail ? `Pembatalan #${detail.id} — ${unitMap[detail.unit_id]?.code ?? `Unit #${detail.unit_id}`}` : ""}
        size="lg"
        footer={
          detail && (detail.status === "requested" || detail.status === "approved") ? (
            <>
              {!rejectMode && (
                <Button variant="secondary" onClick={() => setRejectMode(true)} disabled={busy}>
                  Tolak…
                </Button>
              )}
              {rejectMode ? (
                <>
                  <Button variant="secondary" onClick={() => setRejectMode(false)} disabled={busy}>
                    Kembali
                  </Button>
                  <Button variant="danger" loading={busy}
                    onClick={() => act(() => rejectCancellation(token, detail.id, rejectReason || "ditolak"), "Pembatalan ditolak")}>
                    Konfirmasi Tolak
                  </Button>
                </>
              ) : detail.status === "requested" ? (
                <Button loading={busy}
                  onClick={() => act(() => approveCancellation(token, detail.id), "Disetujui — siap diproses")}>
                  Setujui
                </Button>
              ) : (
                <Button variant="danger" loading={busy}
                  onClick={() => act(() => processCancellation(token, detail.id), "Pembatalan diproses — jurnal diposting, unit kembali tersedia")}>
                  Proses Sekarang
                </Button>
              )}
            </>
          ) : (
            <Button variant="secondary" onClick={() => setDetail(null)}>Tutup</Button>
          )
        }
      >
        {detail && (
          <div className="space-y-4">
            <div className="flex items-center gap-2 flex-wrap">
              <CxStatusBadge s={detail.status} />
              <StageChip stage={detail.stage} />
              <span className="text-xs text-text-secondary">
                Diajukan <Tanggal value={detail.created_at} /> · kejadian <Tanggal value={detail.event_date} />
              </span>
            </div>
            <p className="text-sm"><span className="text-text-secondary">Alasan:</span> {detail.reason}</p>
            {detail.status === "rejected" && detail.reject_reason && (
              <p className="text-sm text-danger">Ditolak: {detail.reject_reason}</p>
            )}

            {rejectMode && (
              <Input
                label="Alasan penolakan"
                value={rejectReason}
                onChange={(e) => setRejectReason(e.target.value)}
                placeholder="cth: data belum lengkap"
              />
            )}

            {/* Ringkasan dampak */}
            {previewLoading ? (
              <div className="animate-pulse h-24 bg-border-subtle rounded" />
            ) : preview ? (
              <>
                <div className="grid grid-cols-2 sm:grid-cols-3 gap-3 text-sm rounded border border-border p-3">
                  <div>
                    <p className="text-xs text-text-secondary">Dana diterima buyer</p>
                    <p className="font-medium"><Rupiah value={preview.received_total} colorSign={false} /></p>
                  </div>
                  <div>
                    <p className="text-xs text-text-secondary">Penalti (hangus)</p>
                    <p className="font-medium"><Rupiah value={preview.penalty} colorSign={false} /></p>
                  </div>
                  <div>
                    <p className="text-xs text-text-secondary">Refund ke buyer</p>
                    <p className="font-semibold"><Rupiah value={preview.refund_amount} colorSign={false} /></p>
                  </div>
                  {preview.stage === "post_bast" && (
                    <>
                      <div>
                        <p className="text-xs text-text-secondary">Pendapatan dibatalkan</p>
                        <p className="font-medium text-danger"><Rupiah value={preview.revenue_reversed} colorSign={false} /></p>
                      </div>
                      <div>
                        <p className="text-xs text-text-secondary">HPP dibatalkan</p>
                        <p className="font-medium"><Rupiah value={preview.cogs_reversed} colorSign={false} /></p>
                      </div>
                      <div>
                        <p className="text-xs text-text-secondary">PPh Final dibatalkan</p>
                        <p className="font-medium"><Rupiah value={preview.tax_reversed} colorSign={false} /></p>
                      </div>
                    </>
                  )}
                </div>
                <p className="text-xs text-text-secondary">
                  Unit kembali menjadi <strong>Tersedia</strong>
                  {preview.stage === "post_bast" && " (masuk stok pada nilai biaya aktual)"}.
                </p>

                {/* Journal Preview */}
                <details className="rounded border border-border">
                  <summary className="cursor-pointer px-3 py-2 text-sm font-medium">
                    Jurnal yang akan diposting ({preview.journals.length})
                  </summary>
                  <div className="divide-y divide-border">
                    {preview.journals.map((j, i) => (
                      <div key={i} className="px-3 py-2">
                        <p className="text-xs font-semibold text-text-secondary mb-1">{i + 1}. {j.label}</p>
                        <table className="w-full text-xs">
                          <tbody>
                            {j.lines.map((l, k) => (
                              <tr key={k}>
                                <td className="py-0.5 pr-2 text-text-secondary whitespace-nowrap">{l.account_code}</td>
                                <td className="py-0.5 pr-2">{l.account_name}</td>
                                <td className="py-0.5 pr-2 text-right w-28">
                                  {l.debit !== "0" && <Rupiah value={l.debit} colorSign={false} />}
                                </td>
                                <td className="py-0.5 text-right w-28">
                                  {l.credit !== "0" && <Rupiah value={l.credit} colorSign={false} />}
                                </td>
                              </tr>
                            ))}
                          </tbody>
                        </table>
                      </div>
                    ))}
                  </div>
                </details>
              </>
            ) : detail.status === "processed" ? (
              <div className="space-y-2 text-sm">
                <p className="text-success">✓ Diproses <Tanggal value={detail.processed_at ?? ""} /></p>
                <div className="flex flex-wrap gap-3 text-xs">
                  {detail.revenue_reversal_journal_id && (
                    <Link href={`/accounting/jurnal/${detail.revenue_reversal_journal_id}`} className="text-accent hover:underline">
                      Jurnal pembalikan pendapatan
                    </Link>
                  )}
                  {detail.cogs_reversal_journal_id && (
                    <Link href={`/accounting/jurnal/${detail.cogs_reversal_journal_id}`} className="text-accent hover:underline">
                      Jurnal pembalikan HPP
                    </Link>
                  )}
                  {detail.tax_reversal_journal_id && (
                    <Link href={`/accounting/jurnal/${detail.tax_reversal_journal_id}`} className="text-accent hover:underline">
                      Jurnal pembalikan PPh
                    </Link>
                  )}
                  {detail.settlement_journal_id && (
                    <Link href={`/accounting/jurnal/${detail.settlement_journal_id}`} className="text-accent hover:underline">
                      Jurnal penyelesaian dana
                    </Link>
                  )}
                </div>
                {detail.refund_id && (
                  <p className="text-xs">
                    Refund #{detail.refund_id} —{" "}
                    <button className="text-accent hover:underline" onClick={() => { setDetail(null); setTab("refund"); }}>
                      lihat di tab Refund
                    </button>
                  </p>
                )}
              </div>
            ) : null}
          </div>
        )}
      </Modal>

      {/* ── Modal bayar refund ── */}
      <Modal
        open={!!payTarget}
        onClose={() => setPayTarget(null)}
        title={payTarget ? `Bayar Refund #${payTarget.id}` : ""}
        size="sm"
        footer={
          <>
            <Button variant="secondary" onClick={() => setPayTarget(null)} disabled={busy}>Batal</Button>
            <Button onClick={handlePayRefund} loading={busy} disabled={!payBank}>
              Bayar Refund
            </Button>
          </>
        }
      >
        {payTarget && (
          <div className="space-y-3">
            <p className="text-sm">
              Bayar <strong><Rupiah value={payTarget.amount} colorSign={false} /></strong> kepada{" "}
              <strong>{payTarget.payee || "buyer"}</strong>.
            </p>
            <CashBankSelect token={token} value={payBank} onChange={setPayBank} label="Dibayar dari" required />
            <p className="text-xs text-text-secondary">
              Jurnal: Dr Hutang Refund / Cr Bank — diposting otomatis.
            </p>
          </div>
        )}
      </Modal>
    </div>
  );
}
