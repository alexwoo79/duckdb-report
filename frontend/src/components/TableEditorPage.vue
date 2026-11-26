<script setup lang="ts">
import {
  computed,
  defineAsyncComponent,
  ref,
  watch,
} from "vue";
import {
  backend,
  errorMessage,
  formatNumber,
} from "../api";
import type {
  ColumnRef,
  DatabaseRef,
  QueryResult,
  SQLCompletionTable,
  TableEditorInfo,
  TableFilter,
  TableRef,
  TableRelation,
  TableStructureOperation,
  TableStructurePreview,
  TableStructureRequest,
  TableStructureResult,
  TableWriteOperation,
  TableWriteResult,
} from "../types";
import AppIcon from "./AppIcon.vue";
import ResultGrid from "./ResultGrid.vue";

const TableWriteForm = defineAsyncComponent(
  () => import("./TableWriteForm.vue"),
);
const SqlPreview = defineAsyncComponent(
  () => import("./SqlPreview.vue"),
);

const props = defineProps<{
  tables: SQLCompletionTable[];
  databases: DatabaseRef[];
  activeTable: TableRef | null;
}>();

const emit = defineEmits<{
  saved: [result: TableWriteResult];
  "schema-changed": [result: TableStructureResult];
  "select-table": [table: TableRef];
}>();

type EditorTab = "properties" | "structure" | "data" | "diagram";

const activeTab = ref<EditorTab>("data");
const selectedTableKey = ref("");
const info = ref<TableEditorInfo | null>(null);
const dataResult = ref<QueryResult | null>(null);
const dataRowLimit = ref(200);
const loading = ref(false);
const refreshing = ref(false);
const errorNote = ref("");
const saveNote = ref("");
const rowEditorOpen = ref(false);
const rowEditorMode = ref<TableWriteOperation>("update");
const rowEditorValues = ref<Record<string, unknown>>({});
const rowEditorFilters = ref<TableFilter[]>([]);
const structureOperation = ref<TableStructureOperation>("add_column");
const structureColumn = ref("");
const structureKeyColumns = ref<string[]>([]);
const structureNewName = ref("");
const structureType = ref("VARCHAR");
const structurePreview = ref<TableStructurePreview | null>(null);
const structureBusy = ref(false);
const structureError = ref("");
const structureConfirm = ref(false);
let loadVersion = 0;

const tabs: Array<{
  id: EditorTab;
  label: string;
  icon: string;
}> = [
  { id: "properties", label: "Properties", icon: "file" },
  { id: "structure", label: "Structure", icon: "edit" },
  { id: "data", label: "Data", icon: "table" },
  { id: "diagram", label: "Diagram", icon: "workflow" },
];

const dataRowLimitOptions = [50, 100, 200, 500, 1000];

const structureOperations: Array<{
  value: TableStructureOperation;
  label: string;
  description: string;
  destructive?: boolean;
}> = [
  {
    value: "add_column",
    label: "添加字段",
    description: "向表末尾增加一个字段。",
  },
  {
    value: "rename_column",
    label: "重命名字段",
    description: "修改字段名，已有数据保留。",
  },
  {
    value: "alter_column_type",
    label: "修改字段类型",
    description: "将现有值转换到另一个 DuckDB 类型。",
  },
  {
    value: "drop_column",
    label: "删除字段",
    description: "永久删除字段及其数据。",
    destructive: true,
  },
  {
    value: "set_not_null",
    label: "设为 NOT NULL",
    description: "禁止该字段写入 NULL。",
  },
  {
    value: "drop_not_null",
    label: "移除 NOT NULL",
    description: "允许该字段写入 NULL。",
  },
  {
    value: "drop_default",
    label: "删除默认值",
    description: "后续新增行不再自动使用默认值。",
  },
  {
    value: "rename_table",
    label: "重命名表",
    description: "修改表名，已有数据和字段保留。",
  },
  {
    value: "add_primary_key",
    label: "添加主键",
    description: "在一个或多个字段上建立 PRIMARY KEY 约束。",
  },
  {
    value: "add_unique_key",
    label: "添加唯一键",
    description: "在一个或多个字段上建立 UNIQUE 约束。",
  },
];

const selectedTable = computed(
  () =>
    props.tables.find(
      (table) => tableKey(table) === selectedTableKey.value,
    ) ?? null,
);

const selectedName = computed(() => {
  const table = info.value?.table ?? selectedTable.value;
  return table ? tableLabel(table) : "未选择数据表";
});

const hasStableKey = computed(
  () => Boolean(info.value?.keyColumns.length && info.value.keyType),
);

const editableRows = computed(
  () => Boolean(info.value?.writable && hasStableKey.value),
);

const keyLabel = computed(() => {
  if (!info.value?.keyColumns.length) return "None";
  return `${info.value.keyType} (${info.value.keyColumns.join(", ")})`;
});

const selectedStructureOperation = computed(
  () =>
    structureOperations.find(
      (operation) => operation.value === structureOperation.value,
    ) ?? structureOperations[0],
);

const structureNeedsColumn = computed(
  () =>
    structureOperation.value !== "rename_table" &&
    structureOperation.value !== "add_column" &&
    structureOperation.value !== "add_primary_key" &&
    structureOperation.value !== "add_unique_key",
);

const structureNeedsKeyColumns = computed(
  () =>
    structureOperation.value === "add_primary_key" ||
    structureOperation.value === "add_unique_key",
);

const structureNeedsNewName = computed(
  () =>
    structureOperation.value === "rename_table" ||
    structureOperation.value === "rename_column",
);

const structureNeedsType = computed(
  () =>
    structureOperation.value === "add_column" ||
    structureOperation.value === "alter_column_type",
);

const structureReady = computed(() => {
  if (!info.value?.writable) return false;
  if (structureOperation.value === "rename_table") {
    return structureNewName.value.trim().length > 0;
  }
  if (structureOperation.value === "add_column") {
    return (
      structureColumn.value.trim().length > 0 &&
      structureType.value.trim().length > 0
    );
  }
  if (structureNeedsKeyColumns.value) {
    return structureKeyColumns.value.length > 0;
  }
  if (!structureColumn.value) return false;
  if (structureNeedsNewName.value) {
    return structureNewName.value.trim().length > 0;
  }
  if (structureNeedsType.value) {
    return structureType.value.trim().length > 0;
  }
  return true;
});

