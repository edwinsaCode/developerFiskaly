"use client";

import { useState, useEffect, useCallback } from "react";
import type { Account, AccountType } from "@/lib/types/api";
import { fetchAccounts, createAccount, updateAccount } from "@/lib/api/ledger";
import { Button } from "@/components/ui/Button";
import { Input, Select } from "@/components/ui/Input";
import { Modal } from "@/components/ui/Modal";
import { Card } from "@/components/ui/Card";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";
import { Skeleton } from "@/components/ui/Skeleton";
import { Can } from "@/components/ui/Can";

// ── Constants ─────────────────────────────────────────────────────────────────

const TYPE_ORDER: AccountType[] = ["asset", "liability", "equity", "revenue", "expense"];

const TYPE_LABELS: Record<AccountType, string> = {
  asset: "Aset",
  liability: "Kewajiban",
  equity: "Ekuitas",
  revenue: "Pendapatan",
  expense: "Beban",
};

const NB_LABELS: Record<string, string> = {
  debit: "D",
  credit: "K",
};

// Palet token "Warm Ledger" — kategori akun tetap dapat dibedakan tanpa
// tabrakan warna; revenue/expense pakai semantik konvensional (hijau/merah).
const TYPE_COLOR: Record<AccountType, string> = {
  asset:     "bg-chart-4/10 text-chart-4 border-chart-4/25",
  liability: "bg-chart-3/10 text-chart-3 border-chart-3/25",
  equity:    "bg-chart-5/10 text-chart-5 border-chart-5/25",
  revenue:   "bg-success-bg text-success border-success/25",
  expense:   "bg-danger-bg text-danger border-danger/25",
};

// ── Form state ────────────────────────────────────────────────────────────────

interface FormState {
  code: string;
  name: string;
  type: AccountType;
  description: string;
  is_active: boolean;
}

const EMPTY_FORM: FormState = {
  code: "",
  name: "",
  type: "asset",
  description: "",
  is_active: true,
};

// ── Auto-numbering ────────────────────────────────────────────────────────────
// Nomor akun mengikuti 5 akun master: 1=Aset, 2=Kewajiban, 3=Ekuitas,
// 4=Pendapatan, 5=Beban (konvensi Jurnal.id). Saran = nomor tertinggi pada
// prefix tipe + 100 (menyisakan ruang sub-akun); bisa diubah manual.

const TYPE_PREFIX: Record<AccountType, string> = {
  asset: "1",
  liability: "2",
  equity: "3",
  revenue: "4",
  expense: "5",
};

function suggestCode(type: AccountType, accounts: Account[]): string {
  const prefix = TYPE_PREFIX[type];
  let max = 0;
  for (const a of accounts) {
    const m = a.code.match(new RegExp(`^${prefix}-(\\d+)$`));
    if (m) max = Math.max(max, parseInt(m[1], 10));
  }
  const next = max > 0 ? max + 100 : 1000;
  return `${prefix}-${String(next).padStart(4, "0")}`;
}

// ── Component ─────────────────────────────────────────────────────────────────

interface Props {
  token: string;
}

