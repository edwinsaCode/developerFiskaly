"use client";

// Increment 9 — Papan Komisi (docs/increment-9-frontend-spec.md M1) + Aturan (M2).
// Finance memproses batch: hitung → setujui → terutang (akrual) → bayar.

import { useCallback, useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { Card } from "@/components/ui/Card";
import { Button } from "@/components/ui/Button";
import { Badge } from "@/components/ui/Badge";
import { Modal } from "@/components/ui/Modal";
import { Input, Select } from "@/components/ui/Input";
import { RupiahInput } from "@/components/ui/RupiahInput";
import { EmptyState } from "@/components/ui/EmptyState";
import { Rupiah } from "@/components/format/Rupiah";
import { CashBankSelect } from "@/components/accounting/CashBankSelect";
import { useToast } from "@/components/ui/Toast";
import { ApiError } from "@/lib/api/client";
import { useUnitMap } from "@/lib/hooks/useUnitMap";
import { fetchSalesPersons, type SalesPerson } from "@/lib/api/party";
import { useMayWrite } from "@/lib/hooks/usePermissions";
import { todayLocalStr } from "@/lib/date";
import {
  fetchCommissions,
  fetchCommissionRules,
  createCommissionRule,
  setCommissionRuleActive,
  calculateCommissions,
  syncCommissionCancellations,
  approveCommission,
  makeCommissionPayable,
  payCommission,
  cancelCommission,
  type Commission,
  type CommissionRule,
  type CommissionStatus,
} from "@/lib/api/commission";

const STATUS_TABS: { key: CommissionStatus | ""; label: string }[] = [
  { key: "calculated", label: "Dihitung" },
  { key: "approved", label: "Disetujui" },
  { key: "payable", label: "Siap Bayar" },
  { key: "paid", label: "Dibayar" },
  { key: "", label: "Semua" },
];

function CmStatusBadge({ s }: { s: CommissionStatus }) {
  const map: Record<CommissionStatus, { v: "warning" | "accent" | "success" | "neutral" | "danger"; label: string }> = {
    calculated:  { v: "warning", label: "Dihitung" },
    approved:    { v: "accent",  label: "Disetujui" },
    payable:     { v: "accent",  label: "Siap Bayar" },
    paid:        { v: "success", label: "Dibayar" },
    cancelled:   { v: "neutral", label: "Dibatalkan" },
    clawed_back: { v: "danger",  label: "Clawback" },
  };
  const m = map[s];
  return <Badge variant={m.v}>{m.label}</Badge>;
}

function pctLabel(rate: string): string {
  const n = parseFloat(rate);
  if (isNaN(n) || n === 0) return "—";
  return `${(n * 100).toLocaleString("id-ID", { maximumFractionDigits: 2 })}%`;
}

export function CommissionBoard({ token }: { token: string }) {
  const mayWrite = useMayWrite();
  const { toast } = useToast();
  const { unitMap } = useUnitMap(token);

  const [tab, setTab] = useState<CommissionStatus | "">("calculated");
  const [rows, setRows] = useState<Commission[]>([]);
  const [rules, setRules] = useState<CommissionRule[]>([]);
  const [salesPersons, setSalesPersons] = useState<Record<number, SalesPerson>>({});
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [selected, setSelected] = useState<Set<number>>(new Set());

  const [showRules, setShowRules] = useState(false);
  const [payTarget, setPayTarget] = useState<Commission[] | null>(null);
  const [payBank, setPayBank] = useState("");

  // Rule form
  const [ruleName, setRuleName] = useState("");
  const [ruleBasis, setRuleBasis] = useState<"percent_of_sale" | "flat_per_unit">("percent_of_sale");
  const [rulePct, setRulePct] = useState("2.5"); // persen — dikonversi ke fraksi saat submit
  const [ruleFlat, setRuleFlat] = useState("");
  const [ruleFrom, setRuleFrom] = useState(todayLocalStr());
  const [ruleSales, setRuleSales] = useState("");

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const [cs, rls] = await Promise.all([
        fetchCommissions(token, tab),
        fetchCommissionRules(token),
      ]);
      setRows(cs);
      setRules(rls);
      setSelected(new Set());
    } catch {
      toast("Gagal memuat komisi", "error");
    } finally {
      setLoading(false);
    }
  }, [token, tab, toast]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  useEffect(() => {
    fetchSalesPersons(token)
      .then((sp) => setSalesPersons(Object.fromEntries(sp.map((s) => [s.id, s]))))
      .catch(() => {});
  }, [token]);

  const selectedRows = useMemo(() => rows.filter((r) => selected.has(r.id)), [rows, selected]);

  function toggleAll() {
    setSelected((prev) =>
      prev.size === rows.length ? new Set() : new Set(rows.map((r) => r.id)),
    );
  }

  async function runBatch(
    items: Commission[],
    fn: (id: number) => Promise<unknown>,
    okMsg: string,
  ) {
    setBusy(true);
    let ok = 0;
    let firstErr: string | null = null;
    for (const c of items) {
      try {
        await fn(c.id);
        ok++;
      } catch (err) {
        if (!firstErr) firstErr = err instanceof ApiError ? err.message : "gagal";
      }
    }
    setBusy(false);
    if (ok > 0) toast(`${ok} komisi ${okMsg}`, "success");
    if (firstErr) toast(firstErr, "error");
    await refresh();
  }

  async function handleCalculate() {
    setBusy(true);
    try {
      const [calc, sync] = [await calculateCommissions(token), await syncCommissionCancellations(token)];
      const parts: string[] = [];
      if (calc.created > 0) parts.push(`${calc.created} komisi baru dihitung`);
      if (sync.cancelled > 0) parts.push(`${sync.cancelled} dibatalkan (sale batal)`);
      if (sync.clawed_back > 0) parts.push(`${sync.clawed_back} clawback`);
      toast(parts.length ? parts.join(" · ") : "Tidak ada komisi baru — semua sudah terhitung", "success");
      await refresh();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal menghitung", "error");
    } finally {
      setBusy(false);
    }
  }

  async function handlePay() {
    if (!payTarget || !payBank) return;
    await runBatch(payTarget, (id) => payCommission(token, id, payBank), "dibayar");
    setPayTarget(null);
  }

  async function handleCreateRule() {
    setBusy(true);
    try {
      const pctFraction = ruleBasis === "percent_of_sale"
        ? (parseFloat(rulePct.replace(",", ".")) / 100).toString()
        : undefined;
      await createCommissionRule(token, {
        name: ruleName.trim(),
        basis: ruleBasis,
        rate: pctFraction,
        flat_amount: ruleBasis === "flat_per_unit" ? ruleFlat : undefined,
        sales_person_id: ruleSales ? parseInt(ruleSales, 10) : undefined,
        effective_from: ruleFrom,
      });
      toast("Aturan komisi dibuat", "success");
      setRuleName("");
      await refresh();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal membuat aturan", "error");
    } finally {
      setBusy(false);
    }
  }

  const totalSelected = selectedRows.length;

  return (
    <div className="space-y-4">
      <div className="flex items-start justify-between flex-wrap gap-3">
        <div>
          <h1 className="display-lg text-2xl text-text-primary">Komisi Sales</h1>
          <p className="text-sm text-text-secondary mt-0.5">
            Hitung otomatis dari penjualan (BAST) × aturan aktif; proses batch: setujui → terutang → bayar.
          </p>
        </div>
        <div className="flex gap-2">
          <Button size="sm" variant="secondary" onClick={() => setShowRules(true)}>
            Aturan Komisi ({rules.filter((r) => r.is_active).length})
          </Button>
          {mayWrite && (
            <Button size="sm" onClick={handleCalculate} loading={busy}>
              Hitung Komisi
            </Button>
          )}
        </div>
      </div>

      {/* Tabs */}
      <div className="flex gap-1 border-b border-border overflow-x-auto">
        {STATUS_TABS.map((t) => (
          <button
            key={t.key || "all"}
            onClick={() => setTab(t.key)}
            className={`px-3 py-2 text-sm whitespace-nowrap border-b-2 -mb-px transition-colors
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
      ) : rows.length === 0 ? (
        <EmptyState
          title={tab === "calculated" ? "Tidak ada komisi menunggu" : "Belum ada komisi"}
          description={
            rules.length === 0
              ? "Buat Aturan Komisi dulu, lalu klik Hitung — sistem memindai semua penjualan (BAST) ber-sales dan menghitung otomatis."
              : "Klik Hitung Komisi untuk memindai penjualan baru."
          }
          action={
            rules.length === 0 && mayWrite ? (
              <Button size="sm" onClick={() => setShowRules(true)}>+ Buat Aturan</Button>
            ) : undefined
          }
        />
      ) : (
        <Card padding="sm" className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs text-text-secondary border-b border-border">
                <th className="py-2 pr-2 w-8">
                  <input type="checkbox" checked={selected.size === rows.length && rows.length > 0} onChange={toggleAll} />
                </th>
                <th className="py-2 pr-3">Sales</th>
                <th className="py-2 pr-3">Unit</th>
                <th className="py-2 pr-3 text-right">Dasar</th>
                <th className="py-2 pr-3 text-right">Rate</th>
                <th className="py-2 pr-3 text-right">Komisi</th>
                <th className="py-2 pr-3">Status</th>
                <th className="py-2" />
              </tr>
            </thead>
            <tbody>
              {rows.map((c) => {
                const u = unitMap[c.unit_id];
                return (
                  <tr key={c.id} className="border-b border-border last:border-0">
                    <td className="py-2 pr-2">
                      <input
                        type="checkbox"
                        checked={selected.has(c.id)}
                        onChange={() =>
                          setSelected((prev) => {
                            const next = new Set(prev);
                            if (next.has(c.id)) next.delete(c.id); else next.add(c.id);
                            return next;
                          })
                        }
                      />
                    </td>
                    <td className="py-2 pr-3 font-medium">
                      {salesPersons[c.sales_person_id]?.name ?? `#${c.sales_person_id}`}
                    </td>
                    <td className="py-2 pr-3">
                      <Link href={`/penjualan/${c.unit_id}`} className="text-accent hover:underline">
                        {u?.code ?? `#${c.unit_id}`}
                      </Link>
                    </td>
                    <td className="py-2 pr-3 text-right"><Rupiah value={c.basis_amount} colorSign={false} /></td>
                    <td className="py-2 pr-3 text-right">{pctLabel(c.rate_snapshot)}</td>
                    <td className="py-2 pr-3 text-right font-semibold"><Rupiah value={c.amount} colorSign={false} /></td>
                    <td className="py-2 pr-3"><CmStatusBadge s={c.status} /></td>
                    <td className="py-2 text-right text-xs">
                      {c.status === "paid" && c.payment_journal_id && (
                        <Link href={`/accounting/jurnal/${c.payment_journal_id}`} className="text-accent hover:underline">
                          jurnal
                        </Link>
                      )}
                      {c.status === "clawed_back" && c.clawback_journal_id && (
                        <Link href={`/accounting/jurnal/${c.clawback_journal_id}`} className="text-accent hover:underline">
                          clawback
                        </Link>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>

          {/* Batch action bar */}
          {mayWrite && totalSelected > 0 && (tab === "calculated" || tab === "approved" || tab === "payable") && (
            <div className="mt-3 flex items-center justify-between rounded border border-accent bg-accent-light px-3 py-2 text-sm">
              <span>Terpilih: {totalSelected}</span>
              <div className="flex gap-2">
                {tab === "calculated" && (
                  <>
                    <Button size="sm" loading={busy}
                      onClick={() => runBatch(selectedRows, (id) => approveCommission(token, id), "disetujui")}>
                      Setujui Terpilih
                    </Button>
                    <Button size="sm" variant="secondary" loading={busy}
                      onClick={() => runBatch(selectedRows, (id) => cancelCommission(token, id, "dibatalkan manual"), "dibatalkan")}>
                      Batalkan
                    </Button>
                  </>
                )}
                {tab === "approved" && (
                  <Button size="sm" loading={busy}
                    onClick={() => runBatch(selectedRows, (id) => makeCommissionPayable(token, id), "diakru (terutang)")}>
                    Jadikan Terutang
                  </Button>
                )}
                {tab === "payable" && (
                  <Button size="sm" loading={busy} onClick={() => { setPayTarget(selectedRows); setPayBank(""); }}>
                    Bayar Terpilih
                  </Button>
                )}
              </div>
            </div>
          )}
        </Card>
      )}

      {/* ── Modal bayar ── */}
      <Modal
        open={!!payTarget}
        onClose={() => setPayTarget(null)}
        title={`Bayar ${payTarget?.length ?? 0} Komisi`}
        size="sm"
        footer={
          <>
            <Button variant="secondary" onClick={() => setPayTarget(null)} disabled={busy}>Batal</Button>
            <Button onClick={handlePay} loading={busy} disabled={!payBank}>Bayar</Button>
          </>
        }
      >
        <div className="space-y-3">
          <CashBankSelect token={token} value={payBank} onChange={setPayBank} label="Dibayar dari" required />
          <p className="text-xs text-text-secondary">
            Jurnal per komisi: Dr Utang Komisi / Cr Bank — diposting otomatis.
          </p>
        </div>
      </Modal>

      {/* ── Modal aturan komisi ── */}
      <Modal
        open={showRules}
        onClose={() => setShowRules(false)}
        title="Aturan Komisi"
        size="lg"
        footer={<Button variant="secondary" onClick={() => setShowRules(false)}>Tutup</Button>}
      >
        <div className="space-y-4">
          {rules.length > 0 && (
            <table className="w-full text-sm">
              <thead>
                <tr className="text-left text-xs text-text-secondary border-b border-border">
                  <th className="py-1.5 pr-3">Nama</th>
                  <th className="py-1.5 pr-3">Basis</th>
                  <th className="py-1.5 pr-3">Scope</th>
                  <th className="py-1.5 pr-3">Aktif</th>
                </tr>
              </thead>
              <tbody>
                {rules.map((r) => (
                  <tr key={r.id} className="border-b border-border last:border-0">
                    <td className="py-1.5 pr-3 font-medium">{r.name}</td>
                    <td className="py-1.5 pr-3">
                      {r.basis === "percent_of_sale"
                        ? `${pctLabel(r.rate)} dari harga jual`
                        : r.basis === "flat_per_unit"
                          ? <>Flat <Rupiah value={r.flat_amount} colorSign={false} /></>
                          : "Berjenjang (segera)"}
                    </td>
                    <td className="py-1.5 pr-3 text-xs">
                      {r.sales_person_id
                        ? (salesPersons[r.sales_person_id]?.name ?? `Sales #${r.sales_person_id}`)
                        : "Semua sales"}
                    </td>
                    <td className="py-1.5 pr-3">
                      <button
                        disabled={!mayWrite}
                        className={`text-xs ${r.is_active ? "text-success" : "text-text-secondary"} ${mayWrite ? "hover:underline" : "cursor-default"}`}
                        onClick={async () => {
                          if (!mayWrite) return;
                          try {
                            await setCommissionRuleActive(token, r.id, !r.is_active);
                            await refresh();
                          } catch (err) {
                            toast(err instanceof ApiError ? err.message : "Gagal", "error");
                          }
                        }}
                      >
                        {r.is_active ? "Aktif ●" : "Nonaktif ○"}
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}

          {/* Form pembuat aturan hanya untuk peran tulis; Viewer tetap
              boleh membaca daftar aturan yang berlaku. */}
          {mayWrite && (
            <div className="rounded border border-border p-3 space-y-3">
              <p className="text-xs font-semibold text-text-secondary uppercase tracking-wide">Aturan Baru</p>
              <Input label="Nama" required value={ruleName} onChange={(e) => setRuleName(e.target.value)}
                placeholder="cth: Komisi standar 2,5%" />
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                <Select label="Basis" value={ruleBasis}
                  onChange={(e) => setRuleBasis(e.target.value as "percent_of_sale" | "flat_per_unit")}>
                  <option value="percent_of_sale">% dari harga jual</option>
                  <option value="flat_per_unit">Flat per unit</option>
                </Select>
                {ruleBasis === "percent_of_sale" ? (
                  <Input label="Persen (%)" value={rulePct} onChange={(e) => setRulePct(e.target.value)}
                    placeholder="2.5" />
                ) : (
                  <RupiahInput label="Nominal Flat" value={ruleFlat} onChange={setRuleFlat} />
                )}
                <Select label="Berlaku untuk" value={ruleSales} onChange={(e) => setRuleSales(e.target.value)}>
                  <option value="">Semua sales</option>
                  {Object.values(salesPersons).map((s) => (
                    <option key={s.id} value={s.id}>{s.name}</option>
                  ))}
                </Select>
                <Input label="Berlaku sejak" type="date" value={ruleFrom}
                  onChange={(e) => setRuleFrom(e.target.value)} />
              </div>
              <Button size="sm" onClick={handleCreateRule} loading={busy} disabled={!ruleName.trim()}>
                Simpan Aturan
              </Button>
            </div>
          )}
        </div>
      </Modal>
    </div>
  );
}