watch(
  () => [props.activeTable, props.tables] as const,
  () => {
    if (
      props.activeTable &&
      props.tables.some(
        (table) => tableKey(table) === tableKey(props.activeTable!),
      )
    ) {
      selectedTableKey.value = tableKey(props.activeTable);
      return;
    }
    if (
      !selectedTableKey.value ||
      !props.tables.some(
        (table) => tableKey(table) === selectedTableKey.value,
      )
    ) {
      selectedTableKey.value = props.tables[0]
        ? tableKey(props.tables[0])
        : "";
    }
  },
  { immediate: true },
);

watch(
  selectedTableKey,
  () => {
    const table = selectedTable.value;
    if (!table) return;
    emit("select-table", table);
    void loadSelectedTable();
  },
  { immediate: true },
);

watch(
  [
    structureOperation,
    structureColumn,
    structureKeyColumns,
    structureNewName,
    structureType,
  ],
  () => {
    structurePreview.value = null;
    structureConfirm.value = false;
    structureError.value = "";
  },
  { deep: true },
);

watch(structureOperation, () => {
  structureNewName.value = "";
  structureKeyColumns.value = [];
  if (structureNeedsType.value) {
    structureType.value = "VARCHAR";
  }
  if (
    structureNeedsColumn.value &&
    !info.value?.columns.some(
      (column) => column.name === structureColumn.value,
    )
  ) {
    structureColumn.value = info.value?.columns[0]?.name ?? "";
  }
});

watch(info, () => {
  structureKeyColumns.value = structureKeyColumns.value.filter((name) =>
    info.value?.columns.some((column) => column.name === name),
  );
  if (
    structureNeedsColumn.value &&
    !info.value?.columns.some(
      (column) => column.name === structureColumn.value,
    )
  ) {
    structureColumn.value = info.value?.columns[0]?.name ?? "";
  }
});

function tableKey(
  table: Pick<TableRef, "catalog" | "schema" | "name">,
) {
  return `${table.catalog}\u0000${table.schema}\u0000${table.name}`;
}

function tableLabel(table: Pick<TableRef, "catalog" | "schema" | "name">) {
  return `${table.catalog}.${table.schema}.${table.name}`;
}

function quoteIdentifier(value: string) {
  return `"${value.replaceAll('"', '""')}"`;
}

function selectStatement(table: TableRef, limit = dataRowLimit.value) {
  const qualified = [table.catalog, table.schema, table.name]
    .map(quoteIdentifier)
    .join(".");
  return `SELECT *\nFROM ${qualified}\nLIMIT ${limit};`;
}

async function loadSelectedTable() {
  const table = selectedTable.value;
  if (!table) {
    info.value = null;
    dataResult.value = null;
    return;
  }
  const version = ++loadVersion;
  loading.value = true;
  errorNote.value = "";
  saveNote.value = "";
  structurePreview.value = null;
  structureConfirm.value = false;
  structureError.value = "";
  try {
    const [details, result] = await Promise.all([
      backend().LoadTableEditorInfo(table),
      backend().RunSQL(
        selectStatement(table),
        dataRowLimit.value,
        "",
      ),
    ]);
    if (version !== loadVersion) return;
    info.value = details;
    dataResult.value = result;
  } catch (error) {
    if (version !== loadVersion) return;
    info.value = null;
    dataResult.value = null;
    errorNote.value = errorMessage(error);
  } finally {
    if (version === loadVersion) loading.value = false;
  }
}

async function refreshData() {
  const table = selectedTable.value;
  if (!table || refreshing.value) return;
  refreshing.value = true;
  errorNote.value = "";
  try {
    dataResult.value = await backend().RunSQL(
      selectStatement(table),
      dataRowLimit.value,
      "",
    );
  } catch (error) {
    errorNote.value = errorMessage(error);
  } finally {
    refreshing.value = false;
  }
}

function structureRequest(): TableStructureRequest | null {
  const table = info.value?.table ?? selectedTable.value;
  if (!table) return null;
  return {
    operation: structureOperation.value,
    table,
    column: structureColumn.value.trim(),
    columns: [...structureKeyColumns.value],
    newName: structureNewName.value.trim(),
    columnType: structureType.value.trim(),
  };
}

async function previewStructureChange() {
  const request = structureRequest();
  if (!request || !structureReady.value || structureBusy.value) return;
  structureBusy.value = true;
  structureError.value = "";
  structureConfirm.value = false;
  try {
    structurePreview.value =
      await backend().PreviewTableStructure(request);
  } catch (error) {
    structurePreview.value = null;
    structureError.value = errorMessage(error);
  } finally {
    structureBusy.value = false;
  }
}

function requestStructureExecution() {
  if (!structurePreview.value || !structureReady.value) return;
  structureConfirm.value = true;
}

async function executeStructureChange() {
  const request = structureRequest();
  if (
    !request ||
    !structurePreview.value ||
    !structureConfirm.value ||
    structureBusy.value
  ) {
    return;
  }
  structureBusy.value = true;
  structureError.value = "";
  try {
    const result = await backend().AlterTableStructure(request);
    structurePreview.value = null;
    structureConfirm.value = false;
    structureNewName.value = "";
    saveNote.value = "表结构已更新";
    emit("schema-changed", result);
    info.value = await backend().LoadTableEditorInfo(result.table);
    dataResult.value = await backend().RunSQL(
      selectStatement(result.table),
      dataRowLimit.value,
      "",
    );
  } catch (error) {
    structureError.value = errorMessage(error);
  } finally {
    structureBusy.value = false;
  }
}

function defaultFilterValue(
  columnName: string,
  value: unknown,
): TableFilter {
  return {
    column: columnName,
    operator: value === null ? "is_null" : "eq",
    value: value === null ? "" : String(value),
  };
}

function openInsertEditor() {
  rowEditorMode.value = "insert";
  rowEditorValues.value = {};
  rowEditorFilters.value = [];
  rowEditorOpen.value = true;
}

