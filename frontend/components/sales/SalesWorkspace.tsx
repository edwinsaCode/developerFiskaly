"use client";

// PS-3 — Sales Workspace + CRM: satu tempat untuk funnel
// Lead → Customer → Booking → Reservasi → PPJB → BAST → Komisi.

import { Fragment, useCallback, useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { Card } from "@/components/ui/Card";
import { Button } from "@/components/ui/Button";
import { Badge } from "@/components/ui/Badge";
import { Modal } from "@/components/ui/Modal";
import { Input, Select } from "@/components/ui/Input";
import { EmptyState } from "@/components/ui/EmptyState";
import { Rupiah } from "@/components/format/Rupiah";
import { Tanggal } from "@/components/format/Tanggal";
import { useToast } from "@/components/ui/Toast";
import { ApiError } from "@/lib/api/client";
import {
  fetchLeads, createLead, updateLead, convertLead,
  fetchSalesPerformance, fetchAdminMarketingPerformance, fetchContractWorkload,
  type Lead, type LeadStatus, type SalesPerformanceReport, type AdminMarketingPerformanceReport,
  type ContractWorkloadReport,
} from "@/lib/api/crm";
import { fetchSalesPersons, fetchCustomers, type SalesPerson, type Customer } from "@/lib/api/party";
import { fetchBookings, type Booking } from "@/lib/api/booking";
import { fetchProjects } from "@/lib/api/projects";
import { BookingStatusBadge, isBookingExpired } from "@/components/booking/bookingUi";
import type { Project } from "@/lib/types/api";

import { useMaySell } from "@/lib/hooks/usePermissions";
// ── badges & labels ───────────────────────────────────────────────────────────

const LEAD_STATUS: Record<LeadStatus, { v: "accent" | "warning" | "success" | "neutral" | "danger"; label: string }> = {
  new:       { v: "accent",  label: "Baru" },
  contacted: { v: "warning", label: "Dihubungi" },
  qualified: { v: "warning", label: "Serius" },
  converted: { v: "success", label: "Jadi Customer" },
  lost:      { v: "neutral", label: "Batal" },
};

const SOURCE_LABEL: Record<string, string> = {
  walk_in: "Datang Langsung", referral: "Referensi", online: "Online",
  ads: "Iklan", expo: "Pameran", other: "Lainnya",
};

function LeadBadge({ s }: { s: LeadStatus }) {
  const m = LEAD_STATUS[s];
  return <Badge variant={m.v}>{m.label}</Badge>;
}

// Tahap kontrak (scheme_state) — label sama dengan yang dipakai KPRPipelineBoard,
// bukan istilah baru, supaya management membaca kata yang sama di semua layar.
const STAGE_LABEL: Record<string, string> = {
  signed: "Ditandatangani", dp_paid: "DP Dibayar", submitted_to_bank: "Diajukan ke Bank",
  bank_approved: "SP3K Terbit", bank_rejected: "Ditolak Bank", akad: "Akad Kredit",
  disbursed: "Dana Cair", fully_paid: "Lunas",
};
function stageLabel(s: string, handedOver: boolean): string {
  if (handedOver) return "Diserahkan (BAST)";
  return STAGE_LABEL[s] ?? s;
}

// Baris drill-down per-kontrak — dipakai di bawah baris Sales & Admin Marketing
// agar management bisa lihat "proyek apa, kontrak mana, sudah sampai tahap apa"
// tanpa membuka halaman lain.
function ContractWorkloadRows({ rows }: { rows: ContractWorkloadReport["rows"] }) {
  if (!rows || rows.length === 0) {
    return (
      <tr>
        <td colSpan={8} className="py-2 px-3 text-xs text-text-tertiary italic">
          Tidak ada kontrak aktif untuk orang ini.
        </td>
      </tr>
    );
  }
  return (
    <>
      {rows.map((c) => (
        <tr key={c.contract_id} className="bg-border-subtle/30 border-b border-border last:border-0">
          <td className="py-1.5 pr-3 pl-6 text-xs text-text-secondary" colSpan={2}>
            {c.project_name} · unit {c.unit_code} — {c.buyer_name}
          </td>
          <td className="py-1.5 pr-3 text-xs text-text-secondary uppercase">{c.payment_type}</td>
          <td className="py-1.5 pr-3 text-xs text-text-secondary" colSpan={2}>
            {stageLabel(c.scheme_state, c.handed_over)}
          </td>
          <td className="py-1.5 pr-3 text-right text-xs"><Rupiah value={c.contract_value} colorSign={false} /></td>
          <td className="py-1.5 pr-3 text-right text-xs" colSpan={2}><Rupiah value={c.collected} colorSign={false} /></td>
        </tr>
      ))}
    </>
  );
}

// ── main ──────────────────────────────────────────────────────────────────────

type Tab = "pipeline" | "customer" | "kinerja" | "admin_marketing";

export function SalesWorkspace({ token, initialTab }: { token: string; initialTab?: string }) {
  const maySell = useMaySell();
  const { toast } = useToast();
  // R7 — deep-link tab (?tab=customer dari command palette / link luar).
  const validInitial: Tab =
    initialTab === "customer" || initialTab === "kinerja" || initialTab === "admin_marketing"
      ? initialTab : "pipeline";
  const [tab, setTab] = useState<Tab>(validInitial);
  const [leads, setLeads] = useState<Lead[]>([]);
  const [perf, setPerf] = useState<SalesPerformanceReport | null>(null);
  const [amPerf, setAmPerf] = useState<AdminMarketingPerformanceReport | null>(null);
  const [workload, setWorkload] = useState<ContractWorkloadReport | null>(null);
  const [expandedSalesId, setExpandedSalesId] = useState<number | null>(null);
  const [expandedAmId, setExpandedAmId] = useState<number | null>(null);
  const [salesPersons, setSalesPersons] = useState<SalesPerson[]>([]);
  const [customers, setCustomers] = useState<Customer[]>([]);
  const [bookings, setBookings] = useState<Booking[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);

  const [showLeadForm, setShowLeadForm] = useState(false);
  const [leadFilter, setLeadFilter] = useState<LeadStatus | "">("");
  const [custDetail, setCustDetail] = useState<Customer | null>(null);
  const [lostTarget, setLostTarget] = useState<Lead | null>(null);
  const [lostReason, setLostReason] = useState("");

  // Lead form state
  const [fName, setFName] = useState("");
  const [fPhone, setFPhone] = useState("");
  const [fSource, setFSource] = useState("walk_in");
  const [fSales, setFSales] = useState("");
  const [fProject, setFProject] = useState("");

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const [ls, pf, amf, wl, bk, cs] = await Promise.all([
        fetchLeads(token),
        fetchSalesPerformance(token).catch(() => null),
        fetchAdminMarketingPerformance(token).catch(() => null),
        fetchContractWorkload(token).catch(() => null),
        fetchBookings(token).catch(() => []),
        fetchCustomers(token).catch(() => []),
      ]);
      setLeads(ls);
      setPerf(pf);
      setAmPerf(amf);
      setWorkload(wl);
      setBookings(bk);
      setCustomers(cs);
    } finally {
      setLoading(false);
    }
  }, [token]);

  useEffect(() => { refresh(); }, [refresh]);
  useEffect(() => {
    fetchSalesPersons(token).then(setSalesPersons).catch(() => {});
    fetchProjects(token).then(setProjects).catch(() => {});
  }, [token]);

  const spName = useMemo(
    () => Object.fromEntries(salesPersons.map((s) => [s.id, s.name])),
    [salesPersons],
  );
  const projName = useMemo(
    () => Object.fromEntries(projects.map((p) => [p.id, p.name])),
    [projects],
  );
  const custById = useMemo(
    () => Object.fromEntries(customers.map((c) => [c.id, c])),
    [customers],
  );

  const visibleLeads = leadFilter ? leads.filter((l) => l.status === leadFilter) : leads;
  const activeLeadCount = leads.filter((l) => l.status !== "converted" && l.status !== "lost").length;

  async function act(fn: () => Promise<unknown>, okMsg: string) {
    setBusy(true);
    try {
      await fn();
      toast(okMsg, "success");
      await refresh();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal memproses", "error");
    } finally {
      setBusy(false);
    }
  }

  async function handleCreateLead() {
    if (!fName.trim()) return;
    await act(async () => {
      await createLead(token, {
        name: fName.trim(),
        phone: fPhone || undefined,
        source: fSource as Lead["source"],
        sales_person_id: fSales ? parseInt(fSales, 10) : undefined,
        project_id: fProject ? parseInt(fProject, 10) : undefined,
      });
      setShowLeadForm(false);
      setFName(""); setFPhone("");
    }, "Lead tercatat");
  }

  return (
    <div className="space-y-4">
      <div className="flex items-start justify-between flex-wrap gap-3">
        <div>
          <h1 className="display-lg text-2xl text-text-primary">Sales &amp; CRM</h1>
          <p className="text-sm text-text-secondary mt-0.5">
            Funnel Lead → Customer → Booking → PPJB → BAST → Komisi dalam satu tempat.
          </p>
        </div>
        {maySell && <Button size="sm" onClick={() => setShowLeadForm(true)}>+ Lead Baru</Button>}
      </div>

      {/* KPI funnel */}
      {perf && (
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
          {[
            { label: "Lead", n: perf.total_leads, sub: `${activeLeadCount} aktif` },
            { label: "Booking", n: perf.total_bookings, sub: `${bookings.filter((b) => b.status === "active").length} aktif` },
            { label: "Kontrak (PPJB)", n: perf.total_contracts, sub: undefined },
            { label: "Terjual (BAST)", n: perf.total_bast, sub: undefined },
          ].map((k, i) => (
            <div key={k.label} className="relative rounded-lg border border-border bg-surface p-4">
              <p className="text-xs text-text-secondary">{k.label}</p>
              <p className="mt-1 text-xl font-semibold tabular-nums">{k.n}</p>
              {k.sub && <p className="text-[11px] text-text-tertiary">{k.sub}</p>}
              {i < 3 && (
                <span className="absolute -right-2.5 top-1/2 hidden -translate-y-1/2 text-text-tertiary sm:block">→</span>
              )}
            </div>
          ))}
        </div>
      )}

      {/* Tabs */}
      <div className="flex gap-1 border-b border-border">
        {([
          { key: "pipeline", label: `Pipeline Lead (${activeLeadCount})` },
          { key: "customer", label: `Customer (${customers.length})` },
          { key: "kinerja", label: "Kinerja Sales" },
          { key: "admin_marketing", label: "Kinerja Admin Marketing" },
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
            {[...Array(4)].map((_, i) => <div key={i} className="h-9 rounded bg-border-subtle" />)}
          </div>
        </Card>
      ) : tab === "pipeline" ? (
        <>
          {/* filter status */}
          <div className="flex flex-wrap gap-1.5">
            {(["", "new", "contacted", "qualified", "converted", "lost"] as const).map((s) => (
              <button
                key={s || "all"}
                onClick={() => setLeadFilter(s)}
                className={`rounded-full border px-3 py-1 text-xs transition-colors
                  ${leadFilter === s
                    ? "border-accent bg-accent-light text-accent font-medium"
                    : "border-border text-text-secondary hover:text-accent hover:border-accent/40"}`}
              >
                {s === "" ? "Semua" : LEAD_STATUS[s].label}
                {" "}({s === "" ? leads.length : leads.filter((l) => l.status === s).length})
              </button>
            ))}
          </div>

          {visibleLeads.length === 0 ? (
            <EmptyState
              title="Belum ada lead"
              description="Catat setiap calon pembeli (pameran, iklan, walk-in). Lead yang serius dikonversi menjadi Customer lalu lanjut Booking."
              action={maySell ? <Button size="sm" onClick={() => setShowLeadForm(true)}>+ Lead Baru</Button> : undefined}
            />
          ) : (
            <Card padding="sm" className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="text-left text-xs text-text-secondary border-b border-border">
                    <th className="py-2 pr-3">Nama</th>
                    <th className="py-2 pr-3">Sumber</th>
                    <th className="py-2 pr-3">Minat Proyek</th>
                    <th className="py-2 pr-3">Sales</th>
                    <th className="py-2 pr-3">Status</th>
                    <th className="py-2" />
                  </tr>
                </thead>
                <tbody>
                  {visibleLeads.map((l) => (
                    <tr key={l.id} className="border-b border-border last:border-0">
                      <td className="py-2 pr-3">
                        <span className="font-medium">{l.name}</span>
                        {l.phone && <span className="block text-xs text-text-tertiary">{l.phone}</span>}
                      </td>
                      <td className="py-2 pr-3 text-xs">{SOURCE_LABEL[l.source] ?? l.source}</td>
                      <td className="py-2 pr-3 text-xs">
                        {l.project_id ? (projName[l.project_id] ?? `#${l.project_id}`) : "—"}
                      </td>
                      <td className="py-2 pr-3 text-xs">
                        {l.sales_person_id ? (spName[l.sales_person_id] ?? `#${l.sales_person_id}`) : "—"}
                      </td>
                      <td className="py-2 pr-3"><LeadBadge s={l.status} /></td>
                      <td className="py-2 text-right">
                        {/* Aksi funnel menulis ke /leads — Viewer tidak punya
                            hak jual, jadi tombolnya tidak ditawarkan. */}
                        <div className="flex justify-end gap-1.5">
                          {!maySell && <span className="text-xs text-text-tertiary">—</span>}
                          {maySell && l.status === "new" && (
                            <Button size="sm" variant="secondary" loading={busy}
                              onClick={() => act(() => updateLead(token, l.id, { status: "contacted" }), "Ditandai dihubungi")}>
                              Dihubungi
                            </Button>
                          )}
                          {maySell && (l.status === "new" || l.status === "contacted") && (
                            <Button size="sm" variant="secondary" loading={busy}
                              onClick={() => act(() => updateLead(token, l.id, { status: "qualified" }), "Ditandai serius")}>
                              Serius
                            </Button>
                          )}
                          {maySell && l.status !== "converted" && l.status !== "lost" && (
                            <>
                              <Button size="sm" loading={busy}
                                onClick={() => act(async () => {
                                  const r = await convertLead(token, l.id);
                                  toast(`Customer "${r.customer.name}" dibuat — lanjutkan Booking dari halaman unit`, "success");
                                }, "Lead dikonversi")}>
                                → Customer
                              </Button>
                              <button
                                className="text-xs text-text-tertiary hover:text-danger px-1"
                                onClick={() => { setLostTarget(l); setLostReason(""); }}
                              >
                                batal
                              </button>
                            </>
                          )}
                          {l.status === "converted" && l.customer_id && (
                            <button
                              className="text-xs text-accent hover:underline"
                              onClick={() => {
                                const c = custById[l.customer_id!];
                                if (c) { setCustDetail(c); setTab("customer"); }
                              }}
                            >
                              lihat customer →
                            </button>
                          )}
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </Card>
          )}
        </>
      ) : tab === "customer" ? (
        customers.length === 0 ? (
          <EmptyState
            title="Belum ada customer"
            description="Customer terbentuk dari konversi lead atau dibuat langsung saat booking/kontrak."
          />
        ) : (
          <Card padding="sm" className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-left text-xs text-text-secondary border-b border-border">
                  <th className="py-2 pr-3">Customer</th>
                  <th className="py-2 pr-3">Kontak</th>
                  <th className="py-2 pr-3">Booking</th>
                  <th className="py-2" />
                </tr>
              </thead>
              <tbody>
                {customers.map((c) => {
                  const cBookings = bookings.filter((b) => b.customer_id === c.id);
                  return (
                    <tr key={c.id} className="border-b border-border last:border-0">
                      <td className="py-2 pr-3">
                        <span className="font-medium">{c.name}</span>
                        <span className="block text-xs text-text-tertiary">{c.code}</span>
                      </td>
                      <td className="py-2 pr-3 text-xs">
                        {[c.phone, c.email].filter(Boolean).join(" · ") || "—"}
                      </td>
                      <td className="py-2 pr-3 text-xs tabular-nums">{cBookings.length}</td>
                      <td className="py-2 text-right">
                        <Button size="sm" variant="secondary" onClick={() => setCustDetail(c)}>
                          Riwayat
                        </Button>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </Card>
        )
      ) : tab === "kinerja" ? (
        /* ── Kinerja Sales ── */
        !perf?.rows || perf.rows.length === 0 ? (
          <EmptyState
            title="Belum ada sales person"
            description="Tambahkan sales person (master) lalu atribusikan lead/booking/kontrak — leaderboard akan terisi otomatis dari data penjualan & komisi."
          />
        ) : (
          <Card padding="sm" className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-left text-xs text-text-secondary border-b border-border">
                  <th className="py-2 pr-3">Sales</th>
                  <th className="py-2 pr-3 text-right">Lead</th>
                  <th className="py-2 pr-3 text-right">Booking</th>
                  <th className="py-2 pr-3 text-right">Kontrak</th>
                  <th className="py-2 pr-3 text-right">BAST</th>
                  <th className="py-2 pr-3 text-right">Nilai BAST</th>
                  <th className="py-2 pr-3 text-right">Collected</th>
                  <th className="py-2 pr-3 text-right">Komisi</th>
                </tr>
              </thead>
              <tbody>
                {perf.rows.map((r, i) => {
                  const expanded = expandedSalesId === r.sales_person_id;
                  return (
                    <Fragment key={r.sales_person_id}>
                      <tr
                        className="border-b border-border last:border-0 cursor-pointer hover:bg-border-subtle/40"
                        onClick={() => setExpandedSalesId(expanded ? null : r.sales_person_id)}
                      >
                        <td className="py-2 pr-3">
                          <span className="text-text-tertiary inline-block w-3">{expanded ? "▾" : "▸"}</span>
                          <span className="font-medium">{i === 0 && r.bast_count > 0 ? "🏆 " : ""}{r.name}</span>
                          {!r.is_active && <Badge variant="neutral" className="ml-1.5">nonaktif</Badge>}
                        </td>
                        <td className="py-2 pr-3 text-right tabular-nums">
                          {r.leads}
                          {r.leads > 0 && (
                            <span className="text-[11px] text-text-tertiary"> ({r.leads_converted}✓)</span>
                          )}
                        </td>
                        <td className="py-2 pr-3 text-right tabular-nums">{r.bookings_total}</td>
                        <td className="py-2 pr-3 text-right tabular-nums">{r.contracts}</td>
                        <td className="py-2 pr-3 text-right tabular-nums font-medium">{r.bast_count}</td>
                        <td className="py-2 pr-3 text-right"><Rupiah value={r.bast_value} colorSign={false} /></td>
                        <td className="py-2 pr-3 text-right"><Rupiah value={r.collected} colorSign={false} /></td>
                        <td className="py-2 pr-3 text-right">
                          <Rupiah value={r.commission_earned} colorSign={false} />
                          <span className="block text-[11px] text-text-tertiary">
                            dibayar <Rupiah value={r.commission_paid} colorSign={false} />
                          </span>
                        </td>
                      </tr>
                      {expanded && (
                        <ContractWorkloadRows
                          rows={(workload?.rows ?? []).filter((c) => c.sales_person_id === r.sales_person_id)}
                        />
                      )}
                    </Fragment>
                  );
                })}
              </tbody>
            </table>
            <p className="mt-2 text-[11px] text-text-tertiary">
              Semua angka derived dari ledger &amp; dokumen (kontrak, BAST, komisi) — sumber yang sama dengan laporan keuangan.
              Klik baris untuk lihat proyek &amp; kontrak yang sedang ditangani.
            </p>
          </Card>
        )
      ) : (
        /* ── Kinerja Admin Marketing (P2) — beban kerja admin/dokumen/KPR, TIDAK terima komisi ── */
        !amPerf?.rows || amPerf.rows.length === 0 ? (
          <EmptyState
            title="Belum ada penugasan Admin Marketing"
            description="Admin Marketing ditugaskan opsional saat konversi Booking → Kontrak (independen dari Sales — komisi tetap ke Sales). Tugaskan lewat halaman kontrak untuk mengisi laporan ini."
          />
        ) : (
          <>
            {(amPerf.total_contracts_assigned > 0 || amPerf.total_contracts_unassigned > 0) && (
              <p className="text-xs text-text-secondary">
                {amPerf.total_contracts_assigned} kontrak aktif sudah punya Admin Marketing
                {amPerf.total_contracts_unassigned > 0 && (
                  <> · <span className="text-warning">{amPerf.total_contracts_unassigned} belum ditugaskan</span></>
                )}
              </p>
            )}
            <Card padding="sm" className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="text-left text-xs text-text-secondary border-b border-border">
                    <th className="py-2 pr-3">Admin Marketing</th>
                    <th className="py-2 pr-3 text-right">Kontrak</th>
                    <th className="py-2 pr-3 text-right">KPR</th>
                    <th className="py-2 pr-3 text-right">Tunai</th>
                    <th className="py-2 pr-3 text-right">BAST</th>
                    <th className="py-2 pr-3">Proyek Ditangani</th>
                    <th className="py-2 pr-3 text-right">Nilai Kontrak</th>
                    <th className="py-2 pr-3 text-right">Collected</th>
                  </tr>
                </thead>
                <tbody>
                  {amPerf.rows.map((r) => {
                    const expanded = expandedAmId === r.admin_marketing_person_id;
                    return (
                      <Fragment key={r.admin_marketing_person_id}>
                        <tr
                          className="border-b border-border last:border-0 cursor-pointer hover:bg-border-subtle/40"
                          onClick={() => setExpandedAmId(expanded ? null : r.admin_marketing_person_id)}
                        >
                          <td className="py-2 pr-3">
                            <span className="text-text-tertiary inline-block w-3">{expanded ? "▾" : "▸"}</span>
                            <span className="font-medium">{r.name}</span>
                            {!r.is_active && <Badge variant="neutral" className="ml-1.5">nonaktif</Badge>}
                          </td>
                          <td className="py-2 pr-3 text-right tabular-nums font-medium">{r.contracts_handled}</td>
                          <td className="py-2 pr-3 text-right tabular-nums">{r.kpr_count}</td>
                          <td className="py-2 pr-3 text-right tabular-nums">{r.cash_count}</td>
                          <td className="py-2 pr-3 text-right tabular-nums">{r.bast_count}</td>
                          <td className="py-2 pr-3 text-xs text-text-secondary max-w-[220px] truncate" title={r.project_names}>
                            {r.project_names || "—"}
                          </td>
                          <td className="py-2 pr-3 text-right"><Rupiah value={r.contract_value} colorSign={false} /></td>
                          <td className="py-2 pr-3 text-right"><Rupiah value={r.collected} colorSign={false} /></td>
                        </tr>
                        {expanded && (
                          <ContractWorkloadRows
                            rows={(workload?.rows ?? []).filter((c) => c.admin_marketing_person_id === r.admin_marketing_person_id)}
                          />
                        )}
                      </Fragment>
                    );
                  })}
                </tbody>
              </table>
              <p className="mt-2 text-[11px] text-text-tertiary">
                Admin Marketing tidak menerima komisi — komisi selalu mengikuti Sales pada kontrak yang sama.
                Klik baris untuk lihat proyek, kontrak, dan tahap yang sedang ditangani.
              </p>
            </Card>
          </>
        )
      )}

      {/* ── Modal: Lead baru ── */}
      <Modal
        open={showLeadForm}
        onClose={() => setShowLeadForm(false)}
        title="Lead Baru"
        size="md"
        footer={
          <>
            <Button variant="secondary" onClick={() => setShowLeadForm(false)} disabled={busy}>Batal</Button>
            <Button onClick={handleCreateLead} loading={busy} disabled={!fName.trim()}>Simpan</Button>
          </>
        }
      >
        <div className="space-y-3">
          <Input label="Nama" required value={fName} onChange={(e) => setFName(e.target.value)} placeholder="cth: Andi Wijaya" />
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <Input label="Telepon" value={fPhone} onChange={(e) => setFPhone(e.target.value)} placeholder="08xx" />
            <Select label="Sumber" value={fSource} onChange={(e) => setFSource(e.target.value)}>
              {Object.entries(SOURCE_LABEL).map(([k, v]) => (
                <option key={k} value={k}>{v}</option>
              ))}
            </Select>
            <Select label="Sales Penanggung Jawab" value={fSales} onChange={(e) => setFSales(e.target.value)}>
              <option value="">— belum ditentukan —</option>
              {salesPersons.filter((s) => s.is_active).map((s) => (
                <option key={s.id} value={s.id}>{s.name}</option>
              ))}
            </Select>
            <Select label="Minat Proyek" value={fProject} onChange={(e) => setFProject(e.target.value)}>
              <option value="">— belum tahu —</option>
              {projects.map((p) => (
                <option key={p.id} value={p.id}>{p.name}</option>
              ))}
            </Select>
          </div>
        </div>
      </Modal>

      {/* ── Modal: tandai batal ── */}
      <Modal
        open={!!lostTarget}
        onClose={() => setLostTarget(null)}
        title={`Tandai Batal — ${lostTarget?.name ?? ""}`}
        size="sm"
        footer={
          <>
            <Button variant="secondary" onClick={() => setLostTarget(null)} disabled={busy}>Tutup</Button>
            <Button variant="danger" loading={busy}
              onClick={() => lostTarget && act(async () => {
                await updateLead(token, lostTarget.id, { status: "lost", lost_reason: lostReason });
                setLostTarget(null);
              }, "Lead ditandai batal")}>
              Tandai Batal
            </Button>
          </>
        }
      >
        <Input label="Alasan" value={lostReason} onChange={(e) => setLostReason(e.target.value)}
          placeholder="cth: memilih developer lain" />
      </Modal>

      {/* ── Drawer: riwayat customer (timeline perjalanan) ── */}
      <Modal
        open={!!custDetail}
        onClose={() => setCustDetail(null)}
        title={custDetail ? `Riwayat — ${custDetail.name}` : ""}
        size="lg"
        footer={<Button variant="secondary" onClick={() => setCustDetail(null)}>Tutup</Button>}
      >
        {custDetail && (
          <CustomerJourney
            customer={custDetail}
            leads={leads.filter((l) => l.customer_id === custDetail.id)}
            bookings={bookings.filter((b) => b.customer_id === custDetail.id)}
          />
        )}
      </Modal>
    </div>
  );
}

// ── Timeline perjalanan customer ──────────────────────────────────────────────

function CustomerJourney({
  customer, leads, bookings,
}: {
  customer: Customer;
  leads: Lead[];
  bookings: Booking[];
}) {
  type Ev = { at: string; title: string; detail?: React.ReactNode; href?: string };
  const events: Ev[] = [];

  for (const l of leads) {
    events.push({ at: l.created_at, title: "Lead tercatat", detail: SOURCE_LABEL[l.source] ?? l.source });
    if (l.converted_at) events.push({ at: l.converted_at, title: "Dikonversi menjadi Customer" });
  }
  for (const b of bookings) {
    events.push({
      at: b.created_at,
      title: `Booking #${b.id}`,
      detail: (
        <span className="inline-flex items-center gap-2">
          <Rupiah value={b.booking_fee} colorSign={false} />
          <BookingStatusBadge status={b.status} expired={isBookingExpired(b)} />
        </span>
      ),
      href: `/penjualan/${b.unit_id}`,
    });
    if (b.converted_contract_id) {
      events.push({
        at: b.closed_event_date ?? b.closed_at ?? b.created_at,
        title: `Kontrak #${b.converted_contract_id} (dari booking)`,
        href: `/penjualan/${b.unit_id}?contract=${b.converted_contract_id}`,
      });
    }
  }
  events.sort((a, b) => a.at.localeCompare(b.at));

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap gap-4 text-sm">
        <span className="text-text-secondary">Kode: <strong className="text-text-primary">{customer.code}</strong></span>
        {customer.phone && <span className="text-text-secondary">Telp: <strong className="text-text-primary">{customer.phone}</strong></span>}
        {customer.email && <span className="text-text-secondary">Email: <strong className="text-text-primary">{customer.email}</strong></span>}
      </div>

      {events.length === 0 ? (
        <p className="text-sm text-text-secondary">
          Belum ada aktivitas tercatat. Booking &amp; kontrak customer ini akan muncul di sini.
        </p>
      ) : (
        <ol className="space-y-3">
          {events.map((e, i) => (
            <li key={i} className="flex items-start gap-3 text-sm">
              <span className="mt-1.5 h-2 w-2 shrink-0 rounded-full bg-accent" />
              <div className="min-w-0">
                <p className="font-medium text-text-primary">
                  {e.href ? (
                    <Link href={e.href} className="text-accent hover:underline">{e.title}</Link>
                  ) : e.title}
                </p>
                <p className="text-xs text-text-secondary">
                  <Tanggal value={e.at} />{e.detail && <> · {e.detail}</>}
                </p>
              </div>
            </li>
          ))}
        </ol>
      )}

      <p className="text-[11px] text-text-tertiary">
        Detail cicilan &amp; statement lengkap: buka unit terkait → Kelola.
      </p>
    </div>
  );
}
