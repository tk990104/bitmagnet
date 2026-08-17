const form = document.querySelector("#search-form");
const queryInput = document.querySelector("#query");
const searchButton = document.querySelector("#search-button");
const status = document.querySelector("#status");
const warnings = document.querySelector("#warnings");
const panel = document.querySelector("#results-panel");
const resultCount = document.querySelector("#result-count");
const dedupeCount = document.querySelector("#dedupe-count");
const resultBody = document.querySelector("#results");
const empty = document.querySelector("#empty");
const sortButtons = [...document.querySelectorAll(".sort")];

const state = { results: [], sort: "seeders", direction: "desc" };
const relativeTime = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });

form.addEventListener("submit", async (event) => {
  event.preventDefault();
  const query = queryInput.value.trim();
  if (!query) return;

  setLoading(true);
  warnings.hidden = true;
  warnings.replaceChildren();
  status.textContent = `Searching both sources for “${query}”…`;

  try {
    const response = await fetch(`/api/search?q=${encodeURIComponent(query)}`, {
      headers: { Accept: "application/json" },
    });
    const payload = await response.json();
    if (!response.ok) {
      showWarnings(payload.warnings || []);
      throw new Error(payload.error || `Search failed (${response.status})`);
    }

    state.results = payload.results || [];
    resultCount.textContent = `${state.results.length} ${state.results.length === 1 ? "match" : "matches"}`;
    dedupeCount.textContent = payload.meta?.deduplicated
      ? `${payload.meta.deduplicated} duplicate ${payload.meta.deduplicated === 1 ? "result" : "results"} merged by info hash`
      : "No duplicate info hashes found";
    showWarnings(payload.warnings || []);
    panel.hidden = false;
    empty.hidden = state.results.length !== 0;
    render();
    status.textContent = `Search complete. ${state.results.length} unified ${state.results.length === 1 ? "result" : "results"}.`;
  } catch (error) {
    state.results = [];
    panel.hidden = true;
    status.textContent = error instanceof Error ? error.message : "Search failed.";
  } finally {
    setLoading(false);
  }
});

sortButtons.forEach((button) => {
  button.addEventListener("click", () => {
    const field = button.dataset.sort;
    if (state.sort === field) {
      state.direction = state.direction === "asc" ? "desc" : "asc";
    } else {
      state.sort = field;
      state.direction = field === "title" || field === "source" ? "asc" : "desc";
    }
    updateSortIndicators();
    render();
  });
});

function setLoading(loading) {
  searchButton.disabled = loading;
  queryInput.disabled = loading;
  searchButton.textContent = loading ? "Searching…" : "Search both";
}

function showWarnings(items) {
  if (!items.length) return;
  warnings.hidden = false;
  const heading = document.createElement("strong");
  heading.textContent = "Partial search";
  warnings.append(heading);
  items.forEach((item) => {
    const message = document.createElement("p");
    message.textContent = item;
    warnings.append(message);
  });
}

function render() {
  const direction = state.direction === "asc" ? 1 : -1;
  const sorted = [...state.results].sort((left, right) => {
    let a;
    let b;
    if (state.sort === "title") {
      a = left.title?.toLocaleLowerCase() || "";
      b = right.title?.toLocaleLowerCase() || "";
    } else if (state.sort === "source") {
      a = left.sources?.join(" ").toLocaleLowerCase() || "";
      b = right.sources?.join(" ").toLocaleLowerCase() || "";
    } else if (state.sort === "age") {
      a = left.publishedAt ? new Date(left.publishedAt).getTime() : 0;
      b = right.publishedAt ? new Date(right.publishedAt).getTime() : 0;
    } else {
      a = left[state.sort] ?? -1;
      b = right[state.sort] ?? -1;
    }
    if (typeof a === "string") return a.localeCompare(b) * direction;
    return (a - b) * direction;
  });

  resultBody.replaceChildren(...sorted.map(createResultRow));
}

function createResultRow(result) {
  const row = document.createElement("tr");

  const titleCell = document.createElement("td");
  titleCell.className = "title-cell";
  const title = document.createElement("strong");
  title.textContent = result.title || "Untitled torrent";
  titleCell.append(title);
  if (result.infoHash) {
    const hash = document.createElement("span");
    hash.className = "hash";
    hash.textContent = result.infoHash;
    hash.title = "Torrent info hash";
    titleCell.append(hash);
  }

  const sourceCell = document.createElement("td");
  sourceCell.className = "sources";
  (result.sources || []).forEach((source) => {
    const badge = document.createElement("span");
    badge.className = source.toLocaleLowerCase().startsWith("bitmagnet") ? "badge bitmagnet" : "badge prowlarr";
    badge.textContent = source;
    sourceCell.append(badge);
  });

  const sizeCell = document.createElement("td");
  sizeCell.className = "numeric mono";
  sizeCell.textContent = formatSize(result.size);

  const seedersCell = document.createElement("td");
  seedersCell.className = "numeric seeders";
  seedersCell.textContent = result.seeders ?? "—";

  const ageCell = document.createElement("td");
  ageCell.className = "age";
  ageCell.textContent = formatAge(result.publishedAt);
  if (result.publishedAt) ageCell.title = new Date(result.publishedAt).toLocaleString();

  const actionCell = document.createElement("td");
  actionCell.className = "action";
  if (result.magnetUri) {
    const open = document.createElement("a");
    open.className = "open-button";
    open.href = result.magnetUri;
    open.textContent = "Open in torrent client";
    open.setAttribute("aria-label", `Open ${result.title || "torrent"} in the registered torrent client`);
    actionCell.append(open);
  } else {
    const unavailable = document.createElement("span");
    unavailable.className = "unavailable";
    unavailable.textContent = "No magnet";
    actionCell.append(unavailable);
  }

  row.append(titleCell, sourceCell, sizeCell, seedersCell, ageCell, actionCell);
  return row;
}

function formatSize(bytes) {
  if (!Number.isFinite(bytes) || bytes <= 0) return "—";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const unit = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const value = bytes / 1024 ** unit;
  return `${value.toFixed(value >= 10 || unit === 0 ? 0 : 1)} ${units[unit]}`;
}

function formatAge(value) {
  if (!value) return "—";
  const timestamp = new Date(value).getTime();
  if (!Number.isFinite(timestamp)) return "—";
  const seconds = Math.round((timestamp - Date.now()) / 1000);
  const ranges = [
    [31536000, "year"],
    [2592000, "month"],
    [86400, "day"],
    [3600, "hour"],
    [60, "minute"],
  ];
  for (const [amount, unit] of ranges) {
    if (Math.abs(seconds) >= amount) return relativeTime.format(Math.round(seconds / amount), unit);
  }
  return relativeTime.format(seconds, "second");
}

function updateSortIndicators() {
  sortButtons.forEach((button) => {
    const active = button.dataset.sort === state.sort;
    button.classList.toggle("active", active);
    button.querySelector("span").textContent = active ? (state.direction === "asc" ? "↑" : "↓") : "";
  });
}