function openRowEditor(row: unknown[]) {
  const details = info.value;
  if (!details || !editableRows.value || !dataResult.value) return;
  const values: Record<string, unknown> = {};
  for (let index = 0; index < dataResult.value.columns.length; index += 1) {
    values[dataResult.value.columns[index]] = row[index];
  }
  rowEditorValues.value = values;
  rowEditorFilters.value = details.keyColumns.map((column) =>
    defaultFilterValue(column, values[column]),
  );
  rowEditorMode.value = "update";
  rowEditorOpen.value = true;
}

async function handleSaved(result: TableWriteResult) {
  rowEditorOpen.value = false;
  activeTab.value = "data";
  saveNote.value = `${result.operation === "insert" ? "新增" : "更新"}成功，受影响 ${result.rowsAffected} 行`;
  emit("saved", result);
  await refreshData();
}

function columnPosition(column: ColumnRef) {
  return (
    info.value?.columns.findIndex((item) => item.name === column.name) ?? -1
  ) + 1;
}

function relationFrom(
  relation: TableRelation,
  selected: "source" | "target",
) {
  return selected === "source" ? relation.source : relation.target;
}

function relationColumns(
  relation: TableRelation,
  selected: "source" | "target",
) {
  return selected === "source"
    ? relation.sourceColumns
    : relation.targetColumns;
}

function isSelectedTable(table: TableRef) {
  return Boolean(
    info.value && tableKey(table) === tableKey(info.value.table),
  );
}
</script>

