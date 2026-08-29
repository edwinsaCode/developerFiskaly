"use client";

// R7 — Command Palette (U6/U9): lompat ke halaman / unit / customer dari mana pun.
// Buka: Ctrl+K (atau ⌘K) atau "/" di luar input. Navigasi: ↑↓ + Enter, Esc tutup.
// Data unit/customer dimuat SEKALI saat pertama dibuka (cache sesi halaman).

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { fetchProjects, fetchUnitsByProject } from "@/lib/api/projects";
import { fetchCustomers, type Customer } from "@/lib/api/party";
import type { Unit } from "@/lib/types/api";

interface Props {
  token: string;
}

interface Item {
  group: "Navigasi" | "Unit" | "Customer";
  label: string;
  hint?: string;
  href: string;
}

const NAV_ITEMS: Item[] = [
  { group: "Navigasi", label: "Dashboard", href: "/dashboard" },
  { group: "Navigasi", label: "Proyek", href: "/proyek" },
  { group: "Navigasi", label: "Unit & Penjualan", href: "/penjualan" },
  { group: "Navigasi", label: "Sales & CRM", href: "/penjualan/sales" },
  { group: "Navigasi", label: "Booking", href: "/penjualan/booking" },
  { group: "Navigasi", label: "KPR", href: "/penjualan/kpr" },
  { group: "Navigasi", label: "Komisi Sales", href: "/penjualan/komisi" },
  { group: "Navigasi", label: "Pembatalan & Refund", href: "/penjualan/pembatalan" },
  { group: "Navigasi", label: "Invoice Customer", href: "/accounting/invoices" },
  { group: "Navigasi", label: "Piutang Customer", href: "/accounting/receivable" },
  { group: "Navigasi", label: "Jadwal Penagihan", href: "/accounting/billing-schedule" },
  { group: "Navigasi", label: "Penagihan", href: "/accounting/collection" },
  { group: "Navigasi", label: "Pengeluaran", href: "/accounting/pengeluaran" },
  { group: "Navigasi", label: "Hutang Usaha", href: "/accounting/hutang" },
  { group: "Navigasi", label: "Master Vendor", href: "/accounting/vendor" },
  { group: "Navigasi", label: "Jurnal", href: "/accounting/jurnal" },
  { group: "Navigasi", label: "Buku Besar", href: "/accounting/gl" },
  { group: "Navigasi", label: "Daftar Akun", href: "/accounting/coa" },
  { group: "Navigasi", label: "Jurnal Berulang", href: "/accounting/recurring" },
  { group: "Navigasi", label: "Periode & Tutup Buku", href: "/accounting/periods" },
  { group: "Navigasi", label: "Pajak", href: "/accounting/pajak" },
  { group: "Navigasi", label: "Laporan", href: "/laporan" },
  { group: "Navigasi", label: "Pengaturan", href: "/pengaturan" },
];

