import { PanduanView } from "@/components/panduan/PanduanView";

export default function PanduanPage() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="display-lg text-2xl text-text-primary">Panduan Sistem</h1>
        <p className="mt-0.5 text-sm text-text-secondary">
          Dokumentasi lengkap alur bisnis, akuntansi, dan modul aplikasi — untuk Client, Accounting,
          Admin, dan Management.
        </p>
      </div>
      <PanduanView />
    </div>
  );
}