<template>
  <section class="view-panel table-editor-view">
    <div class="page-head compact-head">
      <div>
        <span class="eyebrow">TABLE EDITOR</span>
        <h1>表数据工作台</h1>
      </div>
      <div class="page-actions">
        <select v-model="selectedTableKey" class="select-control table-editor-picker">
          <option v-if="tables.length === 0" value="">无可编辑表</option>
          <option
            v-for="table in tables"
            :key="tableKey(table)"
            :value="tableKey(table)"
          >
            {{ tableLabel(table) }}
          </option>
        </select>
        <button
          class="ghost-button"
          :disabled="loading || !selectedTable"
          @click="loadSelectedTable"
        >
          <AppIcon name="refresh" :size="14" />
          刷新
        </button>
        <button
          class="primary-button"
          :disabled="loading || !info?.writable"
          @click="openInsertEditor"
        >
          <AppIcon name="plus" :size="14" />
          新增记录
        </button>
      </div>
    </div>

    <div v-if="tables.length === 0" class="table-editor-empty">
      <AppIcon name="database" :size="34" />
      <strong>尚未接入数据表</strong>
      <p>导入 CSV、Excel、Parquet 或连接 DuckDB 后，可在这里浏览和编辑表。</p>
    </div>

    <div v-else class="table-editor-shell">
      <header class="table-editor-identity">
        <div class="table-editor-name">
          <div class="editor-table-icon">
            <AppIcon name="table" :size="22" />
          </div>
          <div>
            <span class="eyebrow">CURRENT TABLE</span>
            <h2>{{ selectedName }}</h2>
          </div>
        </div>
        <div class="table-editor-badges">
          <span>{{ info?.table.type || selectedTable?.type }}</span>
          <span v-if="info?.keyType" class="key-badge">{{ keyLabel }}</span>
          <span v-else class="warning-badge">无稳定行键</span>
          <span v-if="info && !info.writable" class="readonly-badge">READ ONLY</span>
        </div>
      </header>

      <div class="table-editor-tabs" role="tablist" aria-label="表编辑器视图">
        <button
          v-for="tab in tabs"
          :key="tab.id"
          type="button"
          role="tab"
          :aria-selected="activeTab === tab.id"
          :class="{ active: activeTab === tab.id }"
          @click="activeTab = tab.id"
        >
          <AppIcon :name="tab.icon" :size="14" />
          {{ tab.label }}
        </button>
      </div>

      <div v-if="errorNote" class="table-editor-error">
        <strong>表加载失败</strong>
        <span>{{ errorNote }}</span>
      </div>
      <div v-else-if="loading && !info" class="table-editor-loading">
        <div class="duck-loader compact"><span></span><span></span><span></span></div>
        <p>正在读取表结构与数据…</p>
      </div>

      <div v-else-if="info" class="table-editor-tab-body">
        <div v-if="activeTab === 'properties'" class="properties-panel">
          <div class="property-card-grid">
            <article class="property-card">
              <span>Database</span>
              <strong>{{ info.table.catalog }}</strong>
              <small>{{ info.writable ? "Writable" : "Read only" }}</small>
            </article>
            <article class="property-card">
              <span>Schema</span>
              <strong>{{ info.table.schema }}</strong>
              <small>{{ info.table.type }}</small>
            </article>
            <article class="property-card">
              <span>Estimated rows</span>
              <strong>{{ formatNumber(info.estimatedRows) }}</strong>
              <small>DuckDB metadata estimate</small>
            </article>
            <article class="property-card">
              <span>Columns</span>
              <strong>{{ formatNumber(info.columnCount) }}</strong>
              <small>{{ info.indexCount }} indexes</small>
            </article>
            <article class="property-card wide">
              <span>Row identity</span>
              <strong>{{ keyLabel }}</strong>
              <small>
                {{
                  hasStableKey
                    ? "行编辑使用该键生成更新条件"
                    : "没有主键或非空唯一键，行首更新按钮将保持禁用"
                }}
              </small>
            </article>
          </div>

          <section v-if="info.comment" class="editor-property-section">
            <div class="editor-section-title">
              <span class="eyebrow">COMMENT</span>
              <h3>表说明</h3>
            </div>
            <p class="table-comment">{{ info.comment }}</p>
          </section>

          <section class="editor-property-section">
            <div class="editor-section-title">
              <span class="eyebrow">COLUMNS</span>
              <h3>字段结构</h3>
              <span>{{ info.columns.length }} 列</span>
            </div>
            <div class="property-table-scroll">
              <table class="property-table">
                <thead>
                  <tr>
                    <th>#</th>
                    <th>字段</th>
                    <th>类型</th>
                    <th>可空</th>
                    <th>键</th>
                    <th>默认值</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="column in info.columns" :key="column.name">
                    <td>{{ columnPosition(column) }}</td>
                    <td><code>{{ column.name }}</code></td>
                    <td><span class="type-chip">{{ column.type }}</span></td>
                    <td>{{ column.nullable ? "YES" : "NO" }}</td>
                    <td>
                      <span
                        v-if="info.keys.some((key) => key.columns.includes(column.name))"
                        class="key-chip"
                      >KEY</span>
                      <span v-else class="muted-value">—</span>
                    </td>
                    <td>
                      <code class="default-value">{{ column.defaultValue || "—" }}</code>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>

          <section class="editor-property-section">
            <div class="editor-section-title">
              <span class="eyebrow">DDL</span>
              <h3>定义</h3>
            </div>
            <SqlPreview
              :sql="info.sql"
              empty-text="DuckDB 未返回该对象的 DDL。"
              aria-label="表定义 DDL"
            />
          </section>
        </div>

        <div v-else-if="activeTab === 'structure'" class="structure-panel">
          <div class="structure-head">
            <div>
              <span class="eyebrow">SCHEMA CHANGE</span>
              <h3>编辑表结构</h3>
              <p>每次只执行一个明确的 ALTER TABLE 操作。DDL 会先预览，再要求确认。</p>
            </div>
            <span class="structure-safety-badge">
              <AppIcon name="database" :size="13" />
              Explicit DDL
            </span>
          </div>

          <div v-if="!info.writable" class="structure-blocked">
            <AppIcon name="database" :size="24" />
            <strong>结构编辑不可用</strong>
            <p>当前对象是视图，或所在数据库为只读。</p>
          </div>

          <template v-else>
            <div class="structure-operation-card">
              <label class="form-field structure-operation-field">
                <span>结构操作</span>
                <select v-model="structureOperation">
                  <option
                    v-for="operation in structureOperations"
                    :key="operation.value"
                    :value="operation.value"
                    :disabled="operation.value === 'add_primary_key' && info.keys.some((key) => key.type === 'PRIMARY KEY')"
                  >
                    {{ operation.label }}
                  </option>
                </select>
              </label>
              <div
                class="structure-operation-description"
                :class="{ destructive: selectedStructureOperation.destructive }"
              >
                <strong>{{ selectedStructureOperation.label }}</strong>
                <span>{{ selectedStructureOperation.description }}</span>
              </div>
            </div>

            <section class="structure-existing-keys">
              <div class="structure-existing-keys-head">
                <div>
                  <span class="eyebrow">KEY CONSTRAINTS</span>
                  <h3>现有键</h3>
                </div>
                <span>{{ info.keys.length }} 个</span>
              </div>
              <div v-if="info.keys.length" class="structure-key-list">
                <article v-for="key in info.keys" :key="key.name">
                  <span>{{ key.type }}</span>
                  <strong>{{ key.columns.join(" + ") }}</strong>
                  <code>{{ key.name }}</code>
                </article>
              </div>
              <p v-else>当前表没有主键或唯一键。</p>
              <small>当前 DuckDB 不支持通过 ALTER TABLE 删除已有主键或唯一约束，因此这里只提供新增键。</small>
            </section>

            <form class="structure-form" @submit.prevent="previewStructureChange">
              <label
                v-if="structureOperation === 'add_column'"
                class="form-field"
              >
                <span>新字段名</span>
                <input
                  v-model="structureColumn"
                  type="text"
                  placeholder="例如 customer_note"
                  autocomplete="off"
                />
              </label>

              <label
                v-else-if="structureNeedsColumn"
                class="form-field"
              >
                <span>字段</span>
                <select v-model="structureColumn">
                  <option
                    v-for="column in info.columns"
                    :key="column.name"
                    :value="column.name"
                  >
                    {{ column.name }} · {{ column.type }}
                  </option>
                </select>
              </label>

              <label v-if="structureNeedsNewName" class="form-field">
                <span>{{ structureOperation === "rename_table" ? "新表名" : "新字段名" }}</span>
                <input
                  v-model="structureNewName"
                  type="text"
                  placeholder="输入新的名称"
                  autocomplete="off"
                />
              </label>

              <label v-if="structureNeedsType" class="form-field">
                <span>DuckDB 类型</span>
                <input
                  v-model="structureType"
                  type="text"
                  placeholder="VARCHAR / BIGINT / DECIMAL(18,2)"
                  autocomplete="off"
                />
              </label>

              <fieldset
                v-if="structureNeedsKeyColumns"
                class="structure-key-columns"
              >
                <legend>键字段</legend>
                <label
                  v-for="column in info.columns"
                  :key="column.name"
                  :class="{ selected: structureKeyColumns.includes(column.name) }"
                >
                  <input
                    v-model="structureKeyColumns"
                    type="checkbox"
                    :value="column.name"
                  />
                  <span>
                    <strong>{{ column.name }}</strong>
                    <code>{{ column.type }}</code>
                  </span>
                </label>
              </fieldset>

              <div class="structure-form-actions">
                <p>
                  字段名会作为标识符转义；类型必须属于结构编辑器允许的安全类型集合。
                </p>
                <button
                  type="submit"
                  class="secondary-button"
                  :disabled="!structureReady || structureBusy"
                >
                  <AppIcon name="search" :size="14" />
                  {{ structureBusy ? "正在检查…" : "检查 DDL" }}
                </button>
              </div>
            </form>

            <section v-if="structurePreview" class="structure-preview-card">
              <div class="structure-preview-head">
                <div>
                  <span class="eyebrow">DDL PREVIEW</span>
                  <h3>即将执行</h3>
                </div>
                <span>{{ structurePreview.operation }}</span>
              </div>
              <SqlPreview
                :sql="structurePreview.sql"
                aria-label="即将执行的结构变更 DDL"
              />

              <div
                v-if="structurePreview.warnings.length"
                class="structure-warnings"
              >
                <strong>执行前注意</strong>
                <ul>
                  <li
                    v-for="warning in structurePreview.warnings"
                    :key="warning"
                  >
                    {{ warning }}
                  </li>
                </ul>
              </div>

              <div v-if="structureConfirm" class="structure-confirm">
                <div>
                  <strong>确认修改表结构？</strong>
                  <p>该操作会立即写入 DuckDB，并刷新表元数据。</p>
                </div>
                <div class="modal-action-group">
                  <button
                    type="button"
                    class="ghost-button"
                    :disabled="structureBusy"
                    @click="structureConfirm = false"
                  >
                    返回检查
                  </button>
                  <button
                    type="button"
                    :class="selectedStructureOperation.destructive ? 'danger-button' : 'primary-button'"
                    :disabled="structureBusy"
                    @click="executeStructureChange"
                  >
                    {{ structureBusy ? "正在执行…" : "确认执行" }}
                  </button>
                </div>
              </div>
              <div v-else class="structure-execute-row">
                <span>建议在破坏性变更前导出工作区数据库快照。</span>
                <button
                  type="button"
                  :class="selectedStructureOperation.destructive ? 'danger-button' : 'primary-button'"
                  :disabled="structureBusy"
                  @click="requestStructureExecution"
                >
                  准备执行
                </button>
              </div>
            </section>

            <div v-if="structureError" class="structure-error">
              {{ structureError }}
            </div>

            <div class="structure-snapshot-note">
              <AppIcon name="download" :size="15" />
              <div>
                <strong>需要回退保障？</strong>
                <p>先通过左侧工作区操作“导出数据库”保存快照，再执行删列或类型变更。</p>
              </div>
            </div>
          </template>
        </div>

        <div v-else-if="activeTab === 'data'" class="data-panel">
          <div class="data-toolbar">
            <div>
              <span class="eyebrow">TABLE DATA</span>
              <strong>{{ dataResult?.rowCount ?? 0 }} 行预览</strong>
            </div>
            <div class="data-toolbar-actions">
              <span v-if="saveNote" class="save-note">{{ saveNote }}</span>
              <label class="data-row-limit">
                <span>预览行数</span>
                <select
                  v-model.number="dataRowLimit"
                  :disabled="refreshing || loading"
                  @change="refreshData"
                >
                  <option
                    v-for="limit in dataRowLimitOptions"
                    :key="limit"
                    :value="limit"
                  >
                    {{ limit }}
                  </option>
                </select>
              </label>
              <button
                type="button"
                class="ghost-button small"
                :disabled="refreshing || loading"
                @click="refreshData"
              >
                <AppIcon name="refresh" :size="13" />
                {{ refreshing ? "刷新中…" : "刷新数据" }}
              </button>
              <button
                type="button"
                class="ghost-button small"
                :disabled="!info.writable"
                @click="openInsertEditor"
              >
                <AppIcon name="plus" :size="13" />
                新增
              </button>
            </div>
          </div>
          <div
            class="row-edit-note"
            :class="{ blocked: !editableRows }"
          >
            <AppIcon :name="editableRows ? 'edit' : 'database'" :size="14" />
            <span v-if="editableRows">
              点击每行最左侧的编辑按钮可按 {{ info.keyColumns.join(" + ") }} 更新该行。
            </span>
            <span v-else-if="!info.writable">
              当前数据库或对象为只读，表数据只能查看。
            </span>
            <span v-else>
              未找到主键或非空唯一键，为避免误更新多行，行编辑已禁用。
            </span>
          </div>
          <ResultGrid
            :result="dataResult"
            :editable="editableRows"
            empty-text="这张表当前没有数据"
            @edit-row="openRowEditor"
          />
        </div>

        <div v-else class="diagram-panel">
          <div class="diagram-head">
            <div>
              <span class="eyebrow">ER DIAGRAM</span>
              <h3>{{ info.table.name }} 关系图</h3>
            </div>
            <span>{{ info.relations.length }} 个外键关系</span>
          </div>

          <div v-if="info.relations.length === 0" class="diagram-empty">
            <AppIcon name="workflow" :size="32" />
            <strong>没有检测到外键关系</strong>
            <p>表本身和字段结构仍可在 Properties 与 Data 中查看。</p>
          </div>
          <div v-else class="er-diagram">
            <article
              v-for="relation in info.relations"
              :key="`${relation.source.catalog}.${relation.source.schema}.${relation.source.name}.${relation.name}`"
              class="er-relation"
            >
              <div
                class="er-node"
                :class="{ selected: isSelectedTable(relation.source) }"
              >
                <div class="er-node-head">
                  <span>{{ relation.direction === "incoming" ? "REFERENCING" : "SOURCE" }}</span>
                  <strong>{{ relationFrom(relation, "source").name }}</strong>
                </div>
                <code
                  v-for="column in relationColumns(relation, 'source')"
                  :key="column"
                >{{ column }}</code>
              </div>
              <div class="er-connector">
                <small>{{ relation.name }}</small>
                <span></span>
                <strong>{{ relation.direction === "incoming" ? "← FK" : "FK →" }}</strong>
              </div>
              <div
                class="er-node"
                :class="{ selected: isSelectedTable(relation.target) }"
              >
                <div class="er-node-head">
                  <span>{{ relation.direction === "incoming" ? "PARENT" : "TARGET" }}</span>
                  <strong>{{ relationFrom(relation, "target").name }}</strong>
                </div>
                <code
                  v-for="column in relationColumns(relation, 'target')"
                  :key="column"
                >{{ column }}</code>
              </div>
            </article>
          </div>
        </div>
      </div>
    </div>

    <TableWriteForm
      v-if="rowEditorOpen && selectedTable"
      :tables="tables"
      :databases="databases"
      :initial-table="selectedTable"
      :initial-mode="rowEditorMode"
      :initial-values="rowEditorValues"
      :initial-filters="rowEditorFilters"
      @close="rowEditorOpen = false"
      @saved="handleSaved"
    />
  </section>
