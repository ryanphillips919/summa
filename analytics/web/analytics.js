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

function card(label, value) {
  return `
    <article class="card">
      <div class="label">${label}</div>
      <div class="value">${value}</div>
    </article>
  `;
}

function showError(message) {
  elements.error.textContent = message;
  elements.error.hidden = false;
}

function renderSummary(data) {
  const openingCashBalance = Number(data.opening_cash_balance);

  const projectedEndingCash = data.forecast.reduce(
    (total, point) => total + point.predicted_net_cash_flow,
    openingCashBalance +
      data.daily_cash_flow.reduce(
        (total, point) => total + point.net_cash_flow,
        0,
      ),
  );

  elements.summary.innerHTML = [
    card("Opening cash balance", money.format(openingCashBalance)),
    card("Projected ending cash", money.format(projectedEndingCash)),
    card("Forecast model", data.summary.forecast_model),
    card("Items to review", data.summary.anomaly_count),
  ].join("");
}

function renderFindings(anomalies) {
  elements.findings.innerHTML = anomalies.map((item) => `
    <tr>
      <td>${item.date}</td>
      <td>
        <span class="tag ${item.type}">
          ${item.type.replace("_", " ")}
        </span>
      </td>
      <td>${money.format(item.amount)}</td>
      <td>${item.description || "-"}</td>
      <td>${item.reason}</td>
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
    ? "Solid: actual cash balance. Dashed: projected cash balance. The gray background marks the forecast period."
    : "Solid: actual daily net cash flow. Dashed: forecast daily net cash flow. The gray background marks the forecast period.";

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