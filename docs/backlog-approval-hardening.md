# Backlog — Approval Workflow Micro-Hardening (pra-Event-Bus)

> Dicatat atas keputusan owner saat approve Increment 5.1 (2026-07-03).
> **Future enhancement — TIDAK dikerjakan sekarang** dan tidak boleh menghambat
> Increment 6. Relevan saat menuju Event Bus / distributed workflow.

| # | Item | Catatan desain singkat |
|---|---|---|
| B1 | **Workflow fingerprint/hash** | Hash kanonik atas `workflow_snapshot` (mis. SHA-256 dari JSON ternormalisasi) disimpan di request → verifikasi integritas snapshot terhadap tampering. Additive: satu kolom `workflow_fingerprint`. |
| B2 | **Event version** | Tambah `version` pada `approval.Event` (schema evolution untuk subscriber masa depan). Mulai v1; kenaikan versi = perubahan bentuk payload. |
| B3 | **Correlation ID & Causation ID** | `correlation_id` (alur bisnis end-to-end) + `causation_id` (event/command pemicu) pada seluruh Domain Event — prasyarat tracing di event bus. |
| B4 | **Actor snapshot** | Bekukan display name + role actor SAAT keputusan pada `approval_actions` (kini hanya actor_id + role string) — nama user bisa berubah; audit harus membaca kondisi saat itu (pola unit_name_snapshot P0-4). |
| B5 | **Timestamp UTC terstandar** | Seluruh approval event & kolom waktu tegas UTC (kini mengikuti `time.Now()`/DB local). Audit lintas timezone konsisten. |

Prasyarat implementasi kelak: semua additive; snapshot lama tanpa fingerprint
tetap sah (fingerprint NULL = pra-B1).
