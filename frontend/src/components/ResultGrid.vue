<script setup lang="ts">
import { computed, ref, watch } from "vue";
import type { QueryResult } from "../types";
import { backend, errorMessage, formatNumber } from "../api";
import AppIcon from "./AppIcon.vue";

const props = defineProps<{
  result: QueryResult | null;
  emptyText?: string;
}>();

const page = ref(1);
const pageSize = 50;
const expanded = ref(false);
const exportBusy = ref<"csv" | "parquet" | null>(null);
const exportNote = ref("");
const searchQuery = ref("");
const sortColumn = ref<number | null>(null);
const sortDirection = ref<"asc" | "desc" | null>(null);

watch(
  () => props.result,
  () => {
    page.value = 1;
    expanded.value = false;
    exportNote.value = "";
    searchQuery.value = "";
    sortColumn.value = null;
    sortDirection.value = null;
  },
);

watch([searchQuery, sortColumn, sortDirection], () => {
  page.value = 1;
});

const indexedRows = computed(() =>
  (props.result?.rows ?? []).map((row, originalIndex) => ({
    row,
    originalIndex,
  })),
);

const filteredRows = computed(() => {
  const query = searchQuery.value.trim().toLocaleLowerCase();
  if (!query) return indexedRows.value;
  return indexedRows.value.filter(({ row }) =>
    row.some((value) =>
      formatNumber(value).toLocaleLowerCase().includes(query),
    ),
  );
});

const sortedRows = computed(() => {
  if (sortColumn.value === null || sortDirection.value === null) {
    return filteredRows.value;
  }
  const column = sortColumn.value;
  const direction = sortDirection.value === "asc" ? 1 : -1;
  return [...filteredRows.value].sort((left, right) => {
    const a = left.row[column] ?? null;
    const b = right.row[column] ?? null;
    if (a === null && b === null) return 0;
    if (a === null) return 1;
    if (b === null) return -1;
    if (typeof a === "number" && typeof b === "number") {
      return (a - b) * direction;
    }
    const aNumber = Number(a);
    const bNumber = Number(b);
    if (Number.isFinite(aNumber) && Number.isFinite(bNumber)) {
      return (aNumber - bNumber) * direction;
    }
    return String(a).localeCompare(String(b), "zh-CN", {
      numeric: true,
      sensitivity: "base",
    }) * direction;
  });
});

const totalPages = computed(() => {
  const count = sortedRows.value.length;
  return Math.max(1, Math.ceil(count / pageSize));
});

const visibleRows = computed(() => {
  const start = (page.value - 1) * pageSize;
  return sortedRows.value.slice(start, start + pageSize);
});

function toggleSort(column: number) {
  if (sortColumn.value !== column) {
    sortColumn.value = column;
    sortDirection.value = "asc";
    return;
  }
  if (sortDirection.value === "asc") {
    sortDirection.value = "desc";
  } else if (sortDirection.value === "desc") {
    sortDirection.value = null;
    sortColumn.value = null;
  } else {
    sortDirection.value = "asc";
  }
}

async function exportResult(format: "csv" | "parquet") {
  if (!props.result || exportBusy.value) return;
  exportBusy.value = format;
  exportNote.value = "";
  try {
    const client = backend();
    const result = {
      ...props.result,
      rows: sortedRows.value.map(({ row }) => row),
      rowCount: sortedRows.value.length,
      truncated: false,
    };
    const path =
      format === "csv"
        ? await client.ExportQueryCSV(result)
        : await client.ExportQueryParquet(result);
    if (path) exportNote.value = `已导出：${path}`;
  } catch (error) {
    exportNote.value = errorMessage(error);
  } finally {
    exportBusy.value = null;
  }
}
</script>

