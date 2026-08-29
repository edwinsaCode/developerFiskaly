"use client";

// PRODUCT HARDENING — master Sales Person + Tim Sales. Sebelumnya TIDAK ADA
// UI create → dropdown salesperson di kontrak kosong untuk tenant baru.
// Backend: /sales-persons, /sales-teams (CRUD sudah ada sejak Increment 9).

import { useCallback, useEffect, useMemo, useState } from "react";
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
  fetchSalesPersons,
  fetchSalesTeams,
  createSalesPerson,
  createSalesTeam,
  updateSalesPerson,
  type SalesPerson,
  type SalesTeam,
} from "@/lib/api/party";

// Saran kode berikutnya: prefix + nomor urut 3 digit (SP-001, SP-002, …).
function nextCode(prefix: string, existing: { code?: string }[]): string {
  let max = 0;
  for (const e of existing) {
    const m = (e.code ?? "").match(new RegExp(`^${prefix}-?(\\d+)$`, "i"));
    if (m) max = Math.max(max, parseInt(m[1], 10));
  }
  return `${prefix}-${String(max + 1).padStart(3, "0")}`;
}

export function SalesSection({ token }: { token: string }) {
  const mayWrite = useMayWrite();
  const { toast } = useToast();
  const [persons, setPersons] = useState<SalesPerson[]>([]);
  const [teams, setTeams] = useState<SalesTeam[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [showPerson, setShowPerson] = useState(false);
  const [showTeam, setShowTeam] = useState(false);

  const [pCode, setPCode] = useState("");
  const [pName, setPName] = useState("");
  const [pPhone, setPPhone] = useState("");
  const [pTeam, setPTeam] = useState("");
  const [tCode, setTCode] = useState("");
  const [tName, setTName] = useState("");

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const [ps, ts] = await Promise.all([
        fetchSalesPersons(token),
        fetchSalesTeams(token).catch(() => [] as SalesTeam[]),
      ]);
      setPersons(ps);
      setTeams(ts);
    } catch {
      // EmptyState menjelaskan
    } finally {
      setLoading(false);
    }
  }, [token]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const teamName = useMemo(() => {
    const m = new Map<number, string>();
    teams.forEach((t) => m.set(t.id, t.name));
    return m;
  }, [teams]);

  function openPersonForm() {
    setPCode(nextCode("SP", persons));
    setPName("");
    setPPhone("");
    setPTeam("");
    setShowPerson(true);
  }

  function openTeamForm() {
    setTCode(nextCode("TIM", teams));
    setTName("");
    setShowTeam(true);
  }

  async function handleCreatePerson() {
    setBusy(true);
    try {
      await createSalesPerson(token, {
        code: pCode.trim(),
        name: pName.trim(),
        phone: pPhone.trim() || undefined,
        sales_team_id: pTeam ? parseInt(pTeam, 10) : undefined,
      });
      toast(`Sales ${pName.trim()} ditambahkan — siap dipakai di kontrak`, "success");
      setShowPerson(false);
      await refresh();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal menambah sales", "error");
    } finally {
      setBusy(false);
    }
  }

  async function handleCreateTeam() {
    setBusy(true);
    try {
      await createSalesTeam(token, { code: tCode.trim(), name: tName.trim() });
      toast(`Tim ${tName.trim()} dibuat`, "success");
      setShowTeam(false);
      await refresh();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal membuat tim", "error");
    } finally {
      setBusy(false);
    }
  }

  async function handleToggleActive(p: SalesPerson) {
    try {
      await updateSalesPerson(token, p.id, { is_active: !p.is_active });
      await refresh();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal mengubah status", "error");
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between flex-wrap gap-2">
        <p className="text-sm text-text-secondary">
          Sales person wajib ada sebelum membuat kontrak — komisi dihitung per sales.
        </p>
        {mayWrite && (
          <div className="flex gap-2">
            <Button size="sm" variant="secondary" onClick={openTeamForm}>+ Tim</Button>
            <Button size="sm" onClick={openPersonForm}>+ Sales</Button>
          </div>
        )}
      </div>

      {loading ? (
        <Card padding="sm">
          <div className="animate-pulse space-y-2">
            {[...Array(3)].map((_, i) => <div key={i} className="h-9 rounded bg-border-subtle" />)}
          </div>
        </Card>
      ) : persons.length === 0 ? (
        <EmptyState
          title="Belum ada sales person"
          description="Tambahkan minimal satu sales person — nama ini dipilih saat membuat kontrak penjualan dan menjadi dasar perhitungan komisi."
          action={mayWrite ? <Button size="sm" onClick={openPersonForm}>+ Sales Pertama</Button> : undefined}
        />
      ) : (
        <Card padding="sm" className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border text-left text-xs text-text-secondary">
                <th className="py-2 pr-3">Kode</th>
                <th className="py-2 pr-3">Nama</th>
                <th className="py-2 pr-3">Tim</th>
                <th className="py-2 pr-3">Status</th>
              </tr>
            </thead>
            <tbody>
              {persons.map((p) => (
                <tr key={p.id} className="border-b border-border last:border-0">
                  <td className="py-2 pr-3 font-mono text-xs">{p.code}</td>
                  <td className="py-2 pr-3 font-medium">{p.name}</td>
                  <td className="py-2 pr-3 text-xs text-text-secondary">
                    {p.sales_team_id ? teamName.get(p.sales_team_id) ?? "—" : "—"}
                  </td>
                  <td className="py-2 pr-3">
                    <button onClick={() => handleToggleActive(p)} title="Klik untuk ubah status">
                      {p.is_active
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

      {teams.length > 0 && (
        <Card padding="sm">
          <p className="text-xs font-semibold uppercase tracking-wide text-text-secondary mb-2">
            Tim Sales
          </p>
          <div className="flex flex-wrap gap-2">
            {teams.map((t) => (
              <Badge key={t.id} variant="accent">{t.code} · {t.name}</Badge>
            ))}
          </div>
        </Card>
      )}

      <Modal
        open={showPerson}
        onClose={() => setShowPerson(false)}
        title="Tambah Sales Person"
        footer={
          <>
            <Button variant="secondary" onClick={() => setShowPerson(false)} disabled={busy}>Batal</Button>
            <Button onClick={handleCreatePerson} loading={busy}
              disabled={!pCode.trim() || !pName.trim()}>
              Simpan
            </Button>
          </>
        }
      >
        <div className="space-y-3">
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <Input label="Kode" required value={pCode} onChange={(e) => setPCode(e.target.value)}
              hint="disarankan otomatis — boleh diubah" />
            <Input label="Nama" required value={pName} onChange={(e) => setPName(e.target.value)}
              placeholder="cth: Budi Santoso" />
          </div>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <Input label="No. HP" value={pPhone} onChange={(e) => setPPhone(e.target.value)}
              placeholder="08xxxxxxxxxx" />
            <Select label="Tim (opsional)" value={pTeam} onChange={(e) => setPTeam(e.target.value)}>
              <option value="">— tanpa tim —</option>
              {teams.map((t) => <option key={t.id} value={t.id}>{t.name}</option>)}
            </Select>
          </div>
        </div>
      </Modal>

      <Modal
        open={showTeam}
        onClose={() => setShowTeam(false)}
        title="Buat Tim Sales"
        footer={
          <>
            <Button variant="secondary" onClick={() => setShowTeam(false)} disabled={busy}>Batal</Button>
            <Button onClick={handleCreateTeam} loading={busy}
              disabled={!tCode.trim() || !tName.trim()}>
              Simpan
            </Button>
          </>
        }
      >
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <Input label="Kode" required value={tCode} onChange={(e) => setTCode(e.target.value)} />
          <Input label="Nama" required value={tName} onChange={(e) => setTName(e.target.value)}
            placeholder="cth: Tim Marketing A" />
        </div>
      </Modal>
    </div>
  );
}
