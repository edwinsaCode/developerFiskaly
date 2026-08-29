"use client";

// PRODUCT HARDENING — master Skema Pembayaran + Bank Pembiayaan. Sebelumnya
// hanya bisa dibuat via API/seed → dropdown skema kosong untuk tenant baru.
// Backend: /payment-schemes, /financing-sources (Increment 3).

import { useCallback, useEffect, useState } from "react";
import { Card } from "@/components/ui/Card";
import { Button } from "@/components/ui/Button";
import { Badge } from "@/components/ui/Badge";
import { Modal } from "@/components/ui/Modal";
import { Input, Select } from "@/components/ui/Input";
import { EmptyState } from "@/components/ui/EmptyState";
import { useToast } from "@/components/ui/Toast";
import { ApiError } from "@/lib/api/client";
import { useMayWrite } from "@/lib/hooks/usePermissions";
import {
  fetchPaymentSchemes,
  fetchFinancingSources,
  createPaymentScheme,
  createFinancingSource,
  updatePaymentScheme,
  updateFinancingSource,
  type PaymentScheme,
  type FinancingSource,
  type SchemeParams,
} from "@/lib/api/party";

const POLICY_LABEL: Record<string, string> = {
  cash: "Tunai Keras",
  cash_installment: "Tunai Bertahap",
  kpr: "KPR",
  inhouse: "In-House (cicilan developer)",
};

const GATE_LABEL: Record<string, string> = {
  full_payment: "Lunas penuh",
  akad: "Akad kredit",
  dp_paid: "DP lunas",
};

const FIN_TYPE_LABEL: Record<string, string> = {
  bank_kpr_subsidi: "Bank KPR Subsidi",
  bank_kpr_komersial: "Bank KPR Komersial",
  lainnya: "Lainnya",
};

// Default gate yang lazim per tipe skema — mengurangi salah konfigurasi.
const DEFAULT_GATE: Record<string, string> = {
  cash: "full_payment",
  cash_installment: "full_payment",
  kpr: "akad",
  inhouse: "dp_paid",
};

