import { getTokenAndRole } from "@/lib/auth";
import { fetchProjects } from "@/lib/api/projects";
import { fetchExpenses, fetchExpenseTypes } from "@/lib/api/expense";
import { fetchFixedAssetCategories } from "@/lib/api/fixedAsset";
import { ExpenseForm } from "@/components/expense/ExpenseForm";
import { ExpenseList } from "@/components/expense/ExpenseList";
import { Card } from "@/components/ui/Card";
import type { ExpenseListItem, ExpenseType, FixedAssetCategory, Project } from "@/lib/types/api";

export const metadata = { title: "Transaksi Pengeluaran — NATA ALAM RAYA" };

// Satu pintu masuk untuk semua uang yang keluar dari kas/bank perusahaan.
// Sebelum halaman ini, biaya operasional harus dicatat manual lewat Jurnal Umum
// dan biaya proyek lewat halaman proyek — dua jalan berbeda untuk satu kejadian
// yang sama. Toggle "Jenis Pembelian" menambahkan jalur ketiga (Aset Tetap)
// tanpa membuka pintu baru.
export default async function ExpensePage() {
  const { token, canWrite } = await getTokenAndRole();

  const [projects, expenseTypes, expenses, fixedAssetCategories] = await Promise.all([
    fetchProjects(token).catch(() => [] as Project[]),
    fetchExpenseTypes(token).catch(() => [] as ExpenseType[]),
    fetchExpenses(token).catch(() => [] as ExpenseListItem[]),
    fetchFixedAssetCategories(token).catch(() => [] as FixedAssetCategory[]),
  ]);

  const noTypes = expenseTypes.filter((t) => t.is_active).length === 0;

  return (
    <div className="mx-auto w-full max-w-5xl space-y-6">
      <div>
        <h1 className="display-lg text-2xl text-text-primary">Transaksi Pengeluaran</h1>
        <p className="text-sm text-text-secondary mt-1 max-w-prose">
          Catat semua uang keluar dari kas/bank perusahaan — operasional kantor
          maupun biaya proyek. Bukti kas keluar terbit dan jurnal terposting dalam
          satu langkah.
        </p>
      </div>

      {noTypes && (
        <Card className="border-warning/30 bg-warning-bg/50">
          <p className="text-sm font-medium text-warning">Master jenis pengeluaran kosong</p>
          <p className="text-xs text-text-secondary mt-1 max-w-prose">
            Pengeluaran operasional membutuhkan jenis pengeluaran untuk menentukan akun
            bebannya. Tanpa itu, hanya biaya proyek yang bisa dicatat. Jenis bawaan
            biasanya terpasang saat tenant dibuat — hubungi admin bila daftarnya kosong.
          </p>
        </Card>
      )}

      <ExpenseForm
        token={token}
        canWrite={canWrite}
        projects={projects}
        expenseTypes={expenseTypes}
        fixedAssetCategories={fixedAssetCategories}
      />

      <ExpenseList items={expenses} />
    </div>
  );
}
