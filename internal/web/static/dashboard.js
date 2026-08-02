(() => {
  "use strict";

  const refreshEveryMS = 10000;
  let refreshing = false;

  function rowChanged(current, next) {
    return current.innerHTML !== next.innerHTML;
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
      const changed = !row || rowChanged(row, nextRow);
      if (!row) row = nextRow.cloneNode(true);
      if (changed && row.isConnected) row.innerHTML = nextRow.innerHTML;
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
  }

  async function refresh() {
    if (refreshing || document.hidden) return;
    refreshing = true;
    try {
      const response = await fetch(window.location.pathname, {
        cache: "no-store",
        headers: { "X-StratumStats-Refresh": "1" },
      });
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      const nextDocument = new DOMParser().parseFromString(await response.text(), "text/html");
      const updatedPools = [];

      syncList("normal-pools-list", nextDocument, updatedPools);
      syncList("unsafe-pools-list", nextDocument, updatedPools);

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

  window.setInterval(refresh, refreshEveryMS);
  document.addEventListener("visibilitychange", () => {
    if (!document.hidden) refresh();
  });
})();