export function COAManager({ token }: Props) {
  const [accounts, setAccounts]     = useState<Account[]>([]);
  const [loading, setLoading]       = useState(true);
  const [error, setError]           = useState<string | null>(null);
  const [search, setSearch]         = useState("");
  const [filterType, setFilterType] = useState<AccountType | "all">("all");
  const [modal, setModal]           = useState<{ mode: "add" | "edit"; account?: Account } | null>(null);
  const [form, setForm]             = useState<FormState>(EMPTY_FORM);
  const [codeTouched, setCodeTouched] = useState(false);
  const [formError, setFormError]   = useState<string | null>(null);
  const [saving, setSaving]         = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setAccounts(await fetchAccounts(token));
    } catch (e) {
      setError(e instanceof Error ? e.message : "Gagal memuat daftar akun");
    } finally {
      setLoading(false);
    }
  }, [token]);

  useEffect(() => { load(); }, [load]);

  // ── Modal helpers ───────────────────────────────────────────────────────────

  function openAdd() {
    setForm({ ...EMPTY_FORM, code: suggestCode(EMPTY_FORM.type, accounts) });
    setCodeTouched(false);
    setFormError(null);
    setModal({ mode: "add" });
  }

  function openEdit(account: Account) {
    setForm({
      code:        account.code,
      name:        account.name,
      type:        account.type,
      description: account.description ?? "",
      is_active:   account.is_active,
    });
    setFormError(null);
    setModal({ mode: "edit", account });
  }

  function closeModal() {
    setModal(null);
    setFormError(null);
  }

  // ── Save ────────────────────────────────────────────────────────────────────

  async function handleSave() {
    setFormError(null);
    if (!form.code.trim())  { setFormError("Kode akun wajib diisi"); return; }
    if (!form.name.trim())  { setFormError("Nama akun wajib diisi"); return; }

    setSaving(true);
    try {
      if (modal?.mode === "add") {
        await createAccount(token, {
          code:        form.code.trim(),
          name:        form.name.trim(),
          type:        form.type,
          description: form.description.trim() || undefined,
        });
      } else if (modal?.mode === "edit" && modal.account) {
        await updateAccount(token, modal.account.id, {
          name:        form.name.trim(),
          description: form.description.trim(),
          is_active:   form.is_active,
        });
      }
      closeModal();
      await load();
    } catch (e) {
      setFormError(e instanceof Error ? e.message : "Gagal menyimpan akun");
    } finally {
      setSaving(false);
    }
  }

  // ── Filter / group ──────────────────────────────────────────────────────────

  const filtered = accounts.filter((a) => {
    if (filterType !== "all" && a.type !== filterType) return false;
    if (search) {
      const q = search.toLowerCase();
      return a.code.toLowerCase().includes(q) || a.name.toLowerCase().includes(q);
    }
    return true;
  });

  const grouped = TYPE_ORDER.map((type) => ({
    type,
    label: TYPE_LABELS[type],
    items: filtered.filter((a) => a.type === type),
  })).filter((g) => g.items.length > 0);

  const totalFiltered = filtered.length;

  // ── Render ──────────────────────────────────────────────────────────────────

  return (
    <>
      {/* Toolbar */}
      <div className="flex flex-col sm:flex-row sm:items-center gap-3">
        <div className="flex-1 flex gap-2">
          <div className="relative flex-1 max-w-xs">
            <input
              type="text"
              placeholder="Cari kode atau nama akun..."
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="w-full pl-9 pr-3 py-2 text-sm rounded border border-border bg-surface
                text-text-primary placeholder:text-text-tertiary outline-none
                focus:border-accent focus:ring-1 focus:ring-accent/30 transition-colors"
            />
            <span className="absolute left-3 top-1/2 -translate-y-1/2 text-text-tertiary pointer-events-none">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                <circle cx="11" cy="11" r="8" /><path d="m21 21-4.35-4.35" />
              </svg>
            </span>
          </div>
          <select
            value={filterType}
            onChange={(e) => setFilterType(e.target.value as AccountType | "all")}
            className="h-[38px] px-3 text-sm rounded border border-border bg-surface
              text-text-primary outline-none focus:border-accent focus:ring-1 focus:ring-accent/30"
          >
            <option value="all">Semua Tipe</option>
            {TYPE_ORDER.map((t) => (
              <option key={t} value={t}>{TYPE_LABELS[t]}</option>
            ))}
          </select>
        </div>
        <div className="flex items-center gap-3">
          {!loading && (
            <span className="text-xs text-text-tertiary hidden sm:block">
              {totalFiltered} akun
            </span>
          )}
          <Can roles={["owner", "accountant"]}>
            <Button onClick={openAdd} size="sm">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5">
                <path d="M12 5v14M5 12h14" />
              </svg>
              Tambah Akun
            </Button>
          </Can>
        </div>
      </div>

      {/* Content */}
      {loading ? (
        <div className="space-y-4">
          {[1, 2, 3].map((i) => (
            <Card key={i} padding="none">
              <div className="px-5 py-3 border-b border-border">
                <Skeleton className="h-4 w-24" />
              </div>
              {[1, 2, 3].map((j) => (
                <div key={j} className="px-5 py-3 flex items-center gap-4 border-b border-border last:border-0">
                  <Skeleton className="h-4 w-16" />
                  <Skeleton className="h-4 flex-1" />
                  <Skeleton className="h-4 w-12" />
                  <Skeleton className="h-6 w-14" />
                </div>
              ))}
            </Card>
          ))}
        </div>
      ) : error ? (
        <ErrorState description={error} onRetry={load} />
      ) : accounts.length === 0 ? (
        <EmptyState
          title="Belum ada akun (Chart of Accounts kosong)"
          description="Daftar Akun adalah fondasi seluruh pencatatan: setiap jurnal, biaya, dan penjualan diposting ke akun di sini. Tambahkan akun pertama Anda, atau minta owner menjalankan seed COA standar."
          action={
            <Can roles={["owner", "accountant"]}>
              <Button onClick={openAdd} size="sm">
                Tambah Akun Pertama
              </Button>
            </Can>
          }
        />
      ) : grouped.length === 0 ? (
        <EmptyState title="Tidak ada akun yang cocok" description="Coba ubah filter atau kata kunci pencarian." />
      ) : (
        <div className="space-y-5">
          {grouped.map(({ type, label, items }) => (
            <Card key={type} padding="none">
              {/* Group header */}
              <div className="flex items-center gap-3 px-5 py-3 border-b border-border bg-border-subtle/20">
                <span className={`text-[10px] font-semibold px-2 py-0.5 rounded border ${TYPE_COLOR[type]}`}>
                  {label.toUpperCase()}
                </span>
                <span className="text-xs text-text-tertiary">{items.length} akun</span>
              </div>

              {/* Table */}
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-border bg-border-subtle/10">
                      <th className="text-left px-5 py-2.5 text-xs font-medium text-text-secondary w-28">Kode</th>
                      <th className="text-left px-4 py-2.5 text-xs font-medium text-text-secondary">Nama Akun</th>
                      <th className="text-left px-4 py-2.5 text-xs font-medium text-text-secondary w-16">NB</th>
                      <th className="text-left px-4 py-2.5 text-xs font-medium text-text-secondary w-20">Status</th>
                      <th className="px-4 py-2.5 w-20" />
                    </tr>
                  </thead>
                  <tbody>
                    {items.map((acc) => (
                      <tr
                        key={acc.id}
                        className={`border-b border-border last:border-0 transition-colors
                          ${acc.is_active ? "hover:bg-border-subtle/20" : "opacity-50 hover:bg-border-subtle/20"}`}
                      >
                        <td className="px-5 py-3">
                          <span className="font-mono text-xs text-text-tertiary">{acc.code}</span>
                        </td>
                        <td className="px-4 py-3">
                          <div>
                            <span className="text-text-primary">{acc.name}</span>
                            {acc.description && (
                              <p className="text-xs text-text-tertiary mt-0.5 truncate max-w-xs">{acc.description}</p>
                            )}
                          </div>
                        </td>
                        <td className="px-4 py-3">
                          <span className="font-mono text-xs font-medium text-text-secondary">
                            {NB_LABELS[acc.normal_balance] ?? acc.normal_balance}
                          </span>
                        </td>
                        <td className="px-4 py-3">
                          {acc.is_active ? (
                            <span className="inline-flex items-center gap-1 text-xs text-success">
                              <span className="w-1.5 h-1.5 rounded-full bg-success" />
                              Aktif
                            </span>
                          ) : (
                            <span className="inline-flex items-center gap-1 text-xs text-text-tertiary">
                              <span className="w-1.5 h-1.5 rounded-full bg-border" />
                              Nonaktif
                            </span>
                          )}
                        </td>
                        <td className="px-4 py-3 text-right">
                          <Can roles={["owner", "accountant"]}>
                            {acc.is_system ? (
                              <span className="text-xs text-text-tertiary" title="Akun sistem tidak bisa diedit">sistem</span>
                            ) : (
                              <button
                                onClick={() => openEdit(acc)}
                                className="text-xs text-accent hover:underline"
                              >
                                Edit
                              </button>
                            )}
                          </Can>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </Card>
          ))}
        </div>
      )}

      {/* Add / Edit modal */}
      <Modal
        open={modal !== null}
        onClose={closeModal}
        title={modal?.mode === "add" ? "Tambah Akun Baru" : "Edit Akun"}
        size="md"
        footer={
          <>
            <Button variant="secondary" onClick={closeModal} disabled={saving}>Batal</Button>
            <Button onClick={handleSave} loading={saving}>Simpan</Button>
          </>
        }
      >
        <div className="space-y-4">
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <Input
              label="Kode Akun"
              required
              placeholder="mis. 1-1100"
              value={form.code}
              onChange={(e) => {
                setCodeTouched(true);
                setForm((f) => ({ ...f, code: e.target.value }));
              }}
              disabled={modal?.mode === "edit"}
              hint={
                modal?.mode === "edit"
                  ? "Kode akun tidak bisa diubah setelah dibuat"
                  : "otomatis dari tipe akun — boleh diubah"
              }
            />
            <Select
              label="Tipe"
              required
              value={form.type}
              onChange={(e) => {
                const type = e.target.value as AccountType;
                // Auto-numbering: selama kode belum disentuh manual, saran
                // kode mengikuti prefix tipe (1=Aset … 5=Beban).
                setForm((f) => ({
                  ...f,
                  type,
                  code: modal?.mode === "add" && !codeTouched ? suggestCode(type, accounts) : f.code,
                }));
              }}
              disabled={modal?.mode === "edit"}
            >
              {TYPE_ORDER.map((t) => (
                <option key={t} value={t}>{TYPE_LABELS[t]}</option>
              ))}
            </Select>
          </div>

          <Input
            label="Nama Akun"
            required
            placeholder="mis. Bank BCA - Operasional"
            value={form.name}
            onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
          />

          <Input
            label="Deskripsi"
            placeholder="Keterangan tambahan (opsional)"
            value={form.description}
            onChange={(e) => setForm((f) => ({ ...f, description: e.target.value }))}
          />

          {modal?.mode === "edit" && (
            <label className="flex items-center gap-3 cursor-pointer select-none">
              <input
                type="checkbox"
                checked={form.is_active}
                onChange={(e) => setForm((f) => ({ ...f, is_active: e.target.checked }))}
                className="w-4 h-4 accent-accent"
              />
              <span className="text-sm text-text-primary">Akun aktif</span>
              <span className="text-xs text-text-tertiary">(nonaktifkan untuk menyembunyikan dari pilihan, tanpa menghapus histori)</span>
            </label>
          )}

          {formError && (
            <p className="text-sm text-danger bg-danger-bg border border-danger/20 rounded px-3 py-2">
              {formError}
            </p>
          )}
        </div>
      </Modal>
    </>
  );
}