export function CommandPalette({ token }: Props) {
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState("");
  const [sel, setSel] = useState(0);
  const [dynamic, setDynamic] = useState<Item[] | null>(null); // null = belum dimuat
  const inputRef = useRef<HTMLInputElement>(null);

  // Muat unit + customer sekali saat pertama terbuka.
  const loadDynamic = useCallback(async () => {
    try {
      const [projects, customers] = await Promise.all([
        fetchProjects(token).catch(() => []),
        fetchCustomers(token).catch(() => [] as Customer[]),
      ]);
      const unitLists = await Promise.all(
        projects.map((p) => fetchUnitsByProject(token, p.id).catch(() => [] as Unit[])),
      );
      const items: Item[] = [];
      projects.forEach((p, i) => {
        unitLists[i].forEach((u) => {
          items.push({
            group: "Unit",
            label: u.code,
            hint: `${p.name} · ${u.status}`,
            href: `/penjualan/${u.id}`,
          });
        });
      });
      customers.forEach((c) => {
        items.push({
          group: "Customer",
          label: c.name,
          hint: c.phone || c.code,
          href: `/penjualan/sales?tab=customer`,
        });
      });
      setDynamic(items);
    } catch {
      setDynamic([]);
    }
  }, [token]);

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      const target = e.target as HTMLElement;
      const typing =
        target.tagName === "INPUT" || target.tagName === "TEXTAREA" ||
        target.tagName === "SELECT" || target.isContentEditable;
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setOpen((v) => !v);
      } else if (e.key === "/" && !typing) {
        e.preventDefault();
        setOpen(true);
      } else if (e.key === "Escape") {
        setOpen(false);
      }
    }
    // Discoverability: tombol search di Header membuka palette via event global
    // (tanpa context/prop drilling — Header & palette hidup di cabang berbeda).
    function onOpenEvent() { setOpen(true); }
    window.addEventListener("keydown", onKey);
    window.addEventListener("open-command-palette", onOpenEvent);
    return () => {
      window.removeEventListener("keydown", onKey);
      window.removeEventListener("open-command-palette", onOpenEvent);
    };
  }, []);

  useEffect(() => {
    if (open) {
      setQ(""); setSel(0);
      if (dynamic === null) loadDynamic();
      setTimeout(() => inputRef.current?.focus(), 30);
    }
  }, [open, dynamic, loadDynamic]);

  const results = useMemo(() => {
    const all = [...NAV_ITEMS, ...(dynamic ?? [])];
    const needle = q.trim().toLowerCase();
    const hits = needle
      ? all.filter((i) =>
          i.label.toLowerCase().includes(needle) || (i.hint ?? "").toLowerCase().includes(needle))
      : NAV_ITEMS;
    return hits.slice(0, 12);
  }, [q, dynamic]);

  function go(item: Item) {
    setOpen(false);
    router.push(item.href);
  }

  if (!open) return null;

  return (
    <div
      className="fixed inset-0 z-overlay flex items-start justify-center bg-text-primary/30 backdrop-blur-sm pt-[12vh]"
      onClick={() => setOpen(false)}
    >
      <div
        className="w-full max-w-lg rounded-xl border border-border bg-surface shadow-2xl overflow-hidden"
        onClick={(e) => e.stopPropagation()}
      >
        <input
          ref={inputRef}
          value={q}
          onChange={(e) => { setQ(e.target.value); setSel(0); }}
          onKeyDown={(e) => {
            if (e.key === "ArrowDown") { e.preventDefault(); setSel((s) => Math.min(s + 1, results.length - 1)); }
            else if (e.key === "ArrowUp") { e.preventDefault(); setSel((s) => Math.max(s - 1, 0)); }
            else if (e.key === "Enter" && results[sel]) { e.preventDefault(); go(results[sel]); }
          }}
          placeholder="Lompat ke halaman, unit, atau customer…"
          className="w-full border-b border-border bg-transparent px-4 py-3 text-sm focus:outline-none"
        />
        <ul className="max-h-[50vh] overflow-y-auto py-1.5">
          {results.length === 0 && (
            <li className="px-4 py-6 text-center text-sm text-text-secondary">
              {dynamic === null ? "Memuat data unit & customer…" : "Tidak ada hasil."}
            </li>
          )}
          {results.map((item, i) => (
            <li key={`${item.group}-${item.href}-${item.label}`}>
              <button
                onMouseEnter={() => setSel(i)}
                onClick={() => go(item)}
                className={`flex w-full items-center justify-between px-4 py-2 text-left text-sm
                  ${i === sel ? "bg-accent/10 text-accent" : "text-text-primary"}`}
              >
                <span>
                  {item.label}
                  {item.hint && <span className="ml-2 text-xs text-text-tertiary">{item.hint}</span>}
                </span>
                <span className="text-[10px] uppercase tracking-wide text-text-tertiary">{item.group}</span>
              </button>
            </li>
          ))}
        </ul>
        <div className="border-t border-border px-4 py-2 text-[11px] text-text-tertiary flex gap-3">
          <span>↑↓ pilih</span><span>Enter buka</span><span>Esc tutup</span>
          <span className="ml-auto">Ctrl+K / &quot;/&quot;</span>
        </div>
      </div>
    </div>
  );
}
