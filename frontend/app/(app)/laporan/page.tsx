import { getTokenAndRole } from "@/lib/auth";
import { fetchProjects } from "@/lib/api/projects";
import { monthStartLocalStr, todayLocalStr } from "@/lib/date";
import {
  fetchNeraca,
  fetchPL,
  fetchProjectPL,
  fetchCashFlow,
  fetchPipeline,
  fetchTaxLiability,
  fetchBASTWithoutPPh,
  fetchTrialBalance,
  fetchGeneralLedger,
} from "@/lib/api/reports";
import { fetchRABvsRealisasi, fetchConstructionRealisasi } from "@/lib/api/budget";
import { EmptyState } from "@/components/ui/EmptyState";
import { LaporanNavClient } from "@/components/laporan/LaporanNavClient";
import { ExportExcelButton } from "@/components/laporan/ExportExcelButton";
import { ExportPdfButton } from "@/components/laporan/ExportPdfButton";
import { NeracaSection } from "@/components/laporan/NeracaSection";
import { PLSection } from "@/components/laporan/PLSection";
import { ArusKasSection } from "@/components/laporan/ArusKasSection";
import { PipelineSection } from "@/components/laporan/PipelineSection";
import { TaxSection } from "@/components/laporan/TaxSection";
import { TrialBalanceSection } from "@/components/laporan/TrialBalanceSection";
import { RABvsRealisasiSection } from "@/components/rab/RABvsRealisasiSection";
import type { NeracaReport, PLReport, ArusKasReport, PipelineReport, TaxLiabilityReport, TrialBalance, LedgerEntry, RABvsRealisasiReport, ConstructionRealisasiTree } from "@/lib/types/api";

const today = () => todayLocalStr();
const monthStart = () => monthStartLocalStr();

interface PageProps {
  searchParams: Promise<Record<string, string | undefined>>;
}

