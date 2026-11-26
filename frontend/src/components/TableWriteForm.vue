<script setup lang="ts">
import { computed, reactive, ref, watch } from "vue";
import { backend, errorMessage } from "../api";
import type {
  ColumnRef,
  DatabaseRef,
  SQLCompletionTable,
  TableFilter,
  TableRef,
  TableValue,
  TableWriteOperation,
  TableWriteResult,
} from "../types";
import AppIcon from "./AppIcon.vue";
import SqlPreview from "./SqlPreview.vue";

const props = defineProps<{
  tables: SQLCompletionTable[];
  databases: DatabaseRef[];
  initialTable?: TableRef | null;
  initialMode?: TableWriteOperation;
  initialValues?: Record<string, unknown>;
  initialFilters?: TableFilter[];
}>();

const emit = defineEmits<{
  close: [];
  saved: [result: TableWriteResult];
}>();

type FieldMode = "value" | "null" | "default";

interface FieldEditor {
  selected: boolean;
  mode: FieldMode;
  value: string;
}

interface FilterEditor extends TableFilter {
  id: number;
}

const mode = ref<TableWriteOperation>(props.initialMode ?? "insert");
const selectedTableKey = ref("");
const fields = reactive<Record<string, FieldEditor>>({});
const filters = ref<FilterEditor[]>([]);
const busy = ref(false);
const errorNote = ref("");
const confirmUpdate = ref(false);
let nextFilterID = 1;

const readOnlyCatalogs = computed(
  () =>
    new Set(
      props.databases
        .filter((database) => database.readOnly)
        .map((database) => database.name),
    ),
);

const writableTables = computed(() =>
  props.tables.filter((table) => {
    const type = table.type.toUpperCase();
    return (
      (type === "BASE TABLE" || type === "TABLE") &&
      !readOnlyCatalogs.value.has(table.catalog)
    );
  }),
);

const selectedTable = computed(
  () =>
    writableTables.value.find(
      (table) => tableKey(table) === selectedTableKey.value,
    ) ?? null,
);

const columns = computed(() => selectedTable.value?.columns ?? []);

const operatorOptions: Array<{
  value: TableFilter["operator"];
  label: string;
}> = [
  { value: "eq", label: "=" },
  { value: "ne", label: "≠" },
  { value: "gt", label: ">" },
  { value: "gte", label: "≥" },
  { value: "lt", label: "<" },
  { value: "lte", label: "≤" },
  { value: "like", label: "LIKE" },
  { value: "ilike", label: "ILIKE" },
  { value: "is_null", label: "IS NULL" },
  { value: "is_not_null", label: "IS NOT NULL" },
];

const operatorSQL: Record<TableFilter["operator"], string> = {
  eq: "=",
  ne: "<>",
  gt: ">",
  gte: ">=",
  lt: "<",
  lte: "<=",
  like: "LIKE",
  ilike: "ILIKE",
  is_null: "IS NULL",
  is_not_null: "IS NOT NULL",
};

watch(
  () => props.tables,
  () => {
    if (props.initialTable) {
      const initialKey = tableKey(props.initialTable);
      if (
        writableTables.value.some(
          (table) => tableKey(table) === initialKey,
        )
      ) {
        selectedTableKey.value = initialKey;
        return;
      }
    }
    if (
      !writableTables.value.some(
        (table) => tableKey(table) === selectedTableKey.value,
      )
    ) {
      selectedTableKey.value = writableTables.value[0]
        ? tableKey(writableTables.value[0])
        : "";
    }
  },
  { immediate: true },
);

watch([mode, selectedTableKey], resetEditors, { immediate: true });

function tableKey(table: Pick<SQLCompletionTable, "catalog" | "schema" | "name">) {
  return `${table.catalog}\u0000${table.schema}\u0000${table.name}`;
}

function tableLabel(table: SQLCompletionTable) {
  return `${table.catalog}.${table.schema}.${table.name}`;
}

function likelyKeyColumn(values: ColumnRef[]) {
  return (
    values.find((column) => column.name.toLocaleLowerCase() === "id") ??
    values.find((column) => /(^|_)id$/i.test(column.name)) ??
    values.find((column) => /key|code/i.test(column.name)) ??
    values[0]
  );
}

