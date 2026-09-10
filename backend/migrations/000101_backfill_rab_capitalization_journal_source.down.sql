UPDATE journal_entries je
JOIN journal_lines jl ON jl.journal_entry_id = je.id
SET je.source = 'system'
WHERE je.source = 'rab_capitalization'
  AND jl.description IN (
    'Kapitalisasi RAB Construction disetujui',
    'Koreksi kapitalisasi RAB Construction (revisi turun)'
  );
