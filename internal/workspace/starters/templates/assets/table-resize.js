(() => {
  const MIN_COLUMN_WIDTH = 52;
  const MAX_NATURAL_COLUMN_WIDTH = 320;
  const initializedTables = new WeakSet();
  const resizeStates = new WeakMap();
  const structureRefreshQueued = new WeakSet();
  const measurementCanvas = document.createElement("canvas");
  const measurementContext = measurementCanvas.getContext("2d");
  const numberFormatter = new Intl.NumberFormat("zh-CN", {
    minimumFractionDigits: 0,
    maximumFractionDigits: 2
  });

  function registerColumnVisibilityActions() {
    if (!window.jQuery || !window.jQuery.fn.dataTable) return false;

    const buttonTypes = window.jQuery.fn.dataTable.ext?.buttons;
    if (!buttonTypes) return false;

    const reopenColumnSelection = (trigger) => {
      window.setTimeout(() => {
        if (trigger?.getAttribute("aria-expanded") !== "true") {
          trigger?.click();
        }
      }, 50);
    };

    const setAllColumns = (visible) => (_event, table) => {
      table.columns().visible(visible, false);
      scheduleStructureRefresh(table.table().node());

      const trigger = table.table().container().querySelector(".buttons-colvis");
      reopenColumnSelection(trigger);
    };

    buttonTypes.selectAllColumns = {
      text: "全部选择",
      className: "buttons-colvis-all",
      action: setAllColumns(true)
    };

    buttonTypes.unselectAllColumns = {
      text: "全部取消",
      className: "buttons-colvis-none",
      action: setAllColumns(false)
    };

    return true;
  }

  function positionColumnSelection(menu, trigger) {
    const viewportGap = 8;
    const buttonGap = 4;
    const triggerRect = trigger.getBoundingClientRect();
    const menuRect = menu.getBoundingClientRect();

    let left = triggerRect.right - menuRect.width;
    left = Math.min(
      Math.max(viewportGap, left),
      window.innerWidth - menuRect.width - viewportGap
    );

    let top = triggerRect.bottom + buttonGap;
    if (top + menuRect.height > window.innerHeight - viewportGap) {
      const above = triggerRect.top - menuRect.height - buttonGap;
      top = above >= viewportGap
        ? above
        : Math.max(viewportGap, window.innerHeight - menuRect.height - viewportGap);
    }

    menu.style.setProperty("left", `${Math.round(left)}px`, "important");
    menu.style.setProperty("top", `${Math.round(top)}px`, "important");
    menu.style.setProperty("margin-left", "0", "important");
    menu.style.setProperty("margin-top", "0", "important");
  }

  if (!registerColumnVisibilityActions() && window.jQuery) {
    window.jQuery(document).one("preInit.dt.dtColumnResizeButtons", registerColumnVisibilityActions);
  }

  if (window.jQuery) {
    window.jQuery(document).on("buttons-popover.dt.dtColumnResizeMenu", (event) => {
      const menu = event.target;
      if (!(menu instanceof HTMLElement) || !menu.matches(".dt-button-collection")) return;

      const trigger = document.querySelector(".buttons-colvis[aria-expanded='true']");
      if (!trigger) return;

      window.requestAnimationFrame(() => positionColumnSelection(menu, trigger));
    });
  }

  function getDataTableApi(table) {
    if (window.jQuery) {
      try {
        return window.jQuery(table).DataTable();
      } catch (_error) {
        // The table may not have finished initializing.
      }
    }

    return table?._dt2 || null;
  }

  function columnGroup(table, columnCount) {
    let group = table.querySelector("colgroup");

    if (!group) {
      group = document.createElement("colgroup");
      table.insertBefore(group, table.firstChild);
    }

    while (group.children.length < columnCount) {
      group.appendChild(document.createElement("col"));
    }

    return group;
  }

  function ensureSummaryFooterStructure(table, columnCount) {
    if (!(table instanceof HTMLTableElement)) return null;
    if (!columnCount) return null;

    const footer = table.tFoot || document.createElement("tfoot");
    if (!table.tFoot) table.appendChild(footer);

    let row = footer.rows[0];
    if (!row) row = footer.insertRow();

    if (row.cells.length > columnCount) return row;

    while (row.cells.length < columnCount) {
      const cell = document.createElement("th");
      cell.scope = "col";
      row.appendChild(cell);
    }

    Array.from(row.cells).forEach((cell, index) => {
      cell.dataset.columnIndex = String(index);
    });

    footer.classList.add("dt-summary-footer");
    row.classList.add("dt-summary-row");
    return row;
  }

  function textForMeasurement(element) {
    const clone = element.cloneNode(true);
    clone
      .querySelectorAll(".dt-column-resizer, .dt-column-order, .dtcc")
      .forEach((node) => node.remove());

    return clone.textContent
      .split(/\r?\n/)
      .map((line) => line.trim())
      .filter(Boolean)
      .reduce((longest, line) => (line.length > longest.length ? line : longest), "");
  }

  function measureNaturalColumnWidth(table, index, header) {
    if (!measurementContext) return MIN_COLUMN_WIDTH;

    const headerStyle = window.getComputedStyle(header);
    const horizontalPadding =
      (Number.parseFloat(headerStyle.paddingLeft) || 0) +
      (Number.parseFloat(headerStyle.paddingRight) || 0);
    let longest = 0;

    const measure = (element) => {
      const text = textForMeasurement(element);
      if (!text) return;

      const style = window.getComputedStyle(element);
      measurementContext.font = [
        style.fontStyle,
        style.fontWeight,
        style.fontSize,
        style.fontFamily
      ].join(" ");
      longest = Math.max(longest, measurementContext.measureText(text).width);
    };

    measure(header);

    Array.from(table.tBodies).forEach((body) => {
      Array.from(body.rows).forEach((row) => {
        if (row.classList.contains("child")) return;
        const cell = row.cells[index];
        if (cell) measure(cell);
      });
    });

    const controlAllowance = header.querySelector(".dt-column-order") ? 18 : 0;
    return Math.max(
      MIN_COLUMN_WIDTH,
      Math.min(
        MAX_NATURAL_COLUMN_WIDTH,
        Math.ceil(longest + horizontalPadding + controlAllowance + 8)
      )
    );
  }

  function isColumnVisible(table, index, header) {
    const rendered =
      header.isConnected &&
      !header.hidden &&
      window.getComputedStyle(header).display !== "none" &&
      header.getClientRects().length > 0;

    // The rendered header is authoritative after a column toggle. Some
    // DataTables button actions expose stale visibility via the API until
    // their internal draw has completed.
    if (header.isConnected) return rendered;

    try {
      const api = getDataTableApi(table);
      if (api) return api.column(index).visible();
    } catch (_error) {
      // Fall back to the rendered header state below.
    }

    return false;
  }

  function visibleColumnIndexes(table, state) {
    return state.headers
      .map((header, index) => (isColumnVisible(table, index, header) ? index : -1))
      .filter((index) => index >= 0);
  }

  function applyColumnLayout(table, state) {
    const visibleIndexes = visibleColumnIndexes(table, state);

    state.headers.forEach((_header, index) => {
      const visible = visibleIndexes.includes(index);
      const column = state.group.children[index];
      if (!column) return;

      column.style.setProperty("display", visible ? "" : "none", "important");
      column.style.setProperty("width", `${state.widths[index]}px`, "important");
    });

    const totalWidth = visibleIndexes.reduce(
      (sum, index) => sum + state.widths[index],
      0
    );

    if (totalWidth > 0) {
      table.style.setProperty("width", `${totalWidth}px`, "important");
      table.style.setProperty("max-width", "none", "important");
    }
  }

  function ensureSummaryFooter(table, state) {
    state.footer =
      table.tFoot?.querySelector("tr") ||
      ensureSummaryFooterStructure(table, state.headers.length);
    return state.footer;
  }

  function isNumericColumn(api, index, state) {
    if (state.numericColumns.has(index)) return state.numericColumns.get(index);

    let hasValue = false;
    let numeric = true;
    api.column(index).data().each((value) => {
      if (value === null || value === undefined || value === "") return;
      hasValue = true;
      if (typeof value !== "number" || !Number.isFinite(value)) numeric = false;
    });

    const result = hasValue && numeric;
    state.numericColumns.set(index, result);
    return result;
  }

  function formatNumber(value, digits) {
    if (Number.isFinite(value)) {
      return value.toLocaleString("zh-CN", {
        minimumFractionDigits: digits,
        maximumFractionDigits: digits
      });
    }

    return numberFormatter.format(value);
  }

  function summarizeColumn(api, index, isAverage) {
    let sum = 0;
    let count = 0;
    let hasFraction = false;

    api.column(index, { search: "applied" }).data().each((value) => {
      const numericValue = typeof value === "number" ? value : Number(value);
      if (!Number.isFinite(numericValue)) return;

      sum += numericValue;
      count += 1;
      if (!Number.isInteger(numericValue)) hasFraction = true;
    });

    if (!count) return null;

    return {
      kind: isAverage ? "均值" : "合计",
      value: isAverage ? sum / count : sum,
      digits: isAverage || hasFraction ? 2 : 0
    };
  }

  function updateSummary(table, state) {
    const api = getDataTableApi(table);
    if (!api) return;

    const row = ensureSummaryFooter(table, state);
    const visibleIndexes = visibleColumnIndexes(table, state);
    const rowCount = api.rows({ search: "applied" }).count();
    const cellsByColumnIndex = new Map(
      Array.from(row.cells).map((cell) => [cell.dataset.columnIndex, cell])
    );
    let labelAdded = false;

    state.headers.forEach((header, index) => {
      const indexedCell = cellsByColumnIndex.get(String(index));
      let apiCell = null;
      try {
        apiCell = api.column(index).footer();
      } catch (_error) {
        // Older DataTables builds may not expose footer cells via the API.
      }
      const cell =
        apiCell instanceof HTMLTableCellElement &&
        apiCell.dataset.columnIndex === String(index)
          ? apiCell
          : indexedCell;
      if (!cell) return;

      const isVisible = visibleIndexes.includes(index);
      cell.style.setProperty("display", isVisible ? "" : "none", "important");
      cell.dataset.columnIndex = String(index);
      cell.dataset.columnTitle = header.textContent.replace(/\s+/g, " ").trim();
      cell.classList.remove("dt-summary-numeric");
      cell.replaceChildren();

      if (!isVisible) return;

      if (!labelAdded) {
        const label = document.createElement("span");
        label.className = "dt-summary-label";
        label.textContent = `汇总 · ${formatNumber(rowCount, 0)} 条`;
        cell.appendChild(label);
        labelAdded = true;
      }

      if (!isNumericColumn(api, index, state)) return;

      const headerText = header.textContent.replace(/\s+/g, " ").trim();
      const isAverage = /(?:率|比例|占比|百分比|平均|均值)$/.test(headerText);
      const summary = summarizeColumn(api, index, isAverage);
      if (!summary) return;

      cell.classList.add("dt-summary-numeric");
      cell.setAttribute(
        "aria-label",
        `${headerText}${summary.kind} ${formatNumber(summary.value, summary.digits)}`
      );

      const valueLine = document.createElement("span");
      valueLine.className = "dt-summary-value";

      const kind = document.createElement("span");
      kind.className = "dt-summary-kind";
      kind.textContent = summary.kind;

      const value = document.createElement("span");
      value.textContent = formatNumber(summary.value, summary.digits);

      valueLine.append(kind, value);
      cell.appendChild(valueLine);
    });
  }

  function initializeTable(table) {
    if (!(table instanceof HTMLTableElement)) return null;
    if (!table.tHead || !table.tHead.rows.length) return null;

    const existingState = resizeStates.get(table);
    if (existingState) return existingState;
    if (initializedTables.has(table)) return null;

    const headerRow = table.tHead.rows[0];
    const headers = Array.from(headerRow.cells);
    if (!headers.length) return null;

    initializedTables.add(table);
    table.classList.add("dt-columns-resizable");

    const state = {
      headers,
      group: columnGroup(table, headers.length),
      widths: headers.map((header, index) =>
        measureNaturalColumnWidth(table, index, header)
      ),
      numericColumns: new Map(),
      footer: null
    };
    resizeStates.set(table, state);

    headers.forEach((header, index) => {
      if (header.querySelector(":scope > .dt-column-resizer")) return;

      header.classList.add("dt-column-resizable");
      const handle = document.createElement("span");
      handle.className = "dt-column-resizer";
      handle.setAttribute("role", "separator");
      handle.setAttribute("aria-orientation", "vertical");
      handle.setAttribute("aria-label", `${header.innerText.trim()}：拖动调整列宽`);
      handle.title = "拖动调整列宽";
      header.appendChild(handle);

      handle.addEventListener("pointerdown", (event) => {
        if (event.button !== 0) return;

        event.preventDefault();
        event.stopPropagation();

        const startX = event.clientX;
        const startWidth = state.widths[index];
        document.body.classList.add("dt-column-resizing");

        const move = (moveEvent) => {
          state.widths[index] = Math.max(
            MIN_COLUMN_WIDTH,
            Math.min(
              1600,
              Math.round(startWidth + moveEvent.clientX - startX)
            )
          );
          applyColumnLayout(table, state);
        };

        const finish = () => {
          document.body.classList.remove("dt-column-resizing");
          window.removeEventListener("pointermove", move);
          window.removeEventListener("pointerup", finish);
          window.removeEventListener("pointercancel", finish);
        };

        window.addEventListener("pointermove", move);
        window.addEventListener("pointerup", finish, { once: true });
        window.addEventListener("pointercancel", finish, { once: true });
      });

      handle.addEventListener("click", (event) => {
        event.preventDefault();
        event.stopPropagation();
      });
    });

    applyColumnLayout(table, state);
    return state;
  }

  function refreshTable(table) {
    const state = initializeTable(table);
    if (!state) return;

    applyColumnLayout(table, state);
    updateSummary(table, state);
  }

  function scheduleRefresh(table) {
    window.requestAnimationFrame(() => {
      window.requestAnimationFrame(() => refreshTable(table));
    });
  }

  function scheduleStructureRefresh(table) {
    if (!(table instanceof HTMLTableElement) || structureRefreshQueued.has(table)) return;
    structureRefreshQueued.add(table);

    window.requestAnimationFrame(() => {
      structureRefreshQueued.delete(table);

      const api = getDataTableApi(table);
      if (!api) return;

      try {
        api.columns.adjust();
      } catch (_error) {
        // The draw below still refreshes the rendered column structure.
      }

      try {
        api.responsive?.recalc?.();
      } catch (_error) {
        // The Responsive extension is optional.
      }

      try {
        api.draw(false);
      } catch (_error) {
        // The normal draw listener remains as a fallback.
      }

      refreshTable(table);
    });
  }

  function initializeAll() {
    document.querySelectorAll("table.dataTable").forEach(scheduleRefresh);
  }

  if (window.jQuery) {
    window.jQuery(document).on(
      "click.dtColumnResizeSelection",
      ".buttons-columnVisibility, .buttons-colvisRestore",
      (event) => {
        const container =
          event.currentTarget.closest(".dt-container") ||
          document.querySelector(".dt-container");
        const table = container?.querySelector("table.dataTable");
        if (table) scheduleStructureRefresh(table);
      }
    );
    window.jQuery(document).on("init.dt.dtColumnResize", (_event, settings) => {
      scheduleRefresh(settings.nTable);
    });
    window.jQuery(document).on(
      "column-visibility.dt.dtColumnResize column-reorder.dt.dtColumnResize columns-reordered.dt.dtColumnResize",
      (_event, settings) => {
        scheduleStructureRefresh(settings.nTable);
      }
    );
    window.jQuery(document).on(
      "draw.dt.dtColumnResize responsive-resize.dt.dtColumnResize",
      (_event, settings) => {
        scheduleRefresh(settings.nTable);
      }
    );
  }

  if (window.HTMLWidgets?.addPostRenderHandler) {
    window.HTMLWidgets.addPostRenderHandler(initializeAll);
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", initializeAll, { once: true });
  } else {
    initializeAll();
  }

  window.addEventListener("load", initializeAll, { once: true });
})();