function defaultFieldMode(column: ColumnRef): FieldMode {
  if (mode.value === "update") return "value";
  if (column.defaultValue) return "default";
  if (column.nullable) return "null";
  return "value";
}

function defaultFieldValue(column: ColumnRef) {
  return isBooleanType(column.type) ? "true" : "";
}

function resetEditors() {
  confirmUpdate.value = false;
  errorNote.value = "";
  for (const key of Object.keys(fields)) delete fields[key];
  filters.value = [];
  const table = selectedTable.value;
  if (!table) return;
  for (const column of table.columns) {
    const initialValue = props.initialValues?.[column.name];
    const fromInitialTable =
      props.initialTable != null &&
      tableKey(table) === tableKey(props.initialTable);
    fields[column.name] = {
      selected: false,
      mode:
        fromInitialTable && initialValue !== undefined
          ? initialValue === null
            ? "null"
            : "value"
          : defaultFieldMode(column),
      value:
        fromInitialTable && initialValue !== undefined && initialValue !== null
          ? String(initialValue)
          : defaultFieldValue(column),
    };
  }
  if (
    mode.value === "update" &&
    props.initialFilters?.length &&
    props.initialTable != null &&
    tableKey(table) === tableKey(props.initialTable)
  ) {
    filters.value = props.initialFilters.map((filter) => ({
      ...filter,
      id: nextFilterID++,
    }));
    return;
  }
  const keyColumn = likelyKeyColumn(table.columns);
  if (keyColumn) {
    filters.value = [
      {
        id: nextFilterID++,
        column: keyColumn.name,
        operator: "eq",
        value: "",
      },
    ];
  }
}

function setInsertMode(column: ColumnRef, nextMode: FieldMode) {
  const field = fields[column.name];
  if (!field) return;
  field.mode = nextMode;
  confirmUpdate.value = false;
}

function setUpdateMode(column: ColumnRef, nextMode: FieldMode) {
  const field = fields[column.name];
  if (!field) return;
  field.mode = nextMode;
  confirmUpdate.value = false;
}

function toggleUpdateField(column: ColumnRef, selected: boolean) {
  const field = fields[column.name];
  if (!field) return;
  field.selected = selected;
  if (selected && !field.value && isBooleanType(column.type)) {
    field.value = "true";
  }
  confirmUpdate.value = false;
}

function addFilter() {
  const firstColumn = columns.value[0];
  if (!firstColumn) return;
  filters.value.push({
    id: nextFilterID++,
    column: firstColumn.name,
    operator: "eq",
    value: "",
  });
  confirmUpdate.value = false;
}

function removeFilter(id: number) {
  filters.value = filters.value.filter((filter) => filter.id !== id);
  confirmUpdate.value = false;
}

function isBooleanType(type: string) {
  return /^(BOOL|BOOLEAN)$/i.test(type.trim());
}

function isIntegerType(type: string) {
  return /^(TINYINT|SMALLINT|INTEGER|BIGINT|HUGEINT|UTINYINT|USMALLINT|UINTEGER|UBIGINT|UHUGEINT)$/i.test(
    type.trim(),
  );
}

function isNumericType(type: string) {
  return (
    isIntegerType(type) ||
    /^(DECIMAL|NUMERIC|REAL|FLOAT|DOUBLE)/i.test(type.trim())
  );
}

function inputType(column: ColumnRef) {
  const type = column.type.trim().toUpperCase();
  if (type === "DATE") return "date";
  if (type.startsWith("TIMESTAMP") || type === "DATETIME") return "datetime-local";
  if (type === "TIME") return "time";
  if (isNumericType(type)) return "number";
  return "text";
}

function inputStep(column: ColumnRef) {
  return isIntegerType(column.type) ? "1" : "any";
}

function fieldHint(column: ColumnRef) {
  if (column.defaultValue) return `默认值 ${column.defaultValue}`;
  if (column.nullable) return "允许 NULL";
  return "必填";
}

function displayFieldValue(column: ColumnRef) {
  const field = fields[column.name];
  if (!field || field.mode === "null") return "NULL";
  if (field.mode === "default") return "DEFAULT";
  return field.value === "" ? "(空字符串)" : field.value;
}

function filterNeedsValue(filter: TableFilter) {
  return filter.operator !== "is_null" && filter.operator !== "is_not_null";
}

