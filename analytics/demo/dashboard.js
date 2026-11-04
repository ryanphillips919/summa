import { echarts } from "./echarts-view.js";

const money = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
});

let activeView = "balance";
let dashboardData = null;
let sankeyChart = null;
const cashChart = document.getElementById("cash-chart");

function card(label, value) {
  return `<article class="card"><div class="label">${label}</div><div class="value">${value}</div></article>`;
}

function showError(message) {
  const error = document.getElementById("error");
  error.textContent = message;
  error.hidden = false;
}

function render(result) {
  const { summary, daily_cash_flow: actual, forecast, anomalies } = result;
  document.getElementById("summary").innerHTML = [
    card("Forecast model", summary.forecast_model),
    card("Baseline MAE", summary.baseline_validation_mae == null ? "Need more history" : money.format(summary.baseline_validation_mae)),
    card("Selected-model MAE", summary.selected_model_validation_mae == null ? "Need more history" : money.format(summary.selected_model_validation_mae)),
    card("Items to review", summary.anomaly_count),
  ].join("");

  const actualByDate = new Map(actual.map((point) => [point.date, point.net_cash_flow]));
  const forecastByDate = new Map(forecast.map((point) => [point.date, point.predicted_net_cash_flow]));
  const dates = [...actual.map((point) => point.date), ...forecast.map((point) => point.date)];
  const lower = forecast.map((point) => point.lower_95);
  const band = forecast.map((point) => point.upper_95 - point.lower_95);
  const anomalyDates = [...new Set(anomalies.map((item) => item.date))];

  const balanceView = activeView === "balance";

  document.getElementById("chart-title").textContent = balanceView
    ? "Projected Cash Balance"
    : "Daily Cash Flow Forecast";

  document.getElementById("chart-note").textContent = balanceView
    ? "Solid: actual cash balance. Dashed: projected cash balance. The gray background marks the forecast period."
    : "Solid: actual daily net cash flow. Dashed: forecast daily net cash flow. The gray background marks the forecast period.";

  document.getElementById("balance-view").classList.toggle(
    "active",
    balanceView,
  );

  document.getElementById("flow-view").classList.toggle(
    "active",
    !balanceView,
  );

  const actualSeries = balanceView
    ? dates.map((date) => actualBalanceByDate.get(date) ?? null)
    : dates.map((date) => actualByDate.get(date) ?? null);

  const forecastSeries = balanceView
    ? dates.map((date) => {
        if (date === lastActualDate) {
          return lastActualBalance;
        }

        return forecastBalanceByDate.get(date) ?? null;
      })
    : dates.map((date) => forecastByDate.get(date) ?? null);

  const actualLabel = balanceView
    ? "Actual cash balance"
    : "Actual cash flow";

  const forecastLabel = balanceView
    ? "Projected cash balance"
    : "Forecast cash flow";

  cashChart.option = {
    color: ["#1d4ed8", "#0891b2"],

    tooltip: {
      trigger: "axis",

      formatter(params) {
        const date = params[0]?.axisValue ?? "";
        const lines = [`<strong>${date}</strong>`];

        if (balanceView) {
          const actualBalance = actualBalanceByDate.get(date);
          const forecastBalance = forecastBalanceByDate.get(date);

          if (actualBalance != null) {
            lines.push(`Actual balance: ${money.format(actualBalance)}`);
          }

          if (forecastBalance != null) {
            lines.push(`Projected balance: ${money.format(forecastBalance)}`);
          }
        } else {
          const actualValue = actualByDate.get(date);
          const forecastValue = forecastByDate.get(date);

          if (actualValue != null) {
            lines.push(`Actual: ${money.format(actualValue)}`);
          }

          if (forecastValue != null) {
            lines.push(`Forecast: ${money.format(forecastValue)}`);
          }
        }

        return lines.join("<br>");
      },
    },

    legend: {
      data: [actualLabel, forecastLabel],
    },

    grid: {
      left: 72,
      right: 36,
      top: 60,
      bottom: 90,
    },

    xAxis: {
      type: "category",
      data: dates,
      boundaryGap: false,
      axisLabel: { hideOverlap: true },
    },

    yAxis: {
      type: "value",
      axisLabel: {
        formatter: (value) => money.format(value),
      },
    },

    dataZoom: [
      { type: "inside" },
      { type: "slider", bottom: 18 },
    ],

    series: [
      {
        name: "Actual cash flow", type: "line", smooth: true, symbolSize: 5,
        data: dates.map((date) => actualByDate.get(date) ?? null),
        markLine: anomalyDates.length ? { symbol: "none", lineStyle: { color: "#dc2626", type: "dotted" }, data: anomalyDates.map((date) => ({ xAxis: date })) } : undefined,
      },
      { name: "Forecast", type: "line", smooth: true, symbolSize: 5, lineStyle: { type: "dashed", width: 2 }, data: dates.map((date) => forecastByDate.get(date) ?? null) },
      { name: "Confidence base", type: "line", stack: "interval", symbol: "none", lineStyle: { opacity: 0 }, data: [...Array(actual.length).fill(null), ...lower] },
      { name: "95% interval", type: "line", stack: "interval", symbol: "none", lineStyle: { opacity: 0 }, areaStyle: { color: "#93c5fd", opacity: 0.35 }, data: [...Array(actual.length).fill(null), ...band] },
    ],
  };
}

function render(payload) {
  dashboardData = payload;

  const openingCashBalance = Number(payload.opening_cash_balance);

  if (!Number.isFinite(openingCashBalance)) {
    throw new Error(
      "The analytics response does not include an opening cash balance",
    );
  }

  const { summary, forecast, anomalies, cash_movements: movements } = payload;

  const projectedEndingCash = forecast.length
    ? openingCashBalance +
      payload.daily_cash_flow.reduce(
        (total, point) => total + point.net_cash_flow,
        0,
      ) +
      forecast.reduce(
        (total, point) => total + point.predicted_net_cash_flow,
        0,
      )
    : openingCashBalance;

  document.getElementById("summary").innerHTML = [
    card("Opening cash balance", money.format(openingCashBalance)),
    card("Projected ending cash", money.format(projectedEndingCash)),
    card("Forecast model", summary.forecast_model),
    card("Items to review", summary.anomaly_count),
  ].join("");

  document.getElementById("anomalies").innerHTML = anomalies.map((item) => `
    <tr>
      <td>${item.date}</td>
      <td><span class="tag ${item.type}">${item.type.replace("_", " ")}</span></td>
      <td>${money.format(item.amount)}</td>
      <td>${item.description || "-"}</td>
      <td>${item.reason}</td>
    </tr>`).join("") || "<tr><td colspan=\"5\">No findings.</td></tr>";
}

document.getElementById("balance-view").addEventListener("click", () => {
  activeView = "balance";
  renderChart();
});

document.getElementById("flow-view").addEventListener("click", () => {
  activeView = "flow";
  renderChart();
});

window.addEventListener("resize", () => {
  sankeyChart?.resize();
});

fetch("/api/analytics/demo")
  .then((response) => {
    if (!response.ok) throw new Error(`Could not load analytics-result.json (${response.status})`);
    return response.json();
  })
  .then(render)
  .catch((error) => showError(`${error.message}. Generate the file with the command in the demo instructions, then refresh.`));