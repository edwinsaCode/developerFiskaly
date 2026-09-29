"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { Modal } from "@/components/ui/Modal";
import { Button } from "@/components/ui/Button";
import { Can } from "@/components/ui/Can";
import { useToast } from "@/components/ui/Toast";
import { deleteUnit } from "@/lib/api/projects";
import { ApiError } from "@/lib/api/client";

interface Props {
  token: string;
  projectId: number;
  unitId: number;
  unitCode: string;
}

// Hapus unit kelebihan/salah (mis. dari wizard blok). Backend yang memutuskan:
// unit dengan booking, kontrak, pembayaran, jurnal, alokasi HPP, riwayat
// status, dst. ditolak dengan daftar alasan — layar ini hanya menampilkannya,
// tidak menebak ulang aturannya.
export function DeleteUnitButton({ token, projectId, unitId, unitCode }: Props) {
  const { toast } = useToast();
  const router = useRouter();

  const [open, setOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const [reasons, setReasons] = useState<string[] | null>(null);

  function close() {
    setOpen(false);
    setReasons(null);
  }

  async function handleDelete() {
    setLoading(true);
    try {
      await deleteUnit(token, unitId);
      toast(`Unit ${unitCode} dihapus.`, "success");
      setOpen(false);
      router.push(`/proyek/${projectId}`);
      router.refresh();
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) {
        const r = e.payload?.reasons;
        setReasons(Array.isArray(r) ? r.map(String) : [e.message]);
      } else {
        toast(e instanceof ApiError ? e.message : "Gagal menghapus unit", "error");
      }
    } finally {
      setLoading(false);
    }
  }

  return (
    <Can roles={["owner", "accountant"]}>
      <Button variant="ghost" size="sm" className="text-danger" onClick={() => setOpen(true)}>
        Hapus Unit
      </Button>
      <Modal
        open={open}
        onClose={close}
        title={reasons ? "Unit Tidak Bisa Dihapus" : "Hapus Unit"}
        size="sm"
        footer={
          reasons ? (
            <Button variant="ghost" onClick={close}>Tutup</Button>
          ) : (
            <>
              <Button variant="ghost" onClick={close} disabled={loading}>Batal</Button>
              <Button variant="danger" onClick={handleDelete} loading={loading}>Hapus</Button>
            </>
          )
        }
      >
        {reasons ? (
          <div className="space-y-2 text-sm text-text-secondary">
            <p>
              Unit <span className="font-semibold text-text-primary">{unitCode}</span> sudah
              memiliki riwayat transaksi, sehingga tidak dapat dihapus:
            </p>
            <ul className="list-disc pl-5 space-y-0.5">
              {reasons.map((r) => <li key={r}>{r}</li>)}
            </ul>
            <p className="text-text-tertiary">
              Data historis tidak pernah dihapus. Bila unit ini batal dijual, gunakan
              alur pembatalan.
            </p>
          </div>
        ) : (
          <p className="text-sm text-text-secondary">
            Hapus unit <span className="font-semibold text-text-primary">{unitCode}</span> dari
            proyek? Hanya unit yang belum pernah digunakan (tanpa booking, kontrak,
            pembayaran, jurnal, atau alokasi biaya) yang bisa dihapus. Tindakan ini
            tidak dapat dibatalkan.
          </p>
        )}
      </Modal>
    </Can>
  );
}