function quoteIdentifier(value: string) {
  return `"${value.replaceAll('"', '""')}"`;
}

const previewValues = computed(() => {
  const values: TableValue[] = [];
  for (const column of columns.value) {
    const field = fields[column.name];
    if (!field) continue;
    if (mode.value === "insert" && field.mode === "default") continue;
    if (mode.value === "update" && !field.selected) continue;
    values.push({
      column: column.name,
      value: field.value,
      null: field.mode === "null",
    });
  }
  return values;
});

const previewSQL = computed(() => {
  const table = selectedTable.value;
  if (!table) return "-- 没有可写表";
  const qualifiedTable = [table.catalog, table.schema, table.name]
    .map(quoteIdentifier)
    .join(".");
  if (mode.value === "insert") {
    if (previewValues.value.length === 0) {
      return `INSERT INTO ${qualifiedTable} DEFAULT VALUES`;
    }
    return `INSERT INTO ${qualifiedTable} (${previewValues.value
      .map((value) => quoteIdentifier(value.column))
      .join(", ")}) VALUES (${previewValues.value.map(() => "?").join(", ")})`;
  }
  const assignments = previewValues.value
    .map((value) => `${quoteIdentifier(value.column)} = ?`)
    .join(", ");
  const conditions = filters.value
    .map((filter) => {
      const operator = operatorSQL[filter.operator];
      return filterNeedsValue(filter)
        ? `${quoteIdentifier(filter.column)} ${operator} ?`
        : `${quoteIdentifier(filter.column)} ${operator}`;
    })
    .join(" AND ");
  if (!assignments) return "-- 请勾选要修改的字段";
  if (!conditions) return "-- 请至少添加一个更新条件";
  return `UPDATE ${qualifiedTable}\nSET ${assignments}\nWHERE ${conditions}`;
});

function validateRequest() {
  if (!selectedTable.value) return "请选择要写入的数据表。";
  if (mode.value === "update" && previewValues.value.length === 0) {
    return "请至少勾选一个要修改的字段。";
  }
  if (mode.value === "update" && filters.value.length === 0) {
    return "更新操作必须包含至少一个过滤条件。";
  }
  for (const column of columns.value) {
    const field = fields[column.name];
    if (!field) continue;
    const active =
      mode.value === "insert"
        ? field.mode !== "default"
        : field.selected;
    if (!active || field.mode === "null") continue;
    if (
      field.value.trim() === "" &&
      (isNumericType(column.type) ||
        inputType(column) === "date" ||
        inputType(column) === "datetime-local" ||
        inputType(column) === "time")
    ) {
      return `字段 ${column.name} 需要有效值，或选择 NULL。`;
    }
  }
  return "";
}

function requestSubmit() {
  errorNote.value = validateRequest();
  if (errorNote.value) return;
  if (mode.value === "update" && !confirmUpdate.value) {
    confirmUpdate.value = true;
    return;
  }
  void submit();
}

async function submit() {
  const table = selectedTable.value;
  if (!table || busy.value) return;
  busy.value = true;
  errorNote.value = "";
  try {
    const result = await backend().WriteTableRow({
      operation: mode.value,
      table,
      values: previewValues.value,
      filters:
        mode.value === "update"
          ? filters.value.map(({ column, operator, value }) => ({
              column,
              operator,
              value,
            }))
          : [],
    });
    emit("saved", result);
  } catch (error) {
    errorNote.value = errorMessage(error);
  } finally {
    busy.value = false;
  }
}

function close() {
  if (!busy.value) emit("close");
}
</script>

