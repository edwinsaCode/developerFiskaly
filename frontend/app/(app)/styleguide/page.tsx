"use client";

import { useState } from "react";
import { Button } from "@/components/ui/Button";
import { Card, CardHeader, CardTitle } from "@/components/ui/Card";
import { Badge, StatusBadge } from "@/components/ui/Badge";
import { Input, Select } from "@/components/ui/Input";
import { Table, TableHead, TableBody, TableRow, Th, Td } from "@/components/ui/Table";
import { Skeleton, SkeletonMetric, SkeletonTable } from "@/components/ui/Skeleton";
import { Tabs, TabList, TabTrigger, TabPanel } from "@/components/ui/Tabs";
import { Modal } from "@/components/ui/Modal";
import { EmptyState } from "@/components/ui/EmptyState";
import { useToast } from "@/components/ui/Toast";
import { Rupiah } from "@/components/format/Rupiah";
import { Persen } from "@/components/format/Persen";
import { Tanggal } from "@/components/format/Tanggal";

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="space-y-4">
      <h2 className="text-sm font-bold text-text-secondary uppercase tracking-widest border-b border-border pb-2">
        {title}
      </h2>
      {children}
    </section>
  );
}

export default function StyleguidePage() {
  const { toast } = useToast();
  const [modalOpen, setModalOpen] = useState(false);

  return (
    <div className="mx-auto w-full max-w-4xl space-y-10">
      <div>
        <h1 className="display-lg text-2xl text-text-primary">Design System</h1>
        <p className="text-sm text-text-secondary mt-1">NATA ALAM RAYA — komponen & token</p>
      </div>

      {/* Warna */}
      <Section title="Warna">
        <div className="flex flex-wrap gap-3">
          {[
            ["bg-bg",            "bg"],
            ["bg-surface",       "surface"],
            ["bg-border",        "border"],
            ["bg-accent",        "accent"],
            ["bg-success",       "success"],
            ["bg-warning",       "warning"],
            ["bg-danger",        "danger"],
            ["bg-text-primary",  "text-primary"],
            ["bg-text-secondary","text-secondary"],
          ].map(([cls, label]) => (
            <div key={cls} className="flex flex-col items-center gap-1">
              <div className={`w-12 h-12 rounded border border-border ${cls}`} />
              <span className="text-xs text-text-tertiary">{label}</span>
            </div>
          ))}
        </div>
      </Section>

      {/* Tipografi */}
      <Section title="Format Angka">
        <div className="space-y-1">
          <div className="text-sm text-text-secondary">Rupiah:</div>
          <div className="text-xl font-bold tabular num-right"><Rupiah value="2800000000" /></div>
          <div className="text-sm text-text-secondary mt-3">Persen:</div>
          <div className="text-xl font-bold tabular"><Persen value="72.5" /></div>
          <div className="text-sm text-text-secondary mt-3">Tanggal:</div>
          <div className="text-base"><Tanggal value="2026-06-24T00:00:00Z" /></div>
        </div>
      </Section>

      {/* Button */}
      <Section title="Button">
        <div className="flex flex-wrap gap-3">
          <Button>Primary</Button>
          <Button variant="secondary">Secondary</Button>
          <Button variant="ghost">Ghost</Button>
          <Button variant="danger">Danger</Button>
          <Button size="sm">Kecil</Button>
          <Button loading>Loading</Button>
          <Button disabled>Disabled</Button>
        </div>
      </Section>

      {/* Badge */}
      <Section title="Badge">
        <div className="flex flex-wrap gap-2">
          {(["outstanding","paid","overdue","available","reserved","sold","active","superseded"] as const).map(s => (
            <StatusBadge key={s} status={s} />
          ))}
        </div>
      </Section>

      {/* Input */}
      <Section title="Input">
        <div className="grid grid-cols-2 gap-4">
          <Input label="Nama Proyek" placeholder="Perumahan Bukit Asri" />
          <Input label="Email" type="email" placeholder="nama@perusahaan.com" required />
          <Input label="Error" placeholder="..." error="Field ini wajib diisi" />
          <Select label="Fase">
            <option>Pilih fase</option>
            <option>Fase 1</option>
            <option>Fase 2</option>
          </Select>
        </div>
      </Section>

      {/* Table */}
      <Section title="Table">
        <Card padding="none">
          <Table>
            <TableHead>
              <TableRow>
                <Th>Unit</Th>
                <Th>Status</Th>
                <Th right>Nilai</Th>
              </TableRow>
            </TableHead>
            <TableBody>
              {[
                { unit: "B-01", status: "sold",      nilai: "850000000" },
                { unit: "B-02", status: "reserved",  nilai: "900000000" },
                { unit: "B-03", status: "available", nilai: "875000000" },
              ].map(row => (
                <TableRow key={row.unit}>
                  <Td>{row.unit}</Td>
                  <Td><StatusBadge status={row.status} /></Td>
                  <Td right><Rupiah value={row.nilai} /></Td>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Card>
      </Section>

      {/* Skeleton */}
      <Section title="Skeleton">
        <div className="grid grid-cols-3 gap-4">
          <SkeletonMetric />
          <SkeletonMetric />
          <SkeletonMetric />
        </div>
        <Card padding="none" className="mt-4">
          <SkeletonTable rows={3} cols={3} />
        </Card>
      </Section>

      {/* Tabs */}
      <Section title="Tabs">
        <Tabs defaultTab="neraca">
          <TabList>
            <TabTrigger id="neraca">Neraca</TabTrigger>
            <TabTrigger id="pl">Laba Rugi</TabTrigger>
            <TabTrigger id="pajak">Pajak</TabTrigger>
          </TabList>
          <TabPanel id="neraca" className="pt-4 text-sm text-text-secondary">Panel neraca</TabPanel>
          <TabPanel id="pl"     className="pt-4 text-sm text-text-secondary">Panel laba rugi</TabPanel>
          <TabPanel id="pajak"  className="pt-4 text-sm text-text-secondary">Panel pajak</TabPanel>
        </Tabs>
      </Section>

      {/* EmptyState */}
      <Section title="Empty State">
        <Card>
          <EmptyState
            title="Belum ada data"
            description="Transaksi akan muncul setelah jurnal pertama diposting."
          />
        </Card>
      </Section>

      {/* Toast */}
      <Section title="Toast">
        <div className="flex gap-3">
          <Button onClick={() => toast("Jurnal berhasil diposting", "success")} size="sm">
            Success
          </Button>
          <Button variant="danger" onClick={() => toast("Jurnal tidak balance!", "error")} size="sm">
            Error
          </Button>
          <Button variant="ghost" onClick={() => toast("Fitur ini dalam pengembangan", "warning")} size="sm">
            Warning
          </Button>
        </div>
      </Section>

      {/* Modal */}
      <Section title="Modal">
        <Button variant="secondary" onClick={() => setModalOpen(true)}>
          Buka Modal
        </Button>
        <Modal
          open={modalOpen}
          onClose={() => setModalOpen(false)}
          title="Konfirmasi Posting Jurnal"
          footer={
            <>
              <Button variant="ghost" onClick={() => setModalOpen(false)}>Batal</Button>
              <Button onClick={() => setModalOpen(false)}>Posting</Button>
            </>
          }
        >
          <p className="text-sm text-text-secondary">
            Jurnal yang sudah diposting tidak dapat diubah. Pastikan semua angka sudah benar.
          </p>
        </Modal>
      </Section>
    </div>
  );
}