export function SchemesSection({ token }: { token: string }) {
  const mayWrite = useMayWrite();
  const { toast } = useToast();
  const [schemes, setSchemes] = useState<PaymentScheme[]>([]);
  const [sources, setSources] = useState<FinancingSource[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [showScheme, setShowScheme] = useState(false);
  const [showSource, setShowSource] = useState(false);

  const [sCode, setSCode] = useState("");
  const [sName, setSName] = useState("");
  const [sPolicy, setSPolicy] = useState("kpr");
  const [sDP, setSDP] = useState("10");
  const [sGate, setSGate] = useState("akad");
  const [sCount, setSCount] = useState("12");

  const [fCode, setFCode] = useState("");
  const [fName, setFName] = useState("");
  const [fType, setFType] = useState("bank_kpr_subsidi");

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const [ss, fs] = await Promise.all([
        fetchPaymentSchemes(token),
        fetchFinancingSources(token).catch(() => [] as FinancingSource[]),
      ]);
      setSchemes(ss);
      setSources(fs);
    } catch {
      // EmptyState menjelaskan
    } finally {
      setLoading(false);
    }
  }, [token]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  function openSchemeForm() {
    setSCode("");
    setSName("");
    setSPolicy("kpr");
    setSDP("10");
    setSGate("akad");
    setSCount("12");
    setShowScheme(true);
  }

  function onPolicyChange(p: string) {
    setSPolicy(p);
    setSGate(DEFAULT_GATE[p] ?? "full_payment");
  }

  async function handleCreateScheme() {
    setBusy(true);
    try {
      const params: SchemeParams = {
        dp_percent: sDP.trim() || "0",
        bast_gate: sGate,
      };
      if (sPolicy === "cash_installment") params.installment_count = parseInt(sCount, 10) || 12;
      if (sPolicy === "inhouse") params.tenor_months = parseInt(sCount, 10) || 12;
      if (sPolicy === "kpr") params.final_due_months = parseInt(sCount, 10) || 6;
      await createPaymentScheme(token, {
        code: sCode.trim(),
        name: sName.trim(),
        policy_type: sPolicy,
        params,
      });
      toast(`Skema ${sName.trim()} dibuat — siap dipilih di kontrak`, "success");
      setShowScheme(false);
      await refresh();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal membuat skema", "error");
    } finally {
      setBusy(false);
    }
  }

  async function handleCreateSource() {
    setBusy(true);
    try {
      await createFinancingSource(token, {
        code: fCode.trim(),
        name: fName.trim(),
        type: fType,
      });
      toast(`${fName.trim()} ditambahkan sebagai bank pembiayaan`, "success");
      setShowSource(false);
      setFCode("");
      setFName("");
      await refresh();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal menambah bank", "error");
    } finally {
      setBusy(false);
    }
  }

  async function toggleScheme(s: PaymentScheme) {
    try {
      await updatePaymentScheme(token, s.id, { is_active: !s.is_active });
      await refresh();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal mengubah status", "error");
    }
  }

  async function toggleSource(f: FinancingSource) {
    try {
      await updateFinancingSource(token, f.id, { is_active: !f.is_active });
      await refresh();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal mengubah status", "error");
    }
  }

  const countLabel =
    sPolicy === "cash_installment" ? "Jumlah cicilan" :
    sPolicy === "inhouse" ? "Tenor (bulan)" :
    sPolicy === "kpr" ? "Target pelunasan bank (bulan)" : "";

  return (
    <div className="space-y-6">
      {/* ── Skema Pembayaran ── */}
      <div className="space-y-3">
        <div className="flex items-center justify-between flex-wrap gap-2">
          <div>
            <p className="text-sm font-semibold">Skema Pembayaran</p>
            <p className="text-xs text-text-secondary">
              Menentukan DP, jadwal cicilan, dan syarat serah terima (BAST) per kontrak.
            </p>
          </div>
          {mayWrite && <Button size="sm" onClick={openSchemeForm}>+ Skema</Button>}
        </div>

        {loading ? (
          <Card padding="sm">
            <div className="animate-pulse space-y-2">
              {[...Array(2)].map((_, i) => <div key={i} className="h-9 rounded bg-border-subtle" />)}
            </div>
          </Card>
        ) : schemes.length === 0 ? (
          <EmptyState
            title="Belum ada skema pembayaran"
            description="Kontrak penjualan wajib memilih skema (Tunai / KPR / In-House). Buat minimal satu skema — contoh: KPR Subsidi DP 1%."
            action={mayWrite ? <Button size="sm" onClick={openSchemeForm}>+ Skema Pertama</Button> : undefined}
          />
        ) : (
          <Card padding="sm" className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border text-left text-xs text-text-secondary">
                  <th className="py-2 pr-3">Kode</th>
                  <th className="py-2 pr-3">Nama</th>
                  <th className="py-2 pr-3">Tipe</th>
                  <th className="py-2 pr-3">Status</th>
                </tr>
              </thead>
              <tbody>
                {schemes.map((s) => (
                  <tr key={s.id} className="border-b border-border last:border-0">
                    <td className="py-2 pr-3 font-mono text-xs">{s.code ?? "—"}</td>
                    <td className="py-2 pr-3 font-medium">{s.name}</td>
                    <td className="py-2 pr-3 text-xs">{POLICY_LABEL[s.policy_type] ?? s.policy_type}</td>
                    <td className="py-2 pr-3">
                      <button onClick={() => toggleScheme(s)} title="Klik untuk ubah status">
                        {s.is_active
                          ? <Badge variant="success">Aktif</Badge>
                          : <Badge variant="neutral">Nonaktif</Badge>}
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </Card>
        )}
      </div>

      {/* ── Bank Pembiayaan ── */}
      <div className="space-y-3">
        <div className="flex items-center justify-between flex-wrap gap-2">
          <div>
            <p className="text-sm font-semibold">Bank Pembiayaan (KPR)</p>
            <p className="text-xs text-text-secondary">
              Bank penyalur KPR — dipilih saat kontrak KPR dibuat.
            </p>
          </div>
          {mayWrite && <Button size="sm" variant="secondary" onClick={() => setShowSource(true)}>+ Bank</Button>}
        </div>

        {!loading && sources.length === 0 ? (
          <EmptyState
            title="Belum ada bank pembiayaan"
            description="Untuk kontrak KPR, pilih bank penyalur dari daftar ini — contoh: BTN (KPR Subsidi), BCA (KPR Komersial)."
            action={mayWrite ? <Button size="sm" onClick={() => setShowSource(true)}>+ Bank Pertama</Button> : undefined}
          />
        ) : sources.length > 0 ? (
          <Card padding="sm" className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border text-left text-xs text-text-secondary">
                  <th className="py-2 pr-3">Kode</th>
                  <th className="py-2 pr-3">Nama</th>
                  <th className="py-2 pr-3">Jenis</th>
                  <th className="py-2 pr-3">Status</th>
                </tr>
              </thead>
              <tbody>
                {sources.map((f) => (
                  <tr key={f.id} className="border-b border-border last:border-0">
                    <td className="py-2 pr-3 font-mono text-xs">{f.code ?? "—"}</td>
                    <td className="py-2 pr-3 font-medium">{f.name}</td>
                    <td className="py-2 pr-3 text-xs">{FIN_TYPE_LABEL[f.type ?? ""] ?? f.type ?? "—"}</td>
                    <td className="py-2 pr-3">
                      <button onClick={() => toggleSource(f)} title="Klik untuk ubah status">
                        {f.is_active
                          ? <Badge variant="success">Aktif</Badge>
                          : <Badge variant="neutral">Nonaktif</Badge>}
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </Card>
        ) : null}
      </div>

      {/* Modal skema baru */}
      <Modal
        open={showScheme}
        onClose={() => setShowScheme(false)}
        title="Skema Pembayaran Baru"
        size="lg"
        footer={
          <>
            <Button variant="secondary" onClick={() => setShowScheme(false)} disabled={busy}>Batal</Button>
            <Button onClick={handleCreateScheme} loading={busy}
              disabled={!sCode.trim() || !sName.trim()}>
              Simpan
            </Button>
          </>
        }
      >
        <div className="space-y-3">
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <Input label="Kode" required value={sCode} onChange={(e) => setSCode(e.target.value)}
              placeholder="cth: KPR-SUBSIDI" hint="unik, tidak bisa diubah setelah dibuat" />
            <Input label="Nama" required value={sName} onChange={(e) => setSName(e.target.value)}
              placeholder="cth: KPR Subsidi DP 1%" />
          </div>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <Select label="Tipe skema" value={sPolicy} onChange={(e) => onPolicyChange(e.target.value)}>
              {Object.keys(POLICY_LABEL).map((p) => (
                <option key={p} value={p}>{POLICY_LABEL[p]}</option>
              ))}
            </Select>
            <Input label="DP (%)" value={sDP} onChange={(e) => setSDP(e.target.value)}
              inputMode="decimal" placeholder="10" />
          </div>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            {countLabel && (
              <Input label={countLabel} value={sCount} onChange={(e) => setSCount(e.target.value)}
                inputMode="numeric" />
            )}
            <Select label="Syarat serah terima (BAST)" value={sGate}
              onChange={(e) => setSGate(e.target.value)}>
              {Object.keys(GATE_LABEL).map((g) => (
                <option key={g} value={g}>{GATE_LABEL[g]}</option>
              ))}
            </Select>
          </div>
          <p className="text-xs text-text-tertiary">
            Parameter skema dibekukan ke tiap kontrak saat dibuat — mengubah skema ini
            tidak memengaruhi kontrak yang sudah berjalan.
          </p>
        </div>
      </Modal>

      {/* Modal bank baru */}
      <Modal
        open={showSource}
        onClose={() => setShowSource(false)}
        title="Tambah Bank Pembiayaan"
        footer={
          <>
            <Button variant="secondary" onClick={() => setShowSource(false)} disabled={busy}>Batal</Button>
            <Button onClick={handleCreateSource} loading={busy}
              disabled={!fCode.trim() || !fName.trim()}>
              Simpan
            </Button>
          </>
        }
      >
        <div className="space-y-3">
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <Input label="Kode" required value={fCode} onChange={(e) => setFCode(e.target.value)}
              placeholder="cth: BTN" />
            <Input label="Nama" required value={fName} onChange={(e) => setFName(e.target.value)}
              placeholder="cth: Bank BTN" />
          </div>
          <Select label="Jenis" value={fType} onChange={(e) => setFType(e.target.value)}>
            {Object.keys(FIN_TYPE_LABEL).map((t) => (
              <option key={t} value={t}>{FIN_TYPE_LABEL[t]}</option>
            ))}
          </Select>
        </div>
      </Modal>
    </div>
  );
}
