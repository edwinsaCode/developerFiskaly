"use client";

// PRODUCT HARDENING — manajemen pengguna tenant. Backend: GET /users,
// POST /users, PATCH /users/{id} (RequireRole owner). Non-owner mendapat 403
// → pesan jelas.
//
// W-12: nama, email, password, dan role bisa diisi saat membuat DAN diubah
// setelahnya. Email tetap kredensial login; mengubahnya mengubah cara user
// masuk, jadi form menyebutkannya terang-terangan.

import { useCallback, useEffect, useState } from "react";
import { Card } from "@/components/ui/Card";
import { Button } from "@/components/ui/Button";
import { Badge } from "@/components/ui/Badge";
import { Modal } from "@/components/ui/Modal";
import { Input, Select } from "@/components/ui/Input";
import { EmptyState } from "@/components/ui/EmptyState";
import { useToast } from "@/components/ui/Toast";
import { ApiError } from "@/lib/api/client";
import { fetchUsers, createUser, updateUser, type AppUser } from "@/lib/api/party";
import { ALL_ROLES, ROLE_LABELS, ROLE_DESCRIPTIONS, canManageUsers } from "@/lib/roles";
import { useUserSafe } from "@/lib/context/UserContext";
import type { Role } from "@/lib/types/api";

function roleVariant(role: string): "success" | "accent" | "warning" | "neutral" {
  if (role === "owner") return "success";
  if (role === "accountant") return "accent";
  if (role === "marketing") return "warning";
  return "neutral";
}