</template>

<style scoped>
.table-editor-view {
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.table-editor-picker {
  width: min(390px, 36vw);
}

.table-editor-shell {
  height: 0;
  min-height: 0;
  flex: 1 1 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 13px;
  background: var(--panel);
}

.table-editor-identity {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  padding: 13px 16px;
  border-bottom: 1px solid var(--border);
  background:
    linear-gradient(110deg, color-mix(in srgb, var(--blue-soft) 55%, transparent), transparent 48%),
    var(--panel-2);
}

.table-editor-name {
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 11px;
}

.table-editor-name > div:last-child {
  min-width: 0;
}

.table-editor-name h2 {
  margin: 2px 0 0;
  overflow: hidden;
  font: calc(15px * var(--ui-font-scale)) var(--mono);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.editor-table-icon {
  width: 40px;
  height: 40px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex: 0 0 auto;
  border: 1px solid color-mix(in srgb, var(--blue) 28%, var(--border));
  border-radius: 10px;
  color: var(--blue);
  background: var(--blue-soft);
}

.table-editor-badges {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  flex-wrap: wrap;
  gap: 6px;
}

.table-editor-badges span {
  padding: 4px 7px;
  border: 1px solid var(--border);
  border-radius: 999px;
  color: var(--subtle);
  background: var(--panel);
  font: calc(8px * var(--ui-font-scale)) var(--mono);
}

.table-editor-badges .key-badge {
  color: var(--teal);
  border-color: color-mix(in srgb, var(--teal) 32%, var(--border));
  background: var(--teal-soft);
}

.table-editor-badges .warning-badge {
  color: var(--orange);
  border-color: color-mix(in srgb, var(--orange) 30%, var(--border));
  background: var(--orange-soft);
}

.table-editor-badges .readonly-badge {
  color: var(--red);
  border-color: color-mix(in srgb, var(--red) 30%, var(--border));
  background: var(--red-soft);
}

.table-editor-tabs {
  flex: 0 0 auto;
  height: 41px;
  display: flex;
  gap: 3px;
  padding: 5px 10px 0;
  border-bottom: 1px solid var(--border);
  background: var(--panel-2);
}

.table-editor-tabs button {
  position: relative;
  min-width: 118px;
  height: 35px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
  padding: 0 14px;
  border: 1px solid transparent;
  border-bottom: 0;
  border-radius: 8px 8px 0 0;
  color: var(--subtle);
  background: transparent;
  font-size: calc(9px * var(--ui-font-scale));
  font-weight: 580;
  cursor: pointer;
}

.table-editor-tabs button:hover {
  color: var(--text);
}

.table-editor-tabs button.active {
  color: var(--text);
  border-color: var(--border);
  background: var(--panel);
}

.table-editor-tabs button.active::after {
  content: "";
  position: absolute;
  right: 0;
  bottom: -1px;
  left: 0;
  height: 2px;
  background: var(--panel);
}

.table-editor-tab-body {
  min-height: 0;
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.properties-panel,
.diagram-panel {
  min-height: 0;
  flex: 1;
  height: 100%;
  overflow: auto;
}

.properties-panel {
  padding: 14px 16px 22px;
}

.property-card-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: 9px;
}

.property-card {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 11px 12px;
  border: 1px solid var(--border);
  border-radius: 9px;
  background: var(--panel-2);
}

.property-card.wide {
  grid-column: span 2;
}

.property-card > span {
  color: var(--subtle);
  font-size: calc(8px * var(--ui-font-scale));
  font-weight: 650;
  letter-spacing: 0.06em;
  text-transform: uppercase;
}

.property-card strong {
  overflow: hidden;
  font: calc(11px * var(--ui-font-scale)) var(--mono);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.property-card small {
  color: var(--subtle);
  font-size: calc(8px * var(--ui-font-scale));
}

.editor-property-section {
  margin-top: 16px;
}

.editor-section-title {
  display: flex;
  align-items: flex-end;
  gap: 9px;
  margin-bottom: 7px;
}

.editor-section-title h3 {
  margin: 0;
  font-size: calc(11px * var(--ui-font-scale));
}

.editor-section-title > span:last-child {
  margin-left: auto;
  color: var(--subtle);
  font: calc(8px * var(--ui-font-scale)) var(--mono);
}

.property-table-scroll {
  overflow: auto;
  border: 1px solid var(--border);
  border-radius: 8px;
}

.property-table {
  width: 100%;
  border-collapse: collapse;
  font: calc(9px * var(--ui-font-scale)) var(--mono);
}

.property-table th,
.property-table td {
  height: 31px;
  padding: 5px 9px;
  border-right: 1px solid var(--border);
  border-bottom: 1px solid var(--border);
  text-align: left;
  white-space: nowrap;
}

.property-table th {
  color: var(--muted);
  background: var(--panel-3);
  font-size: calc(8px * var(--ui-font-scale));
  font-weight: 620;
}

.property-table tr:last-child td {
  border-bottom: 0;
}

.type-chip,
.key-chip {
  padding: 2px 5px;
  border-radius: 4px;
  color: var(--blue);
  background: var(--blue-soft);
  font-size: calc(8px * var(--ui-font-scale));
}

.key-chip {
  color: var(--teal);
  background: var(--teal-soft);
}

.muted-value,
.default-value {
  color: var(--subtle);
}

.table-comment {
  margin: 0;
  padding: 10px 11px;
  border: 1px solid var(--border);
  border-radius: 8px;
  color: var(--muted);
  background: var(--panel-2);
  font-size: calc(9px * var(--ui-font-scale));
  line-height: 1.6;
}

.structure-panel {
  height: 100%;
  overflow: auto;
  padding: 15px 16px 24px;
}

.structure-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 18px;
  margin-bottom: 13px;
}

.structure-head h3 {
  margin: 2px 0 0;
  font-size: calc(13px * var(--ui-font-scale));
}

.structure-head p {
  max-width: 650px;
  margin: 5px 0 0;
  color: var(--subtle);
  font-size: calc(9px * var(--ui-font-scale));
  line-height: 1.55;
}

.structure-safety-badge {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 5px 8px;
  border: 1px solid color-mix(in srgb, var(--orange) 30%, var(--border));
  border-radius: 999px;
  color: var(--orange);
  background: var(--orange-soft);
  font: calc(8px * var(--ui-font-scale)) var(--mono);
}

.structure-blocked {
  min-height: 280px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 7px;
  border: 1px dashed var(--border);
  border-radius: 10px;
  color: var(--subtle);
  text-align: center;
}

.structure-blocked strong {
  color: var(--text);
}

.structure-blocked p {
  margin: 0;
  font-size: calc(9px * var(--ui-font-scale));
}

.structure-operation-card {
  display: grid;
  grid-template-columns: minmax(210px, 0.8fr) minmax(260px, 1.2fr);
  align-items: stretch;
  gap: 10px;
  padding: 11px;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--panel-2);
}

.structure-operation-field {
  margin: 0;
}

.structure-operation-description {
  display: flex;
  flex-direction: column;
  justify-content: center;
  gap: 3px;
  padding: 7px 9px;
  border-left: 2px solid var(--blue);
  color: var(--muted);
}

.structure-operation-description.destructive {
  border-left-color: var(--red);
  background: color-mix(in srgb, var(--red-soft) 48%, transparent);
}

.structure-operation-description strong {
  color: var(--text);
  font-size: calc(10px * var(--ui-font-scale));
}

.structure-operation-description span {
  font-size: calc(8px * var(--ui-font-scale));
}

.structure-existing-keys {
  margin-top: 10px;
  padding: 10px 11px;
  border: 1px solid var(--border);
  border-radius: 9px;
  background: var(--panel);
}

.structure-existing-keys-head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 12px;
}

