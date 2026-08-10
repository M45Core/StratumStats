(() => {
  "use strict";

  const refreshEveryMS = 10000;
  let refreshing = false;
  const sortStates = new Map();

  function placeRows(list, rows) {
    let previous = list.querySelector(".measurement-head");
    for (const row of rows) {
      const expected = previous ? previous.nextElementSibling : list.firstElementChild;
      if (row !== expected) list.insertBefore(row, expected);
      previous = row;
    }
  }

  function captureViewport() {
    const focusedRow = document.activeElement?.closest?.(".measurement-row");
    const visibleRow = [...document.querySelectorAll(".measurement-row")].find((row) => {
      const bounds = row.getBoundingClientRect();
      return bounds.bottom > 0 && bounds.top < window.innerHeight;
    });
    const fallback = [...document.querySelectorAll("[data-live-footnote], footer, .pool-section")].find((element) => {
      const bounds = element.getBoundingClientRect();
      return bounds.bottom > 0 && bounds.top < window.innerHeight;
    });
    const anchor = focusedRow?.isConnected ? focusedRow : (visibleRow || fallback);
    return {
      anchor,
      anchorTop: anchor?.getBoundingClientRect().top,
      scrollY: window.scrollY,
    };
  }

  function restoreViewport(viewport) {
    if (viewport.anchor?.isConnected) {
      const shift = viewport.anchor.getBoundingClientRect().top - viewport.anchorTop;
      if (Math.abs(shift) > 0.5) window.scrollBy(0, shift);
      return;
    }
    if (window.scrollY !== viewport.scrollY) window.scrollTo(window.scrollX, viewport.scrollY);
  }

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
    placeRows(list, rows);
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
      const button = list.querySelector("[aria-sort=ascending] .sort-button, [aria-sort=descending] .sort-button");
      const direction = button?.closest("[role=columnheader]")?.getAttribute("aria-sort");
      if (wrapper?.id && button) sortStates.set(wrapper.id, { key: button.dataset.sortKey, type: button.dataset.sortType, direction });
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
    button.setAttribute("aria-label", action + " details for " + poolName);
    button.title = action + " payment and recent performance details";
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

  function syncSectionVisibility(list, nextList, nextDocument) {
    const section = list.closest(".pool-section");
    const nextSection = nextList.closest(".pool-section");
    if (!section || !nextSection) return;
    section.hidden = nextSection.hidden;
    const link = document.querySelector(`.section-jump a[href="#${section.id}"]`);
    const nextLink = nextDocument.querySelector(`.section-jump a[href="#${nextSection.id}"]`);
    if (link && nextLink) link.hidden = nextLink.hidden;
    const jump = document.querySelector(".section-jump");
    const nextJump = nextDocument.querySelector(".section-jump");
    if (jump && nextJump) jump.hidden = nextJump.hidden;
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
    const currentEmpty = list.querySelector(".empty");
    if (nextRows.length > 0) currentEmpty?.remove();

    const orderedRows = [];
    const changedRows = [];
    for (const nextRow of nextRows) {
      const poolID = nextRow.dataset.poolId;
      let row = existing.get(poolID);
      const detailsOpen = row && row.querySelector(".details-toggle")?.getAttribute("aria-expanded") === "true";
      const changed = !row || rowChanged(row, nextRow);
      if (!row) row = nextRow.cloneNode(true);
      row.getAttributeNames().filter((name) => name.startsWith("data-sort-") && !nextRow.hasAttribute(name)).forEach((name) => row.removeAttribute(name));
      nextRow.getAttributeNames().filter((name) => name.startsWith("data-sort-")).forEach((name) => row.setAttribute(name, nextRow.getAttribute(name)));
      if (changed && row.isConnected) {
        row.innerHTML = nextRow.innerHTML;
        setDetailsState(row, detailsOpen);
      }
      orderedRows.push(row);
      if (changed) {
        updatedPools.push(row.querySelector(".measurement-pool strong")?.textContent.trim() || poolID);
        changedRows.push(row);
      }
    }

    placeRows(list, orderedRows);
    changedRows.forEach(flash);

    if (nextRows.length === 0) {
      const nextEmpty = nextList.querySelector(".empty");
      if (!currentEmpty && nextEmpty) {
        list.append(nextEmpty.cloneNode(true));
      } else if (currentEmpty && nextEmpty && currentEmpty.innerHTML !== nextEmpty.innerHTML) {
        currentEmpty.innerHTML = nextEmpty.innerHTML;
      }
    }

    const sortState = sortStates.get(id);
    if (sortState) applySort(wrapper, sortState.key, sortState.type, sortState.direction);
    syncSectionVisibility(list, nextList, nextDocument);
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
      const viewport = captureViewport();
      const root = document.documentElement;
      const previousOverflowAnchor = root.style.overflowAnchor;
      root.style.overflowAnchor = "none";

      try {
        syncList("free-pools-list", nextDocument, updatedPools);
        syncList("normal-pools-list", nextDocument, updatedPools);
        syncList("missing-wallet-pools-list", nextDocument, updatedPools);
        syncList("pending-wallet-pools-list", nextDocument, updatedPools);
        syncList("pplns-pools-list", nextDocument, updatedPools);
        syncList("other-pools-list", nextDocument, updatedPools);
        syncList("no-recent-data-pools-list", nextDocument, updatedPools);

        const summary = document.querySelector("[data-live-summary]");
        const nextSummary = nextDocument.querySelector("[data-live-summary]");
        if (summary && nextSummary && summary.innerHTML !== nextSummary.innerHTML) {
          summary.innerHTML = nextSummary.innerHTML;
        }
        const footnote = document.querySelector("[data-live-footnote]");
        const nextFootnote = nextDocument.querySelector("[data-live-footnote]");
        if (footnote && nextFootnote && footnote.innerHTML !== nextFootnote.innerHTML) {
          footnote.innerHTML = nextFootnote.innerHTML;
        }
      } finally {
        restoreViewport(viewport);
        root.style.overflowAnchor = previousOverflowAnchor;
      }

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