<template>
  <Teleport to="body">
    <div class="modal-backdrop table-write-backdrop" @click.self="close">
      <form class="modal-card table-write-card" @submit.prevent="requestSubmit">
        <div class="modal-head table-write-head">
          <div class="modal-icon"><AppIcon name="table" :size="22" /></div>
          <div>
            <span class="eyebrow">TABLE ROW EDITOR</span>
            <h2>新增与更新数据</h2>
          </div>
          <button type="button" class="text-button" :disabled="busy" @click="close">
            关闭
          </button>
        </div>

        <div class="write-mode-switch" role="tablist" aria-label="写入模式">
          <button
            type="button"
            :class="{ active: mode === 'insert' }"
            :aria-selected="mode === 'insert'"
            role="tab"
            @click="mode = 'insert'"
          >
            <AppIcon name="plus" :size="14" />
            新增记录
          </button>
          <button
            type="button"
            :class="{ active: mode === 'update' }"
            :aria-selected="mode === 'update'"
            role="tab"
            @click="mode = 'update'"
          >
            <AppIcon name="refresh" :size="14" />
            更新记录
          </button>
        </div>

        <div v-if="writableTables.length === 0" class="write-empty">
          <AppIcon name="database" :size="28" />
          <strong>没有可写入的数据表</strong>
          <p>请先创建表，或选择非只读数据库中的基础表。</p>
        </div>

        <template v-else>
          <div class="write-table-picker">
            <label class="form-field">
              <span>目标表</span>
              <select v-model="selectedTableKey">
                <option
                  v-for="table in writableTables"
                  :key="tableKey(table)"
                  :value="tableKey(table)"
                >
                  {{ tableLabel(table) }}
                </option>
              </select>
            </label>
            <div class="write-table-meta">
              <span>{{ columns.length }} 个字段</span>
              <span>{{ selectedTable?.type }}</span>
            </div>
          </div>

          <div class="write-form-scroll">
            <section class="write-section">
              <div class="write-section-head">
                <div>
                  <span class="eyebrow">COLUMN VALUES</span>
                  <h3>{{ mode === "insert" ? "字段值" : "要修改的字段" }}</h3>
                </div>
                <p v-if="mode === 'insert'">
                  可为字段指定值、NULL，或省略并使用数据库默认值。
                </p>
                <p v-else>先勾选字段，再输入新值；未勾选字段保持不变。</p>
              </div>

              <div class="write-fields">
                <div
                  v-for="column in columns"
                  :key="column.name"
                  class="write-field-row"
                  :class="{
                    inactive:
                      mode === 'insert'
                        ? fields[column.name]?.mode === 'default'
                        : !fields[column.name]?.selected,
                  }"
                >
                  <div class="write-field-meta">
                    <strong>{{ column.name }}</strong>
                    <code>{{ column.type }}</code>
                    <span>{{ fieldHint(column) }}</span>
                  </div>

                  <div v-if="mode === 'insert'" class="write-field-control">
                    <div class="write-value-mode" aria-label="字段值模式">
                      <button
                        type="button"
                        :class="{ active: fields[column.name]?.mode === 'value' }"
                        @click="setInsertMode(column, 'value')"
                      >
                        值
                      </button>
                      <button
                        type="button"
                        :class="{ active: fields[column.name]?.mode === 'null' }"
                        @click="setInsertMode(column, 'null')"
                      >
                        NULL
                      </button>
                      <button
                        type="button"
                        :class="{ active: fields[column.name]?.mode === 'default' }"
                        @click="setInsertMode(column, 'default')"
                      >
                        默认
                      </button>
                    </div>
                    <select
                      v-if="fields[column.name]?.mode === 'value' && isBooleanType(column.type)"
                      v-model="fields[column.name].value"
                    >
                      <option value="true">true</option>
                      <option value="false">false</option>
                    </select>
                    <input
                      v-else-if="fields[column.name]?.mode === 'value'"
                      v-model="fields[column.name].value"
                      :type="inputType(column)"
                      :step="inputStep(column)"
                      :placeholder="column.type"
                    />
                    <span v-else class="write-static-value">
                      {{ fields[column.name]?.mode === "null" ? "NULL" : "DEFAULT" }}
                    </span>
                  </div>

                  <div
                    v-else
                    class="write-field-control update-control"
                    :class="{ 'current-only': !fields[column.name]?.selected }"
                  >
                    <label class="write-include">
                      <input
                        type="checkbox"
                        :checked="fields[column.name]?.selected"
                        @change="
                          toggleUpdateField(
                            column,
                            ($event.target as HTMLInputElement).checked,
                          )
                        "
                      />
                      修改
                    </label>
                    <span
                      v-if="!fields[column.name]?.selected"
                      class="write-current-value"
                      :title="displayFieldValue(column)"
                    >
                      {{ displayFieldValue(column) }}
                    </span>
                    <template v-if="fields[column.name]?.selected">
                      <div class="write-value-mode" aria-label="更新值模式">
                        <button
                          type="button"
                          :class="{ active: fields[column.name]?.mode === 'value' }"
                          @click="setUpdateMode(column, 'value')"
                        >
                          值
                        </button>
                        <button
                          type="button"
                          :class="{ active: fields[column.name]?.mode === 'null' }"
                          @click="setUpdateMode(column, 'null')"
                        >
                          NULL
                        </button>
                      </div>
                      <select
                        v-if="fields[column.name]?.mode === 'value' && isBooleanType(column.type)"
                        v-model="fields[column.name].value"
                      >
                        <option value="true">true</option>
                        <option value="false">false</option>
                      </select>
                      <input
                        v-else-if="fields[column.name]?.mode === 'value'"
                        v-model="fields[column.name].value"
                        :type="inputType(column)"
                        :step="inputStep(column)"
                        :placeholder="column.type"
                      />
                      <span v-else class="write-static-value">NULL</span>
                    </template>
                  </div>
                </div>
              </div>
            </section>

            <section v-if="mode === 'update'" class="write-section">
              <div class="write-section-head">
                <div>
                  <span class="eyebrow">MATCH ROWS</span>
                  <h3>更新条件</h3>
                </div>
                <button type="button" class="text-button" @click="addFilter">
                  <AppIcon name="plus" :size="13" /> 添加条件
                </button>
              </div>
              <div class="write-filter-list">
                <div
                  v-for="filter in filters"
                  :key="filter.id"
                  class="write-filter-row"
                >
                  <select v-model="filter.column">
                    <option
                      v-for="column in columns"
                      :key="column.name"
                      :value="column.name"
                    >
                      {{ column.name }}
                    </option>
                  </select>
                  <select v-model="filter.operator">
                    <option
                      v-for="operator in operatorOptions"
                      :key="operator.value"
                      :value="operator.value"
                    >
                      {{ operator.label }}
                    </option>
                  </select>
                  <input
                    v-if="filterNeedsValue(filter)"
                    v-model="filter.value"
                    type="text"
                    placeholder="匹配值"
                  />
                  <span v-else class="write-no-value">无需输入值</span>
                  <button
                    type="button"
                    class="text-button danger-text"
                    :disabled="filters.length === 1"
                    @click="removeFilter(filter.id)"
                  >
                    移除
                  </button>
                </div>
              </div>
              <p class="write-safety-note">
                多个条件之间使用 AND。更新语句必须包含条件，避免误改整张表。
              </p>
            </section>

            <section class="write-section">
              <div class="write-section-head">
                <div>
                  <span class="eyebrow">PARAMETERIZED SQL</span>
                  <h3>执行预览</h3>
                </div>
                <span class="write-parameter-count">
                  {{ previewValues.length + (mode === "update" ? filters.filter(filterNeedsValue).length : 0) }}
                  个参数
                </span>
              </div>
              <SqlPreview
                :sql="previewSQL"
                aria-label="参数化执行 SQL 预览"
                :max-height="128"
              />
            </section>

            <div v-if="errorNote" class="write-error">{{ errorNote }}</div>

            <div v-if="confirmUpdate" class="write-confirm">
              <div>
                <strong>确认执行更新？</strong>
                <p>请检查条件与 SQL 预览。保存后将立即写入 DuckDB。</p>
              </div>
              <button
                type="button"
                class="ghost-button"
                :disabled="busy"
                @click="confirmUpdate = false"
              >
                返回检查
              </button>
            </div>
          </div>

          <div class="modal-actions write-actions">
            <span>值通过参数绑定写入，不拼接进 SQL。</span>
            <div class="modal-action-group">
              <button type="button" class="ghost-button" :disabled="busy" @click="close">
                取消
              </button>
              <button
                type="submit"
                :class="confirmUpdate ? 'danger-button' : 'primary-button'"
                :disabled="busy || writableTables.length === 0"
              >
                <AppIcon :name="mode === 'insert' ? 'plus' : 'refresh'" :size="14" />
                {{
                  busy
                    ? "正在写入…"
                    : confirmUpdate
                      ? "确认执行更新"
                      : mode === "insert"
                        ? "新增记录"
                        : "检查并更新"
                }}
              </button>
            </div>
          </div>
        </template>
      </form>
    </div>
  </Teleport>
