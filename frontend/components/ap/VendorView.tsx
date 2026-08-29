"use client";

// Master Vendor (W-11).
//
// Vendor di sini adalah master MINIMAL: identitas + status PKP. Bukan modul
// procurement — tidak ada kontrak, rating, atau termin default. Status PKP-nya
// yang paling menentukan: ia yang memutuskan apakah sebuah tagihan boleh
// membawa PPN Masukan.

import { useMemo, useState, useTransition } from "react";
import { useRouter } from "next/navigation";

import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { EmptyState } from "@/components/ui/EmptyState";
import { Input, Select, Textarea } from "@/components/ui/Input";
import { Modal } from "@/components/ui/Modal";
import { Table, TableBody, TableHead, TableRow, Td, Th } from "@/components/ui/Table";
import { useToast } from "@/components/ui/Toast";
import { Tanggal } from "@/components/format/Tanggal";
import type { Vendor } from "@/lib/types/api";
import {
  buildVendorBody,
  emptyVendorForm,
  filterVendors,
  validateVendor,
  type VendorFormState,
} from "@/components/ap/apRules";
import { createVendorAction, updateVendorAction } from "@/app/(app)/accounting/vendor/actions";

interface Props {
  vendors: Vendor[];
  canWrite: boolean;
  loadError?: string;
}

function formOf(v: Vendor): VendorFormState {
  return {
    name: v.name,
    npwp: v.npwp ?? "",
    is_pkp: v.is_pkp,
    address: v.address ?? "",
    phone: v.phone ?? "",
    email: v.email ?? "",
    bank_name: v.bank_name ?? "",
    bank_account: v.bank_account ?? "",
    note: v.note ?? "",
    is_active: v.is_active,
  };
}

