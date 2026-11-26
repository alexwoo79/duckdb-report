import {
  PostgreSQL,
  SQLDialect,
} from "@codemirror/lang-sql";
import { HighlightStyle } from "@codemirror/language";
import { tags } from "@lezer/highlight";

export const duckDBDialect = SQLDialect.define({
  ...PostgreSQL.spec,
  keywords: `${PostgreSQL.spec.keywords ?? ""}
    ATTACH DETACH INSTALL LOAD PIVOT UNPIVOT QUALIFY SAMPLE SUMMARIZE
    DESCRIBE EXPORT COPY MACRO COLUMNS EXCLUDE REPLACE RENAME STAR`,
  builtin: `${PostgreSQL.spec.builtin ?? ""}
    list_filter list_transform list_aggregate list_value list_sort unnest
    generate_series range read_csv_auto read_parquet read_json_auto glob
    regexp_matches string_agg arg_max arg_min quantile_cont median
    approx_count_distinct struct_pack map_from_entries json_extract json_value
    epoch_ms strftime date_trunc`,
  types: `${PostgreSQL.spec.types ?? ""}
    HUGEINT UHUGEINT UBIGINT UTINYINT USMALLINT UINTEGER BIGNUM VARINT
    MAP STRUCT UNION LIST ARRAY ENUM UUID JSON`,
  caseInsensitiveIdentifiers: true,
});

export const sqlHighlightStyle = HighlightStyle.define([
  { tag: tags.keyword, color: "var(--orange)", fontWeight: "600" },
  {
    tag: [tags.string, tags.special(tags.string), tags.regexp],
    color: "var(--teal)",
  },
  {
    tag: [tags.number, tags.bool, tags.null, tags.atom],
    color: "var(--blue)",
  },
  {
    tag: [tags.function(tags.variableName), tags.function(tags.propertyName)],
    color: "var(--blue)",
  },
  { tag: [tags.typeName, tags.className], color: "var(--teal)" },
  { tag: tags.operator, color: "var(--muted)" },
  {
    tag: [tags.comment, tags.lineComment, tags.blockComment],
    color: "var(--subtle)",
    fontStyle: "italic",
  },
  { tag: [tags.variableName, tags.propertyName], color: "var(--text)" },
  { tag: [tags.bracket, tags.punctuation], color: "var(--muted)" },
  { tag: tags.invalid, color: "var(--red)" },
]);