</template>

<style scoped>
.table-write-backdrop {
  z-index: 95;
  padding: 24px;
}

.table-write-card {
  width: min(860px, 100%);
  max-height: calc(100vh - 48px);
  display: flex;
  flex-direction: column;
  padding: 0;
  overflow: hidden;
}

.table-write-head {
  flex: 0 0 auto;
  margin: 0;
  padding: 18px 20px 14px;
  border-bottom: 1px solid var(--border);
}

.table-write-head > div:nth-child(2) {
  min-width: 0;
  flex: 1;
}

.write-mode-switch {
  flex: 0 0 auto;
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 4px;
  margin: 12px 20px 0;
  padding: 4px;
  border: 1px solid var(--border);
  border-radius: 9px;
  background: var(--panel-2);
}

.write-mode-switch button {
  height: 34px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
  border: 0;
  border-radius: 6px;
  color: var(--muted);
  background: transparent;
  font-size: calc(10px * var(--ui-font-scale));
  font-weight: 580;
  cursor: pointer;
}

.write-mode-switch button.active {
  color: var(--text);
  background: var(--panel);
  box-shadow: 0 1px 6px rgba(0, 0, 0, 0.16);
}

.write-empty {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 7px;
  min-height: 260px;
  padding: 32px;
  color: var(--subtle);
  text-align: center;
}

