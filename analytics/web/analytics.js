import "./echarts-view.js";
import {
  buildCashChartOption,
  buildSankey,
} from "./chart-options.js";

const money = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
});

const elements = {
  balanceView: document.getElementById("balance-view"),
  cashChart: document.getElementById("cash-chart"),
  cashSummary: document.getElementById("cash-summary"),
  chartNote: document.getElementById("chart-note"),
  chartTitle: document.getElementById("chart-title"),
  error: document.getElementById("error"),
  flowView: document.getElementById("flow-view"),
  findings: document.getElementById("anomalies"),
  sankeyChart: document.getElementById("sankey-chart"),
  summary: document.getElementById("summary"),
};

let activeView = "balance";
let analyticsData;

function card(label, value, detail, tone = "") {
  return `
    <article class="analytics-card">
      <p class="analytics-card-label">${label}</p>
      <p class="analytics-card-value">${value}</p>
      <p class="analytics-card-detail ${tone}">${detail}</p>
    </article>
  `;
}

function dateLabel(date) {
  return new Intl.DateTimeFormat("en-US", {
    month: "short",
    day: "numeric",
  }).format(new Date(`${date}T00:00:00`));
}

function projectedBalances(data, openingCashBalance) {
  let balance = openingCashBalance;
  data.daily_cash_flow.forEach((point) => {
    balance += point.net_cash_flow;
  });
  return data.forecast.map((point) => {
    balance += point.predicted_net_cash_flow;
    return { date: point.date, balance };
  });
}

function showError(message) {
  elements.error.textContent = message;
  elements.error.hidden = false;
}

function escapeHTML(value) {
  const node = document.createElement("span");
  node.textContent = value || "-";
  return node.innerHTML;
}

function renderSummary(data) {
  const openingCashBalance = Number(data.opening_cash_balance);
  const balances = projectedBalances(data, openingCashBalance);
  const ending = balances.at(-1);
  const lowest = balances.reduce(
    (lowestBalance, point) => 
      point.balance < lowestBalance.balance ? point : lowestBalance,
    ending,
  );
  const reviewCount = data.summary.anomaly_count;

  elements.summary.innerHTML = [
    card("Cash at start", money.format(openingCashBalance), "Opening balance for this view"),
    card("Expected cash at end", money.format(ending.balance), `By ${dateLabel(ending.date)}`),
    card("Lowest expected balance", money.format(lowest.balance), `Expected on ${dateLabel(lowest.date)}`, lowest.balance < 0 ? "attention" : "good"),
    card("Needs attention", reviewCount ? `${reviewCount} item${reviewCount === 1 ? "" : "s"}` : "All clear", reviewCount ? "Review the items below" : "No unusual entries found", reviewCount ? "attention" : "good"),
  ].join("");
}

function renderFindings(anomalies) {
  elements.findings.innerHTML = anomalies.map((item) => `
    <tr>
      <td>${escapeHTML(item.date)}</td>
      <td>
        <span class="tag ${item.type}">
          ${item.type === "amount_outlier" ? "Unusual amount" : "Possible duplicate"}
        </span>
      </td>
      <td>${money.format(item.amount)}</td>
      <td>${escapeHTML(item.description)}</td>
      <td>${escapeHTML(item.reason)}</td>
    </tr>
  `).join("") || `
    <tr>
      <td colspan="5">No findings.</td>
    </tr>
  `;
}

function renderCharts() {
  const balanceView = activeView === "balance";

  elements.chartTitle.textContent = balanceView
    ? "Projected Cash Balance"
    : "Daily Cash Flow Forecast";

  elements.chartNote.textContent = balanceView
    ? "Solid: recorded cash balance. Dashed: expected cash balance. The shaded area is the forecast period."
    : "Solid: recorded daily cash flow. Dashed: expected daily cash flow. The shaded area is the forecast period.";

  elements.balanceView.classList.toggle("active", balanceView);
  elements.flowView.classList.toggle("active", !balanceView);

  elements.cashChart.option = buildCashChartOption(
    analyticsData,
    activeView,
  );

  const sankey = buildSankey(analyticsData.cash_movements);

  elements.cashSummary.textContent =
    `The business received ${money.format(sankey.cashIn)}, ` +
    `spent ${money.format(sankey.cashOut)} on operations, and ` +
    `retained ${money.format(sankey.netCash)} in net cash.`;

  elements.sankeyChart.option = sankey.option;
}

function render(data) {
  const openingCashBalance = Number(data.opening_cash_balance);

  if (!Number.isFinite(openingCashBalance)) {
    throw new Error(
      "The analytics response does not include an opening cash balance",
    );
  }

  analyticsData = data;

  renderSummary(data);
  renderFindings(data.anomalies);
  renderCharts();
}

function selectView(view) {
  activeView = view;
  renderCharts();
}

elements.balanceView.addEventListener("click", () => {
  selectView("balance");
});

elements.flowView.addEventListener("click", () => {
  selectView("flow");
});

fetch("/api/analytics")
  .then((response) => {
    if (!response.ok) {
      throw new Error(`Could not load Summa analytics (${response.status})`);
    }

    return response.json();
  })
  .then(render)
  .catch((error) => {
    showError(`${error.message}. Make sure the Summa server is running, then refresh.`);
  });