export function UsersSection({ token }: { token: string }) {
  const { toast } = useToast();
  const me = useUserSafe();
  // Hanya Pemilik yang boleh menambah/mengubah pengguna — backend menolak yang
  // lain dengan 403. Menampilkan tombolnya kepada mereka berarti menjanjikan
  // sesuatu yang pasti gagal setelah form diisi.
  const mayManage = me ? canManageUsers(me.role) : false;
  const [rows, setRows] = useState<AppUser[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [loadErr, setLoadErr] = useState<string | null>(null);

  // Satu form untuk dua mode: `editing === null` berarti tambah.
  const [showForm, setShowForm] = useState(false);
  const [editing, setEditing] = useState<AppUser | null>(null);
  const [fName, setFName] = useState("");
  const [fEmail, setFEmail] = useState("");
  const [fPassword, setFPassword] = useState("");
  const [fRole, setFRole] = useState<Role>("accountant");

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      setRows(await fetchUsers(token));
      setLoadErr(null);
    } catch (err) {
      // Daftar gagal dimuat ≠ daftar kosong. Menelan error di sini membuat
      // layar berbunyi "Belum ada pengguna lain" padahal penyebabnya izin atau
      // jaringan — pesan yang salah, dan pemakai mencari-cari yang tidak ada.
      setRows([]);
      setLoadErr(
        err instanceof ApiError && err.status === 403
          ? "Anda tidak punya izin melihat daftar pengguna."
          : "Daftar pengguna gagal dimuat."
      );
    } finally {
      setLoading(false);
    }
  }, [token]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  function openCreate() {
    setEditing(null);
    setFName("");
    setFEmail("");
    setFPassword("");
    setFRole("accountant");
    setShowForm(true);
  }

  function openEdit(u: AppUser) {
    setEditing(u);
    setFName(u.name ?? "");
    setFEmail(u.email);
    setFPassword(""); // kosong = jangan ubah password
    setFRole(u.role);
    setShowForm(true);
  }

  function errMessage(err: unknown, fallback: string): string {
    if (err instanceof ApiError && err.status === 403) {
      return "Hanya Pemilik yang dapat mengelola pengguna";
    }
    if (err instanceof ApiError) return err.message;
    return fallback;
  }

  async function handleSubmit() {
    setBusy(true);
    try {
      if (editing) {
        // Hanya kirim yang benar-benar berubah — PATCH parsial menjaga field
        // lain apa adanya, termasuk password yang tidak disentuh.
        const patch: { email?: string; name?: string; password?: string; role?: string } = {};
        const name = fName.trim();
        const email = fEmail.trim();
        if (name !== (editing.name ?? "")) patch.name = name;
        if (email !== editing.email) patch.email = email;
        if (fRole !== editing.role) patch.role = fRole;
        if (fPassword) patch.password = fPassword;

        if (Object.keys(patch).length === 0) {
          toast("Tidak ada perubahan", "info");
          setShowForm(false);
          return;
        }
        await updateUser(token, editing.id, patch);
        toast(`Pengguna ${email} diperbarui`, "success");
      } else {
        await createUser(token, {
          email: fEmail.trim(),
          name: fName.trim() || undefined,
          password: fPassword,
          role: fRole,
        });
        toast(`Pengguna ${fEmail.trim()} (${ROLE_LABELS[fRole]}) ditambahkan`, "success");
      }
      setShowForm(false);
      await refresh();
    } catch (err) {
      toast(errMessage(err, editing ? "Gagal memperbarui pengguna" : "Gagal menambah pengguna"), "error");
    } finally {
      setBusy(false);
    }
  }

  // Saat mengubah, password boleh kosong (berarti tidak diganti); saat membuat,
  // ia wajib dan minimal 8 karakter — aturan yang sama ditegakkan backend.
  const passwordOk = editing ? fPassword === "" || fPassword.length >= 8 : fPassword.length >= 8;
  const canSubmit = fEmail.trim() !== "" && passwordOk;

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between flex-wrap gap-2">
        <p className="text-sm text-text-secondary">
          {mayManage
            ? "Undang tim Anda: Accounting untuk transaksi & jurnal, Marketing untuk penjualan, Viewer untuk pemilik/investor."
            : "Daftar pengguna tenant ini. Hanya Pemilik yang dapat menambah atau mengubah pengguna."}
        </p>
        {mayManage && <Button size="sm" onClick={openCreate}>+ Pengguna</Button>}
      </div>

      {loadErr ? (
        <EmptyState title="Daftar pengguna tidak tersedia" description={loadErr} />
      ) : loading ? (
        <Card padding="sm">
          <div className="animate-pulse space-y-2">
            {[...Array(2)].map((_, i) => <div key={i} className="h-9 rounded bg-border-subtle" />)}
          </div>
        </Card>
      ) : rows.length === 0 ? (
        <EmptyState
          title="Belum ada pengguna lain"
          description="Tambahkan Accounting, Marketing, atau Viewer agar tim bisa bekerja bersama — setiap pengguna punya email & password sendiri."
          action={mayManage ? <Button size="sm" onClick={openCreate}>+ Pengguna Pertama</Button> : undefined}
        />
      ) : (
        <Card padding="sm" className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border text-left text-xs text-text-secondary">
                <th className="py-2 pr-3">Nama</th>
                <th className="py-2 pr-3">Email</th>
                <th className="py-2 pr-3">Peran</th>
                <th className="py-2 pr-3">Terdaftar</th>
                <th className="py-2 pr-3 text-right">Aksi</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((u) => (
                <tr key={u.id} className="border-b border-border last:border-0">
                  <td className="py-2 pr-3 font-medium">
                    {/* Pengguna lama tidak punya nama; menampilkan tanda pisah
                        lebih jujur daripada menebak dari email. */}
                    {u.name ? u.name : <span className="text-text-tertiary">—</span>}
                  </td>
                  <td className="py-2 pr-3">{u.email}</td>
                  <td className="py-2 pr-3">
                    <Badge variant={roleVariant(u.role)}>
                      {ROLE_LABELS[u.role] ?? u.role}
                    </Badge>
                  </td>
                  <td className="py-2 pr-3 text-xs text-text-secondary">
                    {u.created_at ? new Date(u.created_at).toLocaleDateString("id-ID") : "—"}
                  </td>
                  <td className="py-2 pr-3 text-right">
                    {mayManage ? (
                      <Button size="sm" variant="secondary" onClick={() => openEdit(u)}>Ubah</Button>
                    ) : (
                      <span className="text-xs text-text-tertiary">—</span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
      )}

      <Modal
        open={showForm}
        onClose={() => setShowForm(false)}
        title={editing ? `Ubah Pengguna — ${editing.email}` : "Tambah Pengguna"}
        footer={
          <>
            <Button variant="secondary" onClick={() => setShowForm(false)} disabled={busy}>Batal</Button>
            <Button onClick={handleSubmit} loading={busy} disabled={!canSubmit}>
              {editing ? "Simpan" : "Tambah"}
            </Button>
          </>
        }
      >
        <div className="space-y-3">
          <Input label="Nama" value={fName} onChange={(e) => setFName(e.target.value)}
            placeholder="Nama lengkap" />
          <Input label="Email (dipakai untuk login)" required type="email" value={fEmail}
            onChange={(e) => setFEmail(e.target.value)} placeholder="nama@perusahaan.com" />
          <Input
            label={editing ? "Password baru (kosongkan bila tidak diubah)" : "Password * (min. 8 karakter)"}
            type="password" value={fPassword} onChange={(e) => setFPassword(e.target.value)} />
          <Select label="Peran" value={fRole} onChange={(e) => setFRole(e.target.value as Role)}>
            {ALL_ROLES.map((r) => (
              <option key={r} value={r}>{ROLE_LABELS[r]} — {ROLE_DESCRIPTIONS[r]}</option>
            ))}
          </Select>
        </div>
      </Modal>
    </div>
  );
}