.write-empty strong {
  color: var(--text);
}

.write-empty p {
  margin: 0;
  font-size: calc(10px * var(--ui-font-scale));
}

.write-table-picker {
  flex: 0 0 auto;
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: end;
  gap: 12px;
  padding: 12px 20px;
  border-bottom: 1px solid var(--border);
}

.write-table-picker .form-field {
  margin: 0;
}

.write-table-meta {
  height: 37px;
  display: flex;
  align-items: center;
  gap: 7px;
  color: var(--subtle);
  font: calc(9px * var(--ui-font-scale)) var(--mono);
}

.write-table-meta span {
  padding: 4px 7px;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: var(--panel-2);
}

.write-form-scroll {
  min-height: 0;
  flex: 1 1 auto;
  overflow: auto;
  padding: 14px 20px 18px;
}

.write-section + .write-section {
  margin-top: 14px;
}

.write-section-head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 18px;
  margin-bottom: 8px;
}

.write-section-head h3 {
  margin: 2px 0 0;
  font-size: calc(12px * var(--ui-font-scale));
}

.write-section-head p {
  max-width: 470px;
  margin: 0;
  color: var(--subtle);
  font-size: calc(9px * var(--ui-font-scale));
  line-height: 1.5;
  text-align: right;
}

.write-fields {
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 9px;
}

.write-field-row {
  display: grid;
  grid-template-columns: minmax(170px, 0.8fr) minmax(260px, 1.4fr);
  align-items: center;
  gap: 14px;
  min-height: 48px;
  padding: 7px 10px;
  border-bottom: 1px solid var(--border);
  background: color-mix(in srgb, var(--panel) 88%, var(--panel-2));
}

.write-field-row:last-child {
  border-bottom: 0;
}

.write-field-row.inactive {
  opacity: 0.62;
}

.write-field-meta {
  min-width: 0;
  display: grid;
  grid-template-columns: minmax(0, auto) auto;
  justify-content: start;
  align-items: center;
  gap: 3px 8px;
}

