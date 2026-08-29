"use client";

// PRODUCT HARDENING — master Pelanggan. Sebelumnya customer HANYA bisa dibuat
// via quick-create di modal booking — tidak ada tempat melihat/melengkapi data
// (NIK, NPWP, alamat — dibutuhkan PPJB & faktur). Backend: /customers (CRUD).

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
  fetchCustomers,
  createCustomer,
  updateCustomer,
  type Customer,
} from "@/lib/api/party";

function nextCustomerCode(existing: Customer[]): string {
  let max = 0;
  for (const c of existing) {
    const m = c.code.match(/^CUST-?(\d+)$/i);
    if (m) max = Math.max(max, parseInt(m[1], 10));
  }
  return `CUST-${String(max + 1).padStart(4, "0")}`;
}

interface FormState {
  code: string;
  name: string;
  type: "individual" | "company";
  id_number: string;
  npwp: string;
  phone: string;
  email: string;
  address: string;
}

const EMPTY_FORM: FormState = {
  code: "", name: "", type: "individual",
  id_number: "", npwp: "", phone: "", email: "", address: "",
};

export function CustomersSection({ token }: { token: string }) {
  const mayWrite = useMayWrite();
  const { toast } = useToast();
  const [rows, setRows] = useState<Customer[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [showForm, setShowForm] = useState(false);
  const [editing, setEditing] = useState<Customer | null>(null);
  const [form, setForm] = useState<FormState>(EMPTY_FORM);
  const [query, setQuery] = useState("");

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      setRows(await fetchCustomers(token));
    } catch {
      // EmptyState menjelaskan
    } finally {
      setLoading(false);
    }
  }, [token]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return rows;
    return rows.filter(
      (c) =>
        c.name.toLowerCase().includes(q) ||
        c.code.toLowerCase().includes(q) ||
        (c.phone ?? "").includes(q),
    );
  }, [rows, query]);

  function set<K extends keyof FormState>(key: K, value: FormState[K]) {
    setForm((f) => ({ ...f, [key]: value }));
  }

  function openCreate() {
    setEditing(null);
    setForm({ ...EMPTY_FORM, code: nextCustomerCode(rows) });
    setShowForm(true);
  }

  function openEdit(c: Customer) {
    setEditing(c);
    setForm({
      code: c.code,
      name: c.name,
      type: c.type ?? "individual",
      id_number: c.id_number ?? "",
      npwp: c.npwp ?? "",
      phone: c.phone ?? "",
      email: c.email ?? "",
      address: c.address ?? "",
    });
    setShowForm(true);
  }

  async function handleSubmit() {
    setBusy(true);
    try {
      if (editing) {
        await updateCustomer(token, editing.id, {
          name: form.name.trim(),
          phone: form.phone.trim(),
          email: form.email.trim(),
          npwp: form.npwp.trim(),
          address: form.address.trim(),
        });
        toast(`Data ${form.name.trim()} diperbarui`, "success");
      } else {
        await createCustomer(token, {
          code: form.code.trim(),
          name: form.name.trim(),
          type: form.type,
          id_number: form.id_number.trim() || undefined,
          npwp: form.npwp.trim() || undefined,
          phone: form.phone.trim() || undefined,
          email: form.email.trim() || undefined,
          address: form.address.trim() || undefined,
        });
        toast(`Pelanggan ${form.name.trim()} ditambahkan`, "success");
      }
      setShowForm(false);
      await refresh();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal menyimpan pelanggan", "error");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between flex-wrap gap-2">
        <p className="text-sm text-text-secondary">
          Data lengkap (NIK, NPWP, alamat) dibutuhkan untuk PPJB, AJB, dan faktur pajak.
        </p>
        <div className="flex gap-2 items-center">
          <Input value={query} onChange={(e) => setQuery(e.target.value)}
            placeholder="Cari nama / kode / HP…" className="w-52" />
          {mayWrite && <Button size="sm" onClick={openCreate}>+ Pelanggan</Button>}
        </div>
      </div>

      {loading ? (
        <Card padding="sm">
          <div className="animate-pulse space-y-2">
            {[...Array(4)].map((_, i) => <div key={i} className="h-9 rounded bg-border-subtle" />)}
          </div>
        </Card>
      ) : filtered.length === 0 ? (
        <EmptyState
          title={query ? "Tidak ada hasil" : "Belum ada pelanggan"}
          description={
            query
              ? `Tidak ada pelanggan yang cocok dengan "${query}".`
              : "Pelanggan juga bisa dibuat cepat dari form booking — tapi lengkapi NIK & NPWP di sini agar dokumen (PPJB/faktur) valid."
          }
          action={!query && mayWrite ? <Button size="sm" onClick={openCreate}>+ Pelanggan Pertama</Button> : undefined}
        />
      ) : (
        <Card padding="sm" className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border text-left text-xs text-text-secondary">
                <th className="py-2 pr-3">Kode</th>
                <th className="py-2 pr-3">Nama</th>
                <th className="py-2 pr-3">Tipe</th>
                <th className="py-2 pr-3">HP</th>
                <th className="py-2 pr-3">NPWP</th>
                <th className="py-2 pr-3">Kelengkapan</th>
                <th className="py-2 pr-3"></th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((c) => {
                const complete = Boolean((c.id_number || c.npwp) && c.phone && c.address);
                return (
                  <tr key={c.id} className="border-b border-border last:border-0">
                    <td className="py-2 pr-3 font-mono text-xs">{c.code}</td>
                    <td className="py-2 pr-3 font-medium">{c.name}</td>
                    <td className="py-2 pr-3 text-xs">
                      {c.type === "company" ? "Perusahaan" : "Perorangan"}
                    </td>
                    <td className="py-2 pr-3 text-xs">{c.phone || "—"}</td>
                    <td className="py-2 pr-3 text-xs font-mono">{c.npwp || "—"}</td>
                    <td className="py-2 pr-3">
                      {complete
                        ? <Badge variant="success">Lengkap</Badge>
                        : <Badge variant="warning">Perlu dilengkapi</Badge>}
                    </td>
                    <td className="py-2 pr-3 text-right">
                      <button className="text-xs text-accent hover:underline"
                        onClick={() => openEdit(c)}>
                        Edit
                      </button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </Card>
      )}

      <Modal
        open={showForm}
        onClose={() => setShowForm(false)}
        title={editing ? `Edit Pelanggan — ${editing.code}` : "Pelanggan Baru"}
        size="lg"
        footer={
          <>
            <Button variant="secondary" onClick={() => setShowForm(false)} disabled={busy}>Batal</Button>
            <Button onClick={handleSubmit} loading={busy}
              disabled={!form.code.trim() || !form.name.trim()}>
              {editing ? "Simpan Perubahan" : "Tambah"}
            </Button>
          </>
        }
      >
        <div className="space-y-3">
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
            <Input label="Kode" required value={form.code} disabled={Boolean(editing)}
              onChange={(e) => set("code", e.target.value)}
              hint={editing ? "kode tidak bisa diubah" : "disarankan otomatis"} />
            <div className="sm:col-span-2">
              <Input label="Nama Lengkap" required value={form.name}
                onChange={(e) => set("name", e.target.value)}
                placeholder="sesuai KTP / akta perusahaan" />
            </div>
          </div>
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
            <Select label="Tipe" value={form.type} disabled={Boolean(editing)}
              onChange={(e) => set("type", e.target.value as FormState["type"])}>
              <option value="individual">Perorangan</option>
              <option value="company">Perusahaan</option>
            </Select>
            <Input label={form.type === "company" ? "No. Akta/NIB" : "NIK (KTP)"}
              value={form.id_number} disabled={Boolean(editing)}
              onChange={(e) => set("id_number", e.target.value)} />
            <Input label="NPWP" value={form.npwp}
              onChange={(e) => set("npwp", e.target.value)} />
          </div>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <Input label="No. HP" value={form.phone}
              onChange={(e) => set("phone", e.target.value)} placeholder="08xxxxxxxxxx" />
            <Input label="Email" type="email" value={form.email}
              onChange={(e) => set("email", e.target.value)} />
          </div>
          <Input label="Alamat" value={form.address}
            onChange={(e) => set("address", e.target.value)}
            placeholder="alamat domisili / kantor" />
        </div>
      </Modal>
    </div>
  );
}
