(() => {
  "use strict";

  const refreshEveryMS = 10000;
  let refreshing = false;
  const sortStates = new Map();

  function applySort(wrapper, key, type, direction) {
    const list = wrapper.querySelector(".measurement-list");
    if (!list) return;
    const rows = [...list.querySelectorAll(".measurement-row[data-pool-id]")];
    rows.sort((left, right) => {
      const leftRaw = left.getAttribute(`data-sort-${key}`) || "";
      const rightRaw = right.getAttribute(`data-sort-${key}`) || "";
      const leftMissing = leftRaw === "";
      const rightMissing = rightRaw === "";
      if (leftMissing !== rightMissing) return leftMissing ? 1 : -1;
      let comparison;
      if (type === "number") {
        comparison = Number(leftRaw) - Number(rightRaw);
      } else {
        comparison = leftRaw.localeCompare(rightRaw, undefined, { sensitivity: "base" });
      }
      if (comparison === 0) {
        comparison = (left.dataset.sortPool || "").localeCompare(right.dataset.sortPool || "", undefined, { sensitivity: "base" });
      }
      return direction === "ascending" ? comparison : -comparison;
    });
    rows.forEach((row) => list.append(row));
    sortStates.set(wrapper.id, { key, type, direction });
    list.querySelectorAll(".sort-button").forEach((button) => {
      const active = button.dataset.sortKey === key;
      const cell = button.closest("[role=columnheader]");
      if (cell) cell.setAttribute("aria-sort", active ? direction : "none");
      const indicator = button.querySelector("b");
      if (indicator) indicator.textContent = active ? (direction === "ascending" ? "↑" : "↓") : "↕";
    });
  }

  function bindSorting() {
    document.querySelectorAll(".measurement-list").forEach((list) => {
      const wrapper = list.parentElement;
      const button = list.querySelector("[aria-sort=ascending] .sort-button");
      if (wrapper?.id && button) sortStates.set(wrapper.id, { key: button.dataset.sortKey, type: button.dataset.sortType, direction: "ascending" });
    });
    document.querySelectorAll(".sort-button").forEach((button) => {
      button.addEventListener("click", () => {
        const wrapper = button.closest(".measurement-list")?.parentElement;
        if (!wrapper?.id) return;
        const current = sortStates.get(wrapper.id);
        const direction = current?.key === button.dataset.sortKey && current.direction === "ascending" ? "descending" : "ascending";
        applySort(wrapper, button.dataset.sortKey, button.dataset.sortType, direction);
      });
    });
  }

  function setDetailsState(row, expanded) {
    const button = row?.querySelector(".details-toggle");
    const panel = row?.querySelector(".measurement-details");
    if (!button || !panel) return;
    button.setAttribute("aria-expanded", String(expanded));
    const action = expanded ? "Hide" : "Show";
    const poolName = row.dataset.sortPool || "pool";
    button.setAttribute("aria-label", action + " payout and history for " + poolName);
    button.title = action + " payout details and recent history";
    panel.hidden = !expanded;
  }

  function bindDetails() {
    document.addEventListener("click", (event) => {
      const button = event.target.closest(".details-toggle");
      if (!button) return;
      const row = button.closest(".measurement-row");
      const expanded = button.getAttribute("aria-expanded") !== "true";
      setDetailsState(row, expanded);
    });
  }

  function comparableRowHTML(row) {
    const clone = row.cloneNode(true);
    setDetailsState(clone, false);
    return clone.innerHTML;
  }

  function rowChanged(current, next) {
    return comparableRowHTML(current) !== comparableRowHTML(next);
  }

  function flash(row) {
    row.classList.remove("row-updated");
    void row.offsetWidth;
    row.classList.add("row-updated");
    window.setTimeout(() => row.classList.remove("row-updated"), 1800);
  }

  function syncList(id, nextDocument, updatedPools) {
    const wrapper = document.getElementById(id);
    const nextWrapper = nextDocument.getElementById(id);
    if (!wrapper || !nextWrapper) return;

    const list = wrapper.querySelector(".measurement-list");
    const nextList = nextWrapper.querySelector(".measurement-list");
    if (!list || !nextList) return;

    const existing = new Map();
    document.querySelectorAll(".measurement-row[data-pool-id]").forEach((row) => {
      existing.set(row.dataset.poolId, row);
    });

    const nextRows = [...nextList.querySelectorAll(".measurement-row[data-pool-id]")];
    const wanted = new Set(nextRows.map((row) => row.dataset.poolId));
    list.querySelectorAll(".measurement-row[data-pool-id]").forEach((row) => {
      if (!wanted.has(row.dataset.poolId)) row.remove();
    });
    list.querySelector(".empty")?.remove();

    for (const nextRow of nextRows) {
      const poolID = nextRow.dataset.poolId;
      let row = existing.get(poolID);
      const detailsOpen = row && row.querySelector(".details-toggle")?.getAttribute("aria-expanded") === "true";
      const changed = !row || rowChanged(row, nextRow);
      if (!row) row = nextRow.cloneNode(true);
      nextRow.getAttributeNames().filter((name) => name.startsWith("data-sort-")).forEach((name) => row.setAttribute(name, nextRow.getAttribute(name)));
      if (changed && row.isConnected) {
        row.innerHTML = nextRow.innerHTML;
        setDetailsState(row, detailsOpen);
      }
      list.append(row);
      if (changed) {
        updatedPools.push(row.querySelector(".measurement-pool strong")?.textContent.trim() || poolID);
        flash(row);
      }
    }

    if (nextRows.length === 0) {
      const empty = nextList.querySelector(".empty");
      if (empty) list.append(empty.cloneNode(true));
    }

    const sortState = sortStates.get(id);
    if (sortState) applySort(wrapper, sortState.key, sortState.type, sortState.direction);
  }

  async function refresh() {
    if (refreshing || document.hidden) return;
    refreshing = true;
    try {
      const response = await fetch(window.location.pathname + window.location.search, {
        cache: "no-store",
        headers: { "X-StratumStats-Refresh": "1" },
      });
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      const nextDocument = new DOMParser().parseFromString(await response.text(), "text/html");
      const updatedPools = [];

      syncList("free-pools-list", nextDocument, updatedPools);
      syncList("normal-pools-list", nextDocument, updatedPools);
      syncList("unsafe-pools-list", nextDocument, updatedPools);
      syncList("pplns-pools-list", nextDocument, updatedPools);
      syncList("other-pools-list", nextDocument, updatedPools);

      const summary = document.querySelector("[data-live-summary]");
      const nextSummary = nextDocument.querySelector("[data-live-summary]");
      if (summary && nextSummary && summary.innerHTML !== nextSummary.innerHTML) {
        summary.innerHTML = nextSummary.innerHTML;
      }
      const footnote = document.querySelector("[data-live-footnote]");
      const nextFootnote = nextDocument.querySelector("[data-live-footnote]");
      if (footnote && nextFootnote) footnote.innerHTML = nextFootnote.innerHTML;

      if (updatedPools.length > 0) {
        const status = document.querySelector("[data-live-status]");
        if (status) status.textContent = `${updatedPools.join(", ")} updated`;
      }
    } catch (error) {
      console.warn("StratumStats live update failed", error);
    } finally {
      refreshing = false;
    }
  }

  bindSorting();
  bindDetails();
  window.setInterval(refresh, refreshEveryMS);
  document.addEventListener("visibilitychange", () => {
    if (!document.hidden) refresh();
  });
})();