.write-field-meta strong {
  overflow: hidden;
  font: calc(10px * var(--ui-font-scale)) var(--mono);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.write-field-meta code {
  padding: 2px 5px;
  border-radius: 4px;
  color: var(--blue);
  background: var(--blue-soft);
  font-size: calc(8px * var(--ui-font-scale));
}

.write-field-meta span {
  grid-column: 1 / -1;
  color: var(--subtle);
  font-size: calc(8px * var(--ui-font-scale));
}

.write-field-control {
  min-width: 0;
  display: grid;
  grid-template-columns: auto minmax(120px, 1fr);
  align-items: center;
  gap: 8px;
}

.write-field-control.update-control {
  grid-template-columns: auto auto minmax(120px, 1fr);
}

.write-field-control.update-control.current-only {
  grid-template-columns: auto minmax(0, 1fr);
}

.write-field-control input,
.write-field-control select,
.write-filter-row input,
.write-filter-row select {
  min-width: 0;
  height: 31px;
  padding: 0 8px;
  border: 1px solid var(--border);
  border-radius: 7px;
  outline: none;
  color: var(--text);
  background: var(--panel-2);
  font-size: calc(10px * var(--ui-font-scale));
}

.write-field-control input:focus,
.write-field-control select:focus,
.write-filter-row input:focus,
.write-filter-row select:focus {
  border-color: color-mix(in srgb, var(--blue) 60%, var(--border));
}

.write-value-mode {
  display: inline-flex;
  padding: 2px;
  border: 1px solid var(--border);
  border-radius: 7px;
  background: var(--panel-2);
}

.write-value-mode button {
  height: 24px;
  min-width: 34px;
  padding: 0 6px;
  border: 0;
  border-radius: 5px;
  color: var(--subtle);
  background: transparent;
  font-size: calc(8px * var(--ui-font-scale));
  cursor: pointer;
}

.write-value-mode button.active {
  color: var(--text);
  background: var(--panel);
  box-shadow: 0 1px 4px rgba(0, 0, 0, 0.15);
}

.write-static-value {
  height: 31px;
  display: flex;
  align-items: center;
  color: var(--subtle);
  font: calc(9px * var(--ui-font-scale)) var(--mono);
}

.write-current-value {
  min-width: 0;
  overflow: hidden;
  color: var(--muted);
  font: calc(9px * var(--ui-font-scale)) var(--mono);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.write-include {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  color: var(--muted);
  font-size: calc(9px * var(--ui-font-scale));
  white-space: nowrap;
}

.write-include input {
  width: 14px;
  height: 14px;
  margin: 0;
  accent-color: var(--orange);
}

.write-filter-list {
  display: flex;
  flex-direction: column;
  gap: 7px;
}

.write-filter-row {
  display: grid;
  grid-template-columns: minmax(120px, 1fr) 105px minmax(130px, 1.2fr) auto;
  align-items: center;
  gap: 7px;
  padding: 7px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel-2);
}

.write-no-value {
  height: 31px;
  display: flex;
  align-items: center;
  color: var(--subtle);
  font-size: calc(9px * var(--ui-font-scale));
}

.write-safety-note {
  margin: 7px 0 0;
  color: var(--subtle);
  font-size: calc(8px * var(--ui-font-scale));
}

.write-parameter-count {
  color: var(--subtle);
  font: calc(8px * var(--ui-font-scale)) var(--mono);
}

.write-error {
  margin-top: 12px;
  padding: 9px 10px;
  border: 1px solid color-mix(in srgb, var(--red) 36%, var(--border));
  border-radius: 8px;
  color: var(--red);
  background: var(--red-soft);
  font-size: calc(9px * var(--ui-font-scale));
}

.write-confirm {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin-top: 12px;
  padding: 11px 12px;
  border: 1px solid color-mix(in srgb, var(--orange) 36%, var(--border));
  border-radius: 9px;
  background: var(--orange-soft);
}

.write-confirm strong {
  color: var(--orange);
  font-size: calc(10px * var(--ui-font-scale));
}

.write-confirm p {
  margin: 3px 0 0;
  color: var(--muted);
  font-size: calc(8px * var(--ui-font-scale));
}

.write-actions {
  flex: 0 0 auto;
  align-items: center;
  margin: 0;
  padding: 12px 20px;
  border-top: 1px solid var(--border);
  background: var(--panel);
}

.write-actions > span {
  margin-right: auto;
  color: var(--subtle);
  font-size: calc(8px * var(--ui-font-scale));
}

@media (max-width: 760px) {
  .table-write-backdrop {
    padding: 10px;
  }

  .table-write-card {
    max-height: calc(100vh - 20px);
  }

  .write-table-picker,
  .write-field-row,
  .write-filter-row {
    grid-template-columns: 1fr;
  }

  .write-table-meta {
    height: auto;
  }

  .write-field-control,
  .write-field-control.update-control {
    grid-template-columns: auto minmax(0, 1fr);
  }

  .write-field-control.update-control .write-value-mode {
    grid-column: 1 / -1;
    justify-self: start;
  }

  .write-section-head p {
    display: none;
  }

  .write-actions {
    align-items: stretch;
    flex-direction: column;
  }

  .write-actions > span {
    margin: 0 0 8px;
  }

  .modal-action-group {
    justify-content: flex-end;
  }
}
</style>
