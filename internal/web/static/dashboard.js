(() => {
  "use strict";

  const refreshEveryMS = 10000;
  const vantageStorageKey = "stratumstats.selectedVantage";
  const transportStorageKey = "stratumstats.selectedTransport";
  const groups = [
    ["free_pools", "free-pools-list", "free-solo-pools", "Free solo", "Overall score: highest first"],
    ["normal_pools", "normal-pools-list", "non-free-solo-pools", "Paid solo", "Overall score: highest first"],
    ["pplns_pools", "pplns-pools-list", "pplns-share-pools", "PPLNS shared", "Overall score: highest first"],
    ["other_pools", "other-pools-list", "other-non-solo-pools", "Other shared", "Overall score: highest first"],
    ["no_recent_data_pools", "no-recent-data-pools-list", "no-recent-data-pools", "No recent data", "Alphabetical"],
    ["missing_wallet_pools", "missing-wallet-pools-list", "missing-worker-wallet-pools", "Worker wallet missing", "Overall score: highest first"],
    ["pending_wallet_pools", "pending-wallet-pools-list", "pending-worker-wallet-pools", "Verification pending", "Overall score: highest first"],
  ];
  const sortStates = new Map();
  let refreshing = false;
  let currentETag = "";
  let renderedVantage = "";
  let renderedBlockHeight = null;

  const escapeHTML = (value) => String(value ?? "").replace(/[&<>"']/g, (character) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[character]);
  const metric = (value) => value == null ? "" : Number(value);
  const fixed = (value, places = 0) => value == null ? "" : Number(value).toFixed(places);
  const feePct = (value) => value == null ? "" : `${Number(value).toFixed(2).replace(/\.?0+$/, "")}%`;
  const miningLoss = (value) => Number(value) < 0.1 ? "&lt;0.1%" : `${Number(value).toFixed(2)}%`;
  const btc = (sats) => `${Math.floor(Number(sats) / 100000000)}.${String(Number(sats) % 100000000).padStart(8, "0")} BTC`;
  const iso = (value) => value ? new Date(value).toISOString() : "";
  const absoluteTime = (value) => new Date(value).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short", timeZone: "UTC" });

  function relativeAge(timestamp, now = Date.now()) {
    const elapsedSeconds = Math.max(0, Math.floor((now - timestamp) / 1000));
    if (elapsedSeconds < 60) return "just now";
    const units = [["day", 86400], ["hour", 3600], ["min", 60]];
    let remaining = elapsedSeconds;
    const parts = [];
    for (const [name, seconds] of units) {
      const value = Math.floor(remaining / seconds);
      if (!value) continue;
      parts.push(`${value} ${name}${value === 1 || name === "min" ? "" : "s"}`);
      remaining %= seconds;
      if (parts.length === 2) break;
    }
    return `${parts.join(" ")} ago`;
  }

  function timeHTML(value, fullDate = false) {
    if (!value) return "";
    const timestamp = iso(value);
    return `<time datetime="${timestamp}" data-relative-time>${fullDate ? absoluteTime(value) : relativeAge(Date.parse(value))}</time>`;
  }

  function updateRelativeTimes() {
    const now = Date.now();
    document.querySelectorAll("time[data-relative-time]").forEach((element) => {
      const timestamp = Date.parse(element.dateTime);
      if (!Number.isFinite(timestamp)) return;
      element.textContent = relativeAge(timestamp, now);
      element.title = new Date(timestamp).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "long" });
    });
  }

  function failures(stats) {
    return (stats?.timeouts || 0) + (stats?.errors || 0) + (stats?.rejected || 0);
  }

  function failureSummary(stats) {
    const parts = [];
    if (stats.timeouts) parts.push(`${stats.timeouts} timed out`);
    if (stats.errors) parts.push(`${stats.errors} error${stats.errors === 1 ? "" : "s"}`);
    if (stats.rejected) parts.push(`${stats.rejected} refused`);
    const total = failures(stats);
    return `${total} failed check${total === 1 ? "" : "s"} (${parts.join(", ")})`;
  }

  function timingHTML(stats = {}) {
    if (!stats.attempts) return "<strong>—</strong><small>unchecked</small>";
    const failed = failures(stats);
    const failure = failed ? `<span class="timing-failures" role="img" aria-label="${escapeHTML(failureSummary(stats))}" title="${escapeHTML(failureSummary(stats))}">${failed} ❌</span>` : "";
    if (stats.certificate_errors) return `<strong class="timing-cert-error" title="The pool security certificate could not be verified">SECURITY ERROR</strong><small>${failure}</small>`;
    if (stats.median_ms != null) return `<strong>${fixed(stats.median_ms)} ms</strong><small>P95 ${fixed(stats.p95_ms)}${failed ? ` · ${failure}` : ""}${stats.unsupported ? ` · unsupported ${stats.unsupported}` : ""}</small>`;
    return `<strong>—</strong><small>${failure}${failed && stats.unsupported ? " · " : ""}${stats.unsupported ? `unsupported ${stats.unsupported}` : ""}</small>`;
  }

  function scoreHTML(pool) {
    if (pool.overall_score == null) return '<span class="score-circle score-unavailable" aria-label="Overall performance score unavailable" title="Score needs recent median and P95 block-delay data">—</span>';
    const score = Number(pool.overall_score);
    const grade = Math.floor(Math.max(0, Math.min(100, score)) + 5) / 10 | 0;
    let factors = "";
    if (pool.score_override_reason) {
      factors = '<strong>Score override</strong><span class="score-penalty"><b>Worker wallet not found</b><em>Score forced to 0</em></span>';
    } else {
      const penalties = [];
      if (pool.recent_fee_increase_penalty) penalties.push(`<span class="score-penalty"><b>Recent fee increase</b><em>${feePct(pool.previous_pool_fee_pct)} → ${feePct(pool.latest_pool_fee_pct)} · −${fixed(pool.recent_fee_increase_penalty, 1)} pts</em></span>`);
      if (pool.high_fee_penalty) penalties.push(`<span class="score-penalty"><b>Fee above 2.5%</b><em>${feePct(pool.latest_pool_fee_pct)} · −${fixed(pool.high_fee_penalty, 1)} pts</em></span>`);
      if (pool.tls_certificate_penalty) penalties.push(`<span class="score-penalty"><b>Invalid TLS certificate</b><em>−${fixed(pool.tls_certificate_penalty, 1)} pts</em></span>`);
      factors = `<strong>Top score factors</strong>${penalties.join("")}<span><b>Availability</b><em>${fixed(pool.availability_pct, 1)}% · 40% weight</em></span><span><b>Mining loss</b><em>${miningLoss(pool.estimated_mining_loss_pct)} · 25% weight</em></span>${penalties.length ? "" : `<span><b>P95 delay</b><em>${fixed(pool.p95_ms)} ms · 20% weight</em></span>`}`;
    }
    return `<span class="score-popover" tabindex="0" aria-label="Overall performance score ${fixed(score)} out of 100" aria-describedby="score-factors-${escapeHTML(pool.row_id)}"><span class="score-circle score-grade-${grade}">${fixed(score)}</span><span id="score-factors-${escapeHTML(pool.row_id)}" class="score-tooltip" role="tooltip">${factors}</span></span>`;
  }

  function payoutHTML(pool) {
    const destinations = pool.latest_payout_destinations || [];
    if (!destinations.length) {
      if (pool.is_solo && pool.latest_coinbase_total_sats) return '<p class="details-empty">The test miner address is private. No other payment was recorded.</p>';
      return `<p class="details-empty">${pool.is_solo ? "Payment" : "Block payment"} details will appear after a new block is checked.${pool.is_solo ? "" : " These are not later payments to miners."}</p>`;
    }
    const rows = destinations.map((destination) => {
      const destinationCode = destination.address
        ? `<a class="wallet-explorer-link" href="https://mempool.space/address/${encodeURIComponent(destination.address)}" target="_blank" rel="noopener noreferrer" aria-label="View ${escapeHTML(destination.address)} on mempool.space" title="View this public Bitcoin address on mempool.space"><code title="${escapeHTML(destination.script_type)}">${escapeHTML(destination.address)}</code><span aria-hidden="true">↗</span></a>`
        : `<code title="Raw coinbase output script">${escapeHTML(destination.script_type)} · ${escapeHTML(destination.script_pubkey.length <= 36 ? destination.script_pubkey : `${destination.script_pubkey.slice(0, 20)}…${destination.script_pubkey.slice(-12)}`)}${destination.script_pubkey_truncated ? "…" : ""}</code>`;
      const percentage = Number(destination.percentage);
      const percentageText = percentage > 0 && percentage < 0.0001 ? "&lt;0.0001%" : `${percentage.toFixed(percentage < 0.01 ? 4 : 2)}%`;
      return `<li class="non-worker-destination"><div class="payout-destination-head"><span><a class="jargon-link" href="/methodology#non-worker-destination" title="A coinbase output not sent to the probe worker; it does not prove ownership">Non-worker destination</a></span><strong>${percentageText}</strong></div>${destinationCode}<div class="payout-amount"><span>${btc(destination.value_sats)}</span><progress max="100" value="${percentage.toFixed(4)}" title="${percentageText} of the block payment">${percentageText}</progress></div></li>`;
    }).join("");
    const truncated = pool.latest_payout_destinations_truncated ? `<p class="retention-note">Showing ${destinations.length} retained destinations; ${btc(pool.latest_payout_omitted_sats)} across smaller destinations is combined.</p>` : "";
    return `<ol class="payout-destinations">${rows}</ol>${truncated}<p class="evidence-note">The test miner address is kept private. Other payments may include pool fees, donations, or payment splits; they do not prove who owns an address.${pool.is_solo ? "" : " These are payments from the block itself, not later payments to miners."}</p>`;
  }

  function latencyHistoryHTML(pool) {
    const history = pool.template_latency_history || [];
    if (!history.length) return '<p class="details-empty">No template deliveries recorded in the last 24 hours.</p>';
    const chart = pool.latency_chart;
    const points = (chart.points || []).map((point) => `<g class="latency-chart-point" tabindex="0" aria-label="${escapeHTML(absoluteTime(point.observed_at))}, ${fixed(point.value)} ms"><title>${escapeHTML(absoluteTime(point.observed_at))} — ${fixed(point.value)} ms</title><circle cx="${fixed(point.x, 1)}" cy="${fixed(point.y, 1)}" r="6"></circle><text x="${fixed(point.label_x, 1)}" y="${fixed(point.label_y, 1)}" text-anchor="${escapeHTML(point.text_anchor)}">${fixed(point.value)} ms</text></g>`).join("");
    const hidden = history.map((point) => `<li>${escapeHTML(absoluteTime(point.observed_at))}: ${fixed(point.value)} ms</li>`).join("");
    return `<div class="latency-chart-shell"><svg class="latency-line-chart" viewBox="0 0 640 204" role="img" aria-labelledby="latency-chart-title-${escapeHTML(pool.row_id)} latency-chart-desc-${escapeHTML(pool.row_id)}"><title id="latency-chart-title-${escapeHTML(pool.row_id)}">Recent block-template latency for ${escapeHTML(pool.pool_name)} endpoint ${escapeHTML(pool.endpoint)}</title><desc id="latency-chart-desc-${escapeHTML(pool.row_id)}">Line graph of ${chart.points.length} endpoint block-template latency samples from the last 24 hours. ${pool.combined_vantage ? "Each point is the median across US regions for one Bitcoin block. " : ""}Lower is better.</desc><g class="latency-chart-grid" aria-hidden="true"><line x1="56" y1="18" x2="624" y2="18"></line><line x1="56" y1="58" x2="624" y2="58"></line><line x1="56" y1="98" x2="624" y2="98"></line><line x1="56" y1="138" x2="624" y2="138"></line><line x1="56" y1="178" x2="624" y2="178"></line><text x="49" y="24" text-anchor="end">${fixed(chart.max_value)} ms</text><text x="49" y="104" text-anchor="end">${fixed(chart.mid_value)} ms</text><text x="49" y="184" text-anchor="end">0 ms</text></g><path class="latency-chart-area" d="${escapeHTML(chart.area_path)}" aria-hidden="true"></path><polyline class="latency-chart-line" points="${escapeHTML(chart.polyline)}" aria-hidden="true"></polyline>${points}</svg><div class="latency-chart-times">${timeHTML(chart.start.observed_at)}<span>time</span>${timeHTML(chart.end.observed_at)}</div></div><ol class="visually-hidden latency-chart-data">${hidden}</ol><p>${pool.combined_vantage ? "Median regional delay for each Bitcoin block;" : "Relative delay until this endpoint's first clean block transition arrives;"} lower is better.</p>`;
  }

  function feeHistoryHTML(pool) {
    if (!pool.is_solo) return "";
    const changes = pool.fee_change_history || [];
    const content = changes.length ? `<ol class="fee-change-list">${changes.map((point) => `<li>${timeHTML(point.observed_at)}<span class="fee-change-values"><span>${feePct(point.previous)}</span><span class="fee-change-arrow" aria-hidden="true">→</span><strong>${feePct(point.value)}</strong></span></li>`).join("")}</ol><p>Only fee changes are shown.</p>` : `<p class="details-empty">${pool.pool_fee_history?.length ? "No recent fee changes." : "No fee history yet."}</p>`;
    return `<div class="history-block"><div class="history-heading"><div><p>Recent changes</p><h3>Observed effective fee</h3></div><span>${changes.length} shown</span></div>${content}</div>`;
  }

  function rowHTML(pool) {
    const rowID = escapeHTML(pool.row_id);
    const tls = pool.endpoint_tls;
    const connection = tls ? pool.tls_handshake_timing : pool.connect_timing;
    const tlsClass = tls ? `connection-tls ${!pool.tls_handshake_timing?.attempts ? "tls-unavailable" : failures(pool.tls_handshake_timing) ? "tls-timing-error" : ""}` : "connection-tcp";
    const poolName = pool.website ? `<a href="${escapeHTML(pool.website)}" target="_blank" rel="noopener noreferrer" aria-label="Visit the website for ${escapeHTML(pool.pool_name)}">${escapeHTML(pool.pool_name)}</a>` : escapeHTML(pool.pool_name);
    const security = pool.tls_handshake_timing?.certificate_errors ? '<small><span class="tls-error-label" title="The pool security certificate could not be verified">Security error</span></small>' : pool.tls_handshake_timing?.errors ? '<small><span class="tls-error-label" title="A secure connection failed">Secure connection failed</span></small>' : "";
    const fee = pool.is_solo && pool.latest_pool_fee_pct != null ? `<strong>${feePct(pool.latest_pool_fee_pct)}</strong>${pool.pool_fee_changed ? `<small class="fee-changed">changed ${feePct(pool.previous_pool_fee_pct)} → ${feePct(pool.latest_pool_fee_pct)}</small>` : ""}` : `<strong>—</strong><small>${pool.is_solo && pool.unsafe_reason ? "not available" : "not measured"}</small>`;
    return `<article class="measurement-row" data-pool-id="${rowID}" data-pool-base-id="${escapeHTML(pool.pool_id)}" data-update-label="${escapeHTML(`${pool.pool_name} ${pool.endpoint}`)}" data-sort-score="${fixed(pool.overall_score, 6)}" data-sort-pool="${escapeHTML(pool.sort_name)}" data-sort-median="${fixed(pool.median_ms, 6)}" data-sort-p95="${fixed(pool.p95_ms, 6)}" data-sort-loss="${fixed(pool.estimated_mining_loss_pct, 6)}" data-sort-availability="${fixed(pool.availability_pct, 6)}" data-sort-connection="${fixed(connection?.median_ms, 6)}" data-sort-setup="${fixed(pool.subscribe_timing?.median_ms, 6)}" data-sort-fee="${fixed(pool.fee_sort_value, 6)}"><div class="score-compact">${scoreHTML(pool)}</div><div class="measurement-pool"><strong>${poolName}</strong><small class="endpoint-address">${escapeHTML(pool.endpoint)}${pool.endpoint_region ? ` · ${escapeHTML(pool.endpoint_region)}` : ""}</small>${security}${pool.unsafe_reason ? `<small class="worker-wallet-status worker-wallet-${escapeHTML(pool.wallet_evidence)}">${escapeHTML(pool.unsafe_reason)}</small>` : ""}<button type="button" class="details-toggle" aria-expanded="false" aria-controls="payout-history-${rowID}" aria-label="Show details for ${escapeHTML(pool.pool_name)} endpoint ${escapeHTML(pool.endpoint)}" title="Show payment and recent performance details"><span>More details</span><span class="details-plus" aria-hidden="true">+</span></button></div><div class="template-median">${pool.median_ms != null ? `<strong>${fixed(pool.median_ms)} ms</strong><div class="median-bar-track" aria-hidden="true"><span class="median-bar ${escapeHTML(pool.latency_class)}"></span></div>` : '<strong>—</strong><div class="median-bar-track" aria-hidden="true"></div>'}</div><div class="p95-value"><strong>${pool.p95_ms == null ? "—" : `${fixed(pool.p95_ms)} ms`}</strong></div><div class="mining-loss-compact">${pool.estimated_mining_loss_pct != null ? `<strong>${miningLoss(pool.estimated_mining_loss_pct)}</strong><div class="mining-loss-bar-track" aria-hidden="true"><span class="mining-loss-bar ${escapeHTML(pool.mining_loss_class)}"></span></div>` : '<strong>—</strong><small>not measured</small><div class="mining-loss-bar-track" aria-hidden="true"></div>'}</div><div class="availability-compact"><strong>${fixed(pool.availability_pct, 1)}%</strong></div><div class="stacked-stat connection-timing ${tlsClass}"><span>${tls && !pool.tls_handshake_timing?.attempts ? "<strong>—</strong>" : timingHTML(connection)}</span></div><div class="stacked-stat"><span><a class="jargon-link" href="/methodology#subscribe" title="Starts a Stratum mining session">Sub</a> ${timingHTML(pool.subscribe_timing)}</span><span><a class="jargon-link" href="/methodology#authorize" title="Pool accepts the worker identity">Auth</a> ${timingHTML(pool.authorize_timing)}</span></div><div class="fee-compact">${fee}</div><div id="payout-history-${rowID}" class="measurement-details" hidden><div class="details-grid"><section class="payout-details" aria-label="Latest coinbase payout"><div class="detail-heading"><div><p>Latest coinbase payout</p><h3>${pool.latest_coinbase_total_sats ? btc(pool.latest_coinbase_total_sats) : "Payment details not available"}</h3></div><div class="detail-meta">${timeHTML(pool.latest_coinbase_observed_at)}<span>${pool.latest_coinbase_output_count} payments</span><span>${pool.coinbase_samples} block checks</span></div></div>${payoutHTML(pool)}</section><section class="history-details" aria-label="Recent performance"><div class="history-block"><div class="history-heading"><div><p>Recent history</p><h3>Block-template latency</h3></div><span>last 24 hours · ${(pool.template_latency_history || []).length} ${pool.combined_vantage ? "blocks" : "checks"}</span></div>${latencyHistoryHTML(pool)}</div>${feeHistoryHTML(pool)}</section></div></div></article>`;
  }

  function listHTML(pools) {
    const columns = [["score", "number", "Score"], ["pool", "text", "Pool / endpoint"], ["median", "number", "Median block delay"], ["p95", "number", "P95"], ["loss", "number", "Est. mining loss"], ["availability", "number", "Availability"], ["connection", "number", "Connect"], ["setup", "number", "Sub / auth"], ["fee", "number", "Measured pool fee"]];
    const head = columns.map(([key, type, label], index) => `<span role="columnheader" aria-sort="${index ? "none" : "descending"}"><button type="button" class="sort-button" data-sort-key="${key}" data-sort-type="${type}">${label} <b aria-hidden="true">${index ? "↕" : "↓"}</b></button></span>`).join("");
    return `<div class="measurement-list"><div class="measurement-head">${head}</div>${pools.length ? pools.map(rowHTML).join("") : '<div class="empty">No pools in this section.</div>'}</div>`;
  }

  function renderControls(data) {
    const available = data.available_vantages || {};
    const transport = data.selected_transport;
    const names = {"us-east": "US East", europe: "Europe", "us-west": "US West", japan: "Japan", singapore: "Southeast Asia"};
    const vantageOptions = [["unknown", "Local", "Collector", available.unknown, "Local collector"], ...(data.vantage_options || []).map(({id, city, country}) => [id, names[id] || id, city, available[id], `${city}, ${country}`])];
    const links = vantageOptions.filter(([, , , show]) => show).map(([id, name, city, , title]) => `<a href="/?vantage=${id}&amp;transport=${transport}" data-vantage="${id}" title="${escapeHTML(title)}"${data.selected_vantage === id ? ' aria-current="page"' : ""}><span>${escapeHTML(name)}</span><small>${escapeHTML(city)}</small></a>`).join("");
    document.querySelector("[data-vantage-selector]").innerHTML = links;
    document.querySelector("[data-vantage-bar]").hidden = !links;
    document.querySelector("[data-transport-selector]").innerHTML = ["plain", "tls"].map((id) => `<a href="/?vantage=${data.selected_vantage}&amp;transport=${id}" data-transport="${id}"${transport === id ? ' aria-current="page"' : ""}>${id === "plain" ? "TCP" : "TLS"}</a>`).join("");
  }

  function render(data) {
    const expanded = new Set([...document.querySelectorAll('.details-toggle[aria-expanded="true"]')].map((button) => button.closest(".measurement-row")?.dataset.poolId));
    renderControls(data);
    document.querySelector("[data-demo-notice]").hidden = !data.demo;
    const snapshot = data.snapshot;
    const blockHeight = Number(snapshot.latest_block_height) || null;
    const heightChanged = renderedVantage === data.selected_vantage && renderedBlockHeight !== null && blockHeight !== null && blockHeight !== renderedBlockHeight;
    const update = data.data_updated_at ? `Region updated ${timeHTML(data.data_updated_at)}` : `No regional data in the last ${snapshot.retention_window_days} days`;
    const height = blockHeight ? `<span class="block-height-pill${heightChanged ? " block-height-changed" : ""}" data-block-height title="Latest solved Bitcoin block observed in this region">Block <strong>${blockHeight.toLocaleString()}</strong></span>` : "";
    const configState = data.vantage_status && !data.vantage_status.config_current ? `<span class="config-pending-pill" title="This regional Scout has not reported the active pool configuration yet">Pool update pending</span>` : "";
    const regionSummary = `<div class="region-summary" aria-label="Regional measurement status" data-region-summary><span class="region-update-pill">${update}</span>${configState}${height}</div>`;
    const jump = document.querySelector("[data-section-jump]");
    jump.innerHTML = '<span class="control-label">Jump to</span>';
    let visible = 0;
    for (const [field, listID, sectionID, label, order] of groups) {
      const pools = data[field] || [];
      const section = document.getElementById(sectionID);
      section.hidden = pools.length === 0;
      document.getElementById(listID).innerHTML = listHTML(pools);
      section.querySelector("[data-section-meta]").innerHTML = `${regionSummary}<p>${order}</p>`;
      if (pools.length) {
        visible++;
        jump.insertAdjacentHTML("beforeend", `<a href="#${sectionID}">${label}</a>`);
      }
    }
    jump.hidden = visible === 0;
    expanded.forEach((id) => setDetailsState(document.querySelector(`.measurement-row[data-pool-id="${CSS.escape(id)}"]`), true));
    if (heightChanged) document.querySelector("[data-live-status]").textContent = `New Bitcoin block ${blockHeight.toLocaleString()} observed in ${data.selected_label}.`;
    renderedVantage = data.selected_vantage;
    renderedBlockHeight = blockHeight;
    document.querySelector("[data-live-footnote]").innerHTML = `${escapeHTML(data.selected_label)}. Median, P95, history, and Stratum timings use the latest ${snapshot.latency_window_hours} hours. No observation older than ${snapshot.retention_window_days} days is used; availability uses eligible block observations within that window. Estimated mining loss combines missed eligible deliveries with median relative delay during available time; values below 0.1% display as &lt;0.1% and receive full mining-loss score. It is not measured revenue loss. Score weights: availability 40%, mining loss 25%, P95 20%, connection/setup responsiveness 10%, and observed fee stability 5% when available. A recent fee increase subtracts up to 15 additional points over 30 days; observed fees above 2.5% subtract up to 10 more points; an invalid TLS certificate subtracts 10 points. A solo pool whose worker wallet is not found receives a score of 0.`;
    updateRelativeTimes();
  }

  function setDetailsState(row, expanded) {
    const button = row?.querySelector(".details-toggle");
    const panel = row?.querySelector(".measurement-details");
    if (!button || !panel) return;
    button.setAttribute("aria-expanded", String(expanded));
    button.querySelector("span").textContent = expanded ? "Fewer details" : "More details";
    panel.hidden = !expanded;
  }

  function placeRows(list, rows) {
    let previous = list.querySelector(".measurement-head");
    for (const row of rows) {
      const expected = previous?.nextElementSibling;
      if (row !== expected) list.insertBefore(row, expected || null);
      previous = row;
    }
  }

  function applySort(wrapper, key, type, direction) {
    const list = wrapper.querySelector(".measurement-list");
    if (!list) return;
    const rows = [...list.querySelectorAll(".measurement-row")];
    rows.sort((left, right) => {
      const leftRaw = left.getAttribute(`data-sort-${key}`) || "";
      const rightRaw = right.getAttribute(`data-sort-${key}`) || "";
      if ((leftRaw === "") !== (rightRaw === "")) return leftRaw === "" ? 1 : -1;
      const comparison = type === "number" ? Number(leftRaw) - Number(rightRaw) : leftRaw.localeCompare(rightRaw, undefined, { sensitivity: "base" });
      return comparison ? (direction === "ascending" ? comparison : -comparison) : (left.dataset.sortPool || "").localeCompare(right.dataset.sortPool || "");
    });
    placeRows(list, rows);
    sortStates.set(wrapper.id, { key, type, direction });
    list.querySelectorAll(".sort-button").forEach((button) => {
      const active = button.dataset.sortKey === key;
      button.closest("[role=columnheader]").setAttribute("aria-sort", active ? direction : "none");
      button.querySelector("b").textContent = active ? (direction === "ascending" ? "↑" : "↓") : "↕";
    });
  }

  async function refresh(initial = false) {
    if (refreshing || (!initial && document.hidden)) return;
    refreshing = true;
    try {
      const params = new URLSearchParams(window.location.search);
      const requestParams = new URLSearchParams(params);
      if (currentETag) requestParams.set("generation", currentETag);
      const headers = currentETag ? { "If-None-Match": currentETag } : {};
      const response = await fetch(`/dashboard-data?${requestParams}`, { cache: "no-cache", headers });
      if (response.status === 304) return;
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      const etag = response.headers.get("ETag") || "";
      if (!initial && etag && etag === currentETag) return;
      const data = await response.json();
      currentETag = etag;
      render(data);
      if (initial && !params.has("vantage")) {
        let saved = "";
        try { saved = localStorage.getItem(vantageStorageKey) || ""; } catch (_) { /* unavailable */ }
        if (saved && saved !== data.selected_vantage && data.available_vantages?.[saved]) {
          params.set("vantage", saved);
          history.replaceState(null, "", `/?${params}`);
          currentETag = "";
          queueMicrotask(() => refresh(true));
        }
      }
    } catch (error) {
      console.warn("StratumStats dashboard update failed", error);
      if (initial) document.querySelector("[data-live-footnote]").textContent = "Measurements are temporarily unavailable.";
    } finally {
      refreshing = false;
    }
  }

  document.addEventListener("click", (event) => {
    const details = event.target.closest(".details-toggle");
    if (details) {
      setDetailsState(details.closest(".measurement-row"), details.getAttribute("aria-expanded") !== "true");
      return;
    }
    const sort = event.target.closest(".sort-button");
    if (sort) {
      const wrapper = sort.closest(".measurement-list")?.parentElement;
      const current = sortStates.get(wrapper.id);
      const direction = current?.key === sort.dataset.sortKey && current.direction === "ascending" ? "descending" : "ascending";
      applySort(wrapper, sort.dataset.sortKey, sort.dataset.sortType, direction);
      return;
    }
    const vantage = event.target.closest("a[data-vantage]")?.dataset.vantage;
    const transport = event.target.closest("a[data-transport]")?.dataset.transport;
    try {
      if (vantage) localStorage.setItem(vantageStorageKey, vantage);
      if (transport) localStorage.setItem(transportStorageKey, transport);
    } catch (_) { /* unavailable */ }
  });

  try {
    const params = new URLSearchParams(window.location.search);
    if (!params.has("transport")) {
      const transport = localStorage.getItem(transportStorageKey);
      if (transport === "tls") {
        params.set("transport", transport);
        history.replaceState(null, "", `/?${params}`);
      }
    }
  } catch (_) { /* unavailable */ }
  refresh(true);
  window.setInterval(updateRelativeTimes, 30000);
  window.setInterval(() => refresh(false), refreshEveryMS);
  document.addEventListener("visibilitychange", () => { if (!document.hidden) refresh(false); });
})();