.structure-existing-keys-head h3 {
  margin: 2px 0 0;
  font-size: calc(10px * var(--ui-font-scale));
}

.structure-existing-keys-head > span {
  color: var(--subtle);
  font: calc(8px * var(--ui-font-scale)) var(--mono);
}

.structure-key-list {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 8px;
}

.structure-key-list article {
  min-width: 180px;
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 2px 7px;
  padding: 7px 8px;
  border: 1px solid color-mix(in srgb, var(--teal) 25%, var(--border));
  border-radius: 7px;
  background: var(--teal-soft);
}

.structure-key-list article > span {
  color: var(--teal);
  font: calc(7px * var(--ui-font-scale)) var(--mono);
}

.structure-key-list article strong {
  overflow: hidden;
  font: calc(9px * var(--ui-font-scale)) var(--mono);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.structure-key-list article code {
  grid-column: 1 / -1;
  overflow: hidden;
  color: var(--subtle);
  font-size: calc(7px * var(--ui-font-scale));
  text-overflow: ellipsis;
  white-space: nowrap;
}

.structure-existing-keys > p,
.structure-existing-keys > small {
  display: block;
  margin: 7px 0 0;
  color: var(--subtle);
  font-size: calc(8px * var(--ui-font-scale));
  line-height: 1.5;
}

.structure-existing-keys > small {
  color: var(--orange);
}

.structure-form {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 9px 11px;
  margin-top: 10px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 10px;
}

.structure-form .form-field {
  margin: 0;
}

.structure-key-columns {
  grid-column: 1 / -1;
  margin: 0;
  padding: 8px;
  border: 1px solid var(--border);
  border-radius: 8px;
}

.structure-key-columns legend {
  padding: 0 5px;
  color: var(--muted);
  font-size: calc(8px * var(--ui-font-scale));
  font-weight: 620;
}

.structure-key-columns {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: 5px;
}

.structure-key-columns label {
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 6px 7px;
  border: 1px solid var(--border);
  border-radius: 6px;
  color: var(--muted);
  background: var(--panel-2);
  cursor: pointer;
}

.structure-key-columns label.selected {
  color: var(--text);
  border-color: color-mix(in srgb, var(--teal) 40%, var(--border));
  background: var(--teal-soft);
}

.structure-key-columns input {
  width: 14px;
  height: 14px;
  margin: 0;
  accent-color: var(--teal);
}

.structure-key-columns label > span {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.structure-key-columns strong {
  overflow: hidden;
  font: calc(8px * var(--ui-font-scale)) var(--mono);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.structure-key-columns code {
  color: var(--subtle);
  font-size: calc(7px * var(--ui-font-scale));
}

.structure-form-actions {
  grid-column: 1 / -1;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  padding-top: 3px;
}

.structure-form-actions p {
  margin: 0;
  color: var(--subtle);
  font-size: calc(8px * var(--ui-font-scale));
  line-height: 1.5;
}

.structure-preview-card {
  margin-top: 11px;
  padding: 12px;
  border: 1px solid color-mix(in srgb, var(--orange) 28%, var(--border));
  border-radius: 10px;
  background:
    linear-gradient(120deg, color-mix(in srgb, var(--orange-soft) 40%, transparent), transparent 42%),
    var(--panel);
}

.structure-preview-head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 8px;
}

.structure-preview-head h3 {
  margin: 2px 0 0;
  font-size: calc(11px * var(--ui-font-scale));
}

.structure-preview-head > span {
  color: var(--subtle);
  font: calc(8px * var(--ui-font-scale)) var(--mono);
}

.structure-warnings {
  margin-top: 9px;
  padding: 9px 10px;
  border-radius: 8px;
  color: var(--muted);
  background: var(--orange-soft);
  font-size: calc(9px * var(--ui-font-scale));
}

.structure-warnings strong {
  color: var(--orange);
}

.structure-warnings ul {
  margin: 5px 0 0;
  padding-left: 18px;
  line-height: 1.55;
}

.structure-execute-row,
.structure-confirm {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  margin-top: 9px;
  padding-top: 9px;
  border-top: 1px solid var(--border);
}

.structure-execute-row > span {
  color: var(--subtle);
  font-size: calc(8px * var(--ui-font-scale));
}

.structure-confirm strong {
  color: var(--red);
  font-size: calc(10px * var(--ui-font-scale));
}

.structure-confirm p {
  margin: 3px 0 0;
  color: var(--muted);
  font-size: calc(8px * var(--ui-font-scale));
}

.structure-error {
  margin-top: 10px;
  padding: 9px 10px;
  border: 1px solid color-mix(in srgb, var(--red) 35%, var(--border));
  border-radius: 8px;
  color: var(--red);
  background: var(--red-soft);
  font-size: calc(9px * var(--ui-font-scale));
}

.structure-snapshot-note {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  margin-top: 11px;
  padding: 9px 10px;
  border: 1px solid var(--border);
  border-radius: 8px;
  color: var(--muted);
  background: var(--panel-2);
}

.structure-snapshot-note strong {
  font-size: calc(9px * var(--ui-font-scale));
}

.structure-snapshot-note p {
  margin: 2px 0 0;
  color: var(--subtle);
  font-size: calc(8px * var(--ui-font-scale));
}

.data-panel {
  height: auto;
  min-height: 0;
  flex: 1 1 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  padding: 10px 12px 12px;
}

.data-toolbar {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  min-height: 35px;
  margin-bottom: 6px;
}

.data-toolbar > div:first-child {
  display: flex;
  align-items: baseline;
  gap: 8px;
}

.data-toolbar strong {
  font: calc(10px * var(--ui-font-scale)) var(--mono);
}

.data-toolbar-actions {
  display: flex;
  align-items: center;
  gap: 7px;
}

.data-row-limit {
  height: 29px;
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 0 7px;
  border: 1px solid var(--border);
  border-radius: 7px;
  color: var(--subtle);
  background: var(--panel-2);
  font-size: calc(8px * var(--ui-font-scale));
}

.data-row-limit select {
  height: 22px;
  padding: 0 18px 0 4px;
  border: 0;
  outline: none;
  color: var(--text);
  background: var(--panel-2);
  font: calc(8px * var(--ui-font-scale)) var(--mono);
}

.save-note {
  color: var(--teal);
  font-size: calc(8px * var(--ui-font-scale));
}

.row-edit-note {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: 7px;
  margin-bottom: 7px;
  padding: 7px 9px;
  border: 1px solid color-mix(in srgb, var(--teal) 26%, var(--border));
  border-radius: 7px;
  color: var(--teal);
  background: var(--teal-soft);
  font-size: calc(8px * var(--ui-font-scale));
}

.row-edit-note.blocked {
  color: var(--orange);
  border-color: color-mix(in srgb, var(--orange) 26%, var(--border));
  background: var(--orange-soft);
}

.data-panel :deep(.result-grid-wrap) {
  height: auto;
  min-height: 0;
  flex: 1 1 0;
  overflow: hidden;
}

.data-panel :deep(.result-scroll) {
  min-height: 0;
  flex: 1 1 0;
  overflow: auto;
}

.data-panel :deep(.pagination) {
  flex: 0 0 auto;
}

.diagram-panel {
  padding: 15px 16px 24px;
  background:
    radial-gradient(circle at 50% 0, color-mix(in srgb, var(--blue-soft) 50%, transparent), transparent 38%),
    var(--panel);
}

.diagram-head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 13px;
}

.diagram-head h3 {
  margin: 2px 0 0;
  font-size: calc(12px * var(--ui-font-scale));
}

.diagram-head > span {
  color: var(--subtle);
  font: calc(8px * var(--ui-font-scale)) var(--mono);
}

.diagram-empty,
.table-editor-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 7px;
  min-height: 280px;
  color: var(--subtle);
  text-align: center;
}