<template>
  <Teleport to="body" :disabled="!expanded">
    <div
      v-if="expanded"
      class="expanded-result-backdrop"
      @click="expanded = false"
    ></div>
    <div class="result-grid-wrap" :class="{ expanded }">
      <div v-if="expanded" class="expanded-result-head">
        <div>
          <span class="eyebrow">QUERY RESULT</span>
          <strong>{{ result?.rowCount.toLocaleString() }} 行 · {{ result?.columns.length }} 列</strong>
        </div>
        <button class="ghost-button small" @click="expanded = false">退出全屏</button>
      </div>
      <div v-if="!result" class="empty-state compact">
        <div class="empty-mark"><slot name="empty-icon">SQL</slot></div>
        <p>{{ emptyText ?? "运行查询后在这里查看结果" }}</p>
      </div>
      <template v-else>
        <div class="result-meta">
          <label class="result-search">
            <AppIcon name="search" :size="13" />
            <input v-model="searchQuery" type="search" placeholder="搜索当前结果" />
          </label>
          <span v-if="searchQuery.trim()">
            筛选 {{ sortedRows.length.toLocaleString() }} / {{ result.rowCount.toLocaleString() }} 行
          </span>
          <span v-else>{{ result.rowCount.toLocaleString() }} 行</span>
          <span v-if="result.truncated" class="warning-text">已截断</span>
          <span>{{ result.elapsedMs }} ms</span>
          <span>{{ result.columns.length }} 列</span>
          <span v-if="exportNote" class="export-note">{{ exportNote }}</span>
          <div class="result-actions">
            <button
              class="text-button"
              :disabled="exportBusy !== null"
              @click="exportResult('csv')"
            >
              <AppIcon name="file" :size="13" />
              {{ exportBusy === "csv" ? "导出 CSV 中…" : "导出 CSV" }}
            </button>
            <button
              class="text-button"
              :disabled="exportBusy !== null"
              @click="exportResult('parquet')"
            >
              <AppIcon name="file" :size="13" />
              {{ exportBusy === "parquet" ? "导出 Parquet 中…" : "导出 Parquet" }}
            </button>
            <button class="text-button" @click="expanded = !expanded">
              <AppIcon name="external" :size="13" />
              {{ expanded ? "恢复" : "放大" }}
            </button>
          </div>
        </div>
        <div class="result-scroll">
          <table class="result-table">
            <thead>
              <tr>
                <th class="row-index">#</th>
                <th
                  v-for="(column, columnIndex) in result.columns"
                  :key="column"
                  :aria-sort="sortColumn === columnIndex ? (sortDirection === 'asc' ? 'ascending' : sortDirection === 'desc' ? 'descending' : 'none') : 'none'"
                >
                  <button class="result-sort" @click="toggleSort(columnIndex)">
                    <span>{{ column }}</span>
                    <span class="sort-indicator">
                      {{ sortColumn === columnIndex && sortDirection === "asc" ? "↑" : sortColumn === columnIndex && sortDirection === "desc" ? "↓" : "↕" }}
                    </span>
                  </button>
                </th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="{ row, originalIndex } in visibleRows" :key="originalIndex">
                <td class="row-index">{{ originalIndex + 1 }}</td>
                <td
                  v-for="(value, columnIndex) in row"
                  :key="columnIndex"
                  :class="{ 'null-cell': value === null }"
                >
                  {{ formatNumber(value) }}
                </td>
              </tr>
              <tr v-if="visibleRows.length === 0">
                <td :colspan="result.columns.length + 1" class="table-empty">
                  {{ searchQuery.trim() ? "没有匹配搜索条件的数据。" : "查询执行成功，没有返回记录。" }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-if="totalPages > 1" class="pagination">
          <button class="ghost-button small" :disabled="page <= 1" @click="page -= 1">
            上一页
          </button>
          <span>{{ page }} / {{ totalPages }}</span>
          <button class="ghost-button small" :disabled="page >= totalPages" @click="page += 1">
            下一页
          </button>
        </div>
      </template>
    </div>
  </Teleport>
</template>
