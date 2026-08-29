"use client";

import { InputHTMLAttributes, SelectHTMLAttributes, TextareaHTMLAttributes, useId } from "react";

// Kontrol isian.
//
// `wrapperClassName` ada supaya field bisa mengatur posisinya sendiri di dalam
// FormGrid (mis. "sm:col-span-2"): `className` menempel ke elemen <input>, jadi
// utilitas grid yang ditaruh di sana tidak akan pernah berlaku.

interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  label?: string;
  error?: string;
  hint?: string;
  wrapperClassName?: string;
  /** Satuan/awalan statis di dalam kotak isian, mis. "Rp" atau "m²". */
  suffix?: string;
}

const baseField =
  "w-full rounded-lg border border-border bg-surface px-3.5 py-2.5 text-sm text-text-primary" +
  " placeholder:text-text-tertiary outline-none transition-shadow transition-colors" +
  " focus:border-accent focus:ring-2 focus:ring-accent/25" +
  " disabled:bg-border-subtle disabled:text-text-secondary disabled:cursor-not-allowed";

const errorField = "border-danger focus:border-danger focus:ring-danger/25";

function FieldLabel({
  htmlFor,
  children,
  required,
}: {
  htmlFor: string;
  children: React.ReactNode;
  required?: boolean;
}) {
  return (
    <label htmlFor={htmlFor} className="text-sm font-medium text-text-primary">
      {children}
      {required && <span className="text-danger ml-0.5">*</span>}
    </label>
  );
}

export function Input({
  label,
  error,
  hint,
  className = "",
  wrapperClassName = "",
  suffix,
  id,
  ...props
}: InputProps) {
  const autoId = useId();
  const fieldId = id ?? autoId;

  return (
    <div className={`flex flex-col gap-1.5 ${wrapperClassName}`}>
      {label && (
        <FieldLabel htmlFor={fieldId} required={props.required}>
          {label}
        </FieldLabel>
      )}
      <div className="relative">
        <input
          id={fieldId}
          aria-invalid={error ? true : undefined}
          className={`${baseField} ${suffix ? "pr-12 field-no-spinner" : ""} ${error ? errorField : ""} ${className}`}
          {...props}
        />
        {suffix && (
          <span className="pointer-events-none absolute inset-y-0 right-3.5 flex items-center text-xs text-text-tertiary">
            {suffix}
          </span>
        )}
      </div>
      {hint && !error && <p className="text-xs text-text-tertiary">{hint}</p>}
      {error && <p className="text-xs text-danger">{error}</p>}
    </div>
  );
}

interface SelectProps extends SelectHTMLAttributes<HTMLSelectElement> {
  label?: string;
  error?: string;
  hint?: string;
  wrapperClassName?: string;
}

export function Select({
  label,
  error,
  hint,
  className = "",
  wrapperClassName = "",
  id,
  children,
  ...props
}: SelectProps) {
  const autoId = useId();
  const fieldId = id ?? autoId;

  return (
    <div className={`flex flex-col gap-1.5 ${wrapperClassName}`}>
      {label && (
        <FieldLabel htmlFor={fieldId} required={props.required}>
          {label}
        </FieldLabel>
      )}
      {/* Chevron digambar sendiri (bukan gambar latar) supaya warnanya ikut
          token teks dan tetap benar di tema gelap. */}
      <div className="relative">
        <select
          id={fieldId}
          aria-invalid={error ? true : undefined}
          className={`${baseField} appearance-none pr-10 ${error ? errorField : ""} ${className}`}
          {...props}
        >
          {children}
        </select>
        <svg
          viewBox="0 0 20 20"
          aria-hidden="true"
          className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 h-4 w-4 text-text-tertiary"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.6"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <path d="M6 8l4 4 4-4" />
        </svg>
      </div>
      {hint && !error && <p className="text-xs text-text-tertiary">{hint}</p>}
      {error && <p className="text-xs text-danger">{error}</p>}
    </div>
  );
}

interface TextareaProps extends TextareaHTMLAttributes<HTMLTextAreaElement> {
  label?: string;
  error?: string;
  hint?: string;
  wrapperClassName?: string;
}

export function Textarea({
  label,
  error,
  hint,
  className = "",
  wrapperClassName = "",
  id,
  ...props
}: TextareaProps) {
  const autoId = useId();
  const fieldId = id ?? autoId;

  return (
    <div className={`flex flex-col gap-1.5 ${wrapperClassName}`}>
      {label && (
        <FieldLabel htmlFor={fieldId} required={props.required}>
          {label}
        </FieldLabel>
      )}
      <textarea
        id={fieldId}
        aria-invalid={error ? true : undefined}
        className={`${baseField} min-h-[80px] resize-y ${error ? errorField : ""} ${className}`}
        {...props}
      />
      {hint && !error && <p className="text-xs text-text-tertiary">{hint}</p>}
      {error && <p className="text-xs text-danger">{error}</p>}
    </div>
  );
}
