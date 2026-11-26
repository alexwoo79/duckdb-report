import { format } from "sql-formatter";

const duckDBFormatOptions = {
  language: "duckdb" as const,
  tabWidth: 2,
  keywordCase: "upper" as const,
  dataTypeCase: "upper" as const,
  functionCase: "preserve" as const,
  identifierCase: "preserve" as const,
  indentStyle: "standard" as const,
  logicalOperatorNewline: "before" as const,
  expressionWidth: 88,
  linesBetweenQueries: 1,
  denseOperators: false,
  newlineBeforeSemicolon: false,
};

export function formatDuckDBSQL(sql: string): string {
  return format(sql, duckDBFormatOptions).trim();
}