.diagram-empty strong,
.table-editor-empty strong {
  color: var(--text);
  font-size: calc(12px * var(--ui-font-scale));
}

.diagram-empty p,
.table-editor-empty p {
  max-width: 430px;
  margin: 0;
  font-size: calc(9px * var(--ui-font-scale));
}

.table-editor-empty {
  flex: 1;
  border: 1px dashed var(--border);
  border-radius: 12px;
}

.er-diagram {
  display: flex;
  flex-direction: column;
  gap: 11px;
}

.er-relation {
  display: grid;
  grid-template-columns: minmax(180px, 1fr) minmax(150px, 0.65fr) minmax(180px, 1fr);
  align-items: stretch;
  gap: 0;
}

.er-node {
  min-width: 0;
  padding: 9px 10px;
  border: 1px solid var(--border-strong);
  border-radius: 9px;
  background: var(--panel);
  box-shadow: var(--shadow);
}

.er-node.selected {
  border-color: color-mix(in srgb, var(--orange) 50%, var(--border));
  box-shadow:
    0 0 0 2px var(--orange-soft),
    var(--shadow);
}

.er-node-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 7px;
  padding-bottom: 6px;
  border-bottom: 1px solid var(--border);
}

.er-node-head span {
  color: var(--subtle);
  font: calc(7px * var(--ui-font-scale)) var(--mono);
}