export default async function LaporanPage({ searchParams }: PageProps) {
  const sp = await searchParams;
  const tab = sp.tab ?? "neraca";
  const asOf = sp.as_of ?? today();
  // Item 5: Start Date opsional (Neraca & L/R) — kosong bila user tak mengisi,
  // BUKAN default ke awal bulan (tanpa Start Date = perilaku lama, life-to-date).
  const startDate = sp.start_date ?? "";
  const periodFrom = sp.from ?? monthStart();
  const periodTo = sp.to ?? today();
  const projectIdStr = sp.project_id ?? "";
  const projectId = projectIdStr ? Number(projectIdStr) : 0;
  const accountIdStr = sp.account_id ?? "";
  const accountId = accountIdStr ? Number(accountIdStr) : null;
  const accountName = sp.account_name ?? null;

  const { token } = await getTokenAndRole();

  // Projects selalu di-fetch untuk dropdown filter
  const projects = await fetchProjects(token).catch(() => []);

  // Data per-tab — hanya fetch yang relevan
  let neracaData: NeracaReport | null = null;
  let neracaError = false;

  let plData: PLReport | null = null;
  let plError = false;
  let plTitle = "Laba Rugi Konsolidasi";

  let arusKasData: ArusKasReport | null = null;
  let arusKasError = false;

  let pipelineData: PipelineReport | null = null;
  let pipelineError = false;

  let taxData: TaxLiabilityReport | null = null;
  let taxError = false;
  let bastWarningCount = 0;

  let trialBalanceData: TrialBalance | null = null;
  let trialBalanceError = false;
  let ledgerData: LedgerEntry[] | null = null;
  let ledgerError = false;

  let rabRealisasiData: RABvsRealisasiReport | null = null;
  let rabRealisasiError = false;
  let constructionTreeData: ConstructionRealisasiTree | null = null;

  if (tab === "neraca") {
    [neracaData, neracaError] = await safe(fetchNeraca(token, asOf, startDate || undefined));
  } else if (tab === "laba-rugi") {
    [plData, plError] = await safe(fetchPL(token, asOf, startDate || undefined));
  } else if (tab === "laba-rugi-proyek") {
    plTitle = "Laba Rugi per Proyek";
    if (projectId) {
      [plData, plError] = await safe(fetchProjectPL(token, projectId, asOf, startDate || undefined));
    }
  } else if (tab === "arus-kas") {
    [arusKasData, arusKasError] = await safe(fetchCashFlow(token, periodFrom, periodTo));
  } else if (tab === "pipeline") {
    [pipelineData, pipelineError] = await safe(fetchPipeline(token));
  } else if (tab === "pajak") {
    const [taxResult, taxErr] = await safe(fetchTaxLiability(token, periodFrom, periodTo));
    taxData = taxResult;
    taxError = taxErr;
    const [bastResult] = await safe(fetchBASTWithoutPPh(token, periodFrom, periodTo));
    bastWarningCount = bastResult?.length ?? 0;
  } else if (tab === "neraca-saldo") {
    [trialBalanceData, trialBalanceError] = await safe(fetchTrialBalance(token, asOf));
    if (accountId) {
      [ledgerData, ledgerError] = await safe(fetchGeneralLedger(token, accountId, { from: periodFrom, to: periodTo }));
    }
  } else if (tab === "rab-realisasi") {
    if (projectId) {
      [rabRealisasiData, rabRealisasiError] = await safe(fetchRABvsRealisasi(token, projectId));
      [constructionTreeData] = await safe(fetchConstructionRealisasi(token, projectId));
    }
  }

  // Export params per tab
  const exportParams = buildExportParams(tab, { asOf, startDate, periodFrom, periodTo, projectId: projectIdStr });

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="display-lg text-2xl text-text-primary">Laporan Keuangan</h1>
          <p className="text-sm text-text-secondary mt-0.5">Semua angka dari server — tidak ada kalkulasi di frontend.</p>
        </div>
        {exportParams && (
          <div className="flex items-center gap-2">
            <ExportExcelButton
              report={exportParams.report}
              params={exportParams.params}
              label="Export Excel"
            />
            <ExportPdfButton
              token={token}
              report={exportParams.report}
              params={exportParams.params}
              label="Export PDF"
            />
          </div>
        )}
      </div>

      <LaporanNavClient
        activeTab={tab}
        projects={projects}
        asOf={asOf}
        startDate={startDate}
        periodFrom={periodFrom}
        periodTo={periodTo}
        projectId={projectIdStr}
      />

      <div className="pt-2">
        {tab === "neraca" && (
          <NeracaSection data={neracaData} error={neracaError} />
        )}
        {tab === "laba-rugi" && (
          <PLSection data={plData} error={plError} title="Laba Rugi Konsolidasi" />
        )}
        {tab === "laba-rugi-proyek" && (
          projectId === 0 ? (
            <div className="bg-surface border border-border rounded-lg">
              <EmptyState
                icon={
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
                    <path d="M18 20V10M12 20V4M6 20v-6" />
                  </svg>
                }
                title="Pilih Proyek"
                description="Gunakan filter di atas untuk memilih proyek dan melihat laporan Laba Rugi per proyek."
              />
            </div>
          ) : (
            <PLSection data={plData} error={plError} title={plTitle} />
          )
        )}
        {tab === "arus-kas" && (
          <ArusKasSection data={arusKasData} error={arusKasError} />
        )}
        {tab === "rab-realisasi" && (
          projectId === 0 ? (
            <div className="bg-surface border border-border rounded-lg">
              <EmptyState
                icon={
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
                    <path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z" /><path d="M14 2v6h6" />
                  </svg>
                }
                title="Pilih Proyek"
                description="Gunakan filter di atas untuk memilih proyek dan melihat RAB vs Realisasi."
              />
            </div>
          ) : (
            <RABvsRealisasiSection report={rabRealisasiData} error={rabRealisasiError} constructionTree={constructionTreeData} />
          )
        )}
        {tab === "pipeline" && (
          <PipelineSection data={pipelineData} error={pipelineError} />
        )}
        {tab === "pajak" && (
          <TaxSection data={taxData} error={taxError} bastWarningCount={bastWarningCount} />
        )}
        {tab === "neraca-saldo" && (
          <TrialBalanceSection
            data={trialBalanceData}
            error={trialBalanceError}
            ledger={ledgerData}
            ledgerError={ledgerError}
            selectedAccountId={accountId}
            selectedAccountName={accountName}
          />
        )}
      </div>
    </div>
  );
}

// Wraps a Promise so failures return [null, true] instead of throwing.
async function safe<T>(promise: Promise<T>): Promise<[T | null, boolean]> {
  try {
    const data = await promise;
    return [data, false];
  } catch {
    return [null, true];
  }
}

function buildExportParams(
  tab: string,
  { asOf, startDate, periodFrom, periodTo, projectId }: { asOf: string; startDate: string; periodFrom: string; periodTo: string; projectId: string },
): { report: string; params: Record<string, string> } | null {
  const dateParams: Record<string, string> = { as_of: asOf };
  if (startDate) dateParams.start_date = startDate;
  switch (tab) {
    case "neraca":
      return { report: "balance-sheet", params: dateParams };
    case "laba-rugi":
      return { report: "income-statement", params: dateParams };
    case "laba-rugi-proyek":
      return projectId
        ? { report: "project-pl", params: { project_id: projectId, ...dateParams } }
        : null;
    case "arus-kas":
      return { report: "cash-flow", params: { from: periodFrom, to: periodTo } };
    case "pipeline":
      return { report: "sales-pipeline", params: {} };
    case "pajak":
      return { report: "tax-liability", params: { from: periodFrom, to: periodTo } };
    case "neraca-saldo":
      return { report: "trial-balance", params: { as_of: asOf } };
    default:
      return null;
  }
}
