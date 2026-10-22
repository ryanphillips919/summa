const money = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
});

let activeView = "balance";
let dashboardData = null;
let cashChart = null;
let sankeyChart = null;

function card(label, value) {
  return `
    <article class="card">
      <div class="label">${label}</div>
      <div class="value">${value}</div>
    </article>
  `;
}

function showError(message) {
  const error = document.getElementById("error");
  error.textContent = message;
  error.hidden = false;
}

function categoryFor(vendor) {
  const categories = {
    aws: "Cloud services",
    "google ads": "Marketing",
    "freelance contractor": "Contractors",
    "city utilities": "Utilities",
    "office depot": "Office supplies",
    "office warehouse": "Office supplies",
    "travel hub": "Travel exception",
  };

  return categories[vendor.trim().toLowerCase()] || "Other operating expense";
}

function renderSankey(movements) {
  const cashIn = movements
    .filter((movement) => movement.amount > 0)
    .reduce((total, movement) => total + movement.amount, 0);

  const byCategory = new Map();

  movements
    .filter((movement) => movement.amount < 0)
    .forEach((movement) => {
      const category = categoryFor(movement.vendor || "");
      const total = byCategory.get(category) || 0;
      byCategory.set(category, total + Math.abs(movement.amount));
    });

  const cashOut = [...byCategory.values()].reduce(
    (total, amount) => total + amount,
    0,
  );

  const netCash = cashIn - cashOut;

  const links = [
    { source: "Customer receipts", target: "Cash available", value: cashIn },
    ...[...byCategory.entries()].map(([category, value]) => ({
      source: "Cash available",
      target: category,
      value,
    })),
  ];

  if (netCash > 0) {
    links.push({
      source: "Cash available",
      target: "Net cash retained",
      value: netCash,
    });
  }

  document.getElementById("cash-summary").textContent =
    `The business received ${money.format(cashIn)}, spent ${money.format(cashOut)} on operations, and retained ${money.format(netCash)} in net cash.`;

  if (sankeyChart) {
    sankeyChart.dispose();
  }

  sankeyChart = echarts.init(document.getElementById("sankey"));

  sankeyChart.setOption({
    tooltip: {
      trigger: "item",
      valueFormatter: (value) => money.format(value),
    },
    series: [
      {
        type: "sankey",
        data: [...new Set(
          links.flatMap((link) => [link.source, link.target]),
        )].map((name) => ({ name })),
        links,
        nodeAlign: "justify",
        layoutIterations: 32,
        label: { color: "#172033", fontWeight: 600 },
        lineStyle: {
          color: "gradient",
          curveness: 0.5,
          opacity: 0.45,
        },
        emphasis: { focus: "adjacency" },
        itemStyle: {
          borderColor: "#ffffff",
          borderWidth: 1,
        },
      },
    ],
  });
}

function buildBalanceData(actual, forecast, openingCashBalance) {
  let runningActualBalance = openingCashBalance;

  const actualBalances = actual.map((point) => {
    runningActualBalance += point.net_cash_flow;

    return {
      date: point.date,
      cashBalance: runningActualBalance,
    };
  });

  let runningForecastBalance = runningActualBalance;

  const forecastBalances = forecast.map((point) => {
    runningForecastBalance += point.predicted_net_cash_flow;

    return {
      date: point.date,
      cashBalance: runningForecastBalance,
    };
  });

  return {
    actualBalances,
    forecastBalances,
    lastActualBalance: runningActualBalance,
  };
}

function renderChart() {
  const {
    daily_cash_flow: actual,
    forecast,
    anomalies,
    opening_cash_balance: openingCashBalance,
  } = dashboardData;

  const { actualBalances, forecastBalances, lastActualBalance } =
    buildBalanceData(actual, forecast, openingCashBalance);

  const actualByDate = new Map(
    actual.map((point) => [point.date, point.net_cash_flow]),
  );

  const forecastByDate = new Map(
    forecast.map((point) => [point.date, point.predicted_net_cash_flow]),
  );

  const actualBalanceByDate = new Map(
    actualBalances.map((point) => [point.date, point.cashBalance]),
  );

  const forecastBalanceByDate = new Map(
    forecastBalances.map((point) => [point.date, point.cashBalance]),
  );

  const dates = [
    ...actual.map((point) => point.date),
    ...forecast.map((point) => point.date),
  ];

  const lastActualDate = actual[actual.length - 1].date;
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

  if (cashChart) {
    cashChart.dispose();
  }

  cashChart = echarts.init(document.getElementById("chart"));

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

  cashChart.setOption({
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
        name: actualLabel,
        type: "line",
        smooth: true,
        symbolSize: 5,
        data: actualSeries,

        markLine: anomalyDates.length
          ? {
              symbol: "none",
              lineStyle: {
                color: "#dc2626",
                type: "dotted",
              },
              data: anomalyDates.map((date) => ({
                xAxis: date,
              })),
            }
          : undefined,

        markArea: forecast.length
          ? {
              silent: true,
              itemStyle: {
                color: "rgba(148, 163, 184, 0.10)",
              },
              label: {
                show: true,
                color: "#64748b",
                formatter: "Forecast period",
              },
              data: [[
                { xAxis: forecast[0].date },
                { xAxis: forecast[forecast.length - 1].date },
              ]],
            }
          : undefined,
      },
      {
        name: forecastLabel,
        type: "line",
        smooth: true,
        symbolSize: 5,
        lineStyle: {
          type: "dashed",
          width: 2,
        },
        data: forecastSeries,
      },
    ],
  });
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

  renderChart();
  renderSankey(movements);
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
  cashChart?.resize();
  sankeyChart?.resize();
});

fetch("/api/analytics/demo")
  .then((response) => {
    if (!response.ok) {
      throw new Error(`Could not load Summa analytics (${response.status})`);
    }

    return response.json();
  })
  .then(render)
  .catch((error) => {
    showError(
      `${error.message}. Make sure the Summa server is running, then refresh.`,
    );
  });