.er-node-head strong {
  overflow: hidden;
  font: calc(10px * var(--ui-font-scale)) var(--mono);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.er-node code {
  display: block;
  padding: 3px 0;
  color: var(--muted);
  font: calc(8px * var(--ui-font-scale)) var(--mono);
}

.er-connector {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 2px;
  color: var(--subtle);
}

.er-connector small {
  max-width: 140px;
  overflow: hidden;
  font: calc(7px * var(--ui-font-scale)) var(--mono);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.er-connector span {
  width: 100%;
  height: 1px;
  background: var(--border-strong);
}

.er-connector strong {
  color: var(--blue);
  font: calc(8px * var(--ui-font-scale)) var(--mono);
}

.table-editor-loading {
  min-height: 320px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  color: var(--subtle);
}

.table-editor-error {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 12px;
  padding: 10px 11px;
  border: 1px solid color-mix(in srgb, var(--red) 35%, var(--border));
  border-radius: 8px;
  color: var(--red);
  background: var(--red-soft);
  font-size: calc(9px * var(--ui-font-scale));
}

@media (max-width: 900px) {
  .property-card-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .er-relation {
    grid-template-columns: minmax(140px, 1fr) 95px minmax(140px, 1fr);
  }
}

@media (max-width: 720px) {
  .table-editor-identity,
  .data-toolbar {
    align-items: flex-start;
    flex-direction: column;
  }

  .property-card-grid {
    grid-template-columns: 1fr;
  }

  .property-card.wide {
    grid-column: auto;
  }

  .structure-operation-card,
  .structure-form {
    grid-template-columns: 1fr;
  }

  .structure-form-actions,
  .structure-execute-row,
  .structure-confirm {
    align-items: stretch;
    flex-direction: column;
  }

  .er-relation {
    grid-template-columns: 1fr;
    gap: 3px;
  }

  .er-connector {
    min-height: 34px;
  }
}
</style>