export function VendorView({ vendors, canWrite, loadError }: Props) {
  const router = useRouter();
  const { toast } = useToast();

  const [q, setQ] = useState("");
  const [showInactive, setShowInactive] = useState(false);

  const [editing, setEditing] = useState<Vendor | null>(null);
  const [creating, setCreating] = useState(false);
  const [detail, setDetail] = useState<Vendor | null>(null);

  const [form, setForm] = useState<VendorFormState>(emptyVendorForm());
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [serverError, setServerError] = useState<string | null>(null);
  const [saving, startSave] = useTransition();

  const rows = useMemo(
    () => filterVendors(vendors, q, showInactive),
    [vendors, q, showInactive],
  );

  function set<K extends keyof VendorFormState>(k: K, v: VendorFormState[K]) {
    setForm((f) => ({ ...f, [k]: v }));
    setErrors((e) => {
      if (!(k in e)) return e;
      const next = { ...e };
      delete next[k as string];
      return next;
    });
  }

  function openCreate() {
    setForm(emptyVendorForm());
    setErrors({});
    setServerError(null);
    setEditing(null);
    setCreating(true);
  }

  function openEdit(v: Vendor) {
    setForm(formOf(v));
    setErrors({});
    setServerError(null);
    setDetail(null);
    setEditing(v);
  }

  function closeForm() {
    setCreating(false);
    setEditing(null);
  }

  function submit() {
    const e = validateVendor(form);
    setErrors(e);
    if (Object.keys(e).length > 0) return;

    const target = editing;
    startSave(async () => {
      setServerError(null);
      const res = target
        ? await updateVendorAction(target.id, buildVendorBody(form, true))
        : await createVendorAction(buildVendorBody(form, false));
      if (!res.ok) {
        setServerError(res.error);
        return;
      }
      toast(target ? "Vendor diperbarui" : `Vendor "${res.data.name}" ditambahkan`, "success");
      closeForm();
      router.refresh();
    });
  }

  const formOpen = creating || editing !== null;

  return (
    <>
      <Card padding="none">
        <div className="flex flex-wrap items-end gap-3 border-b border-border p-4">
          <Input
            label="Cari vendor"
            wrapperClassName="min-w-[240px] flex-1"
            placeholder="Nama, NPWP, email, atau telepon"
            value={q}
            onChange={(e) => setQ(e.target.value)}
          />
          <Select
            label="Status"
            wrapperClassName="w-48"
            value={showInactive ? "all" : "active"}
            onChange={(e) => setShowInactive(e.target.value === "all")}
          >
            <option value="active">Hanya aktif</option>
            <option value="all">Semua (termasuk nonaktif)</option>
          </Select>
          <div className="ml-auto pb-1">
            {canWrite && <Button onClick={openCreate}>Tambah Vendor</Button>}
          </div>
        </div>

        {loadError ? (
          <div className="p-4">
            <p className="text-sm text-danger">Daftar vendor gagal dimuat: {loadError}</p>
          </div>
        ) : rows.length === 0 ? (
          <EmptyState
            title={vendors.length === 0 ? "Belum ada vendor" : "Tidak ada vendor yang cocok"}
            description={
              vendors.length === 0
                ? "Vendor adalah pihak yang menagih perusahaan. Tambahkan vendor lebih dulu — tagihan hutang usaha selalu menunjuk satu vendor, dan status PKP-nya yang menentukan boleh-tidaknya tagihan membawa PPN Masukan."
                : "Ubah kata kunci pencarian atau tampilkan vendor nonaktif."
            }
            action={
              vendors.length === 0 && canWrite ? (
                <Button onClick={openCreate}>Tambah Vendor</Button>
              ) : undefined
            }
          />
        ) : (
          <Table>
            <TableHead>
              <tr>
                <Th>Nama</Th>
                <Th>NPWP</Th>
                <Th>Status Pajak</Th>
                <Th>Kontak</Th>
                <Th>Status</Th>
                <Th>Terdaftar</Th>
                <Th right>Aksi</Th>
              </tr>
            </TableHead>
            <TableBody>
              {rows.map((v) => (
                <TableRow key={v.id} subtle={!v.is_active}>
                  <Td>
                    <button
                      type="button"
                      className="text-left font-medium text-accent hover:underline"
                      onClick={() => setDetail(v)}
                    >
                      {v.name}
                    </button>
                    {v.note && (
                      <p className="mt-0.5 text-[11px] text-text-tertiary">{v.note}</p>
                    )}
                  </Td>
                  <Td mono>{v.npwp || "—"}</Td>
                  <Td>
                    {v.is_pkp ? (
                      <Badge variant="accent">PKP</Badge>
                    ) : (
                      <Badge variant="default">Non-PKP</Badge>
                    )}
                  </Td>
                  <Td>
                    <span className="text-text-secondary">{v.phone || v.email || "—"}</span>
                  </Td>
                  <Td>
                    {v.is_active ? (
                      <Badge variant="success">Aktif</Badge>
                    ) : (
                      <Badge variant="neutral">Nonaktif</Badge>
                    )}
                  </Td>
                  <Td>
                    <Tanggal value={v.created_at} />
                  </Td>
                  <Td right>
                    <div className="flex justify-end gap-2">
                      <Button size="sm" variant="ghost" onClick={() => setDetail(v)}>
                        Detail
                      </Button>
                      {canWrite && (
                        <Button size="sm" variant="secondary" onClick={() => openEdit(v)}>
                          Ubah
                        </Button>
                      )}
                    </div>
                  </Td>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Card>

      {/* ── Detail ── */}
      <Modal
        open={detail !== null}
        onClose={() => setDetail(null)}
        title={detail?.name ?? ""}
        description="Detail vendor"
        size="md"
        footer={
          detail && canWrite ? (
            <Button variant="secondary" onClick={() => openEdit(detail)}>
              Ubah
            </Button>
          ) : undefined
        }
      >
        {detail && (
          <dl className="grid grid-cols-1 gap-x-6 gap-y-3 sm:grid-cols-2">
            <Field label="NPWP" value={detail.npwp || "—"} mono />
            <Field label="Status pajak" value={detail.is_pkp ? "PKP" : "Non-PKP"} />
            <Field label="Telepon" value={detail.phone || "—"} />
            <Field label="Email" value={detail.email || "—"} />
            <Field label="Bank" value={detail.bank_name || "—"} />
            <Field label="No. rekening" value={detail.bank_account || "—"} mono />
            <Field label="Status" value={detail.is_active ? "Aktif" : "Nonaktif"} />
            <div className="sm:col-span-2">
              <Field label="Alamat" value={detail.address || "—"} />
            </div>
            <div className="sm:col-span-2">
              <Field label="Catatan" value={detail.note || "—"} />
            </div>
          </dl>
        )}
      </Modal>

      {/* ── Buat / ubah ── */}
      <Modal
        open={formOpen}
        onClose={closeForm}
        title={editing ? `Ubah Vendor — ${editing.name}` : "Tambah Vendor"}
        description={
          editing
            ? "Vendor tidak pernah dihapus. Untuk berhenti memakainya, nonaktifkan — tagihan lama tetap menunjuk vendor ini."
            : "Isi identitas vendor. Status PKP menentukan boleh-tidaknya tagihan dari vendor ini membawa PPN Masukan."
        }
        size="lg"
        footer={
          <>
            <Button variant="secondary" onClick={closeForm} disabled={saving}>
              Batal
            </Button>
            <Button onClick={submit} loading={saving}>
              {editing ? "Simpan Perubahan" : "Simpan Vendor"}
            </Button>
          </>
        }
      >
        {serverError && (
          <div className="mb-4 rounded-lg border border-danger/30 bg-danger-bg/50 p-3">
            <p className="text-sm text-danger">{serverError}</p>
          </div>
        )}

        <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
          <div className="md:col-span-2">
            <Input
              label="Nama Vendor"
              required
              placeholder="mis. CV Karya Beton"
              value={form.name}
              onChange={(e) => set("name", e.target.value)}
              error={errors.name}
            />
          </div>

          <Input
            label="NPWP"
            placeholder="00.000.000.0-000.000"
            value={form.npwp}
            onChange={(e) => set("npwp", e.target.value)}
            error={errors.npwp}
          />

          <Select
            label="Status Pajak"
            value={form.is_pkp ? "pkp" : "non"}
            onChange={(e) => set("is_pkp", e.target.value === "pkp")}
            hint="Hanya vendor PKP yang tagihannya boleh membawa PPN Masukan."
          >
            <option value="non">Non-PKP</option>
            <option value="pkp">PKP (Pengusaha Kena Pajak)</option>
          </Select>

          <Input
            label="Telepon"
            value={form.phone}
            onChange={(e) => set("phone", e.target.value)}
          />
          <Input
            label="Email"
            value={form.email}
            onChange={(e) => set("email", e.target.value)}
            error={errors.email}
          />

          <Input
            label="Nama Bank"
            value={form.bank_name}
            onChange={(e) => set("bank_name", e.target.value)}
          />
          <Input
            label="No. Rekening"
            value={form.bank_account}
            onChange={(e) => set("bank_account", e.target.value)}
          />

          <div className="md:col-span-2">
            <Textarea
              label="Alamat"
              rows={2}
              value={form.address}
              onChange={(e) => set("address", e.target.value)}
            />
          </div>

          <div className="md:col-span-2">
            <Textarea
              label="Catatan"
              rows={2}
              value={form.note}
              onChange={(e) => set("note", e.target.value)}
            />
          </div>

          {editing && (
            <Select
              label="Status Vendor"
              value={form.is_active ? "aktif" : "nonaktif"}
              onChange={(e) => set("is_active", e.target.value === "aktif")}
              hint="Vendor nonaktif tidak bisa dipilih pada tagihan baru."
            >
              <option value="aktif">Aktif</option>
              <option value="nonaktif">Nonaktif</option>
            </Select>
          )}
        </div>
      </Modal>
    </>
  );
}

function Field({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div>
      <dt className="text-[11px] uppercase tracking-wide text-text-tertiary">{label}</dt>
      <dd className={`mt-0.5 text-sm text-text-primary ${mono ? "font-mono" : ""}`}>
        {value}
      </dd>
    </div>
  );
}